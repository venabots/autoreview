// Package sanitize makes other people's text safe to draw. A PR title, a
// branch name and a review are all written by somebody else, and a terminal
// obeys what it is sent: an escape sequence in a title could repaint the
// screen, and a bidi override could reorder the line around it.
package sanitize

import (
	"strings"
	"unicode"
	"unicode/utf8"
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

// Styled is the same filter over text that a renderer has already styled.
// It keeps the color sequences and the hyperlinks the renderer wrote, and
// drops every other risky character.
//
// Filter the text before it is rendered too, but that is not enough. A
// Markdown renderer decodes `&#x1b;` into a real escape and `&#x202e;` into
// a real bidi override, so text that was clean going in can come out able
// to repaint the screen.
func Styled(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		if n := styleLen(s); n > 0 {
			b.WriteString(s[:n])
			s = s[n:]
			continue
		}
		r, size := utf8.DecodeRuneInString(s)
		if r == '\n' || r == '\t' || !risky(r) {
			b.WriteRune(r)
		}
		s = s[size:]
	}
	return b.String()
}

const (
	sgrStart  = "\x1b["
	linkStart = "\x1b]8;"
	bell      = "\x07"
	st        = "\x1b\\"
)

// styleLen is the length of the color sequence or the hyperlink that s
// starts with, or zero. Only those two change nothing but how text looks
// and where a click leads.
func styleLen(s string) int {
	switch {
	case strings.HasPrefix(s, sgrStart):
		rest := s[len(sgrStart):]
		params := len(rest) - len(strings.TrimLeft(rest, "0123456789;:"))
		if params < len(rest) && rest[params] == 'm' {
			return len(sgrStart) + params + 1
		}
	case strings.HasPrefix(s, linkStart):
		return linkLen(s)
	}
	return 0
}

// linkLen is the length of the hyperlink sequence that s starts with. A
// sequence with no end, or with a risky character before its end, is not
// one: what follows would be read as part of it.
func linkLen(s string) int {
	for i, r := range s[len(linkStart):] {
		at := len(linkStart) + i
		switch {
		case strings.HasPrefix(s[at:], bell):
			return at + len(bell)
		case strings.HasPrefix(s[at:], st):
			return at + len(st)
		case risky(r):
			return 0
		}
	}
	return 0
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
