package install

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"refrail/internal/i18n"
	"runtime"
	"slices"
	"strings"

	"refrail/internal/bundle"
	"refrail/internal/gitrepo"
	"refrail/policy"
)

const marker = "# refrail managed hook v1"
const baselineRef = "refs/refrail/baseline"

var hookNames = []string{"commit-msg", "pre-push"}

type State struct {
	SchemaVersion       int    `json:"schemaVersion"`
	BundlePath          string `json:"bundlePath"`
	BundleDigest        string `json:"bundleDigest"`
	Baseline            string `json:"baseline"`
	BootstrapExecutable string `json:"bootstrapExecutable"`
	BootstrapDigest     string `json:"bootstrapDigest"`
}

func StateDir(r *gitrepo.Repo) string  { return filepath.Join(r.CommonDir, "refrail") }
func statePath(r *gitrepo.Repo) string { return filepath.Join(StateDir(r), "state.json") }

func ReadState(r *gitrepo.Repo) (*State, error) {
	st, err := os.Lstat(StateDir(r))
	if err != nil {
		return nil, i18n.Errorf("Repository is not enabled; run install: %w", err)
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, i18n.Errorf("Installation path must be a regular directory")
	}
	st, err = os.Lstat(statePath(r))
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, i18n.Errorf("Installation state must be a regular file")
	}
	data, err := os.ReadFile(statePath(r))
	if err != nil {
		return nil, err
	}
	var s State
	if err := policy.Decode(data, &s); err != nil {
		return nil, err
	}
	if s.SchemaVersion != 1 || !filepath.IsAbs(s.BundlePath) || !filepath.IsAbs(s.BootstrapExecutable) || len(s.BundleDigest) != 64 || len(s.BootstrapDigest) != 64 || (s.Baseline != "" && (!gitrepo.ValidOID(s.Baseline) || gitrepo.ZeroOID(s.Baseline))) {
		return nil, i18n.Errorf("Invalid installation state")
	}
	return &s, nil
}

func LoadPolicy(r *gitrepo.Repo, c policy.Config) (policy.Config, error) {
	data, err := os.ReadFile(filepath.Join(r.Root, ".refrail.json"))
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	return c.Tighten(data)
}

func Active(r *gitrepo.Repo) (*State, *bundle.Verified, error) {
	s, err := ReadState(r)
	if err != nil {
		return nil, nil, err
	}
	v, err := bundle.Verify(s.BundlePath, true)
	if err != nil {
		return nil, nil, i18n.Errorf("Installed bundle is corrupt or missing; run update with a valid bundle: %w", err)
	}
	if v.Digest != s.BundleDigest {
		return nil, nil, i18n.Errorf("The active bundle manifest has changed")
	}
	return s, v, nil
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(s), "'", "'\"'\"'") + "'"
}
func Wrapper(s *State, hook string) []byte {
	q := shQuote(s.BootstrapExecutable)
	// If the executable is missing, native language detection cannot run. Keep
	// the bootstrap recovery message understandable in both supported languages.
	const missing = "RefRail executable is missing; restore the bundle or reinstall."
	recovery := i18n.Text(i18n.English, missing) + " / " + i18n.Text(i18n.Turkish, missing)
	return []byte("#!/bin/sh\n" + marker + "\nif [ ! -x " + q + " ]; then\n  printf '%s\\n' " + shQuote(recovery) + " >&2\n  exit 2\nfi\nexec " + q + " hook-run " + shQuote(hook) + " \"$@\"\n")
}

func checkConfig(r *gitrepo.Repo) error {
	setting, err := r.HooksPathSetting()
	if err != nil {
		return err
	}
	if setting != "" {
		return i18n.Errorf("core.hooksPath is already configured; automatic merging is not supported. Review the existing hook configuration")
	}
	return nil
}

