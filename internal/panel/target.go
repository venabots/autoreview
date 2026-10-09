package panel

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
	"github.com/venabots/autoreview/internal/worktree"
)

// TargetKind is which of the shapes of review a run is.
//
// The kind decides more than the diff. Uncommitted work exists only in the
// person's own checkout, so panelists read it there and touch nothing.
// Committed work has a commit, so each panelist gets a worktree of its own
// and may run the tests.
type TargetKind int

const (
	Uncommitted TargetKind = iota
	Staged
	Base
	// Tree is the code at HEAD with no change: an audit.
	Tree
)

// Target is what the flags asked to review.
type Target struct {
	Kind TargetKind
	// Ref is the base of a Base target.
	Ref string
	// Path narrows a Tree target, or is "" for the whole repository.
	Path string
}

// Subject is what the panelists are shown. A diff says what changed. With
// no diff there is nothing to hand over but the names of the files.
type Subject struct {
	Diff string
	// Audit is true for a tree audit, which has Files and no Diff. The
	// files are named, not carried: a repository does not fit in a prompt,
	// and a panelist with read tools can open them.
	Audit bool
	Files []string
	// Scope is the path the audit was narrowed to, or "".
	Scope string
}

// Resolved is a target with everything the run needs to review it.
type Resolved struct {
	// Label is what the report says it reviewed.
	Label   string
	Subject Subject
	// Isolated is true when each panelist gets its own worktree and may
	// run commands.
	Isolated bool
	// SHA is the commit every worktree is pinned to, or "" for a target in
	// the working tree.
	SHA string
	// Untracked is the files git does not track. No diff covers them, so
	// the panelists are told to read them from the tree.
	Untracked []string
}

// gitIn is a Git bound to one directory and one context, which every call
// in a resolution shares.
type gitIn struct {
	ctx context.Context
	git worktree.Git
	dir string
}

// out runs git and returns its stdout. A failure names the command, so
// "nothing to review" is never reported for a git that did not run.
func (g gitIn) out(args ...string) (string, error) {
	raw, err := g.git(g.ctx, g.dir, args...)
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w", strings.Join(args, " "), err)
	}
	return string(raw), nil
}

// ok runs a git whose failure is an answer, not an error: `rev-parse
// --verify` fails for a ref that is not there. An ended context is still
// an error, or a ctrl-C would be reported as a missing commit.
func (g gitIn) ok(args ...string) (string, bool, error) {
	raw, err := g.git(g.ctx, g.dir, args...)
	if g.ctx.Err() != nil {
		return "", false, g.ctx.Err()
	}
	return string(raw), err == nil, nil
}

// ResolveTarget works out what the target reviews. root is the repository root;
// cwd is where the person stands, which a relative --tree path is read from.
func ResolveTarget(ctx context.Context, git worktree.Git, t Target, root, cwd string) (Resolved, error) {
	g := gitIn{ctx: ctx, git: git, dir: root}
	if t.Kind == Tree {
		// It has no diff, so the empty-diff advice below has nothing to
		// say about it.
		return resolveTree(g, gitIn{ctx: ctx, git: git, dir: cwd}, t.Path)
	}
	r, err := resolveDiff(g, t)
	if err != nil {
		return Resolved{}, err
	}
	if strings.TrimSpace(r.Subject.Diff) == "" {
		return Resolved{}, emptyDiff(g, t, r.Label)
	}
	return r, nil
}

func resolveDiff(g gitIn, t Target) (Resolved, error) {
	switch t.Kind {
	case Staged:
		diff, err := g.out("diff", "--cached")
		// Nothing untracked is in the index, so nothing untracked is part
		// of what this target reviews.
		return Resolved{Label: "staged changes on " + branch(g), Subject: Subject{Diff: diff}}, err
	case Base:
		return resolveBase(g, t.Ref)
	}
	// Against HEAD and not the index, so staged and unstaged edits both
	// reach the panel: "what I have not committed yet" is the whole of it.
	diff, err := g.out("diff", "HEAD")
	if err != nil {
		return Resolved{}, err
	}
	untracked, err := untrackedFiles(g)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{Label: "uncommitted changes on " + branch(g), Subject: Subject{Diff: diff}, Untracked: untracked}, nil
}

