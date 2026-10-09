package findings

import "strings"

// severities is every severity tag, worst first. The order is used: a
// bullet with two tags reads as the worse one.
var severities = [...]string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}

// tagIn is the severity whose `[TAG]` the text has, or "" when it has none.
func tagIn(text string) string {
	for _, severity := range severities {
		if strings.Contains(text, "["+severity+"]") {
			return severity
		}
	}
	return ""
}

// unlink turns `[text](url)` into `text`, so a location inside a link to
// the PR reads the same as a bare one.
func unlink(s string) string {
	var out strings.Builder
	rest := s
	for {
		open := strings.IndexByte(rest, '[')
		if open < 0 {
			break
		}
		length := strings.IndexByte(rest[open:], ']')
		if length < 0 {
			break
		}
		closing := open + length
		// A tag like "[HIGH]" is a bracket that is not a link. It is kept,
		// and the search goes on after it.
		if !strings.HasPrefix(rest[closing:], "](") {
			out.WriteString(rest[:closing+1])
			rest = rest[closing+1:]
			continue
		}
		end := strings.IndexByte(rest[closing:], ')')
		if end < 0 {
			break
		}
		out.WriteString(rest[:open])
		out.WriteString(rest[open+1 : closing])
		rest = rest[closing+end+1:]
	}
	out.WriteString(rest)
	return out.String()
}

// locationIn is the first `path:line` word, which is how the synthesis says
// where a finding is. It is "" when there is none. A path has a dot or a
// slash in it, so a stray `9:` or a `Fix:` label is not taken for one.
func locationIn(head string) string {
	for _, word := range strings.Fields(head) {
		word = strings.TrimFunc(word, isLocationEdge)
		colon := strings.LastIndexByte(word, ':')
		if colon < 0 {
			continue
		}
		path, line := word[:colon], word[colon+1:]
		if strings.ContainsAny(path, "./") && isLineRange(line) {
			return word
		}
	}
	return ""
}

// isLocationEdge is true for a character that wraps a location and is no
// part of it: a backquote, a bracket, the comma after it.
func isLocationEdge(r rune) bool {
	return !isLetter(r) && !isDigit(r) && r != '/' && r != '.'
}

// isLineRange is true for `104` and for `652-655`.
func isLineRange(s string) bool {
	for part := range strings.SplitSeq(s, "-") {
		if part == "" || strings.ContainsFunc(part, func(r rune) bool { return !isDigit(r) }) {
			return false
		}
	}
	return true
}

// containsAny says s has one of the words in it.
func containsAny(s string, words []string) bool {
	for _, word := range words {
		if strings.Contains(s, word) {
			return true
		}
	}
	return false
}

// asciiLower lowers the letters A to Z and nothing else. strings.ToLower
// also lowers letters outside ASCII (the Kelvin sign becomes "k"), and v1
// does not, so a heading the two versions lowered differently would open a
// section in one and not in the other.
func asciiLower(s string) string {
	lowered := []byte(s)
	for i, b := range lowered {
		if 'A' <= b && b <= 'Z' {
			lowered[i] = b + ('a' - 'A')
		}
	}
	return string(lowered)
}

// isLetter is true for the ASCII letters only.
func isLetter(r rune) bool {
	return ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z')
}

// isDigit is true for the ASCII digits only.
func isDigit(r rune) bool {
	return '0' <= r && r <= '9'
}
