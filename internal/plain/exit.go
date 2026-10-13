package plain

import (
	"fmt"
	"strings"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/report"
	"github.com/venabots/autoreview/internal/words"
)

// AfterView is what a run under the view leaves on the normal screen: one
// line that counts its reviews and its tasks by outcome and says where the
// full summary is. The view already showed every result, and repeating
// them all in the scrollback is noise. A run that did nothing leaves
// nothing.
func AfterView(result engine.Result) (out, errs string) {
	reviews, tasks := result.Final.Reviewed(), result.Final.Tasks
	parts := []string{}
	for _, kind := range []struct {
		jobs []engine.Job
		noun string
	}{{reviews, "review"}, {tasks, "task"}} {
		if len(kind.jobs) > 0 {
			parts = append(parts, counted(kind.jobs, kind.noun))
			errs += failedLine(kind.jobs, kind.noun)
		}
	}
	if len(parts) == 0 {
		return "", ""
	}
	return fmt.Sprintf("%s · summary in %s\n", strings.Join(parts, "; "), logOf(result.Final)), errs
}

// counted is "3 reviews: 2 approved, 1 stopped", with the outcomes in the
// order they first appear.
func counted(jobs []engine.Job, noun string) string {
	order := []string{}
	counts := map[string]int{}
	for _, job := range jobs {
		outcome := report.Outcome(job)
		if counts[outcome] == 0 {
			order = append(order, outcome)
		}
		counts[outcome]++
	}
	parts := make([]string, len(order))
	for i, outcome := range order {
		parts[i] = fmt.Sprintf("%d %s", counts[outcome], outcome)
	}
	return fmt.Sprintf("%s: %s", words.Count(len(jobs), noun), strings.Join(parts, ", "))
}

func logOf(s *engine.Snapshot) string {
	return strings.TrimSuffix(s.RunDir, "/") + "/autoreview.log"
}
