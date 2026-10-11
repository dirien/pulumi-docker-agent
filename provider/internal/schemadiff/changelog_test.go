package schemadiff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The structure of the repository's CHANGELOG.md: an empty Unreleased section followed by a
// release section and link references.
const changelogFixture = `# Changelog

All notable changes to this project are documented in this file.

## [Unreleased]

## [0.1.0] - Unreleased

Initial release.

### Added

- ` + "`AgentConfig`" + ` resource.

[Unreleased]: https://github.com/dirien/pulumi-docker-agent/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/dirien/pulumi-docker-agent/releases/tag/v0.1.0
`

const entry1 = "- Upgrade docker-agent from v1.149.0 to v1.150.0: 1 property added."

func prepend(t *testing.T, changelog, entry string) string {
	t.Helper()
	out, err := PrependChangelog([]byte(changelog), entry)
	if err != nil {
		t.Fatalf("PrependChangelog: %v", err)
	}
	return string(out)
}

func TestPrependChangelogEmptyUnreleased(t *testing.T) {
	got := prepend(t, changelogFixture, entry1)
	want := strings.Replace(changelogFixture, "## [Unreleased]\n\n## [0.1.0]",
		"## [Unreleased]\n\n### Changed\n\n"+entry1+"\n\n## [0.1.0]", 1)
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	// Idempotent.
	if again := prepend(t, got, entry1); again != got {
		t.Errorf("second insert changed the changelog:\n%s", again)
	}
	// Also with surrounding blank lines/trailing whitespace in the entry.
	if again := prepend(t, got, "\n"+entry1+"  \n\n"); again != got {
		t.Errorf("insert of padded entry changed the changelog:\n%s", again)
	}

	// A newer entry goes on top of the existing list.
	entry2 := "- Upgrade docker-agent from v1.150.0 to v1.151.0: 2 enums changed."
	got2 := prepend(t, got, entry2)
	if !strings.Contains(got2, "### Changed\n\n"+entry2+"\n"+entry1+"\n\n## [0.1.0]") {
		t.Errorf("second entry not prepended:\n%s", got2)
	}
	if strings.Count(got2, "### Changed") != 1 {
		t.Errorf("duplicate Changed subsection:\n%s", got2)
	}
}

func TestPrependChangelogExistingChanged(t *testing.T) {
	in := "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- New thing.\n\n### Changed\n\n- Older change.\n\n### Fixed\n\n- Bug.\n\n## [0.1.0]\n"
	got := prepend(t, in, entry1)
	want := "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- New thing.\n\n### Changed\n\n" + entry1 +
		"\n- Older change.\n\n### Fixed\n\n- Bug.\n\n## [0.1.0]\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	// An empty Changed subsection directly followed by another subsection.
	in = "## [Unreleased]\n\n### Changed\n### Fixed\n\n- Bug.\n"
	got = prepend(t, in, entry1)
	if want := "## [Unreleased]\n\n### Changed\n\n" + entry1 + "\n\n### Fixed\n\n- Bug.\n"; got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}

	// The entry is only looked up inside the Unreleased section.
	in = "## [Unreleased]\n\n## [0.1.0]\n\n### Changed\n\n" + entry1 + "\n"
	got = prepend(t, in, entry1)
	if strings.Count(got, entry1) != 2 || !strings.HasPrefix(got, "## [Unreleased]\n\n### Changed\n\n"+entry1+"\n\n## [0.1.0]") {
		t.Errorf("got:\n%s", got)
	}
}

