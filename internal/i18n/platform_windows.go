//go:build windows

package i18n

import "syscall"

var userUILanguage = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

func platformLanguage() string {
	value, _, _ := userUILanguage.Call()
	if value == 0 {
		return ""
	}
	if value&0x3ff == 0x1f {
		return "tr"
	}
	return "en"
}
