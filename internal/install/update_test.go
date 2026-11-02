package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"refrail/internal/gitrepo"
)

func TestUpdateRestoresWrappersOnWriteFailure(t *testing.T) {
	// Fail each replacement in turn, including the final state commit.
	for failAt := 1; failAt <= len(hookNames)+1; failAt++ {
		t.Run(hookNamesOrState(failAt), func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command("git", "init", "--bare", dir)
			cmd.Env = gitrepo.CleanEnv()
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("init: %v %s", err, output)
			}
			r := &gitrepo.Repo{Root: dir, CommonDir: dir}
			if err := os.Mkdir(StateDir(r), 0700); err != nil {
				t.Fatal(err)
			}
			old := &State{BootstrapExecutable: filepath.Join(dir, "old binary"), BootstrapDigest: "old", Baseline: "unchanged"}
			next := &State{BootstrapExecutable: filepath.Join(dir, "new binary"), BootstrapDigest: "new", Baseline: old.Baseline}
			original, _ := json.Marshal(old)
			updated, _ := json.Marshal(next)
			if err := AtomicWrite(statePath(r), original, 0600); err != nil {
				t.Fatal(err)
			}
			for _, name := range hookNames {
				if err := AtomicWrite(filepath.Join(dir, "hooks", name), Wrapper(old, name), 0755); err != nil {
					t.Fatal(err)
				}
			}
			sentinel := errors.New("injected write failure")
			calls := 0
			write := func(path string, data []byte, mode os.FileMode) error {
				calls++
				if calls == failAt {
					return sentinel
				}
				return AtomicWrite(path, data, mode)
			}
			if err := updateInstallation(r, old, next, updated, write); !errors.Is(err, sentinel) {
				t.Fatalf("lost write failure: %v", err)
			}
			state, err := os.ReadFile(statePath(r))
			if err != nil || !bytes.Equal(state, original) {
				t.Fatalf("failed update changed state: %v", err)
			}
			for _, name := range hookNames {
				hook, err := os.ReadFile(filepath.Join(dir, "hooks", name))
				if err != nil || !bytes.Equal(hook, Wrapper(old, name)) {
					t.Fatalf("failed update changed %s: %v", name, err)
				}
			}
		})
	}
}

func hookNamesOrState(index int) string {
	if index <= len(hookNames) {
		return hookNames[index-1]
	}
	return "state"
}
