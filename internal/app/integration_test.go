package app

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"refrail/internal/bundle"
	"refrail/internal/gitrepo"
	"refrail/internal/install"
)

type testRepo struct {
	t                            *testing.T
	dir, remote, cli, packageDir string
}

func projectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func testBundle(t *testing.T, version string) string {
	t.Helper()
	lefthook := os.Getenv("REFRAIL_TEST_LEFTHOOK")
	if lefthook == "" {
		lefthook, _ = exec.LookPath("lefthook")
	}
	if lefthook == "" {
		candidate := filepath.Join(projectRoot(t), "dist", Version, runtime.GOOS+"-"+runtime.GOARCH, "bin", bundle.Executable("lefthook", runtime.GOOS))
		if _, err := os.Stat(candidate); err == nil {
			lefthook = candidate
		}
	}
	if lefthook == "" {
		t.Skip("Build a native bundle or set REFRAIL_TEST_LEFTHOOK for real-hook tests")
	}
	out, err := exec.Command(lefthook, "version").Output()
	if err != nil || strings.TrimSpace(string(out)) != LefthookVersion {
		t.Skip("Lefthook 2.1.17 required for integration tests")
	}
	root := projectRoot(t)
	dir := filepath.Join(t.TempDir(), "bundle")
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(dir, "bin", bundle.Executable("refrail", runtime.GOOS))
	cmd := exec.Command("go", "build", "-buildvcs=false", "-ldflags", "-X refrail/internal/app.Version="+version, "-o", cli, "./cmd/refrail")
	cmd.Dir = root
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, data)
	}
	files := map[string]string{"bin/" + bundle.Executable("lefthook", runtime.GOOS): lefthook, "policy/default.json": filepath.Join(root, "policy", "default.json"), "lefthook.yml": filepath.Join(root, "assets", "lefthook.yml"), "THIRD_PARTY_NOTICES.md": filepath.Join(root, "THIRD_PARTY_NOTICES.md"), "licenses/go.txt": filepath.Join(root, "licenses", "go.txt"), "licenses/lefthook.txt": filepath.Join(root, "licenses", "lefthook.txt")}
	files["LICENSE"] = filepath.Join(root, "LICENSE")
	for name, source := range files {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0644)
		if strings.HasPrefix(name, "bin/") {
			mode = 0755
		}
		if err := os.WriteFile(p, data, mode); err != nil {
			t.Fatal(err)
		}
	}
	seal(t, dir, version)
	return dir
}

func seal(t *testing.T, dir, version string) {
	t.Helper()
	m := bundle.Manifest{SchemaVersion: 1, Version: version, PolicyVersion: "1.0.0", LefthookVersion: LefthookVersion, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() == bundle.ManifestName {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		mode := uint32(0644)
		if strings.HasPrefix(filepath.ToSlash(relative), "bin/") {
			mode = 0755
		}
		m.Files = append(m.Files, bundle.File{Path: filepath.ToSlash(relative), SHA256: bundle.Digest(data), Mode: mode})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, bundle.ManifestName), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("REFRAIL_LANG", "en")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "LEFTHOOK", "LEFTHOOK_EXCLUDE", "LEFTHOOK_CONFIG", "LEFTHOOK_CONFIGS", "REFRAIL_RESULT"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("REFRAIL_HOME", filepath.Join(t.TempDir(), "tools ' dollar $"))
}
func newRepo(t *testing.T, pkg string, legacy bool) *testRepo {
	t.Helper()
	root := t.TempDir()
	r := &testRepo{t: t, dir: filepath.Join(root, "working repo ' dollar $"), remote: filepath.Join(root, "remote.git"), packageDir: pkg, cli: filepath.Join(pkg, "bin", bundle.Executable("refrail", runtime.GOOS))}
	cmd := exec.Command("git", "-c", "init.templateDir=", "init", "--initial-branch=main", r.dir)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init %v %s", err, b)
	}
	cmd = exec.Command("git", "-c", "init.templateDir=", "init", "--bare", "--initial-branch=main", r.remote)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bare %v %s", err, b)
	}
	for _, args := range [][]string{{"config", "user.name", "Integration Test"}, {"config", "user.email", "integration@example.invalid"}, {"config", "commit.gpgsign", "false"}, {"remote", "add", "origin", r.remote}} {
		r.git(true, args...)
	}
	if legacy {
		r.git(true, "commit", "--allow-empty", "-m", "Legacy invalid baseline")
	}
	return r
}
func (r *testRepo) git(ok bool, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	b, err := cmd.CombinedOutput()
	if (err == nil) != ok {
		r.t.Fatalf("git %v success=%v: %v\n%s", args, ok, err, b)
	}
	return string(b)
}
func (r *testRepo) command(code int, args ...string) string {
	r.t.Helper()
	var stdout, stderr bytes.Buffer
	args = append([]string{args[0], "--repo", r.dir}, args[1:]...)
	got := Run(args, strings.NewReader(""), &stdout, &stderr)
	if got != code {
		r.t.Fatalf("CLI %v code=%d want=%d\n%s\n%s", args, got, code, &stdout, &stderr)
	}
	return stdout.String() + stderr.String()
}

