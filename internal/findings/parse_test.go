package findings

import (
	"bytes"
	"testing"

	"github.com/venabots/autoreview/internal/ledger"
)

// parseCase is a review text and the `findings` list that v1 0.23.0 writes
// to the ledger for it. Every wanted list in these tables came from v1's own
// parser, run on the same text. The cases that v1's tests have keep the
// names of those tests.
type parseCase struct {
	name string
	text string
	want string
}

// written is the list as the ledger writes it.
func written(t *testing.T, list []ledger.Finding) string {
	t.Helper()
	items := make([][]byte, len(list))
	for i, f := range list {
		item, err := f.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		items[i] = item
	}
	return "[" + string(bytes.Join(items, []byte(","))) + "]"
}

func checkParse(t *testing.T, cases []parseCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := written(t, Parse(c.text)); got != c.want {
				t.Errorf("Parse(%q)\n got  %s\n want %s", c.text, got, c.want)
			}
		})
	}
}

func TestABulletWithAFlaggedByClauseIsAFinding(t *testing.T) {
	checkParse(t, []parseCase{
		{
			"a single source with its model",
			"- [LOW] src/a.rs:130 — the issue. Fix: the change. Flagged by: codex (gpt-5.5)",
			`[{"severity":"LOW","location":"src/a.rs:130","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1}]`,
		},
		{
			"consensus with inline severities and a note",
			"- [MEDIUM] src/a.rs:130 — the issue. Fix: the change. Flagged by 2: claude (claude-opus-5) [LOW], codex (gpt-5.5) [MEDIUM] — using higher.",
			`[{"severity":"MEDIUM","location":"src/a.rs:130","flagged_by":[{"name":"claude","model":"claude-opus-5"},{"name":"codex","model":"gpt-5.5"}],"count":2}]`,
		},
		{
			"linked locations and bold tags read the same",
			"- **[HIGH]** [apps/api/src/kyb.ts:104](https://github.com/o/r/pull/1/files#diff-abcR104) — bad. Flagged by: claude-fable (claude-fable-5)",
			`[{"severity":"HIGH","location":"apps/api/src/kyb.ts:104","flagged_by":[{"name":"claude-fable","model":"claude-fable-5"}],"count":1}]`,
		},
		{
			"a count without names is still a count",
			"- [MEDIUM] a.ts:90 — issue. Flagged by 3.\n- [LOW] b.ts:1 — issue. Flagged by all 4.",
			`[{"severity":"MEDIUM","location":"a.ts:90","flagged_by":[],"count":3},{"severity":"LOW","location":"b.ts:1","flagged_by":[],"count":4}]`,
		},
		{
			"a bare name with a trailing period",
			"- `[LOW]` `trace.ts:233` — issue. Flagged by: claude-opus.",
			`[{"severity":"LOW","location":"trace.ts:233","flagged_by":[{"name":"claude-opus"}],"count":1}]`,
		},
		{
			"a count with a plus sign is a count",
			"- [LOW] a.rs:1 — issue. Flagged by +3.",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[],"count":3}]`,
		},
		{
			"a count larger than the list is kept",
			"- [LOW] a.rs:1 — issue. Flagged by 5: codex (gpt-5.5)",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":5}]`,
		},
		{
			"a count smaller than the list is raised to it",
			"- [LOW] a.rs:1 — issue. Flagged by 1: codex (gpt-5.5), claude (claude-opus-5)",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5.5"},{"name":"claude","model":"claude-opus-5"}],"count":2}]`,
		},
		{
			"the largest count",
			"- [LOW] a.rs:1 — issue. Flagged by 4294967295.",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[],"count":4294967295}]`,
		},
		{"a count too large to hold is no count", "- [LOW] a.rs:1 — issue. Flagged by 4294967296.", `[]`},
		{"a count of zero is no finding", "- [LOW] a.rs:1 — issue. Flagged by 0.", `[]`},
		{"a clause with nothing after it is no finding", "- [LOW] a.rs:1 — issue. Flagged by:", `[]`},
		{
			"a word for a count before a list keeps the list",
			"- [LOW] a.rs:1 — issue. Flagged by three: codex (gpt-5.5)",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1}]`,
		},
		{
			"only the first clause on a line is read",
			"- [LOW] a.rs:1 — issue. Flagged by: codex. Flagged by: claude",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			"the worse of two tags is the severity",
			"- [CRITICAL] [LOW] a.rs:1 — two tags. Flagged by: codex",
			`[{"severity":"CRITICAL","location":"a.rs:1","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			"a tag in small letters is no severity",
			"- [low] a.rs:1 — a tag in small letters. Flagged by: codex",
			`[{"location":"a.rs:1","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
	})
}

