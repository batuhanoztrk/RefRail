//go:build darwin

package i18n

import (
	"context"
	"os/exec"
	"time"
)

func platformLanguage() string {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/bin/defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return ""
	}
	return firstPreferredLanguage(string(output))
}
