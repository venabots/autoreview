package review

import (
	"fmt"
	"strings"

	"github.com/venabots/autoreview/internal/words"
)

// ReasonsHeader is the line that opens the list of reasons.
const ReasonsHeader = "not approved yet because:"

// maxReasons is how many blockers a reader gets before the rest are
// counted. Three fit under one result line and do not hide the next one,
// and a review with more than three blockers is one a person opens anyway.
const maxReasons = 3

// Reasons is why a PR is not approved yet, each reason already a bullet.
// verdict is the resolved verdict, or "" when nothing landed.
//
// A summary that says "commented" does not say if the cause is a misaligned
// button or a double charge, and a reader who must open every PR to learn
// which is reading a list and not a report.
//
// The list is empty for an approved PR, for a review that named no blocker
// and for a reviewer too old to report one. Whether the PR is approved is
// GitHub's answer, so an approved PR lists nothing, whatever its trailer
// says. Why it is not approved is the reviewer's, because only the reviewer
// knows.
func Reasons(verdict string, t *Trailer) []string {
	if verdict == "approved" || t == nil {
		return nil
	}
	var lines []string
	for _, blocker := range t.Blockers {
		if line, ok := blockerLine(blocker); ok {
			lines = append(lines, "- "+line)
		}
	}
	if len(lines) <= maxReasons {
		return lines
	}
	hidden := len(lines) - maxReasons
	return append(lines[:maxReasons:maxReasons], fmt.Sprintf("- %s not shown", words.Count(hidden, "finding")))
}

// blockerLine is one blocker as a line:
//
//	[HIGH] (money, irreversible) src/pay.rs:88 — a retried checkout charges the card twice
//
// Every part is optional, because every field is. A blocker that says
// nothing at all is dropped and is not drawn as an empty bullet.
func blockerLine(b Blocker) (string, bool) {
	var head []string
	if severity := strings.TrimSpace(b.Severity); severity != "" {
		head = append(head, "["+severity+"]")
	}
	if tag := blockerTag(b); tag != "" {
		head = append(head, "("+tag+")")
	}
	if location := strings.TrimSpace(b.Location); location != "" {
		head = append(head, location)
	}
	start := strings.Join(head, " ")
	gist := strings.TrimSpace(b.Gist)
	switch {
	case start == "" && gist == "":
		return "", false
	case start == "":
		return gist, true
	case gist == "":
		return start, true
	}
	return start + " — " + gist, true
}

// blockerTag is the domain and the reversibility in one parenthesis. A
// domain alone still tells a reader which kind of bug this is, so a
// reviewer that reported one of the two does not lose both.
func blockerTag(b Blocker) string {
	var parts []string
	if domain := strings.TrimSpace(b.Domain); domain != "" {
		parts = append(parts, domain)
	}
	if b.Reversible != nil {
		if *b.Reversible {
			parts = append(parts, "reversible")
		} else {
			parts = append(parts, "irreversible")
		}
	}
	return strings.Join(parts, ", ")
}
