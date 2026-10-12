// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/dirien/pulumi-docker-agent/provider/internal/schemadiff"
)

type diffFlags struct {
	old, new, from, to string
}

func parseDiffFlags(name string, args []string, extra func(*flag.FlagSet)) (diffFlags, error) {
	var f diffFlags
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.StringVar(&f.old, "old", "", "the previous agent-schema.json")
	fs.StringVar(&f.new, "new", "", "the new agent-schema.json")
	fs.StringVar(&f.from, "from", "", "the previous docker-agent version, for example v1.149.0")
	fs.StringVar(&f.to, "to", "", "the new docker-agent version")
	if extra != nil {
		extra(fs)
	}
	if err := fs.Parse(args); err != nil {
		return f, err
	}
	if f.old == "" || f.new == "" || f.from == "" || f.to == "" {
		return f, errors.New(name + ": -old, -new, -from and -to are required")
	}
	return f, nil
}

func compare(f diffFlags) (*schemadiff.Diff, error) {
	oldRaw, err := os.ReadFile(f.old)
	if err != nil {
		return nil, err
	}
	newRaw, err := os.ReadFile(f.new)
	if err != nil {
		return nil, err
	}
	return schemadiff.Compare(oldRaw, newRaw)
}

// cmdDiff prints the Markdown report of the schema changes between two docker-agent releases.
func cmdDiff(args []string) error {
	f, err := parseDiffFlags("diff", args, nil)
	if err != nil {
		return err
	}
	d, err := compare(f)
	if err != nil {
		return err
	}
	_, err = fmt.Print(d.Markdown(f.from, f.to, untypedPath, &schemadiff.Enums{Old: enumClassifier(f.old), New: enumClassifier(f.new)}))
	return err
}

// enumClassifier returns which schema paths the generator turns into Pulumi enums for the
// schema in file, or nil (with a warning) when the generator rejects the schema.
func enumClassifier(file string) schemadiff.EnumClassifier {
	g, _, err := loadGenerator(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "schemagen: the Pulumi enums of %s are unknown: %v\n", file, err)
		return nil
	}
	return func(path string) (string, bool) {
		name, ok := g.enumAt[path]
		return name, ok
	}
}

// cmdChangelog prepends an entry for the upgrade to the Unreleased section of CHANGELOG.md.
func cmdChangelog(args []string) error {
	var file string
	f, err := parseDiffFlags("changelog", args, func(fs *flag.FlagSet) {
		fs.StringVar(&file, "file", "", "the CHANGELOG.md to update")
	})
	if err != nil {
		return err
	}
	if file == "" {
		return errors.New("changelog: -file is required")
	}
	d, err := compare(f)
	if err != nil {
		return err
	}
	current, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	updated, err := schemadiff.PrependChangelog(current, schemadiff.ChangelogEntry(f.from, f.to, d))
	if err != nil {
		return err
	}
	return os.WriteFile(file, updated, 0o644) //nolint:gosec // committed documentation
}
