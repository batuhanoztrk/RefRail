package i18n

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestLanguageSelection(t *testing.T) {
	for _, tc := range []struct {
		name, override, native string
		env                    map[string]string
		want                   Language
	}{
		{"Turkish UI", "", "tr-TR", nil, Turkish},
		{"English UI beats Turkish locale", "", "en-GB", map[string]string{"LANG": "tr_TR.UTF-8"}, English},
		{"Other UI language", "", "de-DE", nil, English},
		{"Explicit Turkish", "tr", "en", nil, Turkish},
		{"Explicit English", "en", "tr", nil, English},
		{"Invalid override uses UI", "xx", "tr", nil, Turkish},
		{"Missing UI uses locale", "", "", map[string]string{"LANG": "tr_TR.UTF-8"}, Turkish},
		{"LC_ALL priority", "", "", map[string]string{"LC_ALL": "C", "LC_MESSAGES": "tr_TR", "LANG": "tr_TR"}, English},
		{"Message locale priority", "", "", map[string]string{"LC_MESSAGES": "tr_TR", "LANG": "en_US"}, Turkish},
		{"LANGUAGE list", "", "", map[string]string{"LANGUAGE": "tr:en"}, Turkish},
		{"Other locale", "", "", map[string]string{"LANG": "fr_FR.UTF-8"}, English},
		{"No language", "", "", nil, English},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := detect(tc.override, func() string { return tc.native }, func(key string) string { return tc.env[key] })
			if got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	called := false
	if got := detect("tr", func() string { called = true; return "en" }, func(string) string { return "" }); got != Turkish || called {
		t.Fatal("explicit override should avoid native lookup")
	}
}

func TestPreferredLanguageList(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"(\n    tr,\n    \"en-GB\"\n)", "tr"}, {"(\n \"en-US\", tr\n)", "en-US"}, {"()", ""}, {"(\"tr-TR\")", "tr-TR"}} {
		if got := firstPreferredLanguage(tc.input); got != tc.want {
			t.Fatalf("%q got %q", tc.input, got)
		}
	}
}

func TestNestedErrorsPreserveLanguageAndCause(t *testing.T) {
	cause := errors.New("sentinel")
	err := Errorf("Cannot execute Lefthook: %w", Errorf("Git operation failed (%s): %w", "rev-parse", cause))
	if !errors.Is(err, cause) {
		t.Fatal("lost wrapped error")
	}
	if got := Render(err, English); !strings.Contains(got, "Cannot execute Lefthook") || !strings.Contains(got, "Git operation failed") {
		t.Fatal(got)
	}
	if got := Render(err, Turkish); !strings.Contains(got, "Lefthook çalıştırılamadı") || !strings.Contains(got, "Git işlemi başarısız") {
		t.Fatal(got)
	}
	if got := Render(errors.Join(err, fmt.Errorf("external error")), Turkish); !strings.Contains(got, "external error") {
		t.Fatal(got)
	}
	// User-provided interpolation values must never be translated as templates.
	if got := Text(Turkish, "Unknown command: %s", "Error:"); !strings.Contains(got, "Error:") {
		t.Fatal(got)
	}
}

func TestPublicMessageTemplatesHaveTurkishTranslations(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	for _, dir := range []string{"internal/app", "internal/gitrepo", "internal/install", "internal/bundle", "policy"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "i18n" {
					return true
				}
				index := 0
				if sel.Sel.Name == "Text" {
					index = 1
				} else if sel.Sel.Name != "Errorf" {
					return true
				}
				if len(call.Args) <= index {
					return true
				}
				literal, ok := call.Args[index].(*ast.BasicLit)
				if !ok {
					return true
				}
				template, err := strconv.Unquote(literal.Value)
				if err != nil {
					return true
				}
				if _, ok := turkish[template]; !ok {
					t.Errorf("missing Turkish template in %s: %q", path, template)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
