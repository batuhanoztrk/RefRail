// Package i18n renders RefRail messages in Turkish or English without global state.
package i18n

import (
	"fmt"
	"os"
	"strings"
)

type Language string

const (
	English Language = "en"
	Turkish Language = "tr"
)

func languageTag(value string) Language {
	base := strings.ToLower(strings.TrimSpace(value))
	if i := strings.IndexAny(base, "-_ .@:"); i >= 0 {
		base = base[:i]
	}
	if base == "tr" {
		return Turkish
	}
	return English
}

// Detect uses the native UI preference on macOS/Windows and locale variables
// elsewhere. REFRAIL_LANG is an optional explicit override for CI and tests.
func Detect() Language {
	return detect(os.Getenv("REFRAIL_LANG"), platformLanguage, os.Getenv)
}

func detect(override string, native func() string, lookup func(string) string) Language {
	if override := strings.ToLower(strings.TrimSpace(override)); override == "en" || override == "tr" {
		return Language(override)
	}
	if value := native(); value != "" {
		return languageTag(value)
	}
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANGUAGE", "LANG"} {
		if value := lookup(key); value != "" {
			return languageTag(value)
		}
	}
	return English
}

func firstPreferredLanguage(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '(' || r == ')' || r == ',' || r == '"' || r == '\'' || r == ' ' || r == '\n' || r == '\r' || r == '\t'
	})
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func Text(language Language, template string, args ...any) string {
	if language == Turkish {
		if translated, ok := turkish[template]; ok {
			template = translated
		}
	}
	if len(args) == 0 {
		return template
	}
	return fmt.Sprintf(strings.ReplaceAll(template, "%w", "%v"), args...)
}

// Message preserves error arguments so nested contexts can be translated at the
// CLI boundary while Error() remains deterministic English for library callers.
type Message struct {
	Template string
	Args     []any
}

func Errorf(template string, args ...any) error { return &Message{template, args} }
func (m *Message) Error() string                { return m.Localized(English) }
func (m *Message) Localized(language Language) string {
	args := make([]any, len(m.Args))
	for i, arg := range m.Args {
		if e, ok := arg.(error); ok {
			args[i] = Render(e, language)
		} else {
			args[i] = arg
		}
	}
	return Text(language, m.Template, args...)
}
func (m *Message) Unwrap() error {
	for _, arg := range m.Args {
		if err, ok := arg.(error); ok {
			return err
		}
	}
	return nil
}

func Render(err error, language Language) string {
	if err == nil {
		return ""
	}
	if localized, ok := err.(interface{ Localized(Language) string }); ok {
		return localized.Localized(language)
	}
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		var lines []string
		for _, child := range group.Unwrap() {
			lines = append(lines, Render(child, language))
		}
		return strings.Join(lines, "\n")
	}
	return Text(language, err.Error())
}
