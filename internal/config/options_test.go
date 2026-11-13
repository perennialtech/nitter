// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestOptionsAreDocumented(t *testing.T) {
	sections := map[string]bool{}
	for _, s := range sectionDocs {
		if s.Doc == "" {
			t.Errorf("[%s] has no description", s.Name)
		}
		sections[s.Name] = true
	}
	seen := map[string]bool{}
	for _, o := range Options {
		name := o.Section + "." + o.Key
		if seen[name] {
			t.Errorf("%s is defined twice", name)
		}
		seen[name] = true
		if !sections[o.Section] {
			t.Errorf("%s: section [%s] has no description", name, o.Section)
		}
		if !strings.HasSuffix(o.Doc, ".") {
			t.Errorf("%s: description %q must be one or more sentences", name, o.Doc)
		}
	}
	for _, e := range EnvVars {
		if !(strings.HasPrefix(e.Name, "NITTER_") || e.Stdlib) || !strings.HasSuffix(e.Doc, ".") {
			t.Errorf("%s: undocumented or misnamed", e.Name)
		}
	}
}

// nonDefault returns a valid value for o other than its default.
func nonDefault(t *testing.T, o Option) string {
	switch o.kind {
	case kindInt:
		n, err := strconv.Atoi(o.Default)
		if err != nil {
			t.Fatal(err)
		}
		return strconv.Itoa(n + 1)
	case kindBool:
		b, err := ParseBool(o.Default)
		if err != nil {
			t.Fatal(err)
		}
		return strconv.FormatBool(!b)
	}
	return o.Default + "x"
}

// TestOptionsCoverConfig checks that every Config field is set from an
// option, so there is no setting FromFile reads without documenting it,
// and that every option sets a field.
func TestOptionsCoverConfig(t *testing.T) {
	defaults, err := FromFile(File{})
	if err != nil {
		t.Fatal(err)
	}
	def := reflect.ValueOf(*defaults)
	covered := map[string]bool{"Preferences": true}
	for _, o := range Options {
		c, err := FromFile(File{o.Section: {o.Key: nonDefault(t, o)}})
		if err != nil {
			t.Fatalf("%s.%s: %v", o.Section, o.Key, err)
		}
		got := reflect.ValueOf(*c)
		changed := false
		for i := range got.NumField() {
			if !reflect.DeepEqual(got.Field(i).Interface(), def.Field(i).Interface()) {
				covered[got.Type().Field(i).Name] = true
				changed = true
			}
		}
		if !changed {
			t.Errorf("%s.%s doesn't set any Config field", o.Section, o.Key)
		}
	}
	for i := range def.NumField() {
		if name := def.Type().Field(i).Name; !covered[name] {
			t.Errorf("Config.%s isn't set by any option", name)
		}
	}
}

func TestEnableRSSTurnsOffEveryFeed(t *testing.T) {
	c, err := FromFile(File{"Config": {"enableRSS": "false", "enableRSSList": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	v := reflect.ValueOf(*c)
	for i := range v.NumField() {
		if name := v.Type().Field(i).Name; strings.HasPrefix(name, "EnableRSS") && v.Field(i).Bool() {
			t.Errorf("%s is on with enableRSS = false", name)
		}
	}
}

func TestEmptyValueUsesDefault(t *testing.T) {
	f, err := Parse("[Server]\nport =\ntitle = \"\"\n")
	if err != nil {
		t.Fatal(err)
	}
	c, err := FromFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 8080 || c.Title != "Nitter" {
		t.Errorf("port %d, title %q", c.Port, c.Title)
	}
}

func TestQuoteRoundTrips(t *testing.T) {
	for _, s := range []string{"", "plain", `a "quoted" \ value`, "tab\tnewline\n", "# not a comment"} {
		f, err := Parse("[S]\nk = " + Quote(s) + "\n")
		if err != nil {
			t.Fatal(err)
		}
		if got := f["S"]["k"]; got != s {
			t.Errorf("Quote(%q) reads back as %q", s, got)
		}
	}
}

// TestEnvironmentIsReadThroughEnvVars fails when code outside this
// package reads an environment variable directly, which would leave it
// out of nitter.example.env.
func TestEnvironmentIsReadThroughEnvVars(t *testing.T) {
	root := filepath.Join("..", "..")
	check := func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			path == filepath.Join(root, "internal", "config", "options.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "os" {
				switch sel.Sel.Name {
				case "Getenv", "LookupEnv", "Environ", "ExpandEnv":
					t.Errorf("%s: os.%s; read environment variables through config.EnvVars",
						path, sel.Sel.Name)
				}
			}
			return true
		})
		return nil
	}
	for _, dir := range []string{"cmd", "internal"} {
		if err := filepath.WalkDir(filepath.Join(root, dir), check); err != nil {
			t.Fatal(err)
		}
	}
}
