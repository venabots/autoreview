package engine

import (
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/venabots/autoreview/internal/review"
)

func TestAReviewGetsTheRunsVerdictLevelAndAVerdictItForbidsIsNoted(t *testing.T) {
	cases := []struct {
		level   review.Verdicts
		landed  string
		noted   bool
		inArgv  string
		message string
	}{
		{review.VerdictsAll, "approved", false, "", ""},
		{review.VerdictsCommentOnly, "approved", true, "--verdicts comment-only", `PR #9 got "approved", which review.verdicts = "comment-only" does not allow`},
		{review.VerdictsApproveOnly, "changes requested", true, "--verdicts approve-only", `PR #9 got "changes requested", which review.verdicts = "approve-only" does not allow`},
		{review.VerdictsApproveOnly, "approved", false, "--verdicts approve-only", ""},
	}
	for _, c := range cases {
		synctest.Test(t, func(t *testing.T) {
			b := newBench(t, setup{prs: []int{9}, edit: func(cfg *Config) { cfg.Verdicts = c.level }, verdict: func(int) review.Readback {
				return review.Readback{Kind: review.ReadbackLanded, Word: c.landed}
			}})
			synctest.Wait()
			argv := strings.Join(b.reviewer.command(t, 9).Args, " ")
			if c.inArgv != "" && !strings.Contains(argv, c.inArgv) || c.inArgv == "" && strings.Contains(argv, "--verdicts") {
				t.Errorf("%v: the prompt is %q", c.level, argv[max(len(argv)-60, 0):])
			}
			b.reviewer.finish(t, 9, 0, answer(9, c.landed))
			<-b.results
			if job := b.job(t, 9); job.Verdicts != c.level {
				t.Errorf("the job carries %v, want %v", job.Verdicts, c.level)
			}
			noted := slices.ContainsFunc(b.notes(), func(n string) bool { return c.message != "" && strings.HasPrefix(n, c.message) })
			if noted != c.noted {
				t.Errorf("%v with %q landed: notes %q", c.level, c.landed, b.notes())
			}
		})
	}
}

func clean(answered, launched int) *review.Trailer {
	zero := uint(0)
	t := &review.Trailer{Findings: &review.Findings{MustFix: &zero, ShouldFix: &zero, Polish: &zero}}
	for i := range launched {
		ok := i < answered
		t.Panel = append(t.Panel, review.Panelist{Name: "p", OK: &ok})
	}
	return t
}

// Under comments only no review approves. A clean review that too few of
// the panel answered did not miss its approval for that reason, and the
// words must not send the person to look for more reviewers.
func TestUnderCommentsOnlyAShortPanelIsNotWhyThereIsNoApproval(t *testing.T) {
	job := Job{Verdict: "commented", Trailer: clean(2, 4)}
	if _, short := job.Shortfall(); !short {
		t.Fatal("with every verdict allowed, a clean review from 2 of 4 is short of the gate")
	}
	job.Verdicts = review.VerdictsCommentOnly
	if _, short := job.Shortfall(); short {
		t.Error("under comments only, the review is said to be short of an approval it could not give")
	}
	if got := job.ReasonsHeader(); got != "the review found:" {
		t.Errorf("under comments only the reasons open with %q", got)
	}
	if got := (Job{}).ReasonsHeader(); got != review.ReasonsHeader {
		t.Errorf("by default the reasons open with %q", got)
	}
}
