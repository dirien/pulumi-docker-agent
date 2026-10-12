// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"go/format"
	"regexp"
	"strings"
	"unicode"
)

// normalize makes a converted program deterministic and fixes what the program generators get
// wrong for this package, then copies the YAML comments into it.
func normalize(code string, ex *example, lang language, schema *pkgSchema) (string, error) {
	if strings.Contains(code, "%!") {
		// A program generator failed on an expression (for example `%!v(PANIC=...)`).
		return "", fmt.Errorf("the program generator failed:\n%s", code)
	}
	var err error
	switch lang.fence {
	case langPython:
		code, err = fixPythonImports(code, schema)
	case langJava:
		code, err = fixJava(code, schema)
	case langGo:
		code, err = fixGoAnyLists(code)
	case langHCL:
		code = normalizeHCL(code)
	}
	if err != nil {
		return "", err
	}
	code = trimLines(code)
	if err := checkResourceNames(code, ex.resources, lang); err != nil {
		return "", err
	}
	if code, err = addComments(code, ex.comments, lang); err != nil {
		return "", err
	}
	if lang.fence == langGo {
		b, err := format.Source([]byte(code))
		if err != nil {
			// The Go program generator writes unformatted code when it produces invalid Go.
			return "", fmt.Errorf("the converted Go program does not compile: %w\n%s", err, code)
		}
		code = string(b)
	}
	if strings.Contains(code, "```") {
		return "", fmt.Errorf("the converted program contains ```:\n%s", code)
	}
	return code, nil
}

// fixGoAnyLists repairs the list values of an untyped map (for example the `required` list of a
// structured output schema), which the Go program generator writes as `pulumi.Any{...}`, a
// composite literal of a function: they become `pulumi.Any([]interface{}{...})`.
func fixGoAnyLists(code string) (string, error) {
	const bad = "pulumi.Any{"
	for {
		start := strings.Index(code, bad)
		if start < 0 {
			return code, nil
		}
		open := start + len(bad) - 1
		end, err := matchingBrace(code, open)
		if err != nil {
			return "", err
		}
		code = code[:start] + "pulumi.Any([]interface{}{" + code[open+1:end] + "})" + code[end+1:]
	}
}

