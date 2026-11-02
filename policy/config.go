// Package policy parses Conventional Commits and applies company restrictions.
package policy

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"io"
	"refrail/internal/i18n"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

//go:embed default.json
var DefaultJSON []byte

type Config struct {
	SchemaVersion     int      `json:"schemaVersion"`
	Version           string   `json:"version"`
	CommitTypes       []string `json:"commitTypes"`
	BranchPrefixes    []string `json:"branchPrefixes"`
	MaxHeaderLength   int      `json:"maxHeaderLength"`
	RequireScope      bool     `json:"requireScope"`
	AllowedScopes     []string `json:"allowedScopes,omitempty"`
	RequireTicket     bool     `json:"requireTicket"`
	PermanentBranches []string `json:"permanentBranches"`
}

type Project struct {
	CommitTypes     *[]string `json:"commitTypes,omitempty"`
	BranchPrefixes  *[]string `json:"branchPrefixes,omitempty"`
	MaxHeaderLength *int      `json:"maxHeaderLength,omitempty"`
	RequireScope    *bool     `json:"requireScope,omitempty"`
	AllowedScopes   *[]string `json:"allowedScopes,omitempty"`
	RequireTicket   *bool     `json:"requireTicket,omitempty"`
}

// Violation is a user-correctable policy error (CLI exit code 1).
type Violation struct {
	Rule    string
	Detail  error
	Example string
}

func (e *Violation) Error() string {
	return e.Localized(i18n.English)
}
func (e *Violation) Localized(language i18n.Language) string {
	return "[" + e.Rule + "] " + i18n.Render(e.Detail, language) + "\n" + i18n.Text(language, "Example: %s", e.Example)
}
func (e *Violation) Unwrap() error { return e.Detail }

func Decode(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return i18n.Errorf("Invalid JSON: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return i18n.Errorf("JSON must contain a single object")
	}
	return nil
}

var token = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func validList(name string, values []string) error {
	if len(values) == 0 {
		return i18n.Errorf("%s must not be empty", name)
	}
	seen := map[string]bool{}
	for _, v := range values {
		if v == "" || strings.TrimSpace(v) != v || seen[v] {
			return i18n.Errorf("%s contains invalid or duplicate values", name)
		}
		seen[v] = true
	}
	return nil
}

func Load(data []byte) (Config, error) {
	var c Config
	if err := Decode(data, &c); err != nil {
		return c, err
	}
	if c.SchemaVersion != 1 || c.Version == "" || c.MaxHeaderLength < 1 {
		return c, i18n.Errorf("Invalid policy version or header length limit")
	}
	for name, values := range map[string][]string{"commitTypes": c.CommitTypes, "branchPrefixes": c.BranchPrefixes, "permanentBranches": c.PermanentBranches} {
		if err := validList(name, values); err != nil {
			return c, err
		}
		for _, v := range values {
			if !token.MatchString(v) {
				return c, i18n.Errorf("%s: invalid value %q", name, v)
			}
		}
	}
	if c.AllowedScopes != nil {
		if err := validList("allowedScopes", c.AllowedScopes); err != nil {
			return c, err
		}
	}
	return c, nil
}

func tightenList(name string, base []string, requested *[]string) ([]string, error) {
	if requested == nil {
		return base, nil
	}
	if err := validList(name, *requested); err != nil {
		return nil, err
	}
	for _, v := range *requested {
		if base != nil && !slices.Contains(base, v) {
			return nil, i18n.Errorf("%s cannot expand the shared policy: %q", name, v)
		}
	}
	return slices.Clone(*requested), nil
}

func (c Config) Tighten(data []byte) (Config, error) {
	var raw map[string]json.RawMessage
	if err := Decode(data, &raw); err != nil {
		return c, err
	}
	if raw == nil {
		return c, i18n.Errorf("Project policy must be a JSON object")
	}
	for key, value := range raw {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return c, i18n.Errorf("%s must not be null", key)
		}
	}
	var p Project
	if err := Decode(data, &p); err != nil {
		return c, err
	}
	var err error
	if c.CommitTypes, err = tightenList("commitTypes", c.CommitTypes, p.CommitTypes); err != nil {
		return c, err
	}
	if c.BranchPrefixes, err = tightenList("branchPrefixes", c.BranchPrefixes, p.BranchPrefixes); err != nil {
		return c, err
	}
	if c.AllowedScopes, err = tightenList("allowedScopes", c.AllowedScopes, p.AllowedScopes); err != nil {
		return c, err
	}
	if p.MaxHeaderLength != nil {
		if *p.MaxHeaderLength < 1 || *p.MaxHeaderLength > c.MaxHeaderLength {
			return c, i18n.Errorf("maxHeaderLength must be between 1 and %d", c.MaxHeaderLength)
		}
		c.MaxHeaderLength = *p.MaxHeaderLength
	}
	if p.RequireScope != nil {
		if c.RequireScope && !*p.RequireScope {
			return c, i18n.Errorf("requireScope cannot be relaxed")
		}
		c.RequireScope = *p.RequireScope
	}
	if p.RequireTicket != nil {
		if c.RequireTicket && !*p.RequireTicket {
			return c, i18n.Errorf("requireTicket cannot be relaxed")
		}
		c.RequireTicket = *p.RequireTicket
	}
	return c, nil
}

