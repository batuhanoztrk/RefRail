package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestWindowsRuntimePathsAcrossGitEntryPoints(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Git installation")
	systemRoot := filepath.Join(t.TempDir(), "Windows")
	for _, toolchain := range []string{"mingw64", "mingw32", "clangarm64"} {
		for _, entry := range []string{"cmd", "bin", filepath.Join(toolchain, "bin")} {
			t.Run(toolchain+"/"+entry, func(t *testing.T) {
				execPath := filepath.Join(root, toolchain, "libexec", "git-core")
				paths := windowsRuntimePaths(filepath.Join(root, entry, "git.exe"), execPath, systemRoot)
				for _, required := range []string{
					execPath,
					filepath.Join(root, toolchain, "bin"),
					filepath.Join(root, "bin"),
					filepath.Join(root, "usr", "bin"),
					filepath.Join(systemRoot, "System32"),
				} {
					if !slices.Contains(paths, required) {
						t.Errorf("missing runtime directory %s in %v", required, paths)
					}
				}
				seen := map[string]bool{}
				for _, path := range paths {
					key := strings.ToLower(filepath.Clean(path))
					if seen[key] {
						t.Errorf("duplicate runtime directory %s", path)
					}
					seen[key] = true
					if !filepath.IsAbs(path) {
						t.Errorf("relative runtime directory %s", path)
					}
				}
				if slices.Contains(paths, filepath.Join(root, toolchain, "usr", "bin")) {
					t.Error("shell directory was derived from the toolchain instead of the Git installation")
				}
			})
		}
	}
}
