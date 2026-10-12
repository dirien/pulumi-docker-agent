// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"
	"testing/fstest"
)

// TestHCLReadsAreVendored checks which of the files that are not vendored by type an HCL
// example's file() calls make syncUpstream copy: text files below the example's directory.
func TestHCLReadsAreVendored(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"team/agent.hcl": {Data: []byte(`agent "root" {
  instruction     = file("prompts/system.md")
  welcome_message = file( "notes/welcome.txt")
  description     = file("../shared/description.txt", { name = "x" })
  add_prompt_files = [file("run.sh"), file("/etc/hosts.txt"), file("missing.txt")]
}
`)},
		"team/prompts/system.md":  {},
		"team/notes/welcome.txt":  {},
		"shared/description.txt":  {},
		"team/run.sh":             {},
		"rag/docs/blork.txt":      {},
		"other/notes/welcome.txt": {},
	}
	others := []string{"team/notes/welcome.txt", "shared/description.txt", "team/run.sh", "rag/docs/blork.txt",
		"other/notes/welcome.txt"}
	got, err := hclReads(fsys, others)
	if err != nil {
		t.Fatal(err)
	}
	// Not shared/description.txt: docker-agent's file() only reads below the configuration's
	// directory. Not the Markdown file, which is vendored anyway, nor run.sh.
	if want := []string{"team/notes/welcome.txt"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("hclReads = %v, want %v", got, want)
	}
}
