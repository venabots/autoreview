package worktree

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// call is one git command a fake saw: the directory and the argv.
type call struct {
	dir  string
	args string
}

// recorder is a fake Git that records each call and answers from a table
// keyed by the argv. An argv with no answer succeeds with no output.
type recorder struct {
	mu      sync.Mutex
	calls   []call
	answers map[string]answer
}

type answer struct {
	out string
	err error
}

func (r *recorder) git(_ context.Context, dir string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call{dir: dir, args: key})
	a := r.answers[key]
	return []byte(a.out), a.err
}

func (r *recorder) seen() []call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

var errNo = errors.New("no")

const (
	root = "/repo"
	wt   = "/runs/run-1/worktrees/pr-4"
)

func fakeRequest() Request {
	return Request{Root: root, Path: wt, PR: 4, Branch: "me/work"}
}

func TestPrepareRunsV1sGitStepsInV1sOrder(t *testing.T) {
	newBranch := map[string]answer{
		"rev-parse --verify --quiet refs/heads/me/work": {err: errNo},
		"rev-parse --abbrev-ref me/work@{upstream}":     {err: errNo},
		"rev-parse HEAD":            {out: "abc123\n"},
		"worktree list --porcelain": {out: "worktree /repo\nHEAD 1\nbranch refs/heads/main\n"},
	}
	oldBranch := map[string]answer{
		"rev-parse --abbrev-ref me/work@{upstream}": {err: errNo},
		"rev-parse HEAD": {out: "abc123\n"},
	}
	fetchStep := call{root, "fetch --quiet origin +refs/heads/me/work:refs/remotes/origin/me/work"}
	cases := []struct {
		name    string
		answers map[string]answer
		created bool
		want    []call
	}{
		{"a new branch is made tracking origin", newBranch, true, []call{
			fetchStep,
			{root, "worktree list --porcelain"},
			{root, "rev-parse --verify --quiet refs/heads/me/work"},
			{root, "rev-parse --abbrev-ref me/work@{upstream}"},
			{root, "worktree add --quiet --track -b me/work " + wt + " origin/me/work"},
			{wt, "rev-parse HEAD"},
		}},
		{"an existing branch is fast-forwarded and set to track", oldBranch, false, []call{
			fetchStep,
			{root, "worktree list --porcelain"},
			{root, "rev-parse --verify --quiet refs/heads/me/work"},
			{root, "merge-base --is-ancestor refs/heads/me/work origin/me/work"},
			{root, "rev-parse --abbrev-ref me/work@{upstream}"},
			{root, "worktree add --quiet " + wt + " me/work"},
			{wt, "merge --quiet --ff-only origin/me/work"},
			{wt, "branch --quiet --set-upstream-to origin/me/work"},
			{wt, "rev-parse HEAD"},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &recorder{answers: c.answers}
			p, err := Prepare(context.Background(), r.git, fakeRequest())
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			want := Prepared{Path: wt, Branch: "me/work", Head: "abc123", CreatedBranch: c.created}
			if p != want {
				t.Errorf("Prepared = %+v, want %+v", p, want)
			}
			if got := r.seen(); !slices.Equal(got, c.want) {
				t.Errorf("git calls:\n got %v\nwant %v", got, c.want)
			}
		})
	}
}

func TestAnExistingBranchThatAlreadyTracksOriginIsNotRetracked(t *testing.T) {
	r := &recorder{answers: map[string]answer{
		"rev-parse --abbrev-ref me/work@{upstream}": {out: "origin/me/work\n"},
	}}
	if _, err := Prepare(context.Background(), r.git, fakeRequest()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	for _, c := range r.seen() {
		if strings.HasPrefix(c.args, "branch ") {
			t.Errorf("changed what a tracked branch tracks: %v", c)
		}
	}
}

func TestAFailedStepUndoesTheWorktreeAndOnlyABranchItMade(t *testing.T) {
	undoWorktree := call{root, "worktree remove --force " + wt}
	undoBranch := call{root, "branch --quiet -d me/work"}
	cases := []struct {
		name    string
		answers map[string]answer
		want    string
		undo    []call
	}{
		{"the add fails on a new branch", map[string]answer{
			"rev-parse --verify --quiet refs/heads/me/work":                     {err: errNo},
			"rev-parse --abbrev-ref me/work@{upstream}":                         {err: errNo},
			"worktree add --quiet --track -b me/work " + wt + " origin/me/work": {err: errors.New("hook said no")},
		}, "could not make a worktree for PR #4: hook said no", []call{undoWorktree, undoBranch}},
		{"the merge fails on your branch", map[string]answer{
			"rev-parse --abbrev-ref me/work@{upstream}": {out: "origin/me/work"},
			"merge --quiet --ff-only origin/me/work":    {err: errNo},
		}, "could not make a worktree for PR #4: could not bring me/work up to origin: no", []call{undoWorktree}},
		{"tracking fails on your branch", map[string]answer{
			"rev-parse --abbrev-ref me/work@{upstream}":       {err: errNo},
			"branch --quiet --set-upstream-to origin/me/work": {err: errNo},
		}, "could not make a worktree for PR #4: could not track origin/me/work: no", []call{undoWorktree}},
		{"the head cannot be read", map[string]answer{
			"rev-parse --verify --quiet refs/heads/me/work": {err: errNo},
			"rev-parse --abbrev-ref me/work@{upstream}":     {err: errNo},
			"rev-parse HEAD": {err: errNo},
		}, "could not make a worktree for PR #4: could not read its head: no", []call{undoWorktree, undoBranch}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &recorder{answers: c.answers}
			_, err := Prepare(context.Background(), r.git, fakeRequest())
			if err == nil || err.Error() != c.want {
				t.Fatalf("error = %v, want %q", err, c.want)
			}
			seen := r.seen()
			if tail := seen[len(seen)-len(c.undo):]; !slices.Equal(tail, c.undo) {
				t.Errorf("undo = %v, want %v", tail, c.undo)
			}
		})
	}
}