// matchingBrace returns the offset of the brace that closes the one at open, skipping Go string
// literals.
func matchingBrace(code string, open int) (int, error) {
	depth := 0
	for i := open; i < len(code); i++ {
		switch code[i] {
		case '"':
			for i++; i < len(code) && code[i] != '"'; i++ {
				if code[i] == '\\' {
					i++
				}
			}
		case '`':
			i += strings.IndexByte(code[i+1:], '`') + 1
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("unbalanced braces at offset %d", open)
}

// trimLines removes trailing whitespace from every line and blank lines at the end.
func trimLines(code string) string {
	lines := strings.Split(code, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

// fixPythonImports removes the import of the default package name (pulumi_docker_agent) that the
// Python program generator adds for functions next to the import of the real package name.
func fixPythonImports(code string, schema *pkgSchema) (string, error) {
	var python struct {
		PackageName string `json:"packageName"`
	}
	if err := schema.languageInfo("python", &python); err != nil {
		return "", err
	}
	wrong := "pulumi_" + strings.ReplaceAll(schema.Name, "-", "_")
	if python.PackageName == "" || python.PackageName == wrong {
		return code, nil
	}
	wrongRE := regexp.MustCompile(`(?m)^import ` + wrong + `( as \w+)?\n`)
	for _, m := range wrongRE.FindAllStringSubmatch(code, -1) {
		right := "import " + python.PackageName + m[1] + "\n"
		if strings.Contains(code, right) {
			code = strings.Replace(code, m[0], "", 1)
		} else {
			code = strings.Replace(code, m[0], right, 1)
		}
	}
	return code, nil
}

var (
	// The plugin version of bin/ changes with every build; the Registry's own HCL snippets leave it out too.
	hclVersionRE = regexp.MustCompile(`(?m)^([ \t]*)source[ \t]+= ("pulumi/[^"]+")\n[ \t]*version[ \t]+= "[^"]*"\n`)
	// Pulumi's create-before-delete replacement: harmless to drop for these resources (no
	// replacement depends on the order), and hand-written HCL does not have it.
	hclLifecycleRE = regexp.MustCompile(`(?m)^[ \t]*lifecycle \{\n[ \t]*create_before_destroy = true\n[ \t]*\}\n`)
	// The schema's replaceOnChanges, which the provider's diff applies anyway.
	hclReplaceRE = regexp.MustCompile(`(?m)^[ \t]*pulumi \{\n[ \t]*replace_on_changes = \[[^\]\n]*\]\n[ \t]*\}\n`)
	hclBlockRE   = regexp.MustCompile(`^(resource|data|output|variable|locals|terraform|provider|module) `)
	// pulumi-hcl writes `prompt ="..."` for template strings.
	hclAssignRE = regexp.MustCompile(`(?m)^([ \t]*[\w"-]+[ \t]+)=([^ =\n])`)
)

// normalizeHCL removes the plugin version and Terraform-only boilerplate and separates the
// top-level blocks with blank lines.
func normalizeHCL(code string) string {
	code = hclVersionRE.ReplaceAllString(code, "${1}source = $2\n")
	code = hclLifecycleRE.ReplaceAllString(code, "")
	code = hclReplaceRE.ReplaceAllString(code, "")
	code = hclAssignRE.ReplaceAllString(code, "$1= $2")
	lines := strings.Split(code, "\n")
	var out []string
	for i, line := range lines {
		if i > 0 && hclBlockRE.MatchString(line) && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// checkResourceNames makes sure that the converted program keeps the resource names: pulumi-java,
// for example, renames a resource called `project` to `projectResource`.
func checkResourceNames(code string, names []string, lang language) error {
	for _, name := range names {
		want := `"` + name + `"`
		if lang.fence == langHCL {
			want = `" "` + name + `" {`
		}
		if !strings.Contains(code, want) {
			return fmt.Errorf("the converted program does not have the resource name %q; rename the resource", name)
		}
	}
	return nil
}

// addComments copies the comments above resources, variables and config keys of the YAML program
// to the statement that declares them in the converted program.
func addComments(code string, comments []comment, lang language) (string, error) {
	if lang.fence == langYAML {
		return code, nil
	}
	lines := strings.Split(code, "\n")
	for _, c := range comments {
		re := declarationRE(c, lang.fence)
		at := -1
		for i, line := range lines {
			if re.MatchString(line) {
				at = i
				break
			}
		}
		if at < 0 {
			return "", fmt.Errorf("cannot find the declaration of %s %q for its comment (%s)", c.kind, c.key, re)
		}
		indent := lines[at][:len(lines[at])-len(strings.TrimLeft(lines[at], " \t"))]
		var insert []string
		if at > 0 {
			prev := strings.TrimSpace(lines[at-1])
			if prev != "" && !strings.HasSuffix(prev, "{") && !strings.HasSuffix(prev, "(") {
				insert = append(insert, "")
			}
		}
		for _, text := range c.lines {
			insert = append(insert, strings.TrimRight(indent+lang.comment+" "+text, " "))
		}
		lines = append(lines[:at], append(insert, lines[at:]...)...)
	}
	return strings.Join(lines, "\n"), nil
}

// declarationRE matches the line that declares a resource, variable or config value in a
// converted program.
func declarationRE(c comment, lang string) *regexp.Regexp {
	quoted := regexp.QuoteMeta(`"` + c.key + `"`)
	if c.kind == "config" {
		if lang == langHCL {
			return regexp.MustCompile(`^variable ` + quoted + ` \{`)
		}
		return regexp.MustCompile(`\(` + quoted + `\)`)
	}
	if c.kind == "resources" {
		switch lang {
		case langGo:
			return regexp.MustCompile(`\(ctx, ` + quoted)
		case langHCL:
			return regexp.MustCompile(`^resource "[^"]+" "` + regexp.QuoteMeta(c.key) + `" \{`)
		case langPython:
			return regexp.MustCompile(`^\s*(\w+ = )?[\w.]+\(` + quoted)
		default:
			return regexp.MustCompile(`^\s*((const|var) \w+ = )?new [\w.]+\(` + quoted)
		}
	}
	camel := regexp.QuoteMeta(camelCase(c.key))
	switch lang {
	case langTypeScript:
		return regexp.MustCompile(`^\s*const ` + camel + ` = `)
	case langPython:
		return regexp.MustCompile(`^\s*` + regexp.QuoteMeta(snakeCase(c.key)) + ` = `)
	case langGo:
		return regexp.MustCompile(`^\s*` + camel + ` := `)
	case langCSharp:
		return regexp.MustCompile(`^\s*var ` + camel + ` = `)
	case langJava:
		return regexp.MustCompile(`^\s*final var ` + camel + ` = `)
	default: // hcl: a data source or a local value
		name := regexp.QuoteMeta(c.key)
		return regexp.MustCompile(`^(data "[^"]+" "` + name + `" \{|\s+` + name + ` += )`)
	}
}

// camelCase turns a Pulumi YAML name into the variable name of the converted programs.
func camelCase(name string) string {
	var b strings.Builder
	upper := false
	for i, r := range name {
		switch {
		case r == '-' || r == '_':
			upper = i > 0
		case upper:
			b.WriteRune(unicode.ToUpper(r))
			upper = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// snakeCase turns a Pulumi YAML name into the variable name of the converted Python program.
func snakeCase(name string) string {
	var b strings.Builder
	for i, r := range camelCase(name) {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}
