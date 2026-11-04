package engine

import (
	"strings"
	"testing"
	"testing/synctest"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/review"
)

// A team holds back the approval of one person's PRs for a human check.
// The review still runs and still requests changes on a blocking finding;
// only its approval becomes a comment. The login is matched as GitHub
// matches it, in any case.
func TestAHeldAuthorsReviewMayRequestChangesButNotApprove(t *testing.T) {
	cases := []struct {
		level  review.Verdicts
		author string
		want   review.Verdicts
	}{
		{review.VerdictsAll, "pm-sam", review.VerdictsRequestChangesOnly},
		{review.VerdictsAll, "PM-Sam", review.VerdictsRequestChangesOnly},
		{review.VerdictsApproveOnly, "pm-sam", review.VerdictsCommentOnly},
		{review.VerdictsAll, "alice", review.VerdictsAll},
	}
	for _, c := range cases {
		synctest.Test(t, func(t *testing.T) {
			list := func() (prs.Listing, error) {
				return prs.Listing{PRs: []prs.PR{{Number: 9, Author: c.author, Engagement: prs.New, CI: prs.CIPassing}}}, nil
			}
			edit := func(cfg *Config) { cfg.Verdicts, cfg.NoApproveAuthors = c.level, []string{"pm-sam"} }
			b := newBench(t, setup{list: list, edit: edit})
			synctest.Wait()
			argv := strings.Join(b.reviewer.command(t, 9).Args, " ")
			if c.want != review.VerdictsAll && !strings.Contains(argv, "--verdicts "+c.want.String()) {
				t.Errorf("%s by @%s: the prompt is %q", c.level, c.author, argv[max(len(argv)-60, 0):])
			}
			b.reviewer.finish(t, 9, 0, answer(9, "commented"))
			<-b.results
			if job := b.job(t, 9); job.Verdicts != c.want {
				t.Errorf("%s by @%s: the job carries %s, want %s", c.level, c.author, job.Verdicts, c.want)
			}
		})
	}
}
