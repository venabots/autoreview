package report

import (
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/task"
)

func TestTasksIsATableOfWhatEachTaskPushed(t *testing.T) {
	tasks := []engine.Job{
		{PR: 4, Task: task.Fix, State: engine.JobDone, Verdict: "pushed", Elapsed: 63 * time.Second, Session: "s4"},
		{PR: 5, Task: task.Conflicts, State: engine.JobFailed, Outcome: "exit 3", Verdict: "nothing pushed", Error: "boom",
			Kept: "uncommitted changes", Worktree: "/logs/run-1/worktrees/pr-5"},
	}
	want := "PR  TASK       RESULT           BRANCH          TIME   SESSION\n" +
		"#4  fix        done             pushed          1m03s  s4\n" +
		"#5  conflicts  failed (exit 3)  nothing pushed  0s     -\n" +
		"PR #5's worktree has uncommitted changes: /logs/run-1/worktrees/pr-5\n" +
		"error #5: boom\n"
	if got := Tasks(tasks); got != want {
		t.Errorf("Tasks =\n%s\nwant\n%s", got, want)
	}
}

func TestAPreparingTaskIsMakingItsWorktree(t *testing.T) {
	if got := Result(engine.Job{State: engine.JobPreparing}); got != "making its worktree" {
		t.Errorf("Result = %q", got)
	}
}

func TestProgressNamesWhatATaskDoes(t *testing.T) {
	now := time.Now()
	cases := map[string]engine.Job{
		"fixing comments 1m03s": {Task: task.Comments, State: engine.JobRunning, StartedAt: now.Add(-63 * time.Second)},
		"making its worktree":   {Task: task.Fix, State: engine.JobPreparing},
	}
	for want, job := range cases {
		if got := Progress(job, now); got != want {
			t.Errorf("Progress = %q, want %q", got, want)
		}
	}
}
