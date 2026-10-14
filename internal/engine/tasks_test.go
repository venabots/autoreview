package engine

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/session"
	"github.com/venabots/autoreview/internal/task"
	"github.com/venabots/autoreview/internal/worktree"
)

// trees is a fake for the worktrees, the head readback and the skill
// check of tasks.
type trees struct {
	mu       sync.Mutex
	made     []int
	released []string
	// prepareErr fails every worktree; hold keeps one from being made
	// until it closes.
	prepareErr error
	hold       chan struct{}
	kept       string
	pushed     string
	readFails  bool
	missing    string
	// own makes each worktree on a branch of the run's own, and borrow
	// hands back the person's own worktree.
	own    bool
	borrow bool
}

func (f *trees) deps(d *Deps) {
	d.Prepare = func(_ context.Context, r task.Request) (worktree.Prepared, error) {
		f.mu.Lock()
		hold := f.hold
		f.mu.Unlock()
		if hold != nil {
			<-hold
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.prepareErr != nil {
			return worktree.Prepared{}, f.prepareErr
		}
		f.made = append(f.made, r.PR)
		tree := worktree.Prepared{Path: "/run/worktrees/pr-" + strconv.Itoa(r.PR), Branch: r.Branch, Head: "sha1"}
		if f.own {
			tree.Branch, tree.OwnBranch = "autoreview/run-1/pr-"+strconv.Itoa(r.PR), true
		}
		if f.borrow {
			tree.Path, tree.Borrowed = "/home/me/work", true
		}
		return tree, nil
	}
	d.Finish = func(_ context.Context, p worktree.Prepared) (string, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.released = append(f.released, p.Path)
		return f.kept, nil
	}
	d.Pushed = func(context.Context, int, string) (string, bool) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.readFails {
			return "unknown", false
		}
		if f.pushed == "" {
			return "pushed", true
		}
		return f.pushed, true
	}
	d.TaskRefusal = func(task.Kind) string { return f.missing }
}

func (f *trees) state() ([]int, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.made), slices.Clone(f.released)
}

func fixOf(pr int) RunTask {
	return RunTask{Request: task.Request{PR: pr, Kind: task.Fix, Title: "My own work", Branch: "me/work"}}
}

func taskOf(t *testing.T, b *bench, pr int) Job {
	t.Helper()
	for _, j := range b.eng.Snapshot().Tasks {
		if j.PR == pr {
			return j
		}
	}
	t.Fatalf("the snapshot has no task on PR #%d", pr)
	return Job{}
}

func TestATaskRunsItsSkillInAWorktreeOfItsOwn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &trees{}
		b := newBench(t, held(setup{deps: f.deps}))
		b.eng.Do(fixOf(4))
		synctest.Wait()

		c := b.reviewer.command(t, 4)
		args := strings.Join(c.Args, " ")
		if c.Dir != "/run/worktrees/pr-4" || !strings.HasSuffix(args, "-- /babysit-pr 4") || !strings.Contains(args, "--session-id ") {
			t.Errorf("the task ran in %q as %q", c.Dir, args[max(len(args)-80, 0):])
		}
		if strings.Contains(args, "--append-system-prompt") || filepath.Base(filepath.Dir(c.Stdout)) != "fix" {
			t.Errorf("a task was asked for a trailer, or wrote its files outside tasks/fix: %s", c.Stdout)
		}
		job := taskOf(t, b, 4)
		if job.State != JobRunning || job.Task != task.Fix || job.Worktree != "/run/worktrees/pr-4" || !session.UUIDShaped(job.Follow) {
			t.Errorf("the running task: %+v", job)
		}

		b.reviewer.finish(t, 4, 0, "done")
		synctest.Wait()
		job = taskOf(t, b, 4)
		if job.State != JobDone || job.Verdict != "pushed" {
			t.Errorf("the task ended %d with verdict %q", job.State, job.Verdict)
		}
		if _, released := f.state(); !slices.Equal(released, []string{"/run/worktrees/pr-4"}) {
			t.Errorf("worktrees given back: %v", released)
		}
		b.eng.Do(Quit{})
		if result := <-b.results; result.Code != ExitOK {
			t.Errorf("code %d", result.Code)
		}
	})
}

