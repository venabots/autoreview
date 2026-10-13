package findings

import "testing"

func TestADisagreementCountsOnlyAgainstThePanelistThatWasOverruled(t *testing.T) {
	checkParse(t, []parseCase{
		{
			"a falsified prose disagreement debits its raiser",
			"### Disagreements\n" +
				"- `apps/web/src/lib/evidence-narrative.ts:760-801` — codex (gpt-5.6-sol) claimed the identity clause ignores `policyGrants`. Verification falsified this. Dropped.\n" +
				"- [packages/svm/src/deposits.ts:652-655](https://github.com/o/r/pull/1/files#diff-abcR652) — codex-gpt-5.6-sol (gpt-5.6-sol) rated the case HIGH; claude re-traced it and disproved the vector.\n" +
				"- `kyx_foundation.ts:302` — grok-4.6 rated the missing index MEDIUM; I downgraded it to polish.",
			`[{"location":"apps/web/src/lib/evidence-narrative.ts:760-801","flagged_by":[{"name":"codex","model":"gpt-5.6-sol"}],"count":1,"dropped":true},` +
				`{"location":"packages/svm/src/deposits.ts:652-655","flagged_by":[{"name":"codex-gpt-5.6-sol","model":"gpt-5.6-sol"}],"count":1,"dropped":true},` +
				`{"location":"kyx_foundation.ts:302","flagged_by":[{"name":"grok-4.6"}],"count":1,"dropped":true}]`,
		},
		{
			// Nobody was overruled, so nobody is debited.
			"an unresolved split is not a dropped finding",
			"### Disagreements\n- `a.ts:1` — codex (gpt-5.5) rated it HIGH, but claude (claude-opus-5) was not sure. Left for the author.",
			`[]`,
		},
		{
			// "said" is the word of a defender. It is not a raise.
			"a defender named first is not debited",
			"### Disagreements\n- `a.ts:1` — claude (claude-opus-5) said the guard is sufficient and disproved the concern. codex (gpt-5.5) rated it HIGH.",
			`[]`,
		},
		{
			// Only codex raised the claim. The bullet names a defender
			// first, so it is skipped, and claude is not debited.
			"a disagreement credits the claimant, not the defender",
			"### Disagreements\n- `src/a.ts:1` — claude (claude-opus-5) and opencode (glm-5.3) both read the path and found the guard sufficient. codex (gpt-5.5) rated it HIGH.",
			`[]`,
		},
		{
			"a star bullet, with a colon after the location",
			"### Disagreements\n* a.ts:1: codex (gpt-5.5) flagged a race. It does not hold.",
			`[{"location":"a.ts:1","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1,"dropped":true}]`,
		},
		{
			"two hyphens after the location",
			"### Disagreements\n- a.ts:1 -- codex (gpt-5.5) argued the lock leaks. Overruled.",
			`[{"location":"a.ts:1","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1,"dropped":true}]`,
		},
		{
			"a tag before the location is the severity",
			"### Disagreements\n- [HIGH] b.rs:2 — codex (gpt-5.5) rated it HIGH. Verification disproved it.",
			`[{"severity":"HIGH","location":"b.rs:2","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1,"dropped":true}]`,
		},
		{
			"the verbs are read in any letter case",
			"### Disagreements\n- a.ts:1 — codex (gpt-5.5) CLAIMED a leak. It is NOT A BUG.",
			`[{"location":"a.ts:1","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1,"dropped":true}]`,
		},
		{
			"a bullet with nothing between the location and the claim",
			"### Disagreements\n- a.ts:1 codex (gpt-5.5) claimed a leak, falsified",
			`[]`,
		},
		{
			"a bullet that names no panelist",
			"### Disagreements\n- a.ts:1 — nobody claimed anything. Dropped.",
			`[]`,
		},
		{
			"the same disagreement twice counts once",
			"### Disagreements\n\n- a.ts:1 — codex (gpt-5.5) claimed a leak. Verification falsified this.\n\n- a.ts:1 — codex (gpt-5.5) claimed a leak. Verification falsified this.",
			`[{"location":"a.ts:1","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1,"dropped":true}]`,
		},
	})
}