func TestPrependChangelogAfterAdded(t *testing.T) {
	in := "## [Unreleased]\n\n### Added\n\n- New thing.\n\n### Fixed\n\n- Bug.\n\n## [0.1.0]\n"
	got := prepend(t, in, entry1)
	want := "## [Unreleased]\n\n### Added\n\n- New thing.\n\n### Changed\n\n" + entry1 + "\n\n### Fixed\n\n- Bug.\n\n## [0.1.0]\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	// Without Added, Changed goes right below the heading.
	in = "## [Unreleased]\n\n### Fixed\n\n- Bug.\n"
	got = prepend(t, in, entry1)
	if want := "## [Unreleased]\n\n### Changed\n\n" + entry1 + "\n\n### Fixed\n\n- Bug.\n"; got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestPrependChangelogUnreleasedLastSection(t *testing.T) {
	// Unreleased at the end of the file, followed only by link references or nothing.
	in := "# Changelog\n\n## [Unreleased]\n\n[Unreleased]: https://example.com\n"
	got := prepend(t, in, entry1)
	if want := "# Changelog\n\n## [Unreleased]\n\n### Changed\n\n" + entry1 + "\n\n[Unreleased]: https://example.com\n"; got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	in = "# Changelog\n\n## [Unreleased]\n"
	got = prepend(t, in, entry1)
	if want := "# Changelog\n\n## [Unreleased]\n\n### Changed\n\n" + entry1 + "\n"; got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestPrependChangelogCreatesUnreleased(t *testing.T) {
	in := "# Changelog\n\nIntro.\n\n## [0.1.0] - 2026-01-01\n\n- First.\n"
	got := prepend(t, in, entry1)
	want := "# Changelog\n\nIntro.\n\n## [Unreleased]\n\n### Changed\n\n" + entry1 + "\n\n## [0.1.0] - 2026-01-01\n\n- First.\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if again := prepend(t, got, entry1); again != got {
		t.Errorf("not idempotent:\n%s", again)
	}

	got = prepend(t, "# Changelog\n", entry1)
	if want := "# Changelog\n\n## [Unreleased]\n\n### Changed\n\n" + entry1 + "\n"; got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestPrependChangelogMultiLineEntryAndCRLF(t *testing.T) {
	entry := "- Upgrade docker-agent.\n  - detail one\n  - detail two"
	in := strings.ReplaceAll(changelogFixture, "\n", "\r\n")
	got := prepend(t, in, entry)
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Error("CRLF line endings not preserved")
	}
	if !strings.Contains(got, "### Changed\r\n\r\n- Upgrade docker-agent.\r\n  - detail one\r\n  - detail two\r\n\r\n## [0.1.0]") {
		t.Errorf("got:\n%q", got)
	}
	if again := prepend(t, got, entry); again != got {
		t.Error("multi-line entry inserted twice")
	}
}

func TestPrependChangelogEmptyEntry(t *testing.T) {
	for _, entry := range []string{"", "\n\n", "  \n"} {
		if _, err := PrependChangelog([]byte(changelogFixture), entry); err == nil {
			t.Errorf("entry %q accepted", entry)
		}
	}
}

func TestPrependChangelogRepositoryFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Skipf("CHANGELOG.md not available: %v", err)
	}
	got := prepend(t, string(raw), entry1)
	i := strings.Index(got, "## [Unreleased]\n\n### Changed\n\n"+entry1+"\n\n## ")
	if i < 0 {
		t.Fatalf("entry not placed below ## [Unreleased]:\n%s", got)
	}
	if strings.Replace(got, "### Changed\n\n"+entry1+"\n\n", "", 1) != string(raw) {
		t.Error("PrependChangelog changed more than the Unreleased section")
	}
}

func TestChangelogEntry(t *testing.T) {
	d := mustCompare(t, oldFixture, newFixture)
	got := ChangelogEntry("v1.149.0", "v1.150.0", d)
	want := "- Upgrade docker-agent from v1.149.0 to v1.150.0 (configuration version `\"11\"`, previously `\"10\"`): " +
		d.Summary() + "; the new configuration keys are typed inputs of `AgentConfig`."
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}

	same := mustCompare(t, newFixture, newFixture)
	if got, want := ChangelogEntry("v1.150.0", "v1.150.1", same),
		"- Upgrade docker-agent from v1.150.0 to v1.150.1 (configuration version `\"11\"`): no configuration schema changes."; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}

	if got, want := ChangelogEntry("v1", "v2", nil), "- Upgrade docker-agent from v1 to v2: no configuration schema changes."; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}

	enumOnly := &Diff{EnumChanges: []EnumChange{{Path: "Toolset.type", Added: []string{"git"}}}, Version: VersionChange{NewLatest: "16", OldLatest: "16"}}
	if got, want := ChangelogEntry("v1", "v2", enumOnly),
		"- Upgrade docker-agent from v1 to v2 (configuration version `\"16\"`): 1 enum changed."; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if !strings.HasPrefix(ChangelogEntry("a", "b", d), "- ") {
		t.Error("entry is not a list item")
	}
}
