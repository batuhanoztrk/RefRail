// release builds self-contained local distribution bundles. It is maintainer-only.
package main

import (
	"archive/zip"
	"bytes"
	"debug/buildinfo"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"refrail/internal/i18n"
	"slices"
	"strings"
	"time"

	"refrail/internal/app"
	"refrail/internal/bundle"
	"refrail/policy"
)

//go:embed lefthook-checksums.txt
var upstreamChecksums string

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Release error:", err)
		os.Exit(1)
	}
}
func run() error {
	version := flag.String("version", "v0.1.0", "Release version")
	targets := flag.String("targets", "darwin/amd64,darwin/arm64,linux/amd64,linux/arm64,windows/amd64,windows/arm64", "Targets (comma-separated)")
	out := flag.String("out", "dist", "Output directory")
	flag.Parse()
	if !bundle.ValidVersion(*version) {
		return i18n.Errorf("Invalid version")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return i18n.Errorf("Run this command at the RefRail repository root")
	}
	output, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(output, *version), 0755); err != nil {
		return err
	}
	checksums := map[string]string{}
	for _, line := range strings.Split(upstreamChecksums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			checksums[strings.TrimPrefix(f[1], "*")] = f[0]
		}
	}
	var archiveSums []string
	for _, target := range strings.Split(*targets, ",") {
		parts := strings.Split(strings.TrimSpace(target), "/")
		if len(parts) != 2 || !slices.Contains([]string{"darwin", "linux", "windows"}, parts[0]) || !slices.Contains([]string{"amd64", "arm64"}, parts[1]) {
			return i18n.Errorf("Unsupported target: %s", target)
		}
		goos, goarch := parts[0], parts[1]
		fmt.Println("Bundle:", goos, goarch)
		temp, err := os.MkdirTemp(filepath.Join(output, *version), ".build-")
		if err != nil {
			return err
		}
		if err := build(root, temp, *version, goos, goarch, checksums); err != nil {
			os.RemoveAll(temp)
			return err
		}
		dest := filepath.Join(output, *version, goos+"-"+goarch)
		if _, err := os.Stat(dest); err == nil {
			if _, err := bundle.Verify(dest, false); err != nil {
				os.RemoveAll(temp)
				return i18n.Errorf("Existing output is not a valid bundle; not overwritten: %s", dest)
			}
			if err := os.RemoveAll(dest); err != nil {
				os.RemoveAll(temp)
				return err
			}
		}
		if err := os.Rename(temp, dest); err != nil {
			os.RemoveAll(temp)
			return err
		}
		name := "refrail_" + *version + "_" + goos + "_" + goarch + ".zip"
		archive := filepath.Join(output, *version, name)
		if err := archiveBundle(dest, archive); err != nil {
			return err
		}
		data, err := os.ReadFile(archive)
		if err != nil {
			return err
		}
		archiveSums = append(archiveSums, bundle.Digest(data)+"  "+name)
	}
	slices.Sort(archiveSums)
	// Preserve sums for previously built targets when doing a native-only rebuild.
	sumsPath := filepath.Join(output, *version, "SHA256SUMS")
	existing, _ := os.ReadFile(sumsPath)
	byName := map[string]string{}
	for _, line := range append(strings.Split(string(existing), "\n"), archiveSums...) {
		f := strings.Fields(line)
		if len(f) == 2 {
			byName[f[1]] = line
		}
	}
	archiveSums = nil
	for _, line := range byName {
		archiveSums = append(archiveSums, line)
	}
	slices.Sort(archiveSums)
	return os.WriteFile(sumsPath, []byte(strings.Join(archiveSums, "\n")+"\n"), 0644)
}

