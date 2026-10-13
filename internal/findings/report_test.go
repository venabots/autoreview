package findings

import "testing"

// The wanted answers in this file came from v1 0.23.0, run on the same
// text. The first cases of each table are v1's own tests.

func TestASynthesisIsToldApartFromASignOff(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"the fixed headings", "### Risk\nLOW\n### Overview\n...", true},
		{"a finding with its panelist", "- [LOW] a.rs:1 — nit. Flagged by: codex (gpt-5)", true},
		{"a sign-off", "Posted the review with gh.", false},
		{"a heading that is not one of the fixed ones", "### Next steps\n- ran the panel and posted it", false},
		{
			"a synthesis quoted as an example inside a fence",
			"The shape is:\n```md\n- [HIGH] a.rs:1 — bug. Flagged by: codex (gpt-5)\n```",
			false,
		},
		{"a fixed heading after a fence that closed", "```\n### Overview\n```\n### Risk", true},
		{"a fixed heading of another level", "## Overview", false},
		{"a fence of tildes is not a fence", "~~~\n### Overview\n~~~", true},
		{"a bucket with nothing in it", "### should-fix\n", true},
		{"no text", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsSynthesis(c.text); got != c.want {
				t.Errorf("IsSynthesis(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

func TestATextCanHoldFindingsAndStillBeNoSynthesis(t *testing.T) {
	// v1 records the findings of such a text and marks the record as not
	// reviewed. A caller must ask both questions and not derive one answer
	// from the other.
	const text = "## Disagreements\n- `a.ts:1` — codex (gpt-5.5) claimed X. Verification falsified this."
	want := `[{"location":"a.ts:1","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1,"dropped":true}]`
	if got := written(t, Parse(text)); got != want {
		t.Errorf("Parse\n got  %s\n want %s", got, want)
	}
	if IsSynthesis(text) {
		t.Error("IsSynthesis = true for a text with none of the fixed headings and no clause")
	}
}

// answer is a value that a text may not have.
type answer[T comparable] struct {
	value T
	ok    bool
}

func some[T comparable](value T) answer[T] {
	return answer[T]{value: value, ok: true}
}

func TestTheRiskIsTheFirstLineUnderItsHeading(t *testing.T) {
	none := answer[string]{}
	cases := []struct {
		name string
		text string
		want answer[string]
	}{
		{"a bold word after a blank line", "### Risk\n\n**MEDIUM** — touches real logic.", some("MEDIUM")},
		{"a word with a colon", "### Risk\nLOW: docs only.", some("LOW")},
		{"a severity under another heading", "### Overview\nLOW", none},
		{"a heading of another level, in capital letters", "## RISK\nHIGH", some("HIGH")},
		{"a bullet with the word in code marks", "### Risk\n- `CRITICAL` — it loses data.", some("CRITICAL")},
		{"a heading with more words is not the risk heading", "### Risk level\nHIGH", none},
		{"only the first line is read", "### Risk\nunknown\nLOW", none},
		{"the next heading ends the section", "### Risk\n### Overview\nLOW", none},
		{"a heading with nothing under it", "### Risk\n", none},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			value, ok := Risk(c.text)
			if got := (answer[string]{value, ok}); got != c.want {
				t.Errorf("Risk(%q) = %+v, want %+v", c.text, got, c.want)
			}
		})
	}
}

func TestARawCountIsTheTaggedBulletsOfAReport(t *testing.T) {
	none := answer[uint64]{}
	cases := []struct {
		name   string
		report string
		want   answer[uint64]
	}{
		{"two tagged bullets", "- [LOW] a.rs:1 — nit\n- [HIGH] b.rs:2 — bug", some(uint64(2))},
		{"a report of no findings is a count of zero", "NO_FINDINGS", some(uint64(0))},
		{"a report with neither is not a count", "I could not review this.", none},
		{
			"a star bullet and a nested bullet count, a bare line does not",
			"* [MEDIUM] a.rs:1 — x\n  - [CRITICAL] b.rs:2 — y\n[HIGH] not a bullet",
			some(uint64(2)),
		},
		{"a tag inside a link", "- [[HIGH]](https://example.com/h) a.rs:1 — a linked tag", some(uint64(1))},
		{"a bullet with no tag is not counted", "- a.rs:1 — no tag\nNO_FINDINGS", some(uint64(0))},
		{"no report", "", none},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			value, ok := CountRaw(c.report)
			if got := (answer[uint64]{value, ok}); got != c.want {
				t.Errorf("CountRaw(%q) = %+v, want %+v", c.report, got, c.want)
			}
		})
	}
}

func TestTheTopSeverityIsTheWorstTagOnABullet(t *testing.T) {
	none := answer[string]{}
	cases := []struct {
		name   string
		report string
		want   answer[string]
	}{
		{"the worst of two", "- [LOW] a.rs:1 — nit\n- [HIGH] b.rs:2 — bug", some("HIGH")},
		{"a report of no findings has none", "NO_FINDINGS", none},
		{
			"a tag on a line that is no bullet is not read",
			"* [MEDIUM] a.rs:1 — x\n  - [LOW] b.rs:2 — y\n[CRITICAL] not a bullet",
			some("MEDIUM"),
		},
		{"a nested bullet is read", "  - [CRITICAL] b.rs:2 — y", some("CRITICAL")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			value, ok := TopSeverity(c.report)
			if got := (answer[string]{value, ok}); got != c.want {
				t.Errorf("TopSeverity(%q) = %+v, want %+v", c.report, got, c.want)
			}
		})
	}
}
