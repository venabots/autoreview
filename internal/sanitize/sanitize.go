// Package sanitize makes other people's text safe to draw. A PR title, a
// branch name and a review are all written by somebody else, and a terminal
// obeys what it is sent: an escape sequence in a title could repaint the
// screen, and a bidi override could reorder the line around it.
package sanitize

import (
	"strings"
	"unicode"
)

// Line drops every character that can rewrite or reorder terminal output:
// the C0 and C1 controls, and the invisible characters terminals honor.
// Tabs and newlines go too, so the result is always one line.
func Line(s string) string {
	return strings.Map(dropRisky, s)
}

// Block is the same filter over many lines. It keeps newlines and tabs:
// they are not the risk, and dropping tabs would corrupt every Go or
// Makefile snippet a reviewer quotes.
func Block(s string) string {
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	clean := make([]string, len(lines))
	for i, line := range lines {
		clean[i] = strings.Map(dropRiskyKeepTab, line)
	}
	return strings.Join(clean, "\n")
}

func dropRisky(r rune) rune {
	if risky(r) {
		return -1
	}
	return r
}

func dropRiskyKeepTab(r rune) rune {
	if r == '\t' {
		return r
	}
	return dropRisky(r)
}

// risky reports the controls, the bidi embedding, override and isolate
// marks, and the zero-width and format family.
func risky(r rune) bool {
	switch {
	case unicode.IsControl(r):
		return true
	case r >= 0x202A && r <= 0x202E:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	case r >= 0x200B && r <= 0x200F:
		return true
	case r == 0x061C, r == 0xFEFF:
		return true
	}
	return false
}
