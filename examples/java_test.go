// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/providertest/pulumitest/assertpreview"
	"github.com/pulumi/providertest/pulumitest/assertrefresh"
	"github.com/pulumi/providertest/pulumitest/opttest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExampleJava runs examples/java like runExample runs the other languages: up, preview and
// refresh without changes, and destroy, with the same output and registry checks.
//
// The program builds against the Java SDK sources in sdk/java through a Gradle composite build
// (`includeBuild` in settings.gradle), so nothing is published to a Maven repository and no
// `make build_java` is needed. It needs a JDK, a Java 11 toolchain for the SDK
// (sdk/java/build.gradle) and Gradle on PATH; without them it skips itself, and fails in CI.
func TestExampleJava(t *testing.T) {
	t.Parallel()
	requireJavaToolchain(t)
	registry := testRegistry(t)
	root := repoRoot(t)
	const lang = "java"

	start := time.Now()
	pt := pulumitest.NewPulumiTest(t, lang,
		// Use bin/pulumi-resource-docker-agent through `plugins.providers` in Pulumi.yaml. The
		// SDK built in the composite build asks for plugin version 0.0.1 (PACKAGE_VERSION is
		// unset), which the version-less project plugin satisfies.
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
	)
	useLocalJavaSDK(t, pt.WorkingDir(), root)
	pt.NewStack(t, "")
	// The Java language host runs `gradle build`, which also compiles sdk/java.
	installDependencies(t, pt.WorkingDir())
	pt.SetConfig(t, "registry", registry)
	t.Logf("[%s] setup (copy, stack, build): %s", lang, time.Since(start).Round(time.Millisecond))

	step := time.Now()
	up := pt.Up(t)
	t.Logf("[%s] up: %s", lang, time.Since(step).Round(time.Millisecond))

	repo := "examples/" + lang + "-agent"
	digest := checkOutputs(t, lang, registry, repo, up.Outputs)
	checkRegistry(t, registry, repo, digest)
	checkDockerAgentCLI(t, stringOutput(t, up.Outputs, "pinnedReference"), stringOutput(t, up.Outputs, "agentYaml"))

	step = time.Now()
	assertpreview.HasNoChanges(t, pt.Preview(t))
	t.Logf("[%s] preview: %s", lang, time.Since(step).Round(time.Millisecond))

	step = time.Now()
	assertrefresh.HasNoChanges(t, pt.Refresh(t))
	t.Logf("[%s] refresh: %s", lang, time.Since(step).Round(time.Millisecond))

	// Neither preview nor refresh pushed again.
	status, gotDigest, _ := getManifest(t, registry, repo, "v1")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, digest, gotDigest, "the tag moved after preview/refresh")

	step = time.Now()
	pt.Destroy(t)
	t.Logf("[%s] destroy: %s", lang, time.Since(step).Round(time.Millisecond))

	status, _, _ = getManifest(t, registry, repo, digest)
	assert.Equal(t, http.StatusNotFound, status,
		"%s@%s still exists after destroy; the registry must allow deletes (REGISTRY_STORAGE_DELETE_ENABLED=true)",
		repo, digest)
	t.Logf("[%s] total: %s", lang, time.Since(start).Round(time.Millisecond))
}

// requireJavaToolchain skips the test when java or gradle is missing, and fails it in CI.
func requireJavaToolchain(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"java", "gradle"} {
		if lookPath(tool) {
			continue
		}
		msg := tool + " is not on PATH; the Java example needs a JDK and Gradle"
		if os.Getenv("CI") != "" {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
}

// useLocalJavaSDK points the copied program's composite build at sdk/java. settings.gradle
// references it relative to the repository, which breaks once pulumitest copies the program to
// a temp dir.
func useLocalJavaSDK(t *testing.T, dir, root string) {
	t.Helper()
	sdk := filepath.Join(root, "sdk", "java")
	requireFile(t, filepath.Join(sdk, "build.gradle"), "make generate_java")
	replaceInFile(t, filepath.Join(dir, "settings.gradle"),
		`includeBuild("../../sdk/java")`, fmt.Sprintf("includeBuild(%q)", filepath.ToSlash(sdk)))
}
