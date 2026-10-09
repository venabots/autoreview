// Package task is work on one of your own PRs: fix it, fix its conflicts,
// or answer its review comments.
//
// A task is not a review, and the difference reaches every part of a run:
// it runs a skill the person installed, in a worktree of its own, writes no
// review, and its result is whether it pushed. Keeping the kind on the job
// lets each of those places ask once instead of guessing from a prompt.
package task

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/venabots/autoreview/internal/gh"
	"github.com/venabots/autoreview/internal/review"
)

// Kind is the work a job does.
type Kind int

const (
	// Review is a review of somebody else's PR: everything that is not a
	// task.
	Review Kind = iota
	// Fix gets one of your PRs mergeable once: conflicts, review comments
	// and CI. Babysitting is running it again as the PR changes.
	Fix
	// Comments answers the review comments on one of your PRs.
	Comments
	// Conflicts merges the base branch into one of your PRs and resolves
	// the conflicts.
	Conflicts
)

// Skill is the installed skill a task runs, or empty for a review.
func (k Kind) Skill() string {
	switch k {
	case Fix:
		return "babysit-pr"
	case Comments:
		return "pr-comment-handler"
	case Conflicts:
		return "sync-main"
	}
	return ""
}

// Doing is the word for the job while it runs: "fixing 1m12s".
func (k Kind) Doing() string {
	switch k {
	case Fix:
		return "fixing"
	case Comments:
		return "fixing comments"
	case Conflicts:
		return "fixing conflicts"
	}
	return "reviewing"
}

// Tag names the directory a task's files go in, so they never overwrite a
// review's. It is empty for a review.
func (k Kind) Tag() string {
	switch k {
	case Fix:
		return "fix"
	case Comments:
		return "comments"
	case Conflicts:
		return "conflicts"
	}
	return ""
}

// Request is a task a person asked for, with what the My PRs list knew of
// the PR at the key press.
type Request struct {
	PR        int
	Kind      Kind
	Title     string
	Branch    string
	CrossRepo bool
}

// SkillRoots is where an orchestrator finds an installed skill. A task's
// skill is never staged, so this is the only place it can come from.
func SkillRoots(o review.Orchestrator, home, codexHome, configDir, repoRoot string) []string {
	if !o.DiscoversStagedSkills() {
		return review.SkillRoots(o.Backend, home, codexHome)
	}
	return []string{filepath.Join(configDir, "skills"), filepath.Join(repoRoot, ".claude", "skills")}
}

// NotInstalled says why a task cannot run, or is empty when its skill is
// installed. It is said before anything is made: a worktree, a session and
// a job slot all spent on a skill that is not there is the failure it
// prevents.
func NotInstalled(k Kind, roots []string, o review.Orchestrator) string {
	if len(review.MissingSkills(roots, []string{k.Skill()})) == 0 {
		return ""
	}
	return fmt.Sprintf("%s is not installed where %s looks (%s)", k.Skill(), o.Label(), o.SkillsHome())
}

// Readback is what a task did to its PR, in GitHub's words: whether its
// head moved from before. ok is false when GitHub could not be asked; the
// result is then unknown, and saying "nothing pushed" would claim what
// nobody checked.
func Readback(ctx context.Context, run gh.Runner, slug string, pr int, before string) (string, bool) {
	out, err := run(ctx, "pr", "view", strconv.Itoa(pr), "--repo", slug, "--json", "headRefOid", "--jq", ".headRefOid")
	if err != nil {
		return "unknown", false
	}
	return Pushed(before, strings.TrimSpace(string(out))), true
}

// Pushed says whether a head moved.
func Pushed(before, after string) string {
	if after == "" || after == before {
		return "nothing pushed"
	}
	return "pushed"
}
