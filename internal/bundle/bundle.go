package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"refrail/internal/i18n"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"refrail/policy"
)

const ManifestName = "manifest.json"

type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}
type Manifest struct {
	SchemaVersion   int    `json:"schemaVersion"`
	Version         string `json:"version"`
	PolicyVersion   string `json:"policyVersion"`
	LefthookVersion string `json:"lefthookVersion"`
	GOOS            string `json:"goos"`
	GOARCH          string `json:"goarch"`
	Files           []File `json:"files"`
}
type Verified struct {
	Root, Digest string
	Manifest     Manifest
	Policy       policy.Config
}

var versionRE = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$`)

func ValidVersion(v string) bool { return versionRE.MatchString(v) }
func Executable(name, goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}
func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func SafePath(p string) bool {
	return p != "." && p != "" && !strings.ContainsAny(p, "\\:\x00") && !strings.HasPrefix(p, "/") && path.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../")
}
func regularFile(root, relative string) ([]byte, error) {
	parts := strings.Split(relative, "/")
	p := root
	for i, part := range parts {
		p = filepath.Join(p, part)
		st, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return nil, i18n.Errorf("Bundle must not contain symbolic links: %s", relative)
		}
		if i == len(parts)-1 && !st.Mode().IsRegular() {
			return nil, i18n.Errorf("Bundle entry must be a regular file: %s", relative)
		}
	}
	return os.ReadFile(p)
}

func Verify(dir string, platform bool) (*Verified, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	raw, err := regularFile(root, ManifestName)
	if err != nil {
		return nil, i18n.Errorf("Cannot read bundle manifest: %w", err)
	}
	var m Manifest
	if err := policy.Decode(raw, &m); err != nil {
		return nil, err
	}
	if m.SchemaVersion != 1 || !ValidVersion(m.Version) || !ValidVersion(m.PolicyVersion) || !ValidVersion(m.LefthookVersion) {
		return nil, i18n.Errorf("Invalid bundle version information")
	}
	if !slices.Contains([]string{"windows", "darwin", "linux"}, m.GOOS) || !slices.Contains([]string{"amd64", "arm64"}, m.GOARCH) {
		return nil, i18n.Errorf("Unsupported bundle platform")
	}
	if platform && (m.GOOS != runtime.GOOS || m.GOARCH != runtime.GOARCH) {
		return nil, i18n.Errorf("Bundle targets %s/%s; this machine is %s/%s", m.GOOS, m.GOARCH, runtime.GOOS, runtime.GOARCH)
	}
	seen := map[string]bool{ManifestName: true}
	for _, file := range m.Files {
		if !SafePath(file.Path) || seen[file.Path] || (file.Mode != 0644 && file.Mode != 0755) {
			return nil, i18n.Errorf("Invalid or duplicate bundle file: %s", file.Path)
		}
		seen[file.Path] = true
		b, err := regularFile(root, file.Path)
		if err != nil {
			return nil, i18n.Errorf("Missing or invalid bundle file %s: %w", file.Path, err)
		}
		if Digest(b) != file.SHA256 {
			return nil, i18n.Errorf("SHA-256 verification failed: %s", file.Path)
		}
		if strings.HasPrefix(file.Path, "bin/") {
			if file.Mode != 0755 {
				return nil, i18n.Errorf("Binary manifest mode must be executable: %s", file.Path)
			}
			if platform && runtime.GOOS != "windows" {
				st, err := os.Stat(filepath.Join(root, filepath.FromSlash(file.Path)))
				if err != nil {
					return nil, err
				}
				if st.Mode().Perm()&0111 == 0 {
					return nil, i18n.Errorf("Binary is not executable: %s", file.Path)
				}
			}
		}
	}
	for _, name := range []string{"bin/" + Executable("refrail", m.GOOS), "bin/" + Executable("lefthook", m.GOOS), "policy/default.json", "lefthook.yml", "LICENSE", "THIRD_PARTY_NOTICES.md", "licenses/lefthook.txt", "licenses/go.txt"} {
		if !seen[name] {
			return nil, i18n.Errorf("Required bundle file is missing: %s", name)
		}
	}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if !seen[filepath.ToSlash(rel)] {
			return i18n.Errorf("Bundle file is not listed in the manifest: %s", rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, "policy", "default.json"))
	if err != nil {
		return nil, err
	}
	c, err := policy.Load(data)
	if err != nil {
		return nil, err
	}
	if c.Version != m.PolicyVersion {
		return nil, i18n.Errorf("Manifest and policy versions do not match")
	}
	return &Verified{Root: root, Digest: Digest(raw), Manifest: m, Policy: c}, nil
}

// CopyVerified copies only manifest-listed files into a new, private directory.
func CopyVerified(v *Verified, dest string) error {
	if err := os.Mkdir(dest, 0700); err != nil {
		return err
	}
	for _, f := range append(slices.Clone(v.Manifest.Files), File{Path: ManifestName, Mode: 0644}) {
		b, err := regularFile(v.Root, f.Path)
		if err != nil {
			return err
		}
		if f.Path != ManifestName && Digest(b) != f.SHA256 {
			return i18n.Errorf("Bundle changed while copying: %s", f.Path)
		}
		if f.Path == ManifestName && Digest(b) != v.Digest {
			return i18n.Errorf("Manifest changed while copying")
		}
		p := filepath.Join(dest, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, fs.FileMode(f.Mode)); err != nil {
			return err
		}
	}
	_, err := Verify(dest, true)
	return err
}
