// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// fixJava repairs what pulumi-java's program generator gets wrong for this package, so that the
// Java examples compile:
//
//   - It imports input types and functions from com.<namespace>.<package> and
//     com.pulumi.<package> instead of the SDK's base package (language.java.basePackage).
//   - It passes enum values as strings: `.safety("restricted")` becomes `.safety(SafetyMode.Restricted)`.
//   - fn::readFile becomes Files.readString, which throws a checked IOException that the stack
//     function cannot throw; it becomes a readFile helper.
//   - A map with a secret config value, `Map.of("KEY", secret)`, is a Map<String, Output<String>>
//     that no builder takes; it becomes `secret.applyValue(_secret -> Map.of("KEY", _secret))`.
//   - It writes a multi-line string as a text block without escaping its backslashes.
func fixJava(code string, schema *pkgSchema) (string, error) {
	code, err := fixJavaTextBlocks(code)
	if err != nil {
		return "", err
	}
	var java struct {
		BasePackage string `json:"basePackage"`
	}
	if err := schema.languageInfo("java", &java); err != nil {
		return "", err
	}
	base := java.BasePackage
	if base == "" {
		base = "com.pulumi"
	}
	pkg := base + "." + strings.ReplaceAll(schema.Name, "-", "")
	wrongPkgRE := regexp.MustCompile(`\bcom\.(?:pulumi|` + regexp.QuoteMeta(schema.Namespace) + `)\.` +
		regexp.QuoteMeta(strings.ReplaceAll(schema.Name, "-", "")) + `\b`)
	code = wrongPkgRE.ReplaceAllLiteralString(code, pkg)

	// Inline fully qualified input types, for example in function calls.
	var imports []string
	inlineRE := regexp.MustCompile(`([^\w.])` + regexp.QuoteMeta(pkg) + `\.inputs\.(\w+)\b`)
	lines := strings.Split(code, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "import ") {
			continue
		}
		for _, m := range inlineRE.FindAllStringSubmatch(line, -1) {
			imports = append(imports, pkg+".inputs."+m[2])
		}
		lines[i] = inlineRE.ReplaceAllString(line, "$1$2")
	}
	code = strings.Join(lines, "\n")

	code, enums, err := fixJavaEnums(code, javaEnumProperties(schema))
	if err != nil {
		return "", err
	}
	for _, e := range enums {
		imports = append(imports, pkg+".enums."+e)
	}

	if code, err = fixJavaReadFile(code, &imports); err != nil {
		return "", err
	}
	code = fixJavaSecretMaps(code)
	return addJavaImports(code, pkg, imports), nil
}

// enumProperty is an enum-typed input property.
type enumProperty struct {
	enum    string            // the enum class
	members map[string]string // value -> member
	kind    string            // "", "array" or "map"
}

// javaEnumProperties maps the Java input class names (AgentConfigArgs, GetAgentArgs,
// RuntimeDefaultsArgs, ...) to their enum-typed properties.
func javaEnumProperties(schema *pkgSchema) map[string]map[string]enumProperty {
	enumOf := func(p *schemaProperty) (enumProperty, bool) {
		kind := ""
		switch {
		case p.Items != nil:
			kind, p = "array", p.Items
		case p.AdditionalProperties != nil:
			kind, p = "map", p.AdditionalProperties
		}
		t, ok := schema.Types[strings.TrimPrefix(p.Ref, "#/types/")]
		if p.Ref == "" || !ok || len(t.Enum) == 0 {
			return enumProperty{}, false
		}
		e := enumProperty{enum: tokenName(strings.TrimPrefix(p.Ref, "#/types/")), members: map[string]string{}, kind: kind}
		for _, v := range t.Enum {
			e.members[fmt.Sprint(v.Value)] = v.Name
		}
		return e, true
	}
	classes := map[string]map[string]enumProperty{}
	add := func(class string, props map[string]*schemaProperty) {
		for name, p := range props {
			if e, ok := enumOf(p); ok {
				if classes[class] == nil {
					classes[class] = map[string]enumProperty{}
				}
				classes[class][name] = e
			}
		}
	}
	for token, r := range schema.Resources {
		add(tokenName(token)+"Args", r.InputProperties)
	}
	for token, f := range schema.Functions {
		if f.Inputs != nil {
			name := tokenName(token)
			add(strings.ToUpper(name[:1])+name[1:]+"Args", f.Inputs.Properties)
		}
	}
	for token, t := range schema.Types {
		if t.Type == "object" {
			add(tokenName(token)+"Args", t.Properties)
		}
	}
	return classes
}

