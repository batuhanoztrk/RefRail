// packaging generates distribution metadata from already-built release assets.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"refrail/internal/bundle"
)

//go:embed templates/*
var templates embed.FS

var stableVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var repositoryName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var packageID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*(\.[A-Za-z0-9][A-Za-z0-9-]*)+$`)

type metadata struct {
	Repository, Tag, Version, Identifier, Publisher string
	Checksums                                       map[string]string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Packaging error:", err)
		os.Exit(1)
	}
}

func run() error {
	version := flag.String("version", "", "Stable release tag, e.g. v0.1.0")
	repository := flag.String("repository", "batuhanoztrk/RefRail", "Public GitHub owner/repository")
	identifier := flag.String("winget-id", "batuhanoztrk.RefRail", "WinGet package identifier")
	publisher := flag.String("publisher", "batuhanoztrk", "WinGet publisher display name")
	dist := flag.String("dist", "", "Directory containing the six ZIPs and SHA256SUMS")
	out := flag.String("out", "", "New output directory for generated metadata")
	flag.Parse()
	if flag.NArg() != 0 || *dist == "" || *out == "" {
		return fmt.Errorf("--dist and --out are required; positional arguments are not supported")
	}
	if !stableVersion.MatchString(*version) || !repositoryName.MatchString(*repository) || !packageID.MatchString(*identifier) {
		return fmt.Errorf("invalid stable tag, GitHub repository, or WinGet identifier")
	}
	if strings.TrimSpace(*publisher) == "" || len(*publisher) > 256 || strings.ContainsAny(*publisher, "\r\n\x00") {
		return fmt.Errorf("publisher must be a nonempty single line of at most 256 bytes")
	}
	m := metadata{Repository: *repository, Tag: *version, Version: strings.TrimPrefix(*version, "v"), Identifier: *identifier, Publisher: *publisher}
	var err error
	m.Checksums, err = verifyArchives(*dist, m.Tag)
	if err != nil {
		return err
	}
	// Never leave apparently complete metadata behind after a failed generation.
	if _, err := os.Lstat(*out); !os.IsNotExist(err) {
		return fmt.Errorf("output already exists or is inaccessible: %s", *out)
	}
	parent := filepath.Dir(*out)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(parent, ".packaging-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	parts := strings.Split(m.Identifier, ".")
	winget := filepath.Join(append([]string{"winget", "manifests", strings.ToLower(m.Identifier[:1])}, append(parts, m.Version)...)...)
	files := map[string]string{
		"homebrew.rb.tmpl":    "homebrew/Formula/refrail.rb",
		"installer.yaml.tmpl": filepath.Join(winget, m.Identifier+".installer.yaml"),
		"locale.yaml.tmpl":    filepath.Join(winget, m.Identifier+".locale.en-US.yaml"),
		"version.yaml.tmpl":   filepath.Join(winget, m.Identifier+".yaml"),
	}
	for source, target := range files {
		data, err := render(source, m)
		if err != nil {
			return err
		}
		p := filepath.Join(temp, target)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0644); err != nil {
			return err
		}
	}
	if err := os.Rename(temp, *out); err != nil {
		return err
	}
	fmt.Println("Package metadata:", *out)
	return nil
}

func render(name string, m metadata) ([]byte, error) {
	funcs := template.FuncMap{
		"quote": func(s string) string { b, _ := json.Marshal(s); return string(b) },
		"sha":   func(target string) string { return m.Checksums["refrail_"+m.Tag+"_"+target+".zip"] },
	}
	t, err := template.New(name).Funcs(funcs).Option("missingkey=error").ParseFS(templates, "templates/"+name)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := t.Execute(&out, m); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func verifyArchives(dir, version string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return nil, err
	}
	sums := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid SHA256SUMS line: %q", line)
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size || sums[fields[1]] != "" {
			return nil, fmt.Errorf("invalid or duplicate checksum: %q", line)
		}
		sums[fields[1]] = strings.ToLower(fields[0])
	}
	for _, goos := range []string{"darwin", "linux", "windows"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			name := "refrail_" + version + "_" + goos + "_" + goarch + ".zip"
			if sums[name] == "" {
				return nil, fmt.Errorf("missing checksum: %s", name)
			}
			p := filepath.Join(dir, name)
			f, err := os.Open(p)
			if err != nil {
				return nil, err
			}
			h := sha256.New()
			_, err = io.Copy(h, f)
			f.Close()
			if err != nil {
				return nil, err
			}
			if hex.EncodeToString(h.Sum(nil)) != sums[name] {
				return nil, fmt.Errorf("SHA-256 mismatch: %s", name)
			}
			if err := verifyArchiveManifest(p, version, goos, goarch); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	return sums, nil
}

func verifyArchiveManifest(filename, version, goos, goarch string) error {
	z, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer z.Close()
	root, err := os.MkdirTemp("", "refrail-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	seen := map[string]bool{}
	var total uint64
	const maxSize = 256 * 1024 * 1024
	for _, entry := range z.File {
		if !strings.HasPrefix(entry.Name, "refrail/") || seen[entry.Name] {
			return fmt.Errorf("unexpected or duplicate ZIP path: %s", entry.Name)
		}
		seen[entry.Name] = true
		relative := strings.TrimPrefix(entry.Name, "refrail/")
		if relative == "" && entry.FileInfo().IsDir() {
			continue
		}
		if !bundle.SafePath(strings.TrimSuffix(relative, "/")) {
			return fmt.Errorf("unsafe ZIP path: %s", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if !entry.Mode().IsRegular() || entry.UncompressedSize64 > maxSize || total > maxSize-entry.UncompressedSize64 {
			return fmt.Errorf("non-regular or oversized ZIP entry: %s", entry.Name)
		}
		total += entry.UncompressedSize64
		p := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return err
		}
		if err := extractFile(entry, p); err != nil {
			return err
		}
	}
	v, err := bundle.Verify(root, false)
	if err != nil {
		return err
	}
	m := v.Manifest
	if m.SchemaVersion != 1 || m.Version != version || m.GOOS != goos || m.GOARCH != goarch {
		return fmt.Errorf("bundle manifest does not match release version/platform")
	}
	return nil
}

func extractFile(entry *zip.File, path string) error {
	in, err := entry.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(out, io.LimitReader(in, int64(entry.UncompressedSize64)+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if uint64(written) != entry.UncompressedSize64 {
		return fmt.Errorf("ZIP entry size mismatch: %s", entry.Name)
	}
	return closeErr
}
