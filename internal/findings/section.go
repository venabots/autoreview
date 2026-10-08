package findings

import (
	"strings"
	"unicode"
)

// section is the part of the review a line is in. The zero value is any part
// that is neither a bucket nor Disagreements.
type section struct {
	// severity is what a bucket heading gives a bullet with no tag of its
	// own: a bullet under `### should-fix` is MEDIUM.
	severity string
	// disagreements marks the section that lists what did not hold up.
	disagreements bool
}

// sectionOf is the section a heading opens.
func sectionOf(heading string) section {
	h := asciiLower(strings.TrimSpace(strings.TrimLeft(heading, "#")))
	switch {
	case strings.Contains(h, "disagreement"):
		return section{disagreements: true}
	case strings.HasPrefix(h, "must-fix"), strings.HasPrefix(h, "must fix"):
		return section{severity: "HIGH"}
	case strings.HasPrefix(h, "should-fix"), strings.HasPrefix(h, "should fix"):
		return section{severity: "MEDIUM"}
	case strings.HasPrefix(h, "polish"):
		return section{severity: "LOW"}
	}
	return section{}
}

// continuesSection is false for a line of prose. A section runs to the next
// heading or the next paragraph. Without that end the Disagreements section
// runs on into what the reviewer wrote after the synthesis, a "Posted these
// comments" summary for example, and its bullets read as dropped findings.
func continuesSection(line string) bool {
	return line == "" || startsBullet(line)
}

// startsBullet says the line starts with a list mark.
func startsBullet(line string) bool {
	return strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*")
}

// isFence is true for a line that opens or closes a fenced code block.
func isFence(line string) bool {
	return strings.HasPrefix(trimStart(line), "```")
}

// logicalLines is the text as one string for each bullet, heading, blank
// line and line of prose, trimmed.
//
// One bullet can wrap, and its `Flagged by` clause or its location can land
// on a wrapped part. A bullet is joined to the lines that continue it, so no
// panelist and no location is lost. A heading stands alone, and a blank line
// ends a bullet, so a later paragraph is not joined to it.
//
// A finding quoted inside a fenced code block is an example and nobody
// raised it. The fences and everything between them are left out.
func logicalLines(text string) []string {
	var units [][]string
	inFence := false
	// strings.Lines keeps the line ending, and every reader below trims it.
	for raw := range strings.Lines(text) {
		if isFence(raw) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		line := strings.TrimSpace(raw)
		last := len(units) - 1
		if line != "" && !standsAlone(line) && last >= 0 && startsBullet(units[last][0]) {
			units[last] = append(units[last], line)
			continue
		}
		units = append(units, []string{line})
	}
	joined := make([]string, len(units))
	for i, parts := range units {
		joined[i] = strings.Join(parts, " ")
	}
	return joined
}

// standsAlone is true for a heading and for a line that opens a bullet.
// Neither one continues the bullet before it.
func standsAlone(line string) bool {
	return strings.HasPrefix(line, "#") ||
		strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") ||
		line == "-" || line == "*"
}

// trimStart is s without the white space in front of it.
func trimStart(s string) string {
	return strings.TrimLeftFunc(s, unicode.IsSpace)
}
