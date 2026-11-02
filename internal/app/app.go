package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"refrail/internal/i18n"
	"runtime"
	"strings"

	"refrail/internal/bundle"
	"refrail/internal/gitrepo"
	"refrail/internal/install"
	"refrail/policy"
)

var Version = "v0.1.0"

const LefthookVersion = "2.1.17"

type VersionInfo struct {
	Version         string `json:"version"`
	PolicyVersion   string `json:"policyVersion"`
	LefthookVersion string `json:"lefthookVersion"`
	GOOS            string `json:"goos"`
	GOARCH          string `json:"goarch"`
}

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	language := i18n.Detect()
	err := run(args, stdin, stdout, stderr, language)
	if err == nil {
		return 0
	}
	fmt.Fprintln(stderr, i18n.Text(language, "Error:"), i18n.Render(err, language))
	var violation *policy.Violation
	if errors.As(err, &violation) {
		return 1
	}
	var child *childError
	if errors.As(err, &child) {
		return child.code
	}
	return 2
}

type childError struct{ code int }

func (e *childError) Error() string { return "Hook validation failed" }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, language i18n.Language) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, i18n.Text(language, "RefRail\nCommands: install, update, uninstall, doctor, commit-msg, pre-push, check, version\nCommand help: refrail <command> --help"))
		return nil
	}
	if args[0] == "hook-run" {
		return hookRun(args[1:], stdin, stdout, stderr)
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, i18n.Text(language, "Usage: refrail %s [options]", command))
		flags.PrintDefaults()
	}
	repoPath := flags.String("repo", ".", i18n.Text(language, "Git working repository directory"))
	var packageDir, file, remote, from, to string
	var jsonOutput bool
	switch command {
	case "install", "update":
		flags.StringVar(&packageDir, "bundle", "", i18n.Text(language, "Extracted distribution bundle directory (defaults to this executable's bundle)"))
	case "uninstall", "doctor":
	case "commit-msg":
		flags.StringVar(&file, "file", os.Getenv("REFRAIL_MESSAGE_FILE"), i18n.Text(language, "Commit message file"))
	case "pre-push":
		flags.StringVar(&remote, "remote", os.Getenv("REFRAIL_REMOTE"), i18n.Text(language, "Destination remote; reference updates are read from stdin"))
	case "check":
		flags.StringVar(&from, "from", "", i18n.Text(language, "Starting commit to exclude"))
		flags.StringVar(&to, "to", "", i18n.Text(language, "Ending commit"))
	case "version":
		flags.BoolVar(&jsonOutput, "json", false, i18n.Text(language, "JSON output"))
	default:
		return i18n.Errorf("Unknown command: %s", command)
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return i18n.Errorf("Unexpected positional argument")
	}
	if command == "version" {
		c, err := policy.Load(policy.DefaultJSON)
		if err != nil {
			return err
		}
		info := VersionInfo{Version, c.Version, LefthookVersion, runtime.GOOS, runtime.GOARCH}
		if root, err := executableBundle(); err == nil {
			if _, err := os.Stat(filepath.Join(root, bundle.ManifestName)); err == nil {
				v, err := bundle.Verify(root, true)
				if err != nil {
					return err
				}
				if v.Manifest.Version != Version {
					return i18n.Errorf("CLI and manifest versions do not match")
				}
				info.PolicyVersion = v.Manifest.PolicyVersion
				info.LefthookVersion = v.Manifest.LefthookVersion
			}
		}
		if jsonOutput {
			return json.NewEncoder(stdout).Encode(info)
		}
		fmt.Fprint(stdout, i18n.Text(language, "refrail %s | policy %s | Lefthook %s | %s/%s\n", info.Version, info.PolicyVersion, info.LefthookVersion, info.GOOS, info.GOARCH))
		return nil
	}
	r, err := gitrepo.Open(*repoPath)
	if err != nil {
		return err
	}
	switch command {
	case "install", "update":
		if packageDir == "" {
			packageDir, err = executableBundle()
			if err != nil {
				return i18n.Errorf("Cannot locate this executable's bundle; pass --bundle explicitly: %w", err)
			}
		}
		if err := install.Install(r, packageDir, command == "update"); err != nil {
			return err
		}
		fmt.Fprintln(stdout, i18n.Text(language, "Lefthook is provided by the bundle; no separate Lefthook installation is needed."))
		fmt.Fprintln(stdout, i18n.Text(language, "Repository enabled. Checks run offline; worktrees of the same clone share this installation."))
		return nil
	case "uninstall":
		if err := install.Uninstall(r); err != nil {
			return err
		}
		fmt.Fprintln(stdout, i18n.Text(language, "Managed hooks and repository state removed; shared bundles were preserved."))
		return nil
	case "doctor":
		if err := install.Doctor(r); err != nil {
			return err
		}
		s, v, err := install.Active(r)
		if err != nil {
			return err
		}
		out, err := exec.Command(filepath.Join(v.Root, "bin", bundle.Executable("lefthook", runtime.GOOS)), "version").Output()
		if err != nil {
			return i18n.Errorf("Cannot execute Lefthook: %w", err)
		}
		if strings.TrimSpace(string(out)) != v.Manifest.LefthookVersion {
			return i18n.Errorf("Lefthook executable and manifest versions do not match")
		}
		fmt.Fprint(stdout, i18n.Text(language, "OK: %s | policy %s | Lefthook %s | %s/%s\nBaseline: %s\n", v.Manifest.Version, v.Manifest.PolicyVersion, v.Manifest.LefthookVersion, v.Manifest.GOOS, v.Manifest.GOARCH, s.Baseline))
		return nil
	}
	c, baseline, err := effectivePolicy(r)
	if err != nil {
		return err
	}
	switch command {
	case "commit-msg":
		return validateMessage(r, c, file)
	case "pre-push":
		return validatePush(r, c, baseline, remote, stdin)
	case "check":
		return checkRange(r, c, from, to)
	}
	return nil
}

