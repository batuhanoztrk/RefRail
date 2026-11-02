package app

import (
	"os"
	"syscall"
)

func isTerminal(f *os.File) bool {
	var mode uint32
	// ANSI is safe only when the console already supports VT output.
	return syscall.GetConsoleMode(syscall.Handle(f.Fd()), &mode) == nil && mode&0x0004 != 0
}
