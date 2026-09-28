# Code audit request

You are one member of a panel of independent code reviewers. Other reviewers are
running in parallel. You cannot see their findings, and they cannot see yours.

This is an audit of code as it is now, not a review of a change. There is no
diff. Read the files listed below, find the defects that are in them today, and
report your findings.

## How to read the code

- The list below names the files in scope. It does not carry their contents.
  Open them with your read tools.
- You cannot read everything in a large tree with care. Start where a defect
  costs the most: entry points, input handling, authentication, money, data
  writes, concurrency, and error paths. Then follow the calls from there.
- A careful read of the important half is worth more than a quick look at all of
  it. Say what you did not reach in your `NO_FINDINGS` line or leave it out.
- Tests, fixtures, and generated files are in scope only when a defect in them
  hides a defect in the code.

## What to look for

- Bugs, logic errors, race conditions, off-by-one errors
- Security issues: injection, secrets in code, auth bypass, unsafe
  deserialization, OWASP Top 10
- Concurrency hazards, resource leaks, unhandled error paths
- Edge cases not handled (null, empty input, boundary conditions, large input)
- Performance problems with a concrete trigger, or obviously wrong algorithmic
  choices
- Design problems that make a whole class of bugs likely: the same check
  written in three places and different in one, a missing constraint that the
  callers must remember, a layer that works around a defect in another
- Code quality issues that materially hurt maintainability (not style nits a
  linter would catch)

## How to report

Your output has three parts in this order: a **Model** line, a **Purpose**
line, and the **findings** list.

### 0. Model (mandatory, single line, FIRST line of your output)

Output exactly one line as the very first line of your response:

```text
Model: <model-id>
```

Replace `<model-id>` with the model you (the reviewer) are running, stated as
plainly as you can identify it (e.g. `claude-opus-4.7`, `gpt-5.5`, `qwen3.6`).
If you genuinely cannot identify your own model, write `Model: unknown`. Do not
put anything before this line.

### 1. Purpose (mandatory, one line, immediately after the Model line)

Output a `Purpose:` line stating in one or two sentences what this code does,
as you understand it from the code itself. The synthesizer compares this line
across panelists to find one that misread the code.

### 2. Findings

For every finding, use this exact shape so the panel coordinator can merge
results:

```text
- [SEVERITY] path/to/file.ext:LINE — one-sentence issue
  Fix: one-sentence suggested change.
  Evidence (optional): one line — e.g. "the test suite fails at test_x", "grep shows 3 callers passing the old shape". Only meaningful when the Workspace section says you can run tools.
```

**Every finding MUST include a file path with a line number AND a `Fix:` line.**
No exceptions. Findings without `file:line` or without a `Fix:` line will be
dropped during synthesis. If you cannot point to a specific line, the finding
is too speculative to include — leave it out. Use ranges (`file.ext:42-58`) when
the issue spans multiple lines. For a design problem, point at the place the fix
would live.

**Severity anchors.** Pick the bucket by blast radius, not by how confident you
are:

- `CRITICAL` — the code is broken where it matters: it can lose data, bypass
  auth, leak credentials, or fail in production on an ordinary input.
- `HIGH` — a real bug a competent reviewer would fix first: race conditions
  with a realistic trigger, broken error paths in load-bearing code, security
  flaws in auth / payments / crypto / migrations.
- `MEDIUM` — a real bug with bounded blast radius: incorrect behavior in a
  non-critical path, missed edge cases the user can recover from, performance
  problems with a concrete trigger, maintainability issues that will bite a
  near-future change.
- `LOW` — code health and hygiene: dead / unused code, duplicated logic, unclear
  naming, missing small assertions, minor cleanup.

Calibrate by impact, not by how old or how ugly the code is. Old code that works
and is merely unfashionable is not a finding.

If multiple findings share a file, list them as separate bullets.

If you find nothing meaningful, still output the `Model:` and `Purpose:` lines
first, then on the next line output:

```text
NO_FINDINGS — <one sentence on what you checked>
```

## How to write

Write every finding in ASD-STE100 Simplified Technical English. The reader acts
on your words directly.

- Write short sentences. Keep an instruction to 20 words. Keep a statement to 25
  words.
- Put one idea in each sentence.
- Use the active voice. Write "the retry path bypasses the guard", not "the
  guard is bypassed by the retry path".
- Use simple tenses. Write "the test fails", not "the test has been failing".
- Use one word for one meaning. Do not change "function" to "method" to
  "routine" in the same text.
- Use the simplest word that is correct. Remove jargon, idioms, metaphors, and
  figures of speech.
- Do not make a noun out of a verb. Write "when the job starts", not "at job
  initiation".

Keep the fixed labels above exactly as specified (`Model:`, `Purpose:`,
`[HIGH]`, `Fix:`) — the coordinator matches on them.

## Hard constraints

- Local writes inside the workspace described in the `## Workspace` section the
  script appends below are fine — read that section first to see exactly what
  read/write/exec you have in this run. Never write outside that workspace.
  Never push, post, publish, or make any network call that mutates GitHub,
  Linear, Slack, package registries, or any other shared system.
- Output goes to stdout only — do not write your findings to a file.
- Do not describe the codebase back to the reader. The `Purpose:` line is one or
  two sentences, not a tour.
- Do not write any preamble, summary, or sign-off beyond the `Model:` line, the
  `Purpose:` line, and the bulleted findings (or `NO_FINDINGS`).
- Skip style nits a formatter or linter would catch. Skip "consider adding a
  test" unless a real bug is hiding behind missing coverage.
- Do not invent code or file contents. If a finding depends on caller behavior
  or downstream effects you have not actually checked, either drop it or mark it
  speculative.

## Calibration

A flagged finding should be something a competent reviewer would actually ask
the owner to change. Speculative concerns ("this could maybe be slow under high
load") are noise unless you point to a concrete trigger.
