package engine

import (
	"testing"
	"testing/synctest"

	"github.com/venabots/autoreview/internal/review"
)

// PR #2931: the conflict fix found main already had the change, stopped,
// and said to close the PR. The view showed only "nothing pushed".
func TestATasksReportReachesItsJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, held(setup{deps: (&trees{}).deps}))
		b.eng.Do(fixOf(4))
		synctest.Wait()
		said := "I stopped.\n\n```autoreview-task\n{\"outcome\":\"should-close\",\"why\":\"main already has this change, from #2943\"}\n```"
		b.reviewer.finish(t, 4, 0, said)
		synctest.Wait()

		job := taskOf(t, b, 4)
		want := review.TaskTrailer{Outcome: review.TaskShouldClose, Why: "main already has this change, from #2943"}
		if job.TaskTrailer == nil || *job.TaskTrailer != want {
			t.Errorf("the task's report is %+v, want %+v", job.TaskTrailer, want)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestATaskThatSaysNothingHasNoReport(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, held(setup{deps: (&trees{}).deps}))
		b.eng.Do(fixOf(4))
		synctest.Wait()
		b.reviewer.finish(t, 4, 0, "done")
		synctest.Wait()
		if job := taskOf(t, b, 4); job.TaskTrailer != nil {
			t.Errorf("a task that wrote no block has the report %+v", job.TaskTrailer)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}
