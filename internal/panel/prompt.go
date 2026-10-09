package panel

import (
	"fmt"
	"strings"

	bundle "github.com/venabots/autoreview"
	"github.com/venabots/autoreview/internal/sanitize"
)

// The templates are compiled in. A panel whose prompt changed on disk under
// it would make two runs incomparable for no visible reason.
var (
	reviewTemplate         = bundle.Prompt("review.md")
	auditTemplate          = bundle.Prompt("audit.md")
	synthesisTemplate      = bundle.Prompt("synthesis.md")
	auditSynthesisTemplate = bundle.Prompt("audit-synthesis.md")
)

// The words around the template are v1's.
const (
	isolatedWorkspace       = "You are running inside a dedicated git worktree pinned to this review's target commit -- the actual checkout, not a free-floating diff. You may:\n\n- Read any file in the tree.\n- Grep / rg across the tree to find callers of changed symbols.\n- Edit files locally (the worktree is thrown away on exit).\n- Run build / test / lint commands to investigate downstream effects.\n- Install dev dependencies if a test runner needs them; bound test runs to ~3 minutes.\n- A failing test is a high-signal finding; surface it under Evidence.\n\nUse that capability when it sharpens a finding. Do NOT do investigation theatre -- only run tools when they harden the report. Local writes inside the worktree are fine; the external-network and GitHub-write rules in Hard Constraints still apply."
	readOnlyWorkspace       = "You are running in the user's actual working tree with **read-only** access. You may:\n\n- Read any file using your built-in read tools (Read / Glob / Grep).\n- Reason about the diff and the surrounding code.\n\nDo NOT modify files, run tests, install packages, or execute shell commands that change state. The `Evidence:` line in the finding shape is not meaningful in this mode -- leave it out.\n\nThe tree is the user's live checkout, so it may hold edits beyond the diff below (a --staged review diffs the index, not the tree). Where a file disagrees with the diff, the diff is what you were asked to review."
	untrackedIntro          = "These files are in the tree but not tracked by git, so no diff covers them. Some may be part of the change and some may be local scratch files -- read one when the diff or the focus points at it, and do not review the rest:\n\n"
	maxFilesShown           = 200
	maxUntrackedShown       = 40
	minFence                = 3
	fileListHeading         = "## Files under review\n\n"
	untrackedOverflowFormat = "\nand %d more untracked files not listed here.\n"
)

// workspace says what the panelist may do. It calibrates the findings: a
// reviewer told it can run the tests reports a failing test as evidence,
// and one told it cannot must not claim it ran anything.
func workspace(isolated bool) string {
	if isolated {
		return isolatedWorkspace
	}
	return readOnlyWorkspace
}

// BuildPrompt is the prompt every panelist reads: the shared template, then
// what this run is looking at.
func BuildPrompt(label string, isolated bool, focus string, subject Subject, untracked []string) string {
	var b strings.Builder
	if subject.Audit {
		b.WriteString(auditTemplate)
	} else {
		b.WriteString(reviewTemplate)
	}
	b.WriteString("\n\n## Review target\n\n" + label)
	b.WriteString("\n\n## Workspace\n\n" + workspace(isolated))
	if len(untracked) > 0 {
		b.WriteString("\n\n" + untrackedNote(untracked))
	}
	if focus != "" {
		b.WriteString("\n\n## Reviewer focus\n\n" + focus)
	}
	if subject.Audit {
		b.WriteString("\n\n" + filesSection(subject.Scope, subject.Files))
	} else {
		b.WriteString("\n\n## Diff\n\n" + fencedDiff(subject.Diff))
	}
	return b.String()
}

// filesSection is the files an audit covers. The synthesis prompt uses it
// too, so the synthesizer checks against the list the panelists were given.
//
// Capped: a large repository has tens of thousands of files, and the list
// is a map, not the territory. A panelist with read tools finds the rest
// under the same scope.
func filesSection(scope string, files []string) string {
	var b strings.Builder
	b.WriteString(fileListHeading)
	if scope != "" {
		fmt.Fprintf(&b, "Only the files under `%s` are in scope. Read code outside it only to follow a call.\n\n", codeSpanSafe(scope))
	}
	for _, f := range files[:min(len(files), maxFilesShown)] {
		fmt.Fprintf(&b, "- `%s`\n", codeSpanSafe(f))
	}
	if len(files) > maxFilesShown {
		fmt.Fprintf(&b, "\nand %d more files not listed here. Find them with your read tools.\n", len(files)-maxFilesShown)
	}
	return b.String()
}

// untrackedNote is what both prompts say about the files git does not
// track, so the two cannot drift apart.
//
// Not "these are part of the change": `git ls-files --others` lists every
// untracked file in the tree, scratch files and local notes too. A
// read-only reviewer cannot tell them apart, and told otherwise would
// review the person's junk drawer.
func untrackedNote(untracked []string) string {
	var b strings.Builder
	b.WriteString(untrackedIntro)
	for _, f := range untracked[:min(len(untracked), maxUntrackedShown)] {
		fmt.Fprintf(&b, "- `%s`\n", codeSpanSafe(f))
	}
	if len(untracked) > maxUntrackedShown {
		// A list that stops without saying so reads as complete.
		fmt.Fprintf(&b, untrackedOverflowFormat, len(untracked)-maxUntrackedShown)
	}
	return b.String()
}

// codeSpanSafe makes a file name safe inside a code span on a line of its
// own: no newline to start a line outside it, no backtick to close it.
func codeSpanSafe(name string) string {
	return strings.ReplaceAll(sanitize.Line(name), "`", "'")
}

// fencedDiff is the diff in a fence longer than any backtick run inside it.
// A context line that is itself ``` would otherwise close the block early,
// and the rest of the diff would read as prose.
func fencedDiff(diff string) string {
	fence := fenceFor(diff)
	block := fence + "diff\n" + diff
	if !strings.HasSuffix(diff, "\n") {
		block += "\n"
	}
	return block + fence + "\n"
}

// fenceFor is a fence longer than any backtick run in s, so nothing s holds
// can close the block. The synthesis prompt fences the untrusted panelist
// reports the same way.
func fenceFor(s string) string {
	return strings.Repeat("`", max(longestBacktickRun(s)+1, minFence))
}

func longestBacktickRun(s string) int {
	longest, current := 0, 0
	for _, r := range s {
		if r != '`' {
			current = 0
			continue
		}
		current++
		longest = max(longest, current)
	}
	return longest
}
