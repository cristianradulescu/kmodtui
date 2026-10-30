package kmod

import "strings"

// Clean replaces control characters (C0, DEL and C1, which includes ESC and
// newlines) with U+FFFD. Text read from sysfs, modinfo and config files can be
// chosen by whoever built the device or module (a USB device picks its own
// product string), so it must not carry escape sequences into the UI or break
// its one-line-per-item layout. Invalid UTF-8 also becomes U+FFFD.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return '�'
		}
		return r
	}, s)
}
