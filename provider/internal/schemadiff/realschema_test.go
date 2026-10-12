package schemadiff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Real agent-schema.json files. SCHEMADIFF_OLD and SCHEMADIFF_NEW override the defaults (an older
// release saved by `make upgrade_docker_agent` in bin/, and the vendored schema); the test skips
// when either is missing. SCHEMADIFF_REPORT writes the Markdown report (without classifier) to that
// file, titled with SCHEMADIFF_FROM and SCHEMADIFF_TO.
func realSchemas(t *testing.T) (oldRaw, newRaw []byte) {
	t.Helper()
	oldPath := os.Getenv("SCHEMADIFF_OLD")
	newPath := os.Getenv("SCHEMADIFF_NEW")
	if newPath == "" {
		newPath = filepath.Join("..", "..", "pkg", "render", "schema", "agent-schema.json")
	}
	if oldPath == "" {
		matches, _ := filepath.Glob(filepath.Join("..", "..", "..", "bin", "agent-schema.*.json"))
		if len(matches) == 0 {
			t.Skip("no old agent-schema.json: set SCHEMADIFF_OLD (and SCHEMADIFF_NEW)")
		}
		oldPath = matches[0]
	}
	var err error
	if oldRaw, err = os.ReadFile(oldPath); err != nil { //nolint:gosec // test input chosen by the developer
		t.Skipf("old schema not available: %v", err)
	}
	if newRaw, err = os.ReadFile(newPath); err != nil { //nolint:gosec // test input chosen by the developer
		t.Skipf("new schema not available: %v", err)
	}
	return oldRaw, newRaw
}

func TestRealSchemas(t *testing.T) {
	oldRaw, newRaw := realSchemas(t)

	self, err := Compare(newRaw, newRaw)
	if err != nil {
		t.Fatal(err)
	}
	if !self.Empty() {
		t.Fatalf("schema compared with itself is not empty: %s", self.Summary())
	}

	d, err := Compare(oldRaw, newRaw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Version.NewLatest == "" {
		t.Fatal("new schema has no config version enum")
	}
	paths, err := PropertyPaths(newRaw)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 100 {
		t.Fatalf("only %d property paths in the new schema", len(paths))
	}
	for _, want := range []string{"Config.agents", "AgentConfig.model"} {
		if !contains(paths, want) {
			t.Errorf("PropertyPaths misses %s", want)
		}
	}

	// Reverse comparison mirrors the forward one.
	rev, err := Compare(newRaw, oldRaw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.AddedProperties) != len(d.RemovedProperties) || len(rev.RemovedProperties) != len(d.AddedProperties) {
		t.Errorf("reverse diff is not symmetric: %s vs %s", rev.Summary(), d.Summary())
	}

	md := d.Markdown("old", "new", func(p string) (bool, string) {
		return p == "Toolset", "example classifier"
	}, nil)
	if !strings.HasPrefix(md, "# docker-agent old → new: configuration schema changes") {
		t.Errorf("unexpected report header:\n%s", md)
	}
	if out := os.Getenv("SCHEMADIFF_REPORT"); out != "" {
		from, to := envOr("SCHEMADIFF_FROM", "old"), envOr("SCHEMADIFF_TO", "new")
		if err := os.WriteFile(out, []byte(d.Markdown(from, to, nil, nil)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("%s\n%s", d.Summary(), ChangelogEntry("old", "new", d))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