func TestATaskIsRefusedBeforeAnythingIsMade(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Config)
		f    *trees
		want string
	}{
		{"under --no-post", func(c *Config) { c.NoPost = true }, &trees{}, "not fixing PR #4: --no-post leaves every PR alone"},
		{"when its skill is missing", nil, &trees{missing: "babysit-pr is not installed where claude looks (~/.claude/skills)"},
			"not fixing PR #4: babysit-pr is not installed where claude looks (~/.claude/skills)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b := newBench(t, held(setup{deps: c.f.deps, edit: c.edit}))
				b.eng.Do(fixOf(4))
				synctest.Wait()
				if made, _ := c.f.state(); len(made) != 0 || len(b.eng.Snapshot().Tasks) != 0 {
					t.Errorf("a refused task made %v", made)
				}
				if !slices.Contains(b.notes(), c.want) {
					t.Errorf("notes = %q, want %q", b.notes(), c.want)
				}
				b.eng.Do(Quit{})
				<-b.results
			})
		})
	}
}

func TestASecondTaskOnAPRWithOneRunningIsRefused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &trees{}
		b := newBench(t, held(setup{deps: f.deps}))
		b.eng.Do(fixOf(4))
		b.eng.Do(RunTask{Request: task.Request{PR: 4, Kind: task.Conflicts}})
		synctest.Wait()
		if !slices.Contains(b.notes(), "not fixing conflicts PR #4: a job on it is already running") {
			t.Errorf("notes = %q", b.notes())
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestAWorktreeThatCannotBeMadeIsANoteAndNotAFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &trees{prepareErr: errors.New("PR #4 is from a fork; its branch is not on origin")}
		b := newBench(t, held(setup{deps: f.deps}))
		b.eng.Do(fixOf(4))
		synctest.Wait()
		if !slices.Contains(b.notes(), "not fixing PR #4: PR #4 is from a fork; its branch is not on origin") {
			t.Errorf("notes = %q", b.notes())
		}
		if len(b.reviewer.started()) != 0 {
			t.Error("a task with no worktree started")
		}
		if got := b.eng.Snapshot().NotStarted[4]; got != "not fixing PR #4: PR #4 is from a fork; its branch is not on origin" {
			t.Errorf("the snapshot says %q about PR #4: the view cannot say why nothing ran", got)
		}
		b.eng.Do(Quit{})
		if result := <-b.results; result.Code != ExitOK {
			t.Errorf("code %d: a task that never ran fails nothing", result.Code)
		}
	})
}

func TestTheNoteAboutATaskThatDidNotStartGoesWhenOneStarts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &trees{prepareErr: errors.New("git fetch failed")}
		b := newBench(t, held(setup{deps: f.deps}))
		b.eng.Do(fixOf(4))
		synctest.Wait()
		before := b.eng.Snapshot()

		f.mu.Lock()
		f.prepareErr = nil
		f.mu.Unlock()
		b.eng.Do(fixOf(4))
		synctest.Wait()
		if got := b.eng.Snapshot().NotStarted[4]; got != "" {
			t.Errorf("a task runs on PR #4 and the snapshot still says %q", got)
		}
		if before.NotStarted[4] == "" {
			t.Error("a published snapshot was changed: its note is gone")
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestATaskWaitsForASlotThatAReviewHolds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &trees{}
		b := newBench(t, held(setup{prs: []int{9}, jobs: 1, deps: f.deps}))
		b.eng.Do(fixOf(4))
		synctest.Wait()
		if job := taskOf(t, b, 4); job.State != JobQueued {
			t.Fatalf("the task is in state %d while the only slot is taken, want queued", job.State)
		}
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		synctest.Wait()
		if job := taskOf(t, b, 4); job.State != JobRunning {
			t.Errorf("the task is in state %d after the review ended, want running", job.State)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestAFailedTaskFailsTheRunAndAKeptWorktreeIsNamed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &trees{kept: "uncommitted changes", readFails: true}
		b := newBench(t, held(setup{deps: f.deps}))
		b.eng.Do(fixOf(4))
		synctest.Wait()
		b.reviewer.finish(t, 4, 3, "boom")
		synctest.Wait()

		job := taskOf(t, b, 4)
		if job.State != JobFailed || job.Verdict != "unknown" || job.Kept != "uncommitted changes" {
			t.Errorf("the task: %+v", job)
		}
		notes := b.notes()
		if !slices.Contains(notes, "PR #4's worktree has uncommitted changes: /run/worktrees/pr-4") ||
			!slices.Contains(notes, "could not read PR #4's branch back from GitHub; whether it pushed is unknown") {
			t.Errorf("notes = %q", notes)
		}
		b.eng.Do(Quit{})
		if result := <-b.results; result.Code != ExitFailed {
			t.Errorf("code %d, want %d", result.Code, ExitFailed)
		}
	})
}

