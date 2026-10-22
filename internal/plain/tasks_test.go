package plain

import (
	"strings"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/task"
)

func TestATaskSaysWhatItDoesAndWhetherItPushed(t *testing.T) {
	cases := []struct {
		name string
		ev   engine.Event
		out  string
	}{
		{"a fix starts", engine.JobStarted{Job: engine.Job{PR: 4, Author: "me", Task: task.Fix}}, "start   #4 (fixing)\n"},
		{"a fix that pushed", engine.JobFinished{Job: engine.Job{PR: 4, Task: task.Fix, State: engine.JobDone, Verdict: "pushed", Elapsed: 63 * time.Second}},
			"done    #4 (fix, 1m03s): pushed\n"},
		{"a task that failed", engine.JobFinished{Job: engine.Job{PR: 4, Task: task.Fix, State: engine.JobFailed, Outcome: "exit 3", Verdict: "nothing pushed", Error: "boom"}},
			"FAILED  #4 (fix, exit 3, 0s): nothing pushed; boom\n"},
		{"a task that says to close the PR", engine.JobFinished{Job: engine.Job{PR: 2931, Task: task.Conflicts, State: engine.JobDone, Verdict: "nothing pushed", Elapsed: 44 * time.Second,
			TaskTrailer: &review.TaskTrailer{Outcome: review.TaskShouldClose, Why: "main already has this change, from #2943"}}},
			"done    #2931 (conflicts, 44s): nothing pushed; should close: main already has this change, from #2943\n"},
		{"a task that says why it pushed nothing", engine.JobFinished{Job: engine.Job{PR: 4, Task: task.Fix, State: engine.JobDone, Verdict: "nothing pushed",
			TaskTrailer: &review.TaskTrailer{Outcome: review.TaskNothingNeeded, Why: "every thread is answered"}}},
			"done    #4 (fix, 0s): nothing pushed; every thread is answered\n"},
		{"a task that timed out", engine.JobFinished{Job: engine.Job{PR: 4, Task: task.Conflicts, State: engine.JobTimedOut, Verdict: "unknown", Elapsed: time.Hour}},
			"TIMEOUT #4 (conflicts, 1h00m): unknown\n"},
	}
	for _, c := range cases {
		if got := say(c.ev); got.out != c.out || got.errs != "" {
			t.Errorf("%s: say = %q and %q, want %q", c.name, got.out, got.errs, c.out)
		}
	}
}

func tasksOnly(tasks ...engine.Job) engine.Result {
	return engine.Result{Final: &engine.Snapshot{RunDir: "/logs/run-1", Tasks: tasks}}
}

func TestFinalNamesAPRATaskSaysToClose(t *testing.T) {
	superseded := engine.Job{PR: 2931, Task: task.Conflicts, State: engine.JobDone, Verdict: "nothing pushed",
		TaskTrailer: &review.TaskTrailer{Outcome: review.TaskShouldClose, Why: "main already has this change, from #2943"}}
	out, _ := Final(tasksOnly(superseded), false)
	if want := "PR #2931: should close: main already has this change, from #2943\n"; !strings.Contains(out, want) {
		t.Errorf("Final = %q, want it to hold %q", out, want)
	}
}

func TestFinalReportsTheTasksUnderTheReviews(t *testing.T) {
	fixed := engine.Job{PR: 4, Task: task.Fix, State: engine.JobDone, Verdict: "pushed"}
	broke := engine.Job{PR: 5, Task: task.Fix, State: engine.JobFailed, Outcome: "exit 3", Verdict: "nothing pushed"}

	out, errs := Final(tasksOnly(fixed, broke), false)
	if !strings.Contains(out, "\n#4  fix   done             pushed") || !strings.Contains(out, "task logs: /logs/run-1/tasks\n") {
		t.Errorf("Final = %q", out)
	}
	if errs != "error: 1 of 2 tasks failed\n" {
		t.Errorf("errs = %q", errs)
	}

	both := finalOf(engine.Job{PR: 9, State: engine.JobDone, Verdict: "approved", PassDir: "/logs/run-1/pass-1"})
	both.Final.Tasks = []engine.Job{fixed}
	out, errs = Final(both, false)
	if strings.Index(out, "#9  done") > strings.Index(out, "#4  fix") || errs != "" {
		t.Errorf("Final = %q and %q: the reviews come first", out, errs)
	}
}

func TestAfterViewCountsTheTasksToo(t *testing.T) {
	result := tasksOnly(
		engine.Job{PR: 4, Task: task.Fix, State: engine.JobDone, Verdict: "pushed"},
		engine.Job{PR: 5, Task: task.Fix, State: engine.JobFailed, Outcome: "exit 3"},
	)
	result.Final.Jobs = []engine.Job{{PR: 9, State: engine.JobDone, Verdict: "approved"}}
	out, errs := AfterView(result)
	if want := "1 review: 1 approved; 2 tasks: 1 pushed, 1 failed · summary in /logs/run-1/autoreview.log\n"; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	if errs != "error: 1 of 2 tasks failed\n" {
		t.Errorf("errs = %q", errs)
	}
}