func resolveBase(g gitIn, base string) (Resolved, error) {
	// A base that starts with a dash reaches git as an option: with
	// `--output=<path>` git writes the diff to a file and prints nothing,
	// and the run would stop with "nothing to review", the wrong cause.
	if strings.HasPrefix(base, "-") {
		return Resolved{}, fmt.Errorf("--base expects a ref, and %q starts with a dash", base)
	}
	_, found, err := g.ok("rev-parse", "--verify", "--quiet", base+"^{commit}")
	if err != nil {
		return Resolved{}, err
	}
	if !found {
		return Resolved{}, fmt.Errorf("--base: no commit named %q in this repository", base)
	}
	// Three dots: what this branch added, not every change on the base
	// since the fork. The second would flag other people's commits as this
	// branch's work.
	diff, err := g.out("diff", base+"...HEAD")
	if err != nil {
		return Resolved{}, err
	}
	sha, err := g.out("rev-parse", "HEAD")
	if err != nil {
		return Resolved{}, err
	}
	counted, err := g.out("rev-list", "--count", base+"..HEAD")
	if err != nil {
		return Resolved{}, err
	}
	commits, _ := strconv.Atoi(strings.TrimSpace(counted))
	return Resolved{
		Label:    fmt.Sprintf("%s on %s vs %s", words.Count(commits, "commit"), branch(g), sanitize.Line(base)),
		Subject:  Subject{Diff: diff},
		Isolated: true,
		// The commit the worktrees pin to. Anything untracked is not in it.
		SHA: strings.TrimSpace(sha),
	}, nil
}

// emptyDiff is the error for a target with nothing in it. An empty diff is
// not a review anyone wants: every panelist would spend a model call to
// report nothing.
//
// A change that only adds files is the common way here, and "the diff is
// empty" is a baffling thing to read while looking at the new files, so
// they are named. They are asked for here, best effort: the run is already
// failing, and a second failure must not replace the reason.
func emptyDiff(g gitIn, t Target, label string) error {
	untracked, _ := untrackedFiles(g)
	if len(untracked) == 0 {
		return fmt.Errorf("nothing to review: the diff for %s is empty", label)
	}
	noun, verb := "files", "are"
	if len(untracked) == 1 {
		noun, verb = "file", "is"
	}
	// Staging is enough for a working-tree diff. A diff against a base
	// sees only what is committed.
	advice := "add them with `git add` first"
	if t.Kind == Base {
		advice = "commit them first"
	}
	return fmt.Errorf("nothing to review: the diff for %s is empty. %d %s %s not tracked by git yet, so no diff covers them: %s. To review them, %s",
		label, len(untracked), noun, verb, summarize(untracked), advice)
}

// untrackedFiles is every file git does not track. The error is returned,
// not read as "there are none": a review that quietly leaves out every new
// file because one git call failed is worse than one that does not start.
//
// -z, so git does not quote a name with an unusual character into a form
// that no read tool can open.
func untrackedFiles(g gitIn) ([]string, error) {
	raw, err := g.out("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	return nulFields(raw), nil
}

// nulFields splits NUL-separated output. Only empty fields are dropped: " "
// is a legal file name, and dropping it would leave a file nobody reviews.
func nulFields(raw string) []string {
	var out []string
	for field := range strings.SplitSeq(raw, "\x00") {
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

// summarize is a list for a message that says how many it did not name,
// and does not stop in silence. The names go to a terminal, and -z took
// git's quoting off them, so each is sanitized.
func summarize(files []string) string {
	const shown = 10
	names := make([]string, 0, min(len(files), shown))
	for _, f := range files[:min(len(files), shown)] {
		names = append(names, sanitize.Line(f))
	}
	list := strings.Join(names, ", ")
	if len(files) <= shown {
		return list
	}
	return fmt.Sprintf("%s, and %d more", list, len(files)-shown)
}

// branch is the branch the person is on, or "HEAD". A branch name can come
// from anyone who pushed one, so it is sanitized before a label carries it
// to the terminal.
func branch(g gitIn) string {
	out, err := g.out("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "HEAD"
	}
	return sanitize.Line(strings.TrimSpace(out))
}