// fixJavaEnums replaces string literals passed to enum-typed builder methods with enum members.
// It follows the builder chains: `XArgs.builder()` starts one, `.build()` ends it, and a
// `.name(...)` call at the same parenthesis depth sets a property of the innermost one.
func fixJavaEnums(code string, classes map[string]map[string]enumProperty) (string, []string, error) {
	type builder struct {
		class string
		depth int
	}
	type call struct {
		open   int
		class  string
		method string
	}
	type edit struct {
		start, end int
		text       string
	}
	var (
		builders []builder
		calls    []call
		edits    []edit
		used     []string
		depth    int
	)
	for i := 0; i < len(code); i++ {
		switch code[i] {
		case '"':
			end, err := skipJavaString(code, i)
			if err != nil {
				return "", nil, err
			}
			i = end
		case '/':
			if strings.HasPrefix(code[i:], "//") {
				i += strings.IndexByte(code[i:]+"\n", '\n')
			}
		case '(':
			c := call{open: i}
			method, receiver := javaCallBefore(code, i)
			switch {
			case method == "builder" && receiver != "":
				builders = append(builders, builder{class: receiver, depth: depth})
			case method == "build" && len(builders) > 0 && builders[len(builders)-1].depth == depth:
				builders = builders[:len(builders)-1]
			case method != "" && receiver == "" && len(builders) > 0 && builders[len(builders)-1].depth == depth:
				c.class, c.method = builders[len(builders)-1].class, method
			}
			calls = append(calls, c)
			depth++
		case ')':
			if len(calls) == 0 {
				return "", nil, fmt.Errorf("unbalanced parentheses at offset %d", i)
			}
			c := calls[len(calls)-1]
			calls = calls[:len(calls)-1]
			depth--
			prop, ok := classes[c.class][c.method]
			if c.method == "" || !ok {
				continue
			}
			for _, arg := range splitJavaArgs(code, c.open+1, i) {
				literal := strings.TrimSpace(code[arg[0]:arg[1]])
				if !strings.HasPrefix(literal, `"`) {
					continue
				}
				if prop.kind == "map" {
					return "", nil, fmt.Errorf("%s.%s: maps of enums are not supported", c.class, c.method)
				}
				value, err := strconv.Unquote(literal)
				if err != nil {
					return "", nil, fmt.Errorf("%s.%s(%s): %w", c.class, c.method, literal, err)
				}
				member, ok := prop.members[value]
				if !ok || member == "" {
					return "", nil, fmt.Errorf("%s.%s: %q is not a value of the enum %s", c.class, c.method, value, prop.enum)
				}
				start := arg[0] + strings.Index(code[arg[0]:arg[1]], literal)
				edits = append(edits, edit{start, start + len(literal), prop.enum + "." + member})
				if !slices.Contains(used, prop.enum) {
					used = append(used, prop.enum)
				}
			}
		}
	}
	if len(calls) != 0 {
		return "", nil, errors.New("unbalanced parentheses")
	}
	slices.SortFunc(edits, func(a, b edit) int { return b.start - a.start })
	for _, e := range edits {
		code = code[:e.start] + e.text + code[e.end:]
	}
	slices.Sort(used)
	return code, used, nil
}

