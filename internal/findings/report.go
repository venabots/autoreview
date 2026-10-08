package findings

import "strings"

// synthesisHeadings are the fixed section headings of a synthesis.
var synthesisHeadings = [...]string{
	"### Overview", "### Risk", "### must-fix", "### should-fix",
	"### polish", "### Disagreements", "### Approach",
}

// IsSynthesis is true when the text holds a real synthesized review and not
// only a sign-off. It is what a ledger record's `reviewed` says.
//
// A review that landed on the PR can leave the session with "Posted the
// review." as its last text. To count that as a review that kept none of its
// findings would sink the keep rate. A synthesis names its panelists or has
// one of its fixed headings. A bare "### Next steps" is neither.
//
// Fenced code is not read, so a message that only quotes a synthesis as an
// example is not one.
func IsSynthesis(reviewText string) bool {
	inFence := false
	for line := range strings.Lines(reviewText) {
		if isFence(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.Contains(line, flaggedBy) || containsAny(line, synthesisHeadings[:]) {
			return true
		}
	}
	return false
}

// Risk is the synthesized risk, from the first line under `### Risk`. It is
// false when the text has no such section or the line names no severity.
func Risk(reviewText string) (string, bool) {
	inRisk := false
	for raw := range strings.Lines(reviewText) {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			inRisk = asciiLower(strings.TrimSpace(strings.TrimLeft(line, "#"))) == "risk"
			continue
		}
		if !inRisk || line == "" {
			continue
		}
		word := strings.TrimLeft(line, "*`- ")
		for _, severity := range severities {
			if strings.HasPrefix(word, severity) {
				return severity, true
			}
		}
		return "", false
	}
	return "", false
}

// CountRaw is how many findings a panelist's own report has: its bullets
// with a severity tag. `NO_FINDINGS` is a report of zero. A report with
// neither is not a count at all, and it is false then: a panelist that could
// not review is not a panelist that found nothing.
func CountRaw(report string) (uint64, bool) {
	var bullets uint64
	for raw := range strings.Lines(report) {
		line := strings.TrimSpace(raw)
		if startsBullet(line) && tagIn(unlink(line)) != "" {
			bullets++
		}
	}
	if bullets == 0 && !strings.Contains(report, "NO_FINDINGS") {
		return 0, false
	}
	return bullets, true
}

// TopSeverity is the worst tag among a report's bullets, and false when no
// bullet has one.
func TopSeverity(report string) (string, bool) {
	for _, severity := range severities {
		tag := "[" + severity + "]"
		for line := range strings.Lines(report) {
			if startsBullet(trimStart(line)) && strings.Contains(line, tag) {
				return severity, true
			}
		}
	}
	return "", false
}
