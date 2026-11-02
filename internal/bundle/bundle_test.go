package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"refrail/policy"
)

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	m := Manifest{SchemaVersion: 1, Version: "v0.1.0", PolicyVersion: "1.0.0", LefthookVersion: "2.1.17", GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	files := map[string][]byte{"policy/default.json": policy.DefaultJSON, "lefthook.yml": []byte("no_auto_install: true\n"), "THIRD_PARTY_NOTICES.md": []byte("notice"), "licenses/go.txt": []byte("license"), "licenses/lefthook.txt": []byte("license"), "bin/" + Executable("refrail", runtime.GOOS): []byte("validator"), "bin/" + Executable("lefthook", runtime.GOOS): []byte("lefthook")}
	files["LICENSE"] = []byte("test license")
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0644)
		if filepath.Dir(name) == "bin" {
			mode = 0755
		}
		if err := os.WriteFile(p, data, mode); err != nil {
			t.Fatal(err)
		}
		m.Files = append(m.Files, File{name, Digest(data), uint32(mode)})
	}
	writeManifest(t, dir, m)
	return dir
}
func writeManifest(t *testing.T, dir string, m Manifest) {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestName), b, 0644); err != nil {
		t.Fatal(err)
	}
}
func TestVerifyRejectsCorruptionAndUnsafeEntries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, string)
	}{
		{"corruption", func(t *testing.T, p string) { os.WriteFile(filepath.Join(p, "lefthook.yml"), []byte("corrupt"), 0644) }},
		{"missing binary", func(t *testing.T, p string) { os.Remove(filepath.Join(p, "bin", Executable("lefthook", runtime.GOOS))) }},
		{"unexpected file", func(t *testing.T, p string) { os.WriteFile(filepath.Join(p, "extra"), []byte("x"), 0644) }},
		{"symlink", func(t *testing.T, p string) {
			if runtime.GOOS == "windows" {
				t.Skip("symlink privilege differs on Windows")
			}
			file := filepath.Join(p, "bin", Executable("lefthook", runtime.GOOS))
			os.Remove(file)
			if err := os.Symlink(filepath.Join(p, "lefthook.yml"), file); err != nil {
				t.Fatal(err)
			}
		}},
		{"traversal", func(t *testing.T, p string) {
			raw, _ := os.ReadFile(filepath.Join(p, ManifestName))
			var m Manifest
			json.Unmarshal(raw, &m)
			m.Files = append(m.Files, File{"../outside", Digest(nil), 0644})
			writeManifest(t, p, m)
		}},
		{"duplicate", func(t *testing.T, p string) {
			raw, _ := os.ReadFile(filepath.Join(p, ManifestName))
			var m Manifest
			json.Unmarshal(raw, &m)
			m.Files = append(m.Files, m.Files[0])
			writeManifest(t, p, m)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := fixture(t)
			if _, err := Verify(p, true); err != nil {
				t.Fatal(err)
			}
			tc.change(t, p)
			if _, err := Verify(p, true); err == nil {
				t.Fatal("accepted unsafe/corrupt bundle")
			}
		})
	}
}

func TestVerifiedCopyAndSafePaths(t *testing.T) {
	p := fixture(t)
	v, err := Verify(p, true)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "installed")
	if err := CopyVerified(v, dest); err != nil {
		t.Fatal(err)
	}
	got, err := Verify(dest, true)
	if err != nil || got.Digest != v.Digest {
		t.Fatalf("copy: %v", err)
	}
	for _, name := range []string{"", ".", "..", "../x", "/x", "C:/x", "a\\b", "a//b", "a/../b", "a/./b", "a\x00b"} {
		if SafePath(name) {
			t.Errorf("unsafe: %q", name)
		}
	}
	if !SafePath("policy/default.json") {
		t.Fatal("valid relative path rejected")
	}
}
