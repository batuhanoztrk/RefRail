package gitrepo

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"refrail/internal/i18n"
	"strings"
)

type Repo struct{ Root, CommonDir string }

func CleanEnv() []string {
	var env []string
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_PREFIX", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES":
			continue
		}
		env = append(env, v)
	}
	return append(env, "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0")
}

func command(dir, input string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = CleanEnv()
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		// Never include remote URLs or inherited credential-bearing Git diagnostics.
		return "", i18n.Errorf("Git operation failed (%s): %w", args[0], err)
	}
	return stdout.String(), nil
}

func Open(path string) (*Repo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	root, err := command(abs, "", "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, i18n.Errorf("Git working repository not found: %w", err)
	}
	root = strings.TrimSpace(root)
	common, err := command(root, "", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	return &Repo{Root: root, CommonDir: strings.TrimSpace(common)}, nil
}

func (r *Repo) Run(args ...string) (string, error) { return command(r.Root, "", args...) }
func (r *Repo) Input(input string, args ...string) (string, error) {
	return command(r.Root, input, args...)
}
func (r *Repo) Path(name string) (string, error) {
	p, err := r.Run("rev-parse", "--path-format=absolute", "--git-path", name)
	return strings.TrimSpace(p), err
}
func (r *Repo) Head() string {
	out, err := r.Run("rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
func (r *Repo) HooksPathSetting() (string, error) {
	out, err := r.Run("config", "--get", "core.hooksPath")
	if err != nil {
		var exitErr *exec.ExitError
		if errorAs(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", err
	}
	value := strings.TrimSpace(out)
	if value == "" {
		return "(boş değer)", nil
	}
	return value, nil
}

// Avoid interpreting revision arguments as options or Git revision expressions.
func ValidOID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func ZeroOID(s string) bool { return ValidOID(s) && strings.Trim(s, "0") == "" }

func (r *Repo) Resolve(revision string) (string, error) {
	out, err := r.Run("rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
	if err != nil {
		return "", i18n.Errorf("Commit not found; obtain the required history with git fetch: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (r *Repo) MessageVariants(message string) ([]string, error) {
	// commit-msg runs before final cleanup, and Git does not expose a per-command
	// --cleanup override to the hook. Validate raw/verbatim, whitespace and strip
	// variants, instead of guessing the eventual mode or modifying the message.
	// This intentionally requires a valid first physical line, even if cleanup
	// could otherwise remove leading blanks/comments and make it look valid.
	variants := []string{strings.ReplaceAll(message, "\r\n", "\n")}
	for _, args := range [][]string{{"stripspace"}, {"stripspace", "--strip-comments"}} {
		clean, err := r.Input(message, args...)
		if err != nil {
			return nil, err
		}
		variants = append(variants, clean)
	}
	return variants, nil
}

func (r *Repo) IsMerging() bool {
	p, err := r.Path("MERGE_HEAD")
	if err != nil {
		return false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	for _, oid := range strings.Fields(string(b)) {
		if !ValidOID(oid) || ZeroOID(oid) {
			return false
		}
		if _, err := r.Resolve(oid); err != nil {
			return false
		}
	}
	return len(strings.Fields(string(b))) > 0
}
