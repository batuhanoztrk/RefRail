package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"refrail/internal/bundle"
	"refrail/policy"
)

func fixtureArchive(t *testing.T, filename, goos, goarch string, edit func(*bundle.Manifest, map[string][]byte)) {
	t.Helper()
	files := map[string][]byte{
		"bin/" + bundle.Executable("refrail", goos):  []byte("validator"),
		"bin/" + bundle.Executable("lefthook", goos): []byte("lefthook"),
		"policy/default.json":                        policy.DefaultJSON,
		"lefthook.yml":                               []byte("commit-msg: {}\n"),
		"LICENSE":                                    []byte("MIT"), "THIRD_PARTY_NOTICES.md": []byte("notices"),
		"licenses/go.txt": []byte("BSD"), "licenses/lefthook.txt": []byte("MIT"),
	}
	c, err := policy.Load(policy.DefaultJSON)
	if err != nil {
		t.Fatal(err)
	}
	m := bundle.Manifest{SchemaVersion: 1, Version: "v0.1.0", PolicyVersion: c.Version, LefthookVersion: "2.1.17", GOOS: goos, GOARCH: goarch}
	for name, data := range files {
		mode := uint32(0644)
		if strings.HasPrefix(name, "bin/") {
			mode = 0755
		}
		m.Files = append(m.Files, bundle.File{Path: name, SHA256: bundle.Digest(data), Mode: mode})
	}
	if edit != nil {
		edit(&m, files)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	files[bundle.ManifestName] = data
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, data := range files {
		w, err := z.Create("refrail/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func fixtureArchives(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var sums []string
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			name := "refrail_v0.1.0_" + goos + "_" + goarch + ".zip"
			p := filepath.Join(dir, name)
			fixtureArchive(t, p, goos, goarch, nil)
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			sums = append(sums, bundle.Digest(data)+"  "+name)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestArchiveContents(t *testing.T) {
	for _, name := range []string{"missing-binary", "modified-policy", "unlisted-file", "manifest-only"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "bundle.zip")
			fixtureArchive(t, p, "windows", "arm64", func(m *bundle.Manifest, files map[string][]byte) {
				switch name {
				case "missing-binary":
					delete(files, "bin/refrail.exe")
				case "modified-policy":
					files["policy/default.json"] = []byte("{}")
				case "unlisted-file":
					files["portable.index.db"] = []byte("index")
				case "manifest-only":
					clear(files)
					m.Files = nil
				}
			})
			if err := verifyArchiveManifest(p, "v0.1.0", "windows", "arm64"); err == nil {
				t.Fatal("invalid bundle accepted")
			}
		})
	}
	for _, name := range []string{"traversal", "symlink", "duplicate", "wrong-root"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "bundle.zip")
			f, err := os.Create(p)
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(f)
			path := "refrail/file"
			if name == "traversal" {
				path = "refrail/../../outside"
			}
			if name == "wrong-root" {
				path = "file"
			}
			header := &zip.FileHeader{Name: path, Method: zip.Deflate}
			if name == "symlink" {
				header.SetMode(os.ModeSymlink | 0777)
			}
			if _, err := z.CreateHeader(header); err != nil {
				t.Fatal(err)
			}
			if name == "duplicate" {
				if _, err := z.Create(path); err != nil {
					t.Fatal(err)
				}
			}
			if err := z.Close(); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			err = verifyArchiveManifest(p, "v0.1.0", "linux", "amd64")
			if err == nil || strings.Contains(err.Error(), "Cannot read bundle manifest") {
				t.Fatalf("expected archive-path rejection, got %v", err)
			}
		})
	}
}

func TestArchiveVerification(t *testing.T) {
	t.Run("all-platforms", func(t *testing.T) {
		sums, err := verifyArchives(fixtureArchives(t), "v0.1.0")
		if err != nil || len(sums) != 6 {
			t.Fatalf("sums=%v err=%v", sums, err)
		}
	})
	t.Run("tampered-archive", func(t *testing.T) {
		dir := fixtureArchives(t)
		if err := os.WriteFile(filepath.Join(dir, "refrail_v0.1.0_windows_arm64.zip"), []byte("tampered"), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := verifyArchives(dir, "v0.1.0"); err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("missing-platform", func(t *testing.T) {
		dir := fixtureArchives(t)
		if err := os.Remove(filepath.Join(dir, "refrail_v0.1.0_darwin_arm64.zip")); err != nil {
			t.Fatal(err)
		}
		if _, err := verifyArchives(dir, "v0.1.0"); err == nil {
			t.Fatal("missing archive accepted")
		}
	})
	t.Run("duplicate-checksum", func(t *testing.T) {
		dir := fixtureArchives(t)
		p := filepath.Join(dir, "SHA256SUMS")
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, append(data, data...), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := verifyArchives(dir, "v0.1.0"); err == nil {
			t.Fatal("duplicate checksum accepted")
		}
	})
	t.Run("wrong-platform-manifest", func(t *testing.T) {
		dir := fixtureArchives(t)
		p := filepath.Join(dir, "refrail_v0.1.0_windows_arm64.zip")
		if err := verifyArchiveManifest(p, "v0.1.0", "windows", "amd64"); err == nil {
			t.Fatal("wrong platform accepted")
		}
	})
}
