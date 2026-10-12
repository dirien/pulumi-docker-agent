// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
)

// fixPrograms applies the program generator fixes of normalize to programs converted by other
// tools, such as the programs `pulumi convert --from docker-agent` generates in
// examples/convert_test.go: `docsgen -fix java App.java` rewrites App.java in place.
func fixPrograms(pulumi, binary, lang string, files []string) error {
	var fix func(code string, schema *pkgSchema) (string, error)
	switch lang {
	case langJava:
		fix = fixJava
	case langGo:
		fix = func(code string, _ *pkgSchema) (string, error) { return fixGoAnyLists(code) }
	case langPython:
		fix = fixPythonImports
	default:
		return fmt.Errorf("-fix %q: expected java, go or python", lang)
	}
	schema, err := loadSchema(pulumi, binary)
	if err != nil {
		return err
	}
	for _, file := range files {
		code, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		fixed, err := fix(string(code), schema)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		if err := os.WriteFile(file, []byte(fixed), 0o600); err != nil {
			return err
		}
	}
	return nil
}
