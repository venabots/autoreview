package plain

import (
	"fmt"
	"strings"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/report"
	"github.com/venabots/autoreview/internal/words"
)

// AfterView is what a run under the view leaves on the normal screen: one
// line that counts its reviews by outcome and says where the full summary
// is. The view already showed every result, and repeating them all in the
// scrollback is noise. A run that reviewed nothing leaves nothing.
func AfterView(result engine.Result) (out, errs string) {
	jobs := result.Final.Reviewed()
	if len(jobs) == 0 {
		return "", ""
	}
	order := []string{}
	counts := map[string]int{}
	failed := 0
	for _, job := range jobs {
		outcome := report.Outcome(job)
		if counts[outcome] == 0 {
			order = append(order, outcome)
		}
		counts[outcome]++
		if job.State != engine.JobDone {
			failed++
		}
	}
	parts := make([]string, len(order))
	for i, outcome := range order {
		parts[i] = fmt.Sprintf("%d %s", counts[outcome], outcome)
	}
	out = fmt.Sprintf("%s: %s · summary in %s\n", words.Count(len(jobs), "review"), strings.Join(parts, ", "), logOf(result.Final))
	if failed > 0 {
		errs = fmt.Sprintf("error: %d of %s failed\n", failed, words.Count(len(jobs), "review"))
	}
	return out, errs
}

func logOf(s *engine.Snapshot) string {
	return strings.TrimSuffix(s.RunDir, "/") + "/autoreview.log"
}