func TestAForkOrAMissingBranchIsRefusedBeforeGitRuns(t *testing.T) {
	cases := []struct {
		name string
		req  Request
		want string
	}{
		{"a fork", Request{PR: 4, Branch: "me/work", CrossRepo: true}, "PR #4 is from a fork; its branch is not on origin"},
		{"no branch", Request{PR: 4}, "PR #4 has no branch this run can check out"},
		{"a branch git would read as an option", Request{PR: 4, Branch: "--upload-pack=x"}, "PR #4 has no branch this run can check out"},
		{"an earlier worktree on disk", Request{PR: 4, Branch: "me/work", Path: t.TempDir()}, "PR #4's worktree from an earlier task is still at "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &recorder{}
			_, err := Prepare(context.Background(), r.git, c.req)
			if err == nil || !strings.HasPrefix(err.Error(), c.want) {
				t.Errorf("error = %v, want %q", err, c.want)
			}
			if got := r.seen(); len(got) != 0 {
				t.Errorf("git ran: %v", got)
			}
		})
	}
}

func TestTheFetchHasV1sDeadlineAndSaysSoWhenItPasses(t *testing.T) {
	var left time.Duration
	slow := func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		deadline, _ := ctx.Deadline()
		left = time.Until(deadline)
		return nil, fmt.Errorf("git fetch stopped: %w", context.DeadlineExceeded)
	}
	_, err := Prepare(context.Background(), slow, fakeRequest())
	if want := "could not fetch PR #4's branch me/work: git fetch took longer than 120s"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
	if left <= FetchTimeout-time.Minute || left > FetchTimeout {
		t.Errorf("the fetch had %v left, want about %v", left, FetchTimeout)
	}
}

func TestACancelledRunIsNotReportedAsASlowFetch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	ended := func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		return nil, fmt.Errorf("git fetch stopped: %w", ctx.Err())
	}
	_, err := Prepare(ctx, ended, fakeRequest())
	if err == nil || strings.Contains(err.Error(), "took longer") {
		t.Errorf("error = %v, want the run's own end, not a fetch timeout", err)
	}
}

func TestAStaleRecordIsForceRemovedOnlyWhenThisToolMadeIt(t *testing.T) {
	base := t.TempDir()
	ours := filepath.Join(base, "run-old", "worktrees", "pr-9")
	yours := filepath.Join(base, "usb", "worktrees", "pr-9")
	list := func(path string) map[string]answer {
		return map[string]answer{
			"rev-parse --abbrev-ref me/work@{upstream}": {err: errNo},
			"worktree list --porcelain":                 {out: "worktree /repo\nbranch refs/heads/main\n\nworktree " + path + "\nbranch refs/heads/me/work\n"},
		}
	}

	r := &recorder{answers: list(ours)}
	if _, err := Prepare(context.Background(), r.git, fakeRequest()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !slices.Contains(r.seen(), call{root, "worktree remove --force " + ours}) {
		t.Errorf("the stale record of ours was not removed: %v", r.seen())
	}

	r = &recorder{answers: list(yours)}
	_, err := Prepare(context.Background(), r.git, fakeRequest())
	if err == nil || !strings.HasPrefix(err.Error(), "me/work is checked out at "+yours+", which is not there") {
		t.Errorf("error = %v", err)
	}
	for _, c := range r.seen() {
		if strings.HasPrefix(c.args, "worktree remove") {
			t.Errorf("removed a record that is not ours: %v", c)
		}
	}
}

func TestARefusalFromGitBeforeTheAddIsPassedOn(t *testing.T) {
	r := &recorder{answers: map[string]answer{"worktree list --porcelain": {err: errors.New("fatal: not a git repository")}}}
	_, err := Prepare(context.Background(), r.git, fakeRequest())
	if err == nil || err.Error() != "fatal: not a git repository" {
		t.Errorf("error = %v", err)
	}
	r = &recorder{answers: map[string]answer{
		"worktree list --porcelain":                          {out: "worktree /runs/run-1/worktrees/pr-1\nbranch refs/heads/me/work\n"},
		"worktree remove --force /runs/run-1/worktrees/pr-1": {err: errors.New("fatal: locked")},
	}}
	_, err = Prepare(context.Background(), r.git, fakeRequest())
	if err == nil || err.Error() != "fatal: locked" {
		t.Errorf("error = %v", err)
	}
}
