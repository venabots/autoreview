# Exit 10 is retried under another orchestrator

Recorded: 2026-09-10
Status: accepted

## Context

An Anthropic outage made every review in a sweep exit 10 within seconds, and
the run exited 1 having reviewed nothing. The same code had appeared before,
when an account hit its usage limit.

Exit 10 is dash-p's `agent-error`: the agent session failed. It says nothing
about the PR. The reviews were not wrong, and the diffs were not unreviewable
-- one provider was unavailable, and every review was pointed at it because
the reviewing agent was hard-coded to claude.

Two failures were being conflated. A panelist that dies is already handled:
`panel-review` marks it missing, the synthesis is told not to count it, and
`auto-review`'s gate withholds approval below 75% coverage. A review is
thinner, and it still lands. But the agent _driving_ that panel has no such
treatment -- when it dies, nothing happens at all.

## Decision

The driving agent is a named thing, `crate::orchestrator::Orchestrator`, and
exit 10 -- only exit 10 -- retries the review once under a different one.

- **`--orchestrator claude|codex[:model]`** chooses it. The panel is still
  chosen by the skill; this is the session that runs the skill.
- **`--fallback`** chooses the stand-in, defaulting to whichever of codex and
  claude is not the orchestrator and is installed. A run says both on its
  first line, because an operator who learns their stand-in from the pass
  that needed it learns it at the worst time.
- **Only exit 10.** A timeout already spent the whole allowance and retrying
  would spend it twice. A signal-death was somebody's decision. An override
  (`$AUTOREVIEW_AUTO_CMD`) has no dash-p behind it to say what its status
  means.
- **Once.** The retry is one more chance, not a promise. If both fail, the run
  fails and the summary names both attempts.
- **A retry is a fresh review.** The failed attempt's session belongs to the
  provider that just failed and cannot be handed to another backend.

What dash-p forwards decides the rest, and every difference is stated rather
than discovered: `--session-id`, `--resume`, `--max-budget-usd` and
`--append-system-prompt` reach claude alone. Under codex the trailer request
moves into the prompt so the summary keeps its risk, findings and panel
columns; the session and budget flags are not sent at all, and the run says so
at startup rather than letting a cap silently not apply.

The skills are the other asymmetry. A run stages them as a `.claude/skills`
directory handed over with `--add-dir`, which claude resolves and codex does
not -- codex reads its own roots. Verified against both CLIs rather than
assumed. So a codex orchestrator is refused before it spends anything unless
the skills are installed where it looks, and a codex _fallback_ that cannot
find them is dropped with a note instead of ending the run.

## Consequences

- A provider outage costs a retry, not a sweep.
- A codex run cannot resume, so `--continue` and every babysit pass after the
  first review from scratch.
- The panel is drawn from the same CLIs, so an outage still costs that
  provider's panelist. Coverage drops under the 75% the approval gate needs:
  reviews post, approvals wait. That is the intended trade -- an approval is
  the one thing that should not be given on thin coverage.
- Two orchestrators is the supported set. opencode reviews as a panelist but
  is not driven here, because nothing hands it a skill by name.
