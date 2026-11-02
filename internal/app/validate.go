package app

import (
	"bufio"
	"errors"
	"io"
	"os"
	"refrail/internal/i18n"
	"strings"

	"refrail/internal/gitrepo"
	"refrail/policy"
)

func validateMessage(r *gitrepo.Repo, c policy.Config, filename string) error {
	if filename == "" {
		return i18n.Errorf("Specify the commit message file with --file")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return i18n.Errorf("Cannot read the commit message file: %w", err)
	}
	if r.IsMerging() {
		return nil
	}
	variants, err := r.MessageVariants(string(data))
	if err != nil {
		return err
	}
	for _, message := range variants {
		if err := c.ValidateCommit(message); err != nil {
			return withCommitMessage(err, message)
		}
	}
	return nil
}

func withCommitMessage(err error, message string) error {
	var violation *policy.Violation
	if errors.As(err, &violation) {
		header, _, _ := strings.Cut(message, "\n")
		return &commitDiagnostic{violation, strings.TrimSuffix(header, "\r")}
	}
	return err
}

func requireFullHistory(r *gitrepo.Repo) error {
	value, err := r.Run("rev-parse", "--is-shallow-repository")
	if err != nil {
		return err
	}
	if strings.TrimSpace(value) == "true" {
		return i18n.Errorf("Cannot validate history in a shallow repository; run git fetch --unshallow")
	}
	return nil
}

func validateHistory(r *gitrepo.Repo, c policy.Config, to string, excludes []string, seen map[string]bool) error {
	args := []string{"rev-list", "--reverse", "--topo-order", to}
	if len(excludes) > 0 {
		args = append(args, "--not")
		args = append(args, excludes...)
	}
	out, err := r.Run(args...)
	if err != nil {
		return i18n.Errorf("Cannot read commit range; obtain the required history with git fetch: %w", err)
	}
	for _, oid := range strings.Fields(out) {
		if seen[oid] {
			continue
		}
		seen[oid] = true
		parents, err := r.Run("rev-list", "--parents", "-n", "1", oid)
		if err != nil {
			return err
		}
		if len(strings.Fields(parents)) > 2 {
			continue
		}
		message, err := r.Run("show", "-s", "--format=%B", oid)
		if err != nil {
			return err
		}
		if err := c.ValidateCommit(strings.TrimRight(message, "\n")); err != nil {
			if v, ok := err.(*policy.Violation); ok {
				return withCommitMessage(&policy.Violation{Rule: v.Rule, Detail: i18n.Errorf("Commit %s: %s", oid[:12], v.Detail), Example: v.Example}, message)
			}
			return err
		}
	}
	return nil
}

func checkRange(r *gitrepo.Repo, c policy.Config, from, to string) error {
	if from == "" || to == "" {
		return i18n.Errorf("check requires --from and --to")
	}
	if err := requireFullHistory(r); err != nil {
		return err
	}
	a, err := r.Resolve(from)
	if err != nil {
		return err
	}
	b, err := r.Resolve(to)
	if err != nil {
		return err
	}
	return validateHistory(r, c, b, []string{a}, map[string]bool{})
}

type pushUpdate struct{ localOID, remoteRef, remoteOID string }

func validatePush(r *gitrepo.Repo, c policy.Config, baseline, remote string, stdin io.Reader) error {
	if remote == "" {
		return i18n.Errorf("pre-push requires --remote")
	}
	var updates []pushUpdate
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		f := strings.Fields(scanner.Text())
		if len(f) != 4 || !gitrepo.ValidOID(f[1]) || !gitrepo.ValidOID(f[3]) {
			return i18n.Errorf("Invalid pre-push stdin; expected Git reference updates with four fields")
		}
		if gitrepo.ZeroOID(f[1]) || !strings.HasPrefix(f[2], "refs/heads/") {
			continue
		}
		name := strings.TrimPrefix(f[2], "refs/heads/")
		if err := c.ValidateBranch(name); err != nil {
			return err
		}
		updates = append(updates, pushUpdate{f[1], f[2], f[3]})
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(updates) == 0 {
		return nil
	}
	if err := requireFullHistory(r); err != nil {
		return err
	}
	if baseline != "" {
		if _, err := r.Resolve(baseline); err != nil {
			return i18n.Errorf("Installation baseline commit not found: %w", err)
		}
	}
	// Only use the configured destination remote's locally known refs for new branches.
	var known []string
	remotes, err := r.Run("remote")
	if err != nil {
		return err
	}
	for _, name := range strings.Fields(remotes) {
		if name != remote {
			continue
		}
		out, err := r.Run("for-each-ref", "--format=%(objectname)", "refs/remotes/"+name+"/")
		if err != nil {
			return err
		}
		for _, oid := range strings.Fields(out) {
			resolved, err := r.Resolve(oid)
			if err != nil {
				return err
			}
			known = append(known, resolved)
		}
	}
	seen := map[string]bool{}
	for _, update := range updates {
		to, err := r.Resolve(update.localOID)
		if err != nil {
			return err
		}
		var excludes []string
		if baseline != "" {
			excludes = append(excludes, baseline)
		}
		if !gitrepo.ZeroOID(update.remoteOID) {
			old, err := r.Resolve(update.remoteOID)
			if err != nil {
				return err
			}
			excludes = append(excludes, old)
		} else {
			excludes = append(excludes, known...)
		}
		if err := validateHistory(r, c, to, excludes, seen); err != nil {
			return err
		}
	}
	return nil
}
