package policy

import (
	"errors"
	"strings"
	"testing"
)

func base(t *testing.T) Config {
	t.Helper()
	c, err := Load(DefaultJSON)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCommitConventionAndCompanyRules(t *testing.T) {
	c := base(t)
	for _, tc := range []struct {
		name, message string
		valid         bool
	}{
		{"simple", "feat: add login", true},
		{"Turkish", "fix(camera): İzin hatasını düzelt", true},
		{"breaking bang", "feat(api)!: change response", true},
		{"breaking footer", "feat: change response\n\nBREAKING CHANGE: response is now a list", true},
		{"body footer", "fix: correct error\n\nExplanation\n\nRefs: #123", true},
		{"CRLF", "docs: update docs\r\n\r\nDescription\r\n", true},
		{"exact Unicode limit", "feat: " + strings.Repeat("ş", 94), true},
		{"Unicode too long", "feat: " + strings.Repeat("ş", 95), false},
		{"missing type", "login fixed", false},
		{"missing separator space", "fix:login", false},
		{"empty scope", "fix(): login", false},
		{"scope whitespace", "fix( auth ): login", false},
		{"invalid nested scope", "fix(a(b)): login", false},
		{"empty description", "fix:   ", false},
		{"unknown type", "hotfix: login", false},
		{"fixup", "fixup! feat: login", false},
		{"squash", "squash! feat: login", false},
		{"automatic revert", "Revert \"feat: login\"", false},
		{"conventional revert", "revert: remove login", true},
		{"fake merge", "Merge branch 'feat/login'", false},
		{"body without blank", "feat: login\nbody", false},
		{"invalid UTF8", "feat: \xff", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateCommit(tc.message)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if err != nil {
				var v *Violation
				if !errors.As(err, &v) {
					t.Fatalf("expected violation: %v", err)
				}
			}
		})
	}
	parsed, err := Parse("feat: login\n\nBREAKING CHANGE: new response")
	if err != nil || !parsed.Breaking {
		t.Fatalf("breaking footer: %+v %v", parsed, err)
	}
}

func TestBranchNames(t *testing.T) {
	c := base(t)
	for _, name := range []string{"main", "master", "develop", "feat/add-login", "feat/1234-add-login", "hotfix/payment-error", "revert/remove-login"} {
		if err := c.ValidateBranch(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"deneme", "feature/add-login", "feat/AddLogin", "feat/giriş", "feat/a/b", "feat/-login", "feat/login-", "feat/a--b", "feat/login name"} {
		if err := c.ValidateBranch(name); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	c.RequireTicket = true
	if c.ValidateBranch("feat/add-login") == nil {
		t.Fatal("ticket should be required")
	}
	if err := c.ValidateBranch("feat/1234-add-login"); err != nil {
		t.Fatal(err)
	}
}

func TestProjectCanOnlyTighten(t *testing.T) {
	c := base(t)
	tight, err := c.Tighten([]byte(`{"commitTypes":["feat","fix"],"branchPrefixes":["feat"],"maxHeaderLength":72,"allowedScopes":["auth"],"requireScope":true,"requireTicket":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := tight.ValidateCommit("feat(auth): login"); err != nil {
		t.Fatal(err)
	}
	if tight.ValidateCommit("feat: login") == nil || tight.ValidateCommit("feat(api): login") == nil || tight.ValidateCommit("docs(auth): login") == nil {
		t.Fatal("project restrictions were not applied")
	}
	if c.MaxHeaderLength != 100 || c.RequireScope {
		t.Fatal("base policy mutated")
	}
	for _, raw := range []string{
		`{"commitTypes":["anything"]}`, `{"branchPrefixes":["feature"]}`, `{"commitTypes":[]}`, `{"commitTypes":["feat","feat"]}`, `{"maxHeaderLength":101}`, `{"maxHeaderLength":0}`, `{"allowedScopes":[]}`, `{"allowedScopes":[" "]}`, `{"requireScope":null}`, `{"command":"echo bypass"}`, `{"permanentBranches":["anything"]}`, `{} {}`, `null`, `[]`,
	} {
		if _, err := c.Tighten([]byte(raw)); err == nil {
			t.Errorf("accepted invalid config: %s", raw)
		}
	}
	if _, err := tight.Tighten([]byte(`{"requireScope":false}`)); err == nil {
		t.Fatal("requireScope was relaxed")
	}
	if _, err := tight.Tighten([]byte(`{"requireTicket":false}`)); err == nil {
		t.Fatal("requireTicket was relaxed")
	}
	if _, err := tight.Tighten([]byte(`{"allowedScopes":["api"]}`)); err == nil {
		t.Fatal("scopes were expanded")
	}
}