func TestTheLocationIsTheFirstPathWithALine(t *testing.T) {
	checkParse(t, []parseCase{
		{
			"a label and a bare number are not a path",
			"- [LOW] see 9: and Fix: then lib/x:12 — y. Flagged by: codex",
			`[{"severity":"LOW","location":"lib/x:12","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			"a range of lines, inside marks",
			"- [LOW] (src/a.rs:10-20), — y. Flagged by: codex",
			`[{"severity":"LOW","location":"src/a.rs:10-20","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			"a dash left after the line is not part of it",
			"- [LOW] a.rs:1-, b.rs:7 — y. Flagged by: codex",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			"a name with no dot and no slash is not a path",
			"- [LOW] Makefile:3 — y. Flagged by: codex",
			`[{"severity":"LOW","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			"no location at all",
			"- [LOW] nothing here — y. Flagged by: codex",
			`[{"severity":"LOW","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			"a bracket that never closes is kept as text",
			"- [LOW] [a.rs:1 — y. Flagged by: codex",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			"a link that never closes is not unwrapped",
			"- [LOW] [a.rs:1](https://example.com/x — y. Flagged by: codex",
			`[{"severity":"LOW","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
	})
}

func TestOnlyPanelistsAreReadFromTheListAfterTheClause(t *testing.T) {
	// The first seven are v1's "a sentence after the list is not a panelist".
	checkParse(t, []parseCase{
		{
			"a sentence after the list",
			"- [LOW] a.rs:1 — issue. Flagged by: claude (claude-fable-5). Verified against the code, twice.",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"claude","model":"claude-fable-5"}],"count":1}]`,
		},
		{
			"a parenthesis that holds a sentence is not a model",
			"- [LOW] a.rs:1 — issue. Flagged by: catenabot (existing thread 3898214499); confirmed by claude-fable-5",
			`[]`,
		},
		{
			"two names with \"and\", then a note",
			"- [LOW] a.rs:1 — issue. Flagged by: codex (gpt-5.6-sol) and opencode (glm-5.3) — glm also noted the cause",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5.6-sol"},{"name":"opencode","model":"glm-5.3"}],"count":2}]`,
		},
		{
			"prose that starts with a number",
			"- [LOW] a.rs:1 — issue. Flagged by: 403 fix in `950d1556c` does not change the message",
			`[]`,
		},
		{
			"\"using\" after a comma is not a panelist",
			"- [LOW] a.rs:1 — issue. Flagged by 2: claude (claude-opus-5) [LOW], codex (gpt-5.5) [MEDIUM], using higher.",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"claude","model":"claude-opus-5"},{"name":"codex","model":"gpt-5.5"}],"count":2}]`,
		},
		{
			"a name with a backend in front, a name like a model, a backend",
			"- [LOW] a.rs:1 — issue. Flagged by: opencode-glm, grok-4.6, codex",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"opencode-glm"},{"name":"grok-4.6"},{"name":"codex"}],"count":3}]`,
		},
		{
			"\"all:\" with a list is a finding",
			"- [HIGH] a.rs:1 — issue. Flagged by all: codex (gpt-5.5), claude (claude-opus-5)",
			`[{"severity":"HIGH","location":"a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5.5"},{"name":"claude","model":"claude-opus-5"}],"count":2}]`,
		},
		{
			"\"all\" and a count before a list",
			"- [LOW] a.rs:1 — issue. Flagged by all 3: codex, claude, opencode",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex"},{"name":"claude"},{"name":"opencode"}],"count":3}]`,
		},
		{
			"an unknown name is kept in list position and not at the end",
			"- [LOW] a.rs:1 — issue. Flagged by: gemini, kimi",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"gemini"}],"count":1}]`,
		},
		{
			"a comma and \"and\" are one separator",
			"- [LOW] a.rs:1 — issue. Flagged by: codex, claude, and gemini",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex"},{"name":"claude"}],"count":2}]`,
		},
		{
			"a backend in capital letters, and a model with a slash",
			"- [LOW] a.rs:1 — issue. Flagged by: opencode (zai-coding-plan/glm-5.3), Claude-Opus",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"opencode","model":"zai-coding-plan/glm-5.3"},{"name":"Claude-Opus"}],"count":2}]`,
		},
		{
			"names in code marks and in bold",
			"- [LOW] a.rs:1 — issue. Flagged by: `codex` (gpt-5.5), **claude** (claude-opus-5)",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5.5"},{"name":"claude","model":"claude-opus-5"}],"count":2}]`,
		},
		{
			"a name inside a link",
			"- [LOW] a.rs:1 — issue. Flagged by: [codex](https://example.com/c) (gpt-5.5)",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1}]`,
		},
		{
			"an empty parenthesis is no model",
			"- [LOW] a.rs:1 — issue. Flagged by: codex ()",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			"a tag that never closes ends the list",
			"- [LOW] a.rs:1 — issue. Flagged by: codex [LOW, claude",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			"a note after two hyphens",
			"- [LOW] a.rs:1 — issue. Flagged by: codex (gpt-5.5) -- the others missed it",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1}]`,
		},
		{
			"a name given twice in one list is kept twice",
			"- [LOW] a.rs:1 — issue. Flagged by: codex, codex",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex"},{"name":"codex"}],"count":2}]`,
		},
		{"a number is not a name", "- [LOW] a.rs:1 — issue. Flagged by: 5.5, codex", `[]`},
	})
}

func TestProseThatIsNotAFindingIsSkipped(t *testing.T) {
	checkParse(t, []parseCase{
		{"the words in a sentence", "Nothing was flagged by anyone.\nFlagged by nobody in particular.", `[]`},
		{"an overview", "### Overview\n**Reviewing:** PR #9", `[]`},
		{"no text", "", `[]`},
		{"blank lines", "\n\n", `[]`},
	})
}

func TestParseNeverReturnsNil(t *testing.T) {
	// The promise is a list for every text. The ledger writes nil as `[]`
	// too, but a caller that compares two results must not get nil for one
	// review with no finding and a list for the next.
	for _, text := range []string{"", "Posted the review with gh.", "- [LOW] a.rs:1 — x. Flagged by: codex"} {
		if Parse(text) == nil {
			t.Errorf("Parse(%q) = nil, want a list", text)
		}
	}
	list := Parse("- [LOW] a.rs:1 — x. Flagged by 2.")
	if list[0].FlaggedBy == nil {
		t.Error("FlaggedBy = nil for a count with no names, want an empty list")
	}
}
