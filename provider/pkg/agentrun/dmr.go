// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	dockerconfig "github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/connhelper/commandconn"
	"github.com/docker/docker-agent/pkg/model/provider/dmr"
	"github.com/docker/docker-agent/pkg/model/provider/dmr/dmrmodels"
)

// Docker Model Runner (D7). docker agent run gives docker-agent the Docker CLI's connection
// (cmd/root/dmr.go withDMRDockerConnection): the `docker model` commands and the HTTP requests
// to Docker Desktop's model runner go through the current Docker context, DOCKER_HOST and its
// TLS settings. Without it docker-agent dials the hard-coded /var/run/docker.sock while the
// `docker model` commands follow the context. A run child does what the CLI does, without the
// CLI's flags. A model pull's output goes to the run's debug log (and a progress record), not
// to the child's stdout, and the pull counts against timeoutSeconds.

// Docker's default context name (github.com/docker/cli/cli/command.DefaultContextName).
const defaultDockerContext = "default"

// withDockerConnection mirrors docker agent run's withDMRDockerConnection with no flags: no
// connection is opened until an HTTP request dials.
func withDockerConnection(ctx context.Context) (context.Context, error) {
	args, err := dockerConnectionArgs()
	if err != nil {
		return ctx, err
	}
	dialArgs := slices.Concat(args, []string{"system", "dial-stdio"})
	return dmrmodels.ContextWithDockerConnection(ctx, args, func(ctx context.Context) (net.Conn, error) {
		return commandconn.New(ctx, "docker", dialArgs...)
	}), nil
}

// dockerConnectionArgs are the docker CLI's connection arguments, as cmd/root/dmr.go
// dmrDockerArgs computes them without flags (the defaults of the CLI's client options:
// DOCKER_CONFIG, DOCKER_HOST, DOCKER_CONTEXT, the config's current context, DOCKER_TLS,
// DOCKER_TLS_VERIFY and DOCKER_CERT_PATH).
func dockerConnectionArgs() ([]string, error) {
	configDir, err := filepath.Abs(dockerconfig.Dir())
	if err != nil {
		return nil, fmt.Errorf("resolving Docker config directory: %w", err)
	}
	args := []string{"--config=" + configDir}
	host := os.Getenv("DOCKER_HOST")
	var contextName string
	switch {
	case host != "":
		contextName = defaultDockerContext
	case os.Getenv("DOCKER_CONTEXT") != "":
		contextName = os.Getenv("DOCKER_CONTEXT")
	default:
		// Docker itself tolerates an unreadable config for connection selection.
		if cfg, err := dockerconfig.Load(configDir); err == nil {
			contextName = cfg.CurrentContext
		}
	}
	if contextName == defaultDockerContext && host != "" {
		if socket, ok := strings.CutPrefix(strings.TrimSpace(host), "unix://"); ok && socket != "" {
			path, err := filepath.Abs(socket)
			if err != nil {
				return nil, fmt.Errorf("resolving Docker socket: %w", err)
			}
			host = "unix://" + path
		}
		args = append(args, "--host="+host)
	} else {
		args = append(args, "--context="+cmp.Or(contextName, defaultDockerContext))
	}

	// cli/flags ClientOptions.InstallFlags: the TLS flags default to DOCKER_TLS and
	// DOCKER_TLS_VERIFY, the certificates to DOCKER_CERT_PATH or the config directory.
	if os.Getenv("DOCKER_TLS") == "" && os.Getenv("DOCKER_TLS_VERIFY") == "" {
		return args, nil
	}
	certPath := cmp.Or(os.Getenv("DOCKER_CERT_PATH"), configDir)
	for _, f := range []struct{ flag, file string }{{"tlscacert", "ca.pem"}, {"tlscert", "cert.pem"}, {"tlskey", "key.pem"}} {
		path := filepath.Join(certPath, f.file)
		// Docker ignores missing default client certificates, but not a missing CA.
		if f.flag != "tlscacert" {
			if _, err := os.Stat(path); os.IsNotExist(err) { //nolint:gosec // DOCKER_CERT_PATH, as docker reads it
				path = ""
			}
		}
		if path != "" {
			if path, err = filepath.Abs(path); err != nil {
				return nil, fmt.Errorf("resolving Docker --%s: %w", f.flag, err)
			}
		}
		args = append(args, "--"+f.flag+"="+path)
	}
	return args, nil
}

// pullOutput receives docker-agent's model pull output (dmr.WithPullOutput): each line goes to
// debug, and the start of a pull to started.
type pullOutput struct {
	debug   func(string)
	started func(model string)
	mu      sync.Mutex
	buf     []byte
	model   string
}

// pullStart and pullDone are docker-agent's first and last lines of a pull (dmr/pull.go
// runModelPull).
var (
	pullStart = regexp.MustCompile(`^Pulling model (\S+)\.\.\.$`)
	pullDone  = regexp.MustCompile(`^Model (\S+) pulled successfully\.$`)
)

// ansiCSI matches terminal escape sequences of `docker model pull`'s progress output.
var ansiCSI = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func (w *pullOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := strings.IndexAny(string(w.buf), "\r\n")
		if i < 0 {
			if len(w.buf) > 64<<10 {
				w.line(string(w.buf))
				w.buf = w.buf[:0]
			}
			return len(p), nil
		}
		w.line(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
}

func (w *pullOutput) line(s string) {
	s = strings.TrimSpace(ansiCSI.ReplaceAllString(s, ""))
	if s == "" {
		return
	}
	if m := pullStart.FindStringSubmatch(s); m != nil {
		w.model = m[1]
		w.started(m[1])
	} else if pullDone.MatchString(s) {
		w.model = ""
	}
	w.debug("docker model pull: " + s)
}

// pulling returns the model of a pull that started and did not finish, or "".
func (w *pullOutput) pulling() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.model
}

// withModelRunner gives a run's context docker agent run's Docker connection and routes model
// pulls to out. Without a docker CLI on PATH there is no connection, as before v0.2.0:
// docker-agent then probes Docker Model Runner's default endpoints itself, while with a
// connection a failing `docker model status` fails the model (a model with a base_url, or
// MODEL_RUNNER_HOST, needs neither).
func withModelRunner(ctx context.Context, out *pullOutput, debug func(string)) context.Context {
	if _, err := exec.LookPath("docker"); err != nil {
		debug("Docker Model Runner gets no Docker connection: the docker CLI is not on PATH")
	} else if c, err := withDockerConnection(ctx); err != nil {
		debug("Docker Model Runner gets no Docker connection: " + err.Error())
	} else {
		ctx = c
	}
	return dmr.WithPullOutput(ctx, out)
}

// pullTimeoutError is the error of a run whose timeout ran out while a model was pulled.
func pullTimeoutError(model string, timeout fmt.Stringer) error {
	return fmt.Errorf("the agent did not finish within %s (timeoutSeconds): it timed out while pulling the Docker Model "+
		"Runner model %s; pull it first (docker model pull %s) or raise timeoutSeconds", timeout, model, model)
}
