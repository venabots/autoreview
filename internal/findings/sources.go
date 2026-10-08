package findings

import "strings"

// backends is every harness a panelist can run on. A name that starts with
// one is a panelist even when nothing else about it says so.
var backends = [...]string{"codex", "claude", "opencode"}

// sourceList reads `a (m) [LOW], b (m2) and c`: names, each with an optional
// model in parentheses and an optional severity of its own, with commas or
// "and" between them.
//
// It stops at the first thing that is not one of those. The sentence that a
// synthesizer sometimes adds after the list ("Verified against the code.")
// must never become a panelist: it would be scored as a model.
func sourceList(list string) []source {
	var out []source
	rest := trimStart(list)
	for {
		next, ok := readEntry(rest)
		if !ok {
			return out
		}
		if !next.separated {
			// The last name has no separator after it to prove that it
			// is in the list, so it must read as a panelist by itself.
			if isPanelist(next.source) {
				out = append(out, next.source)
			}
			return out
		}
		out = append(out, next.source)
		rest = next.rest
	}
}

// entry is one name read off the front of a source list.
type entry struct {
	source
	// separated says a comma or an "and" came after it, so it is in list
	// position and another name can follow.
	separated bool
	// rest is the list after the separator.
	rest string
}

// readEntry reads the name at the front of the list, with its model. It is
// false when the list does not start with a name.
func readEntry(list string) (entry, bool) {
	start := strings.TrimLeft(list, "`* ")
	name := strings.TrimRight(start[:prefixLen(start, isNameByte)], ".")
	if !strings.ContainsFunc(name, isLetter) {
		return entry{}, false
	}
	model, after, ok := readModel(strings.TrimLeft(start[len(name):], "`*"))
	if !ok {
		return entry{}, false
	}
	rest, separated := readSeparator(skipTag(after))
	return entry{source: source{name: name, model: model}, separated: separated, rest: rest}, true
}

// readModel reads `(model)` from the front of s. With no parenthesis there
// is no model, and that is fine. It is false for a parenthesis that holds a
// sentence: that is not a model, so the name before it is not a panelist,
// and nothing after it is one.
func readModel(s string) (model, rest string, ok bool) {
	inner, opened := strings.CutPrefix(trimStart(s), "(")
	if !opened {
		return "", s, true
	}
	n := prefixLen(inner, isModelByte)
	rest, closed := strings.CutPrefix(inner[n:], ")")
	if !closed {
		return "", "", false
	}
	return inner[:n], rest, true
}

// skipTag drops a severity in brackets from the front of s. It records what
// one panelist said. The severity of the finding is the one the synthesis
// chose.
func skipTag(s string) string {
	s = trimStart(s)
	if !strings.HasPrefix(s, "[") {
		return s
	}
	closing := strings.IndexByte(s, ']')
	if closing < 0 {
		return s
	}
	return trimStart(s[closing+1:])
}

// readSeparator reads what stands between two names: a comma, "and", or the
// ", and" of a list of three. Both parts are read. Otherwise "a, b, and c"
// reads "and" as the third name and loses c.
func readSeparator(s string) (rest string, separated bool) {
	afterComma, comma := strings.CutPrefix(s, ",")
	afterComma = trimStart(afterComma)
	if afterAnd, and := strings.CutPrefix(afterComma, "and "); and {
		return trimStart(afterAnd), true
	}
	return afterComma, comma
}

// isPanelist says a name reads as a panelist by itself: it has a model, it
// reads like a model (it has a digit), or it names a known backend. A
// sentence after the list has none of the three. A name in the middle of the
// list that has none either, "gemini" for example, is still kept, because
// the separator after it proves that it is in the list.
func isPanelist(s source) bool {
	if s.model != "" || strings.ContainsFunc(s.name, isDigit) {
		return true
	}
	lower := asciiLower(s.name)
	for _, backend := range backends {
		if lower == backend || strings.HasPrefix(lower, backend+"-") {
			return true
		}
	}
	return false
}

// prefixLen is how many bytes at the front of s pass the test.
func prefixLen(s string, pass func(byte) bool) int {
	for i := range len(s) {
		if !pass(s[i]) {
			return i
		}
	}
	return len(s)
}

func isNameByte(b byte) bool {
	return isLetter(rune(b)) || isDigit(rune(b)) || b == '-' || b == '_' || b == '.'
}

func isModelByte(b byte) bool {
	return isNameByte(b) || b == ':' || b == '/'
}
