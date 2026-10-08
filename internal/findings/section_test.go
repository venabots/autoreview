package findings

import "testing"

func TestTheSectionGivesABulletWhatItsOwnTextDoesNot(t *testing.T) {
	checkParse(t, []parseCase{
		{
			"the bucket heading supplies a missing severity",
			"### should-fix\n\n- src/a.rs:1 — issue. Flagged by: codex (gpt-5)\n\n### polish\n\n- src/b.rs:2 — nit. Flagged by: codex (gpt-5)",
			`[{"severity":"MEDIUM","location":"src/a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5"}],"count":1},{"severity":"LOW","location":"src/b.rs:2","flagged_by":[{"name":"codex","model":"gpt-5"}],"count":1}]`,
		},
		{
			"disagreements are dropped findings",
			"### must-fix\n- [HIGH] a.rs:1 — real. Flagged by: codex (gpt-5)\n### Disagreements\n- [HIGH] b.rs:2 — not a bug on inspection. Flagged by: claude (claude-opus-5)",
			`[{"severity":"HIGH","location":"a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5"}],"count":1},{"severity":"HIGH","location":"b.rs:2","flagged_by":[{"name":"claude","model":"claude-opus-5"}],"count":1,"dropped":true}]`,
		},
		{
			"the disagreements section ends at the next paragraph",
			"### must-fix\n- [HIGH] src/pay.ts:10 — double charge. Flagged by: codex (gpt-5.5)\n\n### Disagreements\n- [MEDIUM] src/x.ts:1 — not a bug on inspection. Flagged by: claude (claude-opus-5)\n\nPosted 1 inline comment:\n- src/pay.ts:10 — codex (gpt-5.5) found the double charge.",
			`[{"severity":"HIGH","location":"src/pay.ts:10","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1},{"severity":"MEDIUM","location":"src/x.ts:1","flagged_by":[{"name":"claude","model":"claude-opus-5"}],"count":1,"dropped":true}]`,
		},
		{
			"a bucket heading with a count and other letter case",
			"### Must Fix (2)\n- a.rs:1 — x. Flagged by: codex",
			`[{"severity":"HIGH","location":"a.rs:1","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			"a heading of another level",
			"## SHOULD FIX\n- a.rs:1 — x. Flagged by: codex",
			`[{"severity":"MEDIUM","location":"a.rs:1","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			// strings.ToLower turns U+0130 into "i" and would read "polish".
			"only the letters A to Z are lowered in a heading",
			"### POL\u0130SH\n- a.rs:1 — x. Flagged by: codex",
			`[{"location":"a.rs:1","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			"a paragraph ends a bucket too",
			"### Polish\nA paragraph ends the section.\n- a.rs:1 — x. Flagged by: codex",
			`[{"location":"a.rs:1","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{
			"a disagreements heading in capital letters",
			"### DISAGREEMENTS\n- [HIGH] a.rs:1 — x. Flagged by: codex",
			`[{"severity":"HIGH","location":"a.rs:1","flagged_by":[{"name":"codex"}],"count":1,"dropped":true}]`,
		},
	})
}

func TestAFindingStatedTwiceCountsOnce(t *testing.T) {
	const line = "- [LOW] a.rs:1 — issue. Flagged by: codex (gpt-5)"
	const medium = "- [MEDIUM] src/a.rs:1 — the issue. Fix: the change."
	checkParse(t, []parseCase{
		{
			"a requoted finding counts once",
			line + "\n\nlater:\n" + line,
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5"}],"count":1}]`,
		},
		{
			"a restated finding keeps every panelist",
			medium + " Flagged by: codex (gpt-5.5)\n\nlater:\n" + medium + " Flagged by 2: codex (gpt-5.5), claude (claude-opus-5)",
			`[{"severity":"MEDIUM","location":"src/a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5.5"},{"name":"claude","model":"claude-opus-5"}],"count":2}]`,
		},
		{
			"the higher count is kept",
			"- [LOW] a.rs:1 — x. Flagged by 3.\n\n- [LOW] a.rs:1 — x. Flagged by: codex",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex"}],"count":3}]`,
		},
		{
			"a later copy under Disagreements drops the finding",
			"### must-fix\n- [HIGH] a.rs:1 — bug. Flagged by: codex (gpt-5)\n### Disagreements\n- [HIGH] a.rs:1 — bug. Flagged by: codex (gpt-5)",
			`[{"severity":"HIGH","location":"a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5"}],"count":1,"dropped":true}]`,
		},
		{
			"two wrapped findings stay separate",
			"### should-fix\n- [MEDIUM] src/a.rs:1 — first bug.\n  Flagged by: codex (gpt-5)\n\n- [MEDIUM] src/a.rs:1 — second, different bug.\n  Flagged by: claude (claude-opus-5)",
			`[{"severity":"MEDIUM","location":"src/a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5"}],"count":1},{"severity":"MEDIUM","location":"src/a.rs:1","flagged_by":[{"name":"claude","model":"claude-opus-5"}],"count":1}]`,
		},
	})
}

func TestABulletThatWrapsIsReadAsOneLine(t *testing.T) {
	checkParse(t, []parseCase{
		{
			"a bullet that wraps across lines keeps every panelist",
			"### must-fix\n- [HIGH] src/a.rs:42 — the issue runs long\n  and wraps onto the next line. Fix: do the thing.\n  Flagged by 2: claude\n  (claude-opus-4.7), opencode (qwen3.6-plus)",
			`[{"severity":"HIGH","location":"src/a.rs:42","flagged_by":[{"name":"claude","model":"claude-opus-4.7"},{"name":"opencode","model":"qwen3.6-plus"}],"count":2}]`,
		},
		{
			"lines that end with a carriage return",
			"- [LOW] a.rs:1 — x.\r\n  Flagged by: codex (gpt-5)\r\n",
			`[{"severity":"LOW","location":"a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5"}],"count":1}]`,
		},
		{
			"a blank line ends the bullet",
			"- [LOW] a.rs:1 — x.\n\nFlagged by: codex (gpt-5)",
			`[{"flagged_by":[{"name":"codex","model":"gpt-5"}],"count":1}]`,
		},
		{
			"a heading ends the bullet",
			"- [LOW] a.rs:1 — x.\n### polish\nFlagged by: codex (gpt-5)",
			`[{"flagged_by":[{"name":"codex","model":"gpt-5"}],"count":1}]`,
		},
		{
			"the next bullet ends the bullet",
			"- [LOW] a.rs:1 — x.\n- [LOW] b.rs:2 — y.\n  Flagged by: codex (gpt-5)",
			`[{"severity":"LOW","location":"b.rs:2","flagged_by":[{"name":"codex","model":"gpt-5"}],"count":1}]`,
		},
	})
}

func TestAFindingQuotedInACodeFenceIsNotCounted(t *testing.T) {
	checkParse(t, []parseCase{
		{
			"a fenced example",
			"The shape is:\n```md\n- [HIGH] a.rs:1 — bug. Flagged by: codex (gpt-5.5)\n```\nThat is the format.",
			`[]`,
		},
		{
			"a real finding outside the fence is still read",
			"### must-fix\n- [HIGH] a.rs:1 — real. Flagged by: codex (gpt-5.5)\n\nExample:\n```md\n- [LOW] b.rs:2 — example. Flagged by: claude (claude-opus-5)\n```",
			`[{"severity":"HIGH","location":"a.rs:1","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1}]`,
		},
		{
			"a fence with space in front of it",
			"  ```md\n- [LOW] a.rs:1 — x. Flagged by: codex\n  ```\n- [LOW] b.rs:2 — y. Flagged by: codex",
			`[{"severity":"LOW","location":"b.rs:2","flagged_by":[{"name":"codex"}],"count":1}]`,
		},
		{"a fence that never closes hides the rest", "```\n- [LOW] a.rs:1 — x. Flagged by: codex\n", `[]`},
	})
}
