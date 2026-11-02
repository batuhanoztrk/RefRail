package app

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"refrail/internal/i18n"
	"refrail/policy"
)

func TestCommitDiagnosticPresentation(t *testing.T) {
	c, err := policy.Load(policy.DefaultJSON)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		language                                      i18n.Language
		heading, message, expected, optional, example string
	}{
		{i18n.English, "Commit message does not meet the policy", "Your message:", "Expected: type(scope)!: description", "scope and ! are optional", "Example:"},
		{i18n.Turkish, "Commit mesajı uygun değil", "Mesajınız:", "Beklenen: type(scope)!: açıklama", "scope ve ! isteğe bağlıdır", "Örnek:"},
	} {
		t.Run(string(tc.language), func(t *testing.T) {
			err := withCommitMessage(c.ValidateCommit("test commit\nbody"), "test commit\nbody")
			var v *policy.Violation
			if !errors.As(err, &v) {
				t.Fatal("commit context lost policy error")
			}
			var out bytes.Buffer
			printViolation(&out, err, v, tc.language, false)
			for _, want := range []string{tc.heading, "[commit-format]", tc.message + " test commit", tc.expected, tc.optional, tc.example, "git commit -m \"feat(auth): add biometric login\""} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q in %q", want, out.String())
				}
			}
			if strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), "body") {
				t.Fatal(out.String())
			}
			out.Reset()
			printViolation(&out, err, v, tc.language, true)
			for _, want := range []string{"\x1b[31m", "\x1b[33m", "\x1b[32m", "\x1b[2m[commit-format]\x1b[0m"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing color %q in %q", want, out.String())
				}
			}
		})
	}
}

func TestDiagnosticRetainsReasonAndEscapesInput(t *testing.T) {
	for _, tc := range []struct{ rule, detail, example string }{
		{"commit-type", "Unknown type: \x1b[31mbogus", "fix: correct login"},
		{"branch-name", "Bad branch: feature\x1b]0;title\a", "feat/add-login"},
		{"commit-format", "Separate the header from the body/footer with a blank line", "docs: explain setup"},
		{"commit-format", "Commit abcdef123456: expected format", "docs: explain setup"},
	} {
		t.Run(tc.rule+tc.detail, func(t *testing.T) {
			v := &policy.Violation{Rule: tc.rule, Detail: errors.New(tc.detail), Example: tc.example}
			err := withCommitMessage(v, "mesaj ğü\x1b[2J\r\nprivate body")
			var out bytes.Buffer
			printViolation(&out, err, v, i18n.English, false)
			text := out.String()
			if strings.ContainsAny(text, "\x1b\a\r") || strings.Contains(text, "private body") || !strings.Contains(text, "Reason: "+displayText(tc.detail)) || !strings.Contains(text, "mesaj ğü\\x1b[2J") {
				t.Fatal(text)
			}
			if tc.rule == "branch-name" && (!strings.Contains(text, "Branch name does not meet the policy") || !strings.Contains(text, "git branch -m \"feat/add-login\"")) {
				t.Fatal(text)
			}
		})
	}
}

func TestExampleCommands(t *testing.T) {
	for _, tc := range []struct{ example, want string }{
		{"feat(auth): add login", `git commit -m "feat(auth): add login"`},
		{"feat(team's): add login", `git commit -m "feat(team's): add login"`},
		{`feat(team\name): add login`, `feat(team\\name): add login`},
		{"feat($team): add login", "feat($team): add login"},
		{"feat(\"team\"): add login", `feat(\"team\"): add login`},
		{"feat: add login\x1b[2J", `feat: add login\x1b[2J`},
	} {
		if got := exampleCommand(&policy.Violation{Rule: "scope", Example: tc.example}); got != tc.want {
			t.Fatalf("%q: got %q, want %q", tc.example, got, tc.want)
		}
	}
}

func TestPlainWriter(t *testing.T) {
	input := "\x1b[31mred\x1b[0m \x1b[38;2;56;56;56mdim\x1b[m\nğü \x1b[2Jother \x1b[31"
	want := "red dim\nğü \x1b[2Jother \x1b[31"
	for chunk := 1; chunk <= len(input); chunk++ {
		var out bytes.Buffer
		w := &plainWriter{target: &out}
		for start := 0; start < len(input); start += chunk {
			end := min(start+chunk, len(input))
			if n, err := w.Write([]byte(input[start:end])); err != nil || n != end-start {
				t.Fatalf("chunk %d: n=%d err=%v", chunk, n, err)
			}
		}
		w.flush()
		if out.String() != want {
			t.Fatalf("chunk %d: %q", chunk, out.String())
		}
	}
}

func TestPlainOutputsShareDestination(t *testing.T) {
	var out bytes.Buffer
	stdout, stderr := plainOutputs(&out, &out)
	var workers sync.WaitGroup
	for _, w := range []*plainWriter{stdout, stderr} {
		workers.Go(func() {
			for range 1000 {
				if _, err := w.Write([]byte("\x1b[31mline\x1b[m\n")); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Wait()
	stdout.flush()
	stderr.flush()
	if got := out.String(); got != strings.Repeat("line\n", 2000) {
		t.Fatalf("shared output corrupted: %d bytes", len(got))
	}
}

func TestColorSelection(t *testing.T) {
	for _, tc := range []struct {
		name, inherited, noColor, term string
		want                           bool
	}{
		{"redirected", "", "", "xterm", false},
		{"hook inherited", "1", "", "xterm", true},
		{"hook plain", "0", "", "xterm", false},
		{"NO_COLOR wins", "1", "1", "xterm", false},
		{"NO_COLOR zero is nonempty", "1", "0", "xterm", false},
		{"dumb terminal", "1", "", "dumb", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REFRAIL_OUTPUT_COLOR", tc.inherited)
			t.Setenv("NO_COLOR", tc.noColor)
			t.Setenv("TERM", tc.term)
			if got := outputColor(&bytes.Buffer{}); got != tc.want {
				t.Fatalf("color=%v, want %v", got, tc.want)
			}
		})
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm")
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if terminalColor(file) {
		t.Fatal("character device is not necessarily a terminal")
	}
}

func TestPolicyAndOperationalExitCodes(t *testing.T) {
	t.Setenv("REFRAIL_LANG", "en")
	t.Setenv("NO_COLOR", "1")
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"not-a-command"}, 2, "Error: Unknown command"},
		{[]string{"hook-run"}, 2, "Error: Unsupported hook"},
	} {
		var out bytes.Buffer
		if got := Run(tc.args, strings.NewReader(""), &out, &out); got != tc.code || !strings.Contains(out.String(), tc.want) {
			t.Fatalf("%v: exit %d output %q", tc.args, got, out.String())
		}
	}
}