type Commit struct {
	Header, Type, Scope, Description string
	Breaking                         bool
}

var headerPattern = regexp.MustCompile(`^([a-z][a-z0-9-]*)(?:\(([^()\r\n]+)\))?(!)?: (.+)$`)

// Parse only handles the convention; allowed types and company limits are separate.
func Parse(message string) (Commit, error) {
	if !utf8.ValidString(message) {
		return Commit{}, i18n.Errorf("Commit message must be valid UTF-8")
	}
	message = strings.ReplaceAll(message, "\r\n", "\n")
	header, rest, _ := strings.Cut(message, "\n")
	m := headerPattern.FindStringSubmatch(header)
	if m == nil || strings.TrimSpace(m[4]) == "" || (m[2] != "" && strings.TrimSpace(m[2]) != m[2]) {
		return Commit{}, i18n.Errorf("Expected type(scope)!: description; scope and ! are optional")
	}
	c := Commit{Header: header, Type: m[1], Scope: m[2], Description: m[4], Breaking: m[3] != ""}
	if strings.TrimSpace(rest) != "" && !strings.HasPrefix(rest, "\n") {
		return Commit{}, i18n.Errorf("Separate the header from the body/footer with a blank line")
	}
	for _, line := range strings.Split(rest, "\n") {
		if strings.HasPrefix(line, "BREAKING CHANGE: ") || strings.HasPrefix(line, "BREAKING-CHANGE: ") {
			c.Breaking = true
		}
	}
	return c, nil
}

func (c Config) ValidateCommit(message string) error {
	commit, err := Parse(message)
	if err != nil {
		return &Violation{"commit-format", err, "feat(auth): add biometric login"}
	}
	if !slices.Contains(c.CommitTypes, commit.Type) {
		return &Violation{"commit-type", i18n.Errorf("Commit type is not allowed: %s", commit.Type), "fix(auth): correct login validation"}
	}
	if utf8.RuneCountInString(commit.Header) > c.MaxHeaderLength {
		return &Violation{"header-length", i18n.Errorf("Header must not exceed %d Unicode characters.", c.MaxHeaderLength), "docs: explain setup steps"}
	}
	if c.RequireScope && commit.Scope == "" {
		return &Violation{"scope-required", i18n.Errorf("Scope is required."), "feat(auth): add login"}
	}
	if commit.Scope != "" && c.AllowedScopes != nil && !slices.Contains(c.AllowedScopes, commit.Scope) {
		return &Violation{"scope", i18n.Errorf("Scope is not allowed: %s", commit.Scope), "feat(" + c.AllowedScopes[0] + "): add login"}
	}
	return nil
}

var branchPattern = regexp.MustCompile(`^([a-z][a-z0-9-]*)/([a-z0-9]+(?:-[a-z0-9]+)*)$`)
var ticketPattern = regexp.MustCompile(`^[0-9]+-[a-z0-9]+(?:-[a-z0-9]+)*$`)

func (c Config) ValidateBranch(name string) error {
	if slices.Contains(c.PermanentBranches, name) {
		return nil
	}
	m := branchPattern.FindStringSubmatch(name)
	if m == nil || !slices.Contains(c.BranchPrefixes, m[1]) {
		return &Violation{"branch-name", i18n.Errorf("Branch must use an allowed prefix and a lowercase, hyphen-separated description: %s", name), "feat/1234-add-login"}
	}
	if c.RequireTicket && !ticketPattern.MatchString(m[2]) {
		return &Violation{"branch-ticket", i18n.Errorf("Branch name must include a ticket number."), "fix/1234-camera-permission"}
	}
	return nil
}
