package app

import (
	"os"
	"syscall"
	"unsafe"
)

func isTerminal(f *os.File) bool {
	var termios syscall.Termios
	// TCGETS is the Linux terminal-attributes ioctl on amd64 and arm64.
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, f.Fd(), 0x5401, uintptr(unsafe.Pointer(&termios)), 0, 0, 0)
	return errno == 0
}