// fixJavaTextBlocks escapes the backslashes of the text blocks (`"""`) pulumi-java writes for
// multi-line strings, which hold the string verbatim: `\(` does not compile, and a `\` at the
// end of a line would join two lines.
func fixJavaTextBlocks(code string) (string, error) {
	var b strings.Builder
	from := 0
	for i := 0; i < len(code); i++ {
		switch {
		case code[i] == '"':
			end, err := skipJavaString(code, i)
			if err != nil {
				return "", err
			}
			if strings.HasPrefix(code[i:], `"""`) {
				b.WriteString(code[from : i+3])
				b.WriteString(strings.ReplaceAll(code[i+3:end-2], `\`, `\\`))
				from = end - 2
			}
			i = end
		case strings.HasPrefix(code[i:], "//"):
			i += strings.IndexByte(code[i:]+"\n", '\n')
		}
	}
	return b.String() + code[from:], nil
}

// skipJavaString returns the offset of the closing quote of the string literal or text block
// (`"""`, which pulumi-java writes for multi-line strings) at start.
func skipJavaString(code string, start int) (int, error) {
	if strings.HasPrefix(code[start:], `"""`) {
		for i := start + 3; i < len(code); i++ {
			switch {
			case code[i] == '\\':
				i++
			case strings.HasPrefix(code[i:], `"""`):
				return i + 2, nil
			}
		}
		return 0, fmt.Errorf("unterminated text block at offset %d", start)
	}
	for i := start + 1; i < len(code); i++ {
		switch code[i] {
		case '\\':
			i++
		case '"':
			return i, nil
		case '\n':
			return 0, fmt.Errorf("unterminated string literal at offset %d", start)
		}
	}
	return 0, fmt.Errorf("unterminated string literal at offset %d", start)
}

var javaIdentRE = regexp.MustCompile(`(?:(\w+)\s*)?\.\s*(\w+)\s*$|(\w+)\s*$`)

// javaCallBefore returns the method name before the parenthesis at open and, for
// `Receiver.builder(`, the receiver's simple class name. A method called on a chain
// (`\n    .name(`) has no receiver.
func javaCallBefore(code string, open int) (method, receiver string) {
	start := max(open-200, 0)
	m := javaIdentRE.FindStringSubmatch(code[start:open])
	if m == nil {
		return "", ""
	}
	if m[2] == "" {
		return "", "" // a call without a dot: new X(, f(
	}
	if m[2] == "builder" {
		return m[2], m[1]
	}
	if m[1] != "" {
		return "", "" // a method of a variable or class, for example Map.of(
	}
	return m[2], ""
}

// splitJavaArgs splits the arguments between start and end at top-level commas.
func splitJavaArgs(code string, start, end int) [][2]int {
	var args [][2]int
	depth, from := 0, start
	for i := start; i < end; i++ {
		switch code[i] {
		case '"':
			if j, err := skipJavaString(code, i); err == nil {
				i = j
			}
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, [2]int{from, i})
				from = i + 1
			}
		}
	}
	if strings.TrimSpace(code[from:end]) != "" {
		args = append(args, [2]int{from, end})
	}
	return args
}

var javaReadFileRE = regexp.MustCompile(`Files\.readString\(Paths\.get\(("(?:[^"\\]|\\.)*")\)\)`)

const javaReadFileHelper = `
    private static String readFile(String path) {
        try {
            return Files.readString(Paths.get(path));
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
    }
}
`

// fixJavaReadFile moves Files.readString into a helper that throws an unchecked exception.
func fixJavaReadFile(code string, imports *[]string) (string, error) {
	if !strings.Contains(code, "Files.readString(") {
		return code, nil
	}
	code = javaReadFileRE.ReplaceAllString(code, "readFile($1)")
	if strings.Contains(code, "Files.readString(") {
		return "", errors.New("cannot replace every Files.readString call")
	}
	end := strings.LastIndex(code, "}")
	if end < 0 {
		return "", errors.New("the Java program has no class")
	}
	*imports = append(*imports, "java.io.IOException", "java.io.UncheckedIOException", "java.nio.file.Files",
		"java.nio.file.Paths")
	return strings.TrimRight(code[:end], " \t\n") + "\n" + javaReadFileHelper, nil
}

var (
	javaSecretRE    = regexp.MustCompile(`(?m)^\s*final var (\w+) = config\.(?:require|get)Secret\(`)
	javaSecretMapRE = regexp.MustCompile(`\(Map\.of\(("(?:[^"\\]|\\.)*"), (\w+)\)\)`)
)

// fixJavaSecretMaps turns `.env(Map.of("KEY", secret))`, with a secret config value, into
// `.env(secret.applyValue(_secret -> Map.of("KEY", _secret)))`.
func fixJavaSecretMaps(code string) string {
	secrets := map[string]bool{}
	for _, m := range javaSecretRE.FindAllStringSubmatch(code, -1) {
		secrets[m[1]] = true
	}
	return javaSecretMapRE.ReplaceAllStringFunc(code, func(s string) string {
		m := javaSecretMapRE.FindStringSubmatch(s)
		if !secrets[m[2]] {
			return s
		}
		return fmt.Sprintf("(%s.applyValue(_%s -> Map.of(%s, _%s)))", m[2], m[2], m[1], m[2])
	})
}

// addJavaImports adds imports after the package's own imports and removes duplicates.
func addJavaImports(code, pkg string, imports []string) string {
	lines := strings.Split(code, "\n")
	last, lastPkg := -1, -1
	for i, line := range lines {
		if strings.HasPrefix(line, "import ") {
			last = i
			if strings.HasPrefix(line, "import "+pkg+".") {
				lastPkg = i
			}
		}
	}
	at := lastPkg
	if at < 0 {
		at = last
	}
	var add []string
	for _, imp := range imports {
		add = append(add, "import "+imp+";")
	}
	if at >= 0 {
		lines = slices.Insert(lines, at+1, add...)
	}
	seen := map[string]bool{}
	out := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, "import ") {
			if seen[line] {
				continue
			}
			seen[line] = true
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