func TestInstalledLifecycleAndPush(t *testing.T) {
	isolate(t)
	pkg := testBundle(t, "v0.1.0")
	r := newRepo(t, pkg, true)
	baseline := strings.TrimSpace(r.git(true, "rev-parse", "HEAD"))
	r.command(0, "install", "--bundle", pkg)
	r.command(0, "install", "--bundle", pkg)
	r.command(0, "doctor")
	if status := r.git(true, "status", "--porcelain"); status != "" {
		t.Fatalf("install dirtied tree: %s", status)
	}
	if anchor := strings.TrimSpace(r.git(true, "rev-parse", "refs/refrail/baseline")); anchor != baseline {
		t.Fatal("baseline anchor differs")
	}
	r.git(true, "push", "origin", "main")
	r.git(false, "commit", "--allow-empty", "-m", "Merge branch 'fake'")
	r.git(false, "commit", "--allow-empty", "-m", "invalid message")
	r.git(true, "switch", "-c", "feat/1234-add-login")
	r.git(true, "commit", "--allow-empty", "-m", "feat(auth): giriş ekle")
	r.git(false, "push", "origin", "HEAD:BadBranch")
	r.git(false, "push", "origin", "HEAD:feat/good-target", "HEAD:bad-target")
	r.git(false, "show-ref", "--verify", "refs/remotes/origin/feat/good-target")
	r.git(true, "push", "-u", "origin", "HEAD")
	r.git(true, "-c", "core.hooksPath="+os.DevNull, "commit", "--allow-empty", "-m", "invalid bypass")
	if output := r.git(false, "push", "origin", "HEAD"); !strings.Contains(output, "commit-format") {
		t.Fatalf("wrong rejection: %s", output)
	}
	r.git(true, "commit", "--amend", "--allow-empty", "-m", "fix(auth): girişi düzelt")
	r.git(true, "push", "origin", "HEAD")
	r.git(true, "reset", "--hard", "HEAD~1")
	r.git(true, "commit", "--allow-empty", "-m", "fix: replace commit")
	r.git(true, "push", "--force", "origin", "HEAD")
	r.git(true, "tag", "v1.0.0")
	r.git(true, "push", "origin", "v1.0.0")
	r.git(true, "push", "origin", "--delete", "feat/1234-add-login")
	worktree := filepath.Join(t.TempDir(), "linked worktree")
	r.git(true, "worktree", "add", "-b", "feat/worktree", worktree)
	linked := *r
	linked.dir = worktree
	linked.command(0, "doctor")
	linked.git(true, "commit", "--allow-empty", "-m", "feat: linked worktree")
	message := filepath.Join(t.TempDir(), "message.txt")
	if err := os.WriteFile(message, []byte("fix: CRLF temizle\r\n\r\n# comment\r\nbody\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	r.command(0, "commit-msg", "--file", message)
	if err := os.WriteFile(filepath.Join(r.dir, ".refrail.json"), []byte(`{"requireScope":true,"allowedScopes":["auth"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	r.git(false, "commit", "--allow-empty", "-m", "feat: no scope")
	r.git(false, "commit", "--allow-empty", "-m", "feat(api): wrong scope")
	r.git(true, "commit", "--allow-empty", "-m", "feat(auth): proper scope")
	os.WriteFile(filepath.Join(r.dir, ".refrail.json"), []byte(`{"maxHeaderLength":101}`), 0644)
	r.command(2, "commit-msg", "--file", message)
	os.Remove(filepath.Join(r.dir, ".refrail.json"))
	r.command(0, "uninstall")
	r.git(false, "show-ref", "--verify", "refs/refrail/baseline")
	r.git(true, "commit", "--allow-empty", "-m", "uninstalled invalid allowed")
}

func TestUpdateRollbackAndCorruption(t *testing.T) {
	isolate(t)
	pkg := testBundle(t, "v0.1.0")
	next := testBundle(t, "v0.2.0")
	r := newRepo(t, pkg, true)
	r.command(0, "install", "--bundle", pkg)
	repo, err := gitrepo.Open(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	before, err := install.ReadState(repo)
	if err != nil {
		t.Fatal(err)
	}
	hooks, _ := repo.Path("hooks")
	wrapper, _ := os.ReadFile(filepath.Join(hooks, "commit-msg"))
	r.git(true, "commit", "--allow-empty", "-m", "feat: before update")
	r.command(0, "update", "--bundle", next)
	after, err := install.ReadState(repo)
	if err != nil {
		t.Fatal(err)
	}
	if before.Baseline != after.Baseline || before.BootstrapExecutable != after.BootstrapExecutable {
		t.Fatal("update changed baseline/bootstrap")
	}
	newWrapper, _ := os.ReadFile(filepath.Join(hooks, "commit-msg"))
	if !bytes.Equal(wrapper, newWrapper) {
		t.Fatal("update rewrote hooks")
	}
	r.git(true, "commit", "--allow-empty", "-m", "feat: after update")
	if output := r.command(0, "doctor"); !strings.Contains(output, "v0.2.0") {
		t.Fatal(output)
	}
	r.command(0, "update", "--bundle", pkg)
	r.git(true, "commit", "--allow-empty", "-m", "fix: rollback works")
	if err := os.WriteFile(filepath.Join(next, "policy", "default.json"), []byte("corrupt"), 0644); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := os.ReadFile(filepath.Join(install.StateDir(repo), "state.json"))
	r.command(2, "update", "--bundle", next)
	current, _ := os.ReadFile(filepath.Join(install.StateDir(repo), "state.json"))
	if !bytes.Equal(snapshot, current) {
		t.Fatal("failed update changed state")
	}
	active, err := install.ReadState(repo)
	if err != nil {
		t.Fatal(err)
	}
	lefthook := filepath.Join(active.BundlePath, "bin", bundle.Executable("lefthook", runtime.GOOS))
	if err := os.Rename(lefthook, lefthook+".missing"); err != nil {
		t.Fatal(err)
	}
	r.command(2, "doctor")
	r.git(false, "commit", "--allow-empty", "-m", "feat: must fail closed")
	if err := os.Rename(lefthook+".missing", lefthook); err != nil {
		t.Fatal(err)
	}
	r.command(0, "doctor")
	r.command(0, "uninstall")
}

func TestUnbornMergeAndShallowHistory(t *testing.T) {
	isolate(t)
	pkg := testBundle(t, "v0.1.0")
	r := newRepo(t, pkg, false)
	r.command(0, "install", "--bundle", pkg)
	r.git(false, "commit", "--allow-empty", "-m", "invalid root")
	r.git(true, "commit", "--allow-empty", "-m", "feat: initial commit")
	r.git(true, "push", "origin", "main")
	r.git(true, "switch", "-c", "feat/source")
	r.git(true, "commit", "--allow-empty", "-m", "feat: source")
	r.git(true, "switch", "main")
	r.git(true, "commit", "--allow-empty", "-m", "feat: target")
	r.git(true, "merge", "--no-ff", "--no-edit", "feat/source")
	r.git(true, "push", "origin", "main")
	r.git(false, "commit", "--allow-empty", "-m", "Merge branch 'fake'")
	clone := filepath.Join(t.TempDir(), "shallow")
	remoteURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(r.remote)}).String()
	cmd := exec.Command("git", "-c", "init.templateDir=", "clone", "--depth=1", remoteURL, clone)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone %v: %s", err, output)
	}
	s := *r
	s.dir = clone
	s.command(0, "install", "--bundle", pkg)
	if output := s.command(2, "check", "--from", "HEAD", "--to", "HEAD"); !strings.Contains(output, "unshallow") {
		t.Fatal(output)
	}
}

func TestInstallConflictsDoNotOverwrite(t *testing.T) {
	isolate(t)
	pkg := testBundle(t, "v0.1.0")
	for _, kind := range []string{"existing hook", "hooksPath", "empty hooksPath", "lock"} {
		t.Run(kind, func(t *testing.T) {
			r := newRepo(t, pkg, true)
			r.t = t
			switch kind {
			case "existing hook":
				p := filepath.Join(r.dir, ".git", "hooks", "pre-commit")
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
					t.Fatal(err)
				}
			case "hooksPath":
				r.git(true, "config", "core.hooksPath", "somewhere")
			case "empty hooksPath":
				r.git(true, "config", "core.hooksPath", "")
			case "lock":
				if err := os.Mkdir(filepath.Join(r.dir, ".git", "refrail.lock"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			r.command(2, "install", "--bundle", pkg)
			if _, err := os.Stat(filepath.Join(r.dir, ".git", "refrail", "state.json")); !os.IsNotExist(err) {
				t.Fatal("failed install changed state")
			}
			if kind == "existing hook" {
				data, _ := os.ReadFile(filepath.Join(r.dir, ".git", "hooks", "pre-commit"))
				if string(data) != "#!/bin/sh\nexit 0\n" {
					t.Fatal("overwrote existing hook")
				}
			}
		})
	}
}

func TestCommitCleanupCannotHideInvalidMessages(t *testing.T) {
	isolate(t)
	pkg := testBundle(t, "v0.1.0")
	r := newRepo(t, pkg, true)
	r.command(0, "install", "--bundle", pkg)
	// With -m Git defaults to whitespace cleanup and retains comment lines.
	// Removing this header would validate a different message than Git stores.
	r.git(false, "commit", "--allow-empty", "-m", "# retained header\nfix: description")
	r.git(true, "config", "commit.cleanup", "strip")
	r.git(false, "commit", "--allow-empty", "--cleanup=whitespace", "-m", "# retained override header\nfix: description")
	r.git(false, "commit", "--allow-empty", "-m", "# stripped header\nfix: description")
	r.git(false, "commit", "--allow-empty", "--cleanup=verbatim", "-m", "\nfix: hidden leading blank")
	for _, mode := range []string{"strip", "whitespace", "verbatim", "scissors"} {
		r.git(true, "commit", "--allow-empty", "--cleanup="+mode, "-m", "fix: valid first line\n\n# comment in body")
	}
	r.git(true, "config", "commit.cleanup", "whitespace")
	r.git(false, "commit", "--allow-empty", "-m", "# retained header\nfix: description")
}

func TestUninstallFailurePreservesInstallation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permission fault injection")
	}
	isolate(t)
	pkg := testBundle(t, "v0.1.0")
	r := newRepo(t, pkg, true)
	r.command(0, "install", "--bundle", pkg)
	repo, err := gitrepo.Open(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := repo.Path("hooks")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(hooks, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(hooks, 0755) })
	r.command(2, "uninstall")
	if err := os.Chmod(hooks, 0755); err != nil {
		t.Fatal(err)
	}
	r.command(0, "doctor")
	r.git(true, "commit", "--allow-empty", "-m", "fix: installation survived")
	// A late Git-ref failure must also restore hooks and state after staging.
	refLock := filepath.Join(repo.CommonDir, "refs", "refrail", "baseline.lock")
	if err := os.WriteFile(refLock, []byte("fault injection"), 0600); err != nil {
		t.Fatal(err)
	}
	r.command(2, "uninstall")
	if err := os.Remove(refLock); err != nil {
		t.Fatal(err)
	}
	r.command(0, "doctor")
	unknown := filepath.Join(install.StateDir(repo), "user-note.txt")
	if err := os.WriteFile(unknown, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	r.command(2, "uninstall")
	r.command(0, "doctor")
	if data, err := os.ReadFile(unknown); err != nil || string(data) != "preserve" {
		t.Fatal("unexpected metadata was removed")
	}
	if err := os.Remove(unknown); err != nil {
		t.Fatal(err)
	}
	r.command(0, "uninstall")
}

func TestLocalizedCLIAndHooks(t *testing.T) {
	isolate(t)
	pkg := testBundle(t, "v0.1.0")
	r := newRepo(t, pkg, true)
	r.command(0, "install", "--bundle", pkg)
	for _, tc := range []struct{ language, commands, errorLabel, format string }{{"en", "Commands:", "Error:", "Expected type(scope)"}, {"tr", "Komutlar:", "Hata:", "beklenen biçim type(scope)"}} {
		t.Run(tc.language, func(t *testing.T) {
			t.Setenv("REFRAIL_LANG", tc.language)
			var out, stderr bytes.Buffer
			if code := Run([]string{"help"}, strings.NewReader(""), &out, &stderr); code != 0 || !strings.Contains(out.String(), tc.commands) {
				t.Fatalf("help: %d %s %s", code, &out, &stderr)
			}
			out.Reset()
			stderr.Reset()
			if code := Run([]string{"not-a-command"}, strings.NewReader(""), &out, &stderr); code != 2 || !strings.Contains(stderr.String(), tc.errorLabel) {
				t.Fatalf("error: %d %s", code, &stderr)
			}
			output := r.git(false, "commit", "--allow-empty", "-m", "invalid message")
			if !strings.Contains(output, tc.format) || !strings.Contains(output, tc.errorLabel) {
				t.Fatalf("hook lost language %s: %s", tc.language, output)
			}
			if output := r.command(0, "doctor"); !strings.Contains(output, "Lefthook") {
				t.Fatal(output)
			}
		})
	}
}
