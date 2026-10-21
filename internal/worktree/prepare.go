package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// repoLock lets one task at a time change the person's repo. Making a
// worktree on a new branch writes .git/config, and git refuses a second
// writer at once instead of waiting: two tasks that started together left
// one of them with "could not lock config file" and no worktree.
var repoLock sync.Mutex

// Prepare makes the worktree for a task on r.PR, or says why not. The error
// is for the footer, so it names the PR, and it is a sentence a person
// reads.
//
// The checks run in this order so that nothing reaches git that git could
// take as an option, and nothing is fetched for a PR that would be refused
// anyway.
func Prepare(ctx context.Context, git Git, r Request) (Prepared, error) {
	repoLock.Lock()
	defer repoLock.Unlock()
	return prepare(ctx, git, r)
}

func prepare(ctx context.Context, git Git, r Request) (Prepared, error) {
	if err := refuseBeforeGit(r); err != nil {
		return Prepared{}, err
	}
	if err := clearLeftover(ctx, git, r); err != nil {
		return Prepared{}, err
	}
	if err := fetch(ctx, git, r); err != nil {
		return Prepared{}, err
	}
	at, err := checkedOutAt(ctx, git, r.Root, r.Branch)
	if err != nil {
		return Prepared{}, err
	}
	remote := "origin/" + r.Branch
	local := "refs/heads/" + r.Branch
	_, verifyErr := run(ctx, git, r.Root, "rev-parse", "--verify", "--quiet", local)
	exists := verifyErr == nil
	// Commits of yours that origin lacks are refused wherever the branch is
	// checked out: the task pushes from origin, and your branch would then
	// part from the PR.
	if exists {
		if _, err := run(ctx, git, r.Root, "merge-base", "--is-ancestor", local, remote); err != nil {
			return Prepared{}, fmt.Errorf("your local branch %s has commits that are not on origin; push or move them first", r.Branch)
		}
	}
	if at != "" {
		return borrowOrOwn(ctx, git, r, at, remote)
	}
	// A branch of yours that tracks somewhere else would push the task's
	// work there, and changing what it tracks is not this run's to do.
	upstream, trackErr := run(ctx, git, r.Root, "rev-parse", "--abbrev-ref", r.Branch+"@{upstream}")
	tracked := trackErr == nil
	if tracked && upstream != remote {
		return create(ctx, git, r, ownBranch(r), remote, false, false)
	}
	return create(ctx, git, r, r.Branch, remote, exists, !tracked)
}

// borrowOrOwn runs the task in your worktree at, which has the PR's branch
// checked out, when that is safe, and on a branch of its own when not. A
// second worktree of the same branch is not possible, and a branch of its
// own for every task leaves a branch you never made.
//
// Your worktree is not borrowed when it has changes: the task commits what
// it finds, and would commit your work in progress with its own. It is not
// borrowed either when the branch tracks somewhere else: the push would go
// there. Your main checkout is borrowed on the same terms as any other
// worktree.
func borrowOrOwn(ctx context.Context, git Git, r Request, at, remote string) (Prepared, error) {
	changes, err := run(ctx, git, at, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return Prepared{}, fmt.Errorf("could not read your worktree of PR #%d at %s: %w", r.PR, at, err)
	}
	upstream, trackErr := run(ctx, git, r.Root, "rev-parse", "--abbrev-ref", r.Branch+"@{upstream}")
	tracked := trackErr == nil
	if changes != "" || (tracked && upstream != remote) {
		return create(ctx, git, r, ownBranch(r), remote, false, false)
	}
	yours := r
	yours.Path = at
	head, err := finishSetup(ctx, git, yours, remote, true, !tracked)
	if err != nil {
		return Prepared{}, fmt.Errorf("could not use your worktree of PR #%d at %s: %w", r.PR, at, err)
	}
	return Prepared{Path: at, Branch: r.Branch, Head: head, Borrowed: true}, nil
}

// ownBranch is the branch a task gets when your branch of the PR's name is
// checked out in a worktree with changes, or tracks somewhere else. On your
// branch, the task would commit your work, or push to the wrong place.
// The name holds the run, so it never meets a branch that is there
// already, and it says where it came from if it is ever left behind.
func ownBranch(r Request) string {
	run := filepath.Base(filepath.Dir(filepath.Dir(r.Path)))
	return "autoreview/" + run + "/pr-" + strconv.Itoa(r.PR)
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

// create adds the worktree on the local branch named local, and finishes
// it. A hook can fail after the worktree is made; either way, nothing is
// left behind that would refuse the next task on this PR.
func create(ctx context.Context, git Git, r Request, local, remote string, existed, untracked bool) (Prepared, error) {
	var err error
	if existed {
		_, err = run(ctx, git, r.Root, "worktree", "add", "--quiet", r.Path, local)
	} else {
		_, err = run(ctx, git, r.Root, "worktree", "add", "--quiet", "--track", "-b", local, r.Path, remote)
	}
	if err == nil {
		var head string
		head, err = finishSetup(ctx, git, r, remote, existed, untracked)
		if err == nil {
			return Prepared{Path: r.Path, Branch: local, Head: head, CreatedBranch: !existed, OwnBranch: local != r.Branch}, nil
		}
	}
	undo(ctx, git, r, local, remote, !existed)
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
// the branch with it when Prepare made that too. The branch goes only when
// origin holds all of it: one that appeared between the check and the add
// is not ours, and may hold work.
//
// git branch -d is not that check. The add can make the branch and then
// fail to record what it tracks, and -d then compares the branch with your
// HEAD. It kept the branch, and every later task on the PR was refused.
func undo(ctx context.Context, git Git, r Request, local, remote string, createdBranch bool) {
	_, _ = git(ctx, r.Root, "worktree", "remove", "--force", r.Path)
	if !createdBranch {
		return
	}
	if _, err := git(ctx, r.Root, "merge-base", "--is-ancestor", local, remote); err == nil {
		_, _ = git(ctx, r.Root, "branch", "--quiet", "-D", local)
	}
}

// exists is true when something is at path. As in v1, an error reading it
// counts as nothing there.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
