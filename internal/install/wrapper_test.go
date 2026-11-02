package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingBootstrapRecoveryIsReadableInBothLanguages(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("shell required for wrapper test")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "commit-msg")
	state := &State{BootstrapExecutable: filepath.Join(dir, "missing ' binary")}
	if err := os.WriteFile(path, Wrapper(state, "commit-msg"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, language := range []string{"en", "tr"} {
		t.Run(language, func(t *testing.T) {
			cmd := exec.Command(sh, path)
			for _, item := range os.Environ() {
				key, _, _ := strings.Cut(item, "=")
				if !strings.EqualFold(key, "REFRAIL_LANG") {
					cmd.Env = append(cmd.Env, item)
				}
			}
			cmd.Env = append(cmd.Env, "REFRAIL_LANG="+language)
			output, err := cmd.CombinedOutput()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 2 {
				t.Fatalf("expected exit2: %v %s", err, output)
			}
			if !strings.Contains(string(output), "RefRail executable is missing") || !strings.Contains(string(output), "RefRail aracı eksik") {
				t.Fatalf("unreadable recovery: %s", output)
			}
		})
	}
}