func preflight(r *gitrepo.Repo, s *State) error {
	if err := checkConfig(r); err != nil {
		return err
	}
	hooks, err := r.Path("hooks")
	if err != nil {
		return err
	}
	if st, err := os.Lstat(hooks); err == nil && (!st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
		return i18n.Errorf("Hooks path must be a regular directory")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(hooks)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".sample") {
			continue
		}
		st, err := entry.Info()
		if err != nil {
			return err
		}
		managed := slices.Contains(hookNames, name)
		if s != nil && managed {
			if !st.Mode().IsRegular() {
				return i18n.Errorf("Managed hook is not a regular file: %s", name)
			}
			data, err := os.ReadFile(filepath.Join(hooks, name))
			if err != nil {
				return err
			}
			if string(data) != string(Wrapper(s, name)) {
				return i18n.Errorf("Managed hook has changed: %s; inspect the file", name)
			}
			continue
		}
		if managed || st.Mode()&os.ModeSymlink != 0 || (!entry.IsDir() && (runtime.GOOS == "windows" || st.Mode().Perm()&0111 != 0)) {
			return i18n.Errorf("Existing hook conflicts: %s; no hooks were overwritten", name)
		}
	}
	if s != nil {
		for _, name := range hookNames {
			p := filepath.Join(hooks, name)
			st, err := os.Stat(p)
			if err != nil {
				return i18n.Errorf("Managed hook is missing: %s", name)
			}
			if runtime.GOOS != "windows" && st.Mode().Perm()&0111 == 0 {
				return i18n.Errorf("Hook is not executable: %s", name)
			}
		}
	}
	return nil
}

func AtomicWrite(filename string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(filename), ".company-tmp-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, filename)
}

func lock(r *gitrepo.Repo) (func(), error) {
	p := filepath.Join(r.CommonDir, "refrail.lock")
	if err := os.Mkdir(p, 0700); err != nil {
		return nil, i18n.Errorf("Cannot acquire installation lock; another operation or a stale lock may exist: %w", err)
	}
	return func() { os.Remove(p) }, nil
}

// LockHook prevents lifecycle changes while the active hook is executing.
func LockHook(r *gitrepo.Repo) (func(), error) { return lock(r) }

func packageHome() (string, error) {
	if p := os.Getenv("REFRAIL_HOME"); p != "" {
		return filepath.Abs(p)
	}
	p, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(p, "refrail"), nil
}

func cache(v *bundle.Verified) (string, error) {
	home, err := packageHome()
	if err != nil {
		return "", err
	}
	packages := filepath.Join(home, "packages")
	if err := os.MkdirAll(packages, 0700); err != nil {
		return "", err
	}
	prefix := v.Manifest.Version + "-" + v.Manifest.GOOS + "-" + v.Manifest.GOARCH + "-" + v.Digest[:12] + "-"
	entries, err := os.ReadDir(packages)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		p := filepath.Join(packages, entry.Name(), "bundle")
		if found, err := bundle.Verify(p, true); err == nil && found.Digest == v.Digest {
			return p, nil
		}
	}
	parent, err := os.MkdirTemp(packages, prefix)
	if err != nil {
		return "", err
	}
	p := filepath.Join(parent, "bundle")
	if err := bundle.CopyVerified(v, p); err != nil {
		os.RemoveAll(parent)
		return "", err
	}
	return p, nil
}

