//go:build !darwin && !linux && !windows

package app

import "os"

func isTerminal(f *os.File) bool { return false }
