# A tree review audits HEAD

Recorded: 2026-09-28
Status: accepted

## Context

Every panel target was a diff. There was no way to ask the panel about code as
it is now, for example a repository that was just cloned. The workaround was
an orphan branch whose second commit added every file, so `--base HEAD~1`
showed the whole tree as new. That puts the whole tree in each prompt, and the
prompt still asks for a change's goal and approach.

## Decision

`panel --tree [PATH]` audits the committed files at HEAD, or only those under
PATH.

- HEAD, not the working tree. HEAD has a commit to pin, so each panelist gets a
  worktree and may run the tests. The target line names the commit, so a user
  with uncommitted edits can see they are not part of it.
- The prompt names the files and does not carry them. A large repository does
  not fit in a prompt, and a panelist with read tools can open what it needs.
  The list stops at 200 names and says how many it left out.
- The audit has its own templates for the panelists and the synthesis. The
  change-review templates ask for a goal and an approach, which an audit does
  not have. The output contract is the same (`Model:`, the finding shape,
  `NO_FINDINGS`, the synthesis headings), so the ledger and the report parse
  both.
- The audit synthesis does not read a finding that only one panelist raised as
  a disagreement. Each panelist chooses where to spend its time, so the others
  may not have read that code.
- PATH is read from the directory the user runs `panel` from, so
  `panel --tree .` inside `src/` audits `src/`.

## Consequences

- An audit of a large repository is only as deep as each panelist chooses to
  go. The prompt tells it to start where a defect costs most and to say what it
  did not reach. A narrower PATH gives a deeper audit.
- The two pairs of templates can drift. Unit tests pin the labels they must
  share.