func Install(r *gitrepo.Repo, dir string, update bool) error {
	v, err := bundle.Verify(dir, true)
	if err != nil {
		return err
	}
	if _, err := LoadPolicy(r, v.Policy); err != nil {
		return err
	}
	var old *State
	if _, err := os.Lstat(StateDir(r)); err == nil {
		old, err = ReadState(r)
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if update && old == nil {
		return i18n.Errorf("Repository is not enabled; run install first")
	}
	if old != nil && !update {
		if old.BundleDigest != v.Digest {
			return i18n.Errorf("Repository uses a different bundle; use update")
		}
		if err := Doctor(r); err != nil {
			return err
		}
		return nil
	}
	if err := preflight(r, old); err != nil {
		return err
	}
	if old == nil {
		if _, err := r.Resolve(baselineRef); err == nil {
			return i18n.Errorf("Baseline reference already exists; inspect the installation state")
		}
	}
	unlock, err := lock(r)
	if err != nil {
		return err
	}
	defer unlock()
	// Recheck after taking the lock so concurrent installs cannot overwrite state.
	if old == nil {
		if _, err := os.Lstat(StateDir(r)); !os.IsNotExist(err) {
			return i18n.Errorf("Installation state changed concurrently; retry the operation")
		}
	} else {
		current, err := ReadState(r)
		if err != nil {
			return err
		}
		if *current != *old {
			return i18n.Errorf("Installation state changed concurrently; retry the operation")
		}
	}
	if err := preflight(r, old); err != nil {
		return err
	}
	p, err := cache(v)
	if err != nil {
		return err
	}
	s := &State{SchemaVersion: 1, BundlePath: p, BundleDigest: v.Digest, Baseline: r.Head()}
	if old != nil {
		s.Baseline = old.Baseline
		// Verify the existing installation before replacing its launcher.
		data, err := os.ReadFile(old.BootstrapExecutable)
		if err != nil || bundle.Digest(data) != old.BootstrapDigest {
			return i18n.Errorf("Bootstrap executable is missing or corrupt; restore it from a trusted bundle or uninstall and install again")
		}
	}
	s.BootstrapExecutable = filepath.Join(p, "bin", bundle.Executable("refrail", runtime.GOOS))
	data, err := os.ReadFile(s.BootstrapExecutable)
	if err != nil {
		return err
	}
	s.BootstrapDigest = bundle.Digest(data)
	data, err = json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if old != nil {
		return updateInstallation(r, old, s, data, AtomicWrite)
	}
	if err := os.Mkdir(StateDir(r), 0700); err != nil {
		return err
	}
	hooks, err := r.Path("hooks")
	if err != nil {
		os.Remove(StateDir(r))
		return err
	}
	if err := os.MkdirAll(hooks, 0755); err != nil {
		os.Remove(StateDir(r))
		return err
	}
	var created []string
	baselineCreated := false
	rollback := func() {
		for _, p := range created {
			os.Remove(p)
		}
		if baselineCreated {
			r.Run("update-ref", "-d", baselineRef, s.Baseline)
		}
		os.Remove(statePath(r))
		os.Remove(StateDir(r))
	}
	if err := AtomicWrite(statePath(r), data, 0600); err != nil {
		rollback()
		return err
	}
	if s.Baseline != "" {
		if _, err := r.Run("update-ref", baselineRef, s.Baseline, strings.Repeat("0", len(s.Baseline))); err != nil {
			rollback()
			return err
		}
		baselineCreated = true
	}
	for _, name := range hookNames {
		file := filepath.Join(hooks, name)
		f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
		if err != nil {
			rollback()
			return err
		}
		created = append(created, file)
		_, werr := f.Write(Wrapper(s, name))
		cerr := f.Close()
		if werr != nil {
			rollback()
			return werr
		}
		if cerr != nil {
			rollback()
			return cerr
		}
	}
	return nil
}

// The caller holds the lifecycle lock. Refresh launchers before committing the
// new state so obsolete bootstrap behavior does not survive a bundle update.
// Every replacement is atomic; on failure restore already-replaced wrappers.
func updateInstallation(r *gitrepo.Repo, old, next *State, data []byte, write func(string, []byte, os.FileMode) error) error {
	if old.BootstrapExecutable == next.BootstrapExecutable && old.BootstrapDigest == next.BootstrapDigest {
		return write(statePath(r), data, 0600)
	}
	hooks, err := r.Path("hooks")
	if err != nil {
		return err
	}
	type replacedHook struct {
		path string
		name string
		mode os.FileMode
	}
	var replaced []replacedHook
	rollback := func(cause error) error {
		failures := []error{cause}
		for i := len(replaced) - 1; i >= 0; i-- {
			hook := replaced[i]
			if err := write(hook.path, Wrapper(old, hook.name), hook.mode); err != nil {
				failures = append(failures, err)
			}
		}
		return errors.Join(failures...)
	}
	for _, name := range hookNames {
		path := filepath.Join(hooks, name)
		info, err := os.Stat(path)
		if err != nil {
			return rollback(err)
		}
		if err := write(path, Wrapper(next, name), info.Mode().Perm()); err != nil {
			return rollback(err)
		}
		replaced = append(replaced, replacedHook{path, name, info.Mode().Perm()})
	}
	if err := write(statePath(r), data, 0600); err != nil {
		return rollback(err)
	}
	return nil
}

func Doctor(r *gitrepo.Repo) error {
	s, _, err := Active(r)
	if err != nil {
		return err
	}
	if err := preflight(r, s); err != nil {
		return err
	}
	b, err := os.ReadFile(s.BootstrapExecutable)
	if err != nil {
		return err
	}
	if bundle.Digest(b) != s.BootstrapDigest {
		return i18n.Errorf("Bootstrap SHA-256 does not match")
	}
	if s.Baseline != "" {
		if _, err := r.Resolve(s.Baseline); err != nil {
			return err
		}
		anchor, err := r.Resolve(baselineRef)
		if err != nil || anchor != s.Baseline {
			return i18n.Errorf("Installation baseline reference is missing or changed")
		}
	}
	_, v, err := Active(r)
	if err != nil {
		return err
	}
	_, err = LoadPolicy(r, v.Policy)
	return err
}

func Uninstall(r *gitrepo.Repo) error {
	s, err := ReadState(r)
	if err != nil {
		return err
	}
	if err := preflight(r, s); err != nil {
		return err
	}
	unlock, err := lock(r)
	if err != nil {
		return err
	}
	defer unlock()
	if err := preflight(r, s); err != nil {
		return err
	}
	current, err := ReadState(r)
	if err != nil {
		return err
	}
	if *current != *s {
		return i18n.Errorf("Installation state changed concurrently; retry")
	}
	entries, err := os.ReadDir(StateDir(r))
	if err != nil {
		return err
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		return i18n.Errorf("Installation metadata contains unexpected files; inspect the directory before uninstalling")
	}
	hooks, err := r.Path("hooks")
	if err != nil {
		return err
	}
	if s.Baseline != "" {
		anchor, err := r.Resolve(baselineRef)
		if err != nil || anchor != s.Baseline {
			return i18n.Errorf("Baseline reference has changed; inspect it before uninstalling")
		}
	}
	stage, err := os.MkdirTemp(r.CommonDir, ".company-policy-uninstall-")
	if err != nil {
		return err
	}
	type movedFile struct{ original, backup string }
	var moved []movedFile
	stateDirRemoved := false
	rollback := func(cause error) error {
		var failures []error
		if stateDirRemoved {
			if err := os.Mkdir(StateDir(r), 0700); err != nil {
				failures = append(failures, err)
			}
		}
		for i := len(moved) - 1; i >= 0; i-- {
			if err := os.Rename(moved[i].backup, moved[i].original); err != nil {
				failures = append(failures, err)
			}
		}
		if len(failures) == 0 {
			os.Remove(stage)
		}
		return errors.Join(append([]error{cause}, failures...)...)
	}
	for _, name := range hookNames {
		original, backup := filepath.Join(hooks, name), filepath.Join(stage, name)
		if err := os.Rename(original, backup); err != nil {
			return rollback(err)
		}
		moved = append(moved, movedFile{original, backup})
	}
	backup := filepath.Join(stage, "state.json")
	if err := os.Rename(statePath(r), backup); err != nil {
		return rollback(err)
	}
	moved = append(moved, movedFile{statePath(r), backup})
	if err := os.Remove(StateDir(r)); err != nil {
		return rollback(err)
	}
	stateDirRemoved = true
	// Delete the baseline only after all reversible filesystem changes succeeded.
	if s.Baseline != "" {
		if _, err := r.Run("update-ref", "-d", baselineRef, s.Baseline); err != nil {
			return rollback(err)
		}
	}
	for _, file := range moved {
		if err := os.Remove(file.backup); err != nil {
			return i18n.Errorf("Installation removed; cannot clean up the temporary backup: %w", err)
		}
	}
	return os.Remove(stage)
}