func TestQuitStopsATaskAndGivesBackEveryWorktree(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		f := &trees{}
		rev := &reviewer{onStop: proc.Exit{Signaled: true}}
		b := newBench(t, held(setup{jobs: 1, deps: f.deps, reviewer: rev}))
		b.eng.Do(fixOf(4))
		synctest.Wait()
		b.eng.Do(fixOf(5))
		synctest.Wait()
		f.mu.Lock()
		f.hold = release
		f.mu.Unlock()
		b.eng.Do(fixOf(6))
		synctest.Wait()

		b.eng.Do(Quit{})
		synctest.Wait()
		if b.eng.Snapshot().Phase != Stopping {
			t.Fatalf("phase %d, want a run that waits for the worktree still being made", b.eng.Snapshot().Phase)
		}
		close(release)
		result := <-b.results

		if result.Code != ExitInterrupted {
			t.Errorf("code %d", result.Code)
		}
		if job := taskOf(t, b, 4); job.State != JobFailed || job.Outcome != "stopped" {
			t.Errorf("the running task: state %d, outcome %q", job.State, job.Outcome)
		}
		_, released := f.state()
		for _, path := range []string{"/run/worktrees/pr-4", "/run/worktrees/pr-5", "/run/worktrees/pr-6"} {
			if !slices.Contains(released, path) {
				t.Errorf("%s was not given back: %v", path, released)
			}
		}
		if got := b.reviewer.started(); !slices.Equal(got, []int{4}) {
			t.Errorf("started %v, want only the task that held the slot", got)
		}
	})
}

func TestStopJobStopsATask(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &trees{}
		rev := &reviewer{onStop: proc.Exit{Code: 10}}
		b := newBench(t, held(setup{deps: f.deps, reviewer: rev}))
		b.eng.Do(fixOf(4))
		synctest.Wait()
		b.eng.Do(StopJob{PR: 4})
		synctest.Wait()
		if job := taskOf(t, b, 4); job.State != JobFailed || job.Outcome != "stopped" {
			t.Errorf("the task: state %d, outcome %q", job.State, job.Outcome)
		}
		if !slices.Contains(b.notes(), "stopped the task on PR #4") {
			t.Errorf("notes = %q", b.notes())
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestATaskPastItsTimeLimitIsStoppedAndTimesOut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &trees{}
		rev := &reviewer{onStop: proc.Exit{Signaled: true}}
		b := newBench(t, held(setup{deps: f.deps, reviewer: rev}))
		b.eng.Do(fixOf(4))
		synctest.Wait()
		time.Sleep(2 * time.Hour)
		synctest.Wait()
		if job := taskOf(t, b, 4); job.State != JobTimedOut {
			t.Errorf("the task is in state %d, want timed out", job.State)
		}
		b.eng.Do(Quit{})
		if result := <-b.results; result.Code != ExitFailed {
			t.Errorf("code %d, want %d", result.Code, ExitFailed)
		}
	})
}

func TestATaskWhoseProcessDoesNotStartFailsAndGivesBackNothingTwice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &trees{}
		rev := &reviewer{refuse: []int{4}}
		b := newBench(t, held(setup{deps: f.deps, reviewer: rev}))
		b.eng.Do(fixOf(4))
		synctest.Wait()
		if job := taskOf(t, b, 4); job.State != JobFailed || job.Error == "" {
			t.Errorf("the task: %+v", job)
		}
		b.eng.Do(Quit{})
		<-b.results
		if _, released := f.state(); !slices.Equal(released, []string{"/run/worktrees/pr-4"}) {
			t.Errorf("worktrees given back: %v", released)
		}
	})
}