// Package managers expose the CLI through a link outside the bundle. Resolve
// that link before looking for the manifest alongside the real bin directory.
func executableBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return filepath.Dir(filepath.Dir(exe)), nil
}

func effectivePolicy(r *gitrepo.Repo) (policy.Config, string, error) {
	var c policy.Config
	var baseline string
	if _, err := os.Lstat(install.StateDir(r)); err == nil {
		s, v, err := install.Active(r)
		if err != nil {
			return c, "", err
		}
		c, baseline = v.Policy, s.Baseline
	} else if os.IsNotExist(err) {
		var err error
		c, err = policy.Load(policy.DefaultJSON)
		if err != nil {
			return c, "", err
		}
	} else {
		return c, "", err
	}
	c, err := install.LoadPolicy(r, c)
	return c, baseline, err
}

func child(cmd *exec.Cmd, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		var e *exec.ExitError
		if errors.As(err, &e) {
			code := e.ExitCode()
			if code != 1 && code != 2 {
				code = 2
			}
			return &childError{code}
		}
		return i18n.Errorf("Cannot execute hook binary: %w", err)
	}
	return nil
}

func hookRun(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) < 1 || (args[0] != "commit-msg" && args[0] != "pre-push") {
		return i18n.Errorf("Unsupported hook")
	}
	hook := args[0]
	if hook == "commit-msg" && len(args) != 2 || hook == "pre-push" && len(args) != 3 {
		return i18n.Errorf("Missing Git hook arguments")
	}
	r, err := gitrepo.Open(".")
	if err != nil {
		return err
	}
	s, v, err := install.Active(r)
	if err != nil {
		return err
	}
	current, err := os.Executable()
	if err != nil {
		return err
	}
	executable := filepath.Join(v.Root, "bin", bundle.Executable("refrail", runtime.GOOS))
	currentInfo, err := os.Stat(current)
	if err != nil {
		return err
	}
	targetInfo, err := os.Stat(executable)
	if err != nil {
		return err
	}
	if !os.SameFile(currentInfo, targetInfo) {
		cmd := exec.Command(executable, append([]string{"hook-run"}, args...)...)
		cmd.Dir = r.Root
		return child(cmd, stdin, stdout, stderr)
	}
	unlock, err := install.LockHook(r)
	if err != nil {
		return err
	}
	defer unlock()
	locked, _, err := install.Active(r)
	if err != nil {
		return err
	}
	if locked.BundleDigest != s.BundleDigest || locked.BundlePath != s.BundlePath {
		return i18n.Errorf("The active bundle changed concurrently; retry the Git operation")
	}
	if err := install.Doctor(r); err != nil {
		return err
	}
	lefthook := filepath.Join(v.Root, "bin", bundle.Executable("lefthook", runtime.GOOS))
	// Policies must run even for empty commits and branch-only pushes. Lefthook
	// otherwise skips pre-push commands when there are no matching changed files.
	cmd := exec.Command(lefthook, "run", "--no-auto-install", "--no-tty", "--colors", "off", "--skip-lfs", "--force", hook)
	cmd.Dir = r.Root
	var env []string
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "REFRAIL_") && key != "REFRAIL_HOME" && key != "REFRAIL_LANG" {
			continue
		}
		if strings.HasPrefix(key, "LEFTHOOK_CONFIG") || key == "LEFTHOOK_BIN" {
			continue
		}
		env = append(env, item)
	}
	// Git for Windows runs the validator through sh. Use shell-compatible path
	// separators while retaining native paths for Go's direct process execution.
	env = append(env, "LEFTHOOK_CONFIG="+filepath.Join(v.Root, "lefthook.yml"), "LEFTHOOK_BIN="+lefthook, "REFRAIL_BIN="+filepath.ToSlash(executable), "NO_COLOR=1")
	if hook == "commit-msg" {
		env = append(env, "REFRAIL_MESSAGE_FILE="+args[1])
	} else {
		env = append(env, "REFRAIL_REMOTE="+args[1])
	}
	cmd.Env = env
	// Lefthook normalizes failing job exit codes. Persist the validator's code via
	// a private result file so policy (1) and operational errors (2) stay distinct.
	result, err := os.CreateTemp(install.StateDir(r), "result-")
	if err != nil {
		return err
	}
	resultPath := result.Name()
	result.Close()
	defer os.Remove(resultPath)
	cmd.Env = append(cmd.Env, "REFRAIL_RESULT="+resultPath)
	err = child(cmd, stdin, stdout, stderr)
	if err != nil {
		data, readErr := os.ReadFile(resultPath)
		if readErr == nil && strings.TrimSpace(string(data)) == "1" {
			return &childError{1}
		}
		return &childError{2}
	}
	return nil
}
