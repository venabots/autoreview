package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Prepare makes the worktree for a task on r.PR, or says why not. The error
// is for the footer, so it names the PR, and it is a sentence a person
// reads.
//
// The checks run in this order so that nothing reaches git that git could
// take as an option, and nothing is fetched for a PR that would be refused
// anyway.
func Prepare(ctx context.Context, git Git, r Request) (Prepared, error) {
	if err := refuseBeforeGit(r); err != nil {
		return Prepared{}, err
	}
	if err := fetch(ctx, git, r); err != nil {
		return Prepared{}, err
	}
	at, err := checkedOutAt(ctx, git, r.Root, r.Branch)
	if err != nil {
		return Prepared{}, err
	}
	if at != "" {
		return Prepared{}, fmt.Errorf("PR #%d's branch %s is checked out at %s; switch it away first", r.PR, r.Branch, at)
	}
	remote := "origin/" + r.Branch
	local := "refs/heads/" + r.Branch
	_, verifyErr := run(ctx, git, r.Root, "rev-parse", "--verify", "--quiet", local)
	exists := verifyErr == nil
	if exists {
		if _, err := run(ctx, git, r.Root, "merge-base", "--is-ancestor", local, remote); err != nil {
			return Prepared{}, fmt.Errorf("your local branch %s has commits that are not on origin; push or move them first", r.Branch)
		}
	}
	// A branch of yours that tracks somewhere else would push the task's
	// work there, and changing what it tracks is not this run's to do.
	upstream, trackErr := run(ctx, git, r.Root, "rev-parse", "--abbrev-ref", r.Branch+"@{upstream}")
	tracked := trackErr == nil
	if tracked && upstream != remote {
		return Prepared{}, fmt.Errorf("your local branch %s tracks %s, not %s", r.Branch, upstream, remote)
	}
	return create(ctx, git, r, remote, exists, !tracked)
}

// refuseBeforeGit turns away what no git command can help with. A branch
// that starts with "-" would reach git as an option.
func refuseBeforeGit(r Request) error {
	if r.CrossRepo {
		return fmt.Errorf("PR #%d is from a fork; its branch is not on origin", r.PR)
	}
	if r.Branch == "" || strings.HasPrefix(r.Branch, "-") {
		return fmt.Errorf("PR #%d has no branch this run can check out", r.PR)
	}
	if exists(r.Path) {
		return fmt.Errorf("PR #%d's worktree from an earlier task is still at %s", r.PR, r.Path)
	}
	return nil
}

// fetch brings the PR's branch from origin within FetchTimeout, or says why
// not in git's words.
func fetch(ctx context.Context, git Git, r Request) error {
	fctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	refspec := "+refs/heads/" + r.Branch + ":refs/remotes/origin/" + r.Branch
	_, err := git(fctx, r.Root, "fetch", "--quiet", "origin", refspec)
	if err == nil {
		return nil
	}
	why := err.Error()
	if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		why = fmt.Sprintf("git fetch took longer than %ds", int(FetchTimeout.Seconds()))
	}
	return fmt.Errorf("could not fetch PR #%d's branch %s: %s", r.PR, r.Branch, why)
}

// checkedOutAt is where a worktree has branch checked out, or "".
//
// A record whose directory is gone, from a worktree this tool made, is
// removed: one left by an earlier run would otherwise block the PR for
// ever. Only that record goes. Any other missing worktree may be on a disk
// that is not mounted right now, and is not this run's to drop.
func checkedOutAt(ctx context.Context, git Git, root, branch string) (string, error) {
	list, err := run(ctx, git, root, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	wanted := "branch refs/heads/" + branch
	path := ""
	for line := range strings.SplitSeq(list, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			path = p
			continue
		}
		if line != wanted || path == "" {
			continue
		}
		return judgeRecord(ctx, git, root, branch, path)
	}
	return "", nil
}

// judgeRecord decides what a worktree record of the branch means: a live
// checkout, a stale one of ours to drop, or a missing one to report.
func judgeRecord(ctx context.Context, git Git, root, branch, path string) (string, error) {
	switch {
	case exists(path):
		return path, nil
	case madeHere(path):
		if _, err := run(ctx, git, root, "worktree", "remove", "--force", path); err != nil {
			return "", err
		}
		return "", nil
	default:
		return "", fmt.Errorf("%s is checked out at %s, which is not there; if it is gone for good, run git worktree prune", branch, path)
	}
}

// create adds the worktree and finishes it. A hook can fail after the
// worktree is made; either way, nothing is left behind that would refuse
// the next task on this PR.
func create(ctx context.Context, git Git, r Request, remote string, existed, untracked bool) (Prepared, error) {
	var err error
	if existed {
		_, err = run(ctx, git, r.Root, "worktree", "add", "--quiet", r.Path, r.Branch)
	} else {
		_, err = run(ctx, git, r.Root, "worktree", "add", "--quiet", "--track", "-b", r.Branch, r.Path, remote)
	}
	if err == nil {
		var head string
		head, err = finishSetup(ctx, git, r, remote, existed, untracked)
		if err == nil {
			return Prepared{Path: r.Path, Branch: r.Branch, Head: head, CreatedBranch: !existed}, nil
		}
	}
	undo(ctx, git, r, !existed)
	return Prepared{}, fmt.Errorf("could not make a worktree for PR #%d: %w", r.PR, err)
}

// finishSetup brings a worktree made on an existing branch up to origin,
// and reads the commit it starts at. The branch is behind origin, never
// ahead (checked before), so the merge only moves it forward.
func finishSetup(ctx context.Context, git Git, r Request, remote string, existed, untracked bool) (string, error) {
	if existed {
		if _, err := run(ctx, git, r.Path, "merge", "--quiet", "--ff-only", remote); err != nil {
			return "", fmt.Errorf("could not bring %s up to origin: %w", r.Branch, err)
		}
		if untracked {
			if _, err := run(ctx, git, r.Path, "branch", "--quiet", "--set-upstream-to", remote); err != nil {
				return "", fmt.Errorf("could not track %s: %w", remote, err)
			}
		}
	}
	head, err := run(ctx, git, r.Path, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("could not read its head: %w", err)
	}
	return head, nil
}

// undo takes back a worktree that Prepare made and could not finish, and
// the branch with it when Prepare made that too. Lowercase -d: a branch
// that appeared between the check and the add is not ours, and -d refuses
// one that holds anything origin lacks.
func undo(ctx context.Context, git Git, r Request, createdBranch bool) {
	_, _ = git(ctx, r.Root, "worktree", "remove", "--force", r.Path)
	if createdBranch {
		_, _ = git(ctx, r.Root, "branch", "--quiet", "-d", r.Branch)
	}
}

// exists is true when something is at path. As in v1, an error reading it
// counts as nothing there.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
