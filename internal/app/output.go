package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"refrail/internal/i18n"
	"refrail/policy"
)

// Commit context belongs to CLI presentation, not the policy's error contract.
type commitDiagnostic struct {
	*policy.Violation
	header string
}

func (e *commitDiagnostic) Unwrap() error { return e.Violation }

func styled(enabled bool, code, value string) string {
	if !enabled {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

// Escape control characters in user input, preserving readable Unicode.
func displayText(value string) string {
	quoted := strconv.QuoteToGraphic(value)
	return quoted[1 : len(quoted)-1]
}

func printError(w io.Writer, err error, language i18n.Language) {
	fmt.Fprintln(w, styled(outputColor(w), "31", i18n.Text(language, "Error:")), i18n.Render(err, language))
}

func printViolation(w io.Writer, err error, v *policy.Violation, language i18n.Language, color bool) {
	title := i18n.Text(language, "Commit message does not meet the policy")
	if strings.HasPrefix(v.Rule, "branch-") {
		title = i18n.Text(language, "Branch name does not meet the policy")
	}
	fmt.Fprintf(w, "%s  %s\n\n", styled(color, "31", "✗ "+title), styled(color, "2", "["+v.Rule+"]"))
	var commit *commitDiagnostic
	if errors.As(err, &commit) {
		fmt.Fprintf(w, "  %s %s\n", i18n.Text(language, "Your message:"), displayText(commit.header))
	}
	if detail, ok := v.Detail.(*i18n.Message); ok && detail.Template == "Expected type(scope)!: description; scope and ! are optional" {
		fmt.Fprintf(w, "  %s %s\n", i18n.Text(language, "Expected:"), styled(color, "33", i18n.Text(language, "type(scope)!: description")))
		fmt.Fprintf(w, "  %s\n", i18n.Text(language, "scope and ! are optional"))
	} else {
		fmt.Fprintf(w, "  %s %s\n", i18n.Text(language, "Reason:"), styled(color, "33", displayText(i18n.Render(v.Detail, language))))
	}
	fmt.Fprintf(w, "\n  %s %s\n\n", i18n.Text(language, "Example:"), styled(color, "32", exampleCommand(v)))
}

func exampleCommand(v *policy.Violation) string {
	// Ordinary examples use double quotes supported by sh, PowerShell, and
	// cmd.exe. For shell-sensitive values show the example itself rather than
	// suggesting a command whose escaping depends on the user's shell.
	if strings.ContainsAny(v.Example, "\\\"$`%!\r\n") || displayText(v.Example) != v.Example {
		return displayText(v.Example)
	}
	command := "git commit -m "
	if strings.HasPrefix(v.Rule, "branch-") {
		command = "git branch -m "
	}
	return command + "\"" + v.Example + "\""
}

// plainWriter removes SGR styling even when escape sequences cross writes.
// Other bytes are preserved, including non-SGR escape sequences.
type plainWriter struct {
	target io.Writer
	escape []byte
	lock   *sync.Mutex
}

func plainOutputs(stdout, stderr io.Writer) (*plainWriter, *plainWriter) {
	// os/exec may copy both streams concurrently. Serialize writes even when
	// the caller passed the same non-thread-safe destination for both.
	lock := new(sync.Mutex)
	return &plainWriter{target: stdout, lock: lock}, &plainWriter{target: stderr, lock: lock}
}

func (w *plainWriter) Write(p []byte) (int, error) {
	if w.lock != nil {
		w.lock.Lock()
		defer w.lock.Unlock()
	}
	out := make([]byte, 0, len(p))
	for _, b := range p {
		if len(w.escape) != 0 {
			if len(w.escape) == 1 && b == '[' || len(w.escape) > 1 && (b >= '0' && b <= '9' || b == ';' || b == ':') {
				w.escape = append(w.escape, b)
				continue
			}
			if len(w.escape) > 1 && b == 'm' {
				w.escape = w.escape[:0]
				continue
			}
			out = append(out, w.escape...)
			w.escape = w.escape[:0]
		}
		if b == '\x1b' {
			w.escape = append(w.escape, b)
		} else {
			out = append(out, b)
		}
	}
	if n, err := w.target.Write(out); err != nil {
		return 0, err
	} else if n != len(out) {
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}

func (w *plainWriter) flush() {
	if w.lock != nil {
		w.lock.Lock()
		defer w.lock.Unlock()
	}
	if len(w.escape) != 0 {
		_, _ = w.target.Write(w.escape)
		w.escape = nil
	}
}

func terminalColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	return ok && isTerminal(f)
}

func outputColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	// Lefthook captures validator streams even when Git runs in a terminal.
	switch os.Getenv("REFRAIL_OUTPUT_COLOR") {
	case "1":
		return true
	case "0":
		return false
	}
	return terminalColor(w)
}
