//go:build !darwin && !windows

package i18n

func platformLanguage() string { return "" }