func upstreamName(goos, goarch string) string {
	osName := map[string]string{"darwin": "MacOS", "linux": "Linux", "windows": "Windows"}[goos]
	arch := map[string]string{"amd64": "x86_64", "arm64": "arm64"}[goarch]
	name := "lefthook_" + app.LefthookVersion + "_" + osName + "_" + arch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func download(root, name, checksum string) ([]byte, error) {
	if len(checksum) != 64 {
		return nil, i18n.Errorf("Pinned upstream checksum is missing: %s", name)
	}
	dir := filepath.Join(root, ".cache", "lefthook", app.LefthookVersion)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, name)
	if data, err := os.ReadFile(p); err == nil && bundle.Digest(data) == checksum {
		return data, nil
	}
	url := "https://github.com/evilmartians/lefthook/releases/download/v" + app.LefthookVersion + "/" + name
	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, i18n.Errorf("Lefthook download returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 128*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 128*1024*1024 || bundle.Digest(data) != checksum {
		return nil, i18n.Errorf("Upstream SHA-256 verification failed: %s", name)
	}
	if err := os.WriteFile(p, data, 0644); err != nil {
		return nil, err
	}
	return data, nil
}

func write(root, name string, data []byte, mode os.FileMode) error {
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	return os.WriteFile(p, data, mode)
}

func build(root, dest, version, goos, goarch string, checksums map[string]string) error {
	if err := os.MkdirAll(filepath.Join(dest, "bin"), 0755); err != nil {
		return err
	}
	cli := filepath.Join(dest, "bin", bundle.Executable("refrail", goos))
	cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -X refrail/internal/app.Version="+version, "-o", cli, "./cmd/refrail")
	cmd.Dir = root
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key != "GOOS" && key != "GOARCH" && key != "CGO_ENABLED" {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	name := upstreamName(goos, goarch)
	data, err := download(root, name, checksums[name])
	if err != nil {
		return err
	}
	lefthook := filepath.Join(dest, "bin", bundle.Executable("lefthook", goos))
	if err := write(dest, "bin/"+bundle.Executable("lefthook", goos), data, 0755); err != nil {
		return err
	}
	info, err := buildinfo.ReadFile(lefthook)
	if err != nil {
		return i18n.Errorf("Lefthook build information: %w", err)
	}
	if err := write(dest, "licenses/lefthook-build-info.txt", []byte(info.String()), 0644); err != nil {
		return err
	}
	for _, file := range []string{"LICENSE", "THIRD_PARTY_NOTICES.md", "licenses/lefthook.txt", "licenses/go.txt", "README.md", "docs/tr.md", "CONTRIBUTING.md", "examples/refrail.strict.json"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			return err
		}
		if err := write(dest, file, data, 0644); err != nil {
			return err
		}
	}
	for source, target := range map[string]string{"policy/default.json": "policy/default.json", "assets/lefthook.yml": "lefthook.yml"} {
		data, err := os.ReadFile(filepath.Join(root, source))
		if err != nil {
			return err
		}
		if err := write(dest, target, data, 0644); err != nil {
			return err
		}
	}
	base, err := os.ReadFile(filepath.Join(dest, "policy", "default.json"))
	if err != nil {
		return err
	}
	c, err := policy.Load(base)
	if err != nil {
		return err
	}
	m := bundle.Manifest{SchemaVersion: 1, Version: version, PolicyVersion: c.Version, LefthookVersion: app.LefthookVersion, GOOS: goos, GOARCH: goarch}
	err = filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dest, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		mode := uint32(0644)
		if st.Mode().Perm()&0111 != 0 || strings.HasPrefix(filepath.ToSlash(rel), "bin/") {
			mode = 0755
		}
		m.Files = append(m.Files, bundle.File{Path: filepath.ToSlash(rel), SHA256: bundle.Digest(b), Mode: mode})
		return nil
	})
	if err != nil {
		return err
	}
	slices.SortFunc(m.Files, func(a, b bundle.File) int { return strings.Compare(a.Path, b.Path) })
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := write(dest, bundle.ManifestName, append(raw, '\n'), 0644); err != nil {
		return err
	}
	_, err = bundle.Verify(dest, false)
	return err
}

func archiveBundle(dir, filename string) error {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		header := &zip.FileHeader{Name: filepath.ToSlash(rel), Method: zip.Deflate}
		header.SetMode(st.Mode())
		header.SetModTime(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
		w, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	})
	if err != nil {
		archive.Close()
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}
	return os.WriteFile(filename, buffer.Bytes(), 0644)
}
