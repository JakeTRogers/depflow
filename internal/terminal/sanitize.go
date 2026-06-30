// Package terminal strips control bytes from untrusted strings before they reach a terminal.
package terminal

import "strings"

// Sanitize strips terminal control bytes (escape sequences, bell, etc.) from value, preserving
// tab/newline/carriage-return. Use this on any GitHub-controlled string (PR titles, labels,
// check names, workflow run names) before writing it to a terminal or live progress display.
func Sanitize(value string) string {
	if value == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(value))

	changed := false
	for i := 0; i < len(value); i++ {
		if shouldStripControlByte(value[i]) {
			changed = true
			continue
		}
		builder.WriteByte(value[i])
	}

	if !changed {
		return value
	}

	return builder.String()
}

func shouldStripControlByte(value byte) bool {
	switch {
	case value == '\t', value == '\n', value == '\r':
		return false
	case value <= 0x08:
		return true
	case value >= 0x0B && value <= 0x0C:
		return true
	case value >= 0x0E && value <= 0x1F:
		return true
	case value == 0x1B:
		return true
	case value == 0x7F:
		return true
	default:
		return false
	}
}
