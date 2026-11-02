// sandbox creates a disposable local Git playground and exercises installed hooks.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"refrail/internal/i18n"
	"runtime"
	"strings"

	"refrail/internal/bundle"
)

type Step struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output"`
}
type Report struct {
	Platform                string `json:"platform"`
	WithoutLanguageRuntimes bool   `json:"withoutLanguageRuntimes"`
	WithoutSystemLefthook   bool   `json:"withoutSystemLefthook"`
	Steps                   []Step `json:"steps"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Sandbox error:", err)
		os.Exit(1)
	}
}
func run() error {
	packageDir := flag.String("bundle", "", "Extracted native bundle directory")
	out := flag.String("out", "sandbox/demo", "New sandbox output directory")
	flag.Parse()
	if *packageDir == "" {
		return i18n.Errorf("--bundle is required")
	}
	v, err := bundle.Verify(*packageDir, true)
	if err != nil {
		return err
	}
	dir, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		return i18n.Errorf("Output directory already exists or is inaccessible; choose a new --out")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return err
	}
	git, err = filepath.Abs(git)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, ".refrail-sandbox"), []byte("disposable local Git sandbox\n"), 0644); err != nil {
		return err
	}
	work := filepath.Join(dir, "working repo")
	remote := filepath.Join(dir, "remote.git")
	cli := filepath.Join(v.Root, "bin", bundle.Executable("refrail", runtime.GOOS))
	var env []string
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "GIT_") || (strings.HasPrefix(key, "REFRAIL_") && key != "REFRAIL_LANG") {
			continue
		}
		env = append(env, item)
	}
	env = append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "REFRAIL_HOME="+filepath.Join(dir, "tool-home"))
	report := Report{Platform: runtime.GOOS + "/" + runtime.GOARCH}
	runStep := func(name, cwd, executable string, args []string, wantSuccess bool) error {
		cmd := exec.Command(executable, args...)
		cmd.Dir = cwd
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			code = 2
			if e, ok := err.(*exec.ExitError); ok {
				code = e.ExitCode()
			}
		}
		passed := (err == nil) == wantSuccess
		report.Steps = append(report.Steps, Step{name, passed, code, string(output)})
		fmt.Printf("%s: %v\n", name, passed)
		if !passed {
			return i18n.Errorf("%s did not produce the expected result: %s", name, string(output))
		}
		return nil
	}
	defer func() {
		data, _ := json.MarshalIndent(report, "", "  ")
		os.WriteFile(filepath.Join(dir, "report.json"), append(data, '\n'), 0644)
	}()
	for _, s := range []struct {
		name string
		args []string
	}{
		{"bare remote", []string{"-c", "init.templateDir=", "init", "--bare", "--initial-branch=main", remote}},
		{"working repo", []string{"-c", "init.templateDir=", "init", "--initial-branch=main", work}},
	} {
		if err := runStep(s.name, dir, git, s.args, true); err != nil {
			return err
		}
	}
	for _, args := range [][]string{{"config", "user.name", "Policy Sandbox"}, {"config", "user.email", "sandbox@example.invalid"}, {"config", "commit.gpgsign", "false"}, {"remote", "add", "origin", remote}, {"commit", "--allow-empty", "-m", "Legacy baseline"}} {
		if err := runStep("prepare "+args[0], work, git, args, true); err != nil {
			return err
		}
	}
	// Limit PATH before install, not just before running hooks. This proves the
	// installer does not depend on a preinstalled Lefthook or language runtime.
	runtimePath, err := runtimeOnlyPath(dir, git)
	if err != nil {
		return err
	}
	for i, item := range env {
		if strings.EqualFold(strings.SplitN(item, "=", 2)[0], "PATH") {
			env = append(env[:i], env[i+1:]...)
			break
		}
	}
	env = append(env, "PATH="+runtimePath)
	for _, name := range []string{"node", "npm", "go", "rustc", "cargo"} {
		if executableIn(name, runtimePath) {
			return i18n.Errorf("Runtime-only PATH still contains %s", name)
		}
	}
	if executableIn("lefthook", runtimePath) {
		return i18n.Errorf("Runtime-only PATH still contains system Lefthook")
	}
	report.WithoutLanguageRuntimes = true
	report.WithoutSystemLefthook = true
	if err := runStep("install without system Lefthook", dir, cli, []string{"install", "--repo", work, "--bundle", v.Root}, true); err != nil {
		return err
	}
	if err := runStep("idempotent install without system Lefthook", dir, cli, []string{"install", "--repo", work, "--bundle", v.Root}, true); err != nil {
		return err
	}
	if err := runStep("doctor without language runtimes", work, cli, []string{"doctor"}, true); err != nil {
		return err
	}
	for _, s := range []struct {
		name string
		args []string
		ok   bool
	}{
		{"first legacy push", []string{"push", "origin", "main"}, true},
		{"feature branch", []string{"switch", "-c", "feat/1234-add-login"}, true},
		{"reject invalid commit", []string{"commit", "--allow-empty", "-m", "login fixed"}, false},
		{"valid Unicode commit", []string{"commit", "--allow-empty", "-m", "feat(auth): biyometrik giriş ekle"}, true},
		{"reject renamed destination", []string{"push", "origin", "HEAD:BadBranch"}, false},
		{"valid new branch push", []string{"push", "-u", "origin", "HEAD"}, true},
		{"bypass commit hook", []string{"-c", "core.hooksPath=" + os.DevNull, "commit", "--allow-empty", "-m", "bypassed invalid message"}, true},
		{"pre-push catches bypass", []string{"push", "origin", "HEAD"}, false},
		{"amend valid message", []string{"commit", "--amend", "--allow-empty", "-m", "fix(auth): giriş hatasını düzelt"}, true},
		{"push repaired commit", []string{"push", "origin", "HEAD"}, true},
	} {
		if err := runStep(s.name, work, git, s.args, s.ok); err != nil {
			return err
		}
	}
	if err := runStep("final doctor", work, cli, []string{"doctor"}, true); err != nil {
		return err
	}
	fmt.Println("Sandbox repository:", work)
	fmt.Println("Report:", filepath.Join(dir, "report.json"))
	return nil
}

func executableIn(name, pathValue string) bool {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, dir := range filepath.SplitList(pathValue) {
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

func runtimeOnlyPath(dir, git string) (string, error) {
	if runtime.GOOS == "windows" {
		gitDir := filepath.Dir(git)
		root := filepath.Dir(gitDir)
		paths := []string{gitDir, filepath.Join(root, "bin"), filepath.Join(root, "usr", "bin"), filepath.Join(os.Getenv("SystemRoot"), "System32")}
		return strings.Join(paths, string(os.PathListSeparator)), nil
	}
	p := filepath.Join(dir, "runtime-bin")
	if err := os.Mkdir(p, 0755); err != nil {
		return "", err
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		return "", err
	}
	for name, target := range map[string]string{"git": git, "sh": sh} {
		if err := os.Symlink(target, filepath.Join(p, name)); err != nil {
			return "", err
		}
	}
	return p, nil
}
