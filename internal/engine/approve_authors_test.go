package engine

import (
	"strings"
	"testing"
	"testing/synctest"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/review"
)

// A team approves by review only the PRs of the people it names. The PR
// of anyone else is still reviewed and still gets a request for changes
// on a blocking finding; only its approval becomes a comment. The login
// is matched as GitHub matches it, in any case, and an empty list lets a
// review approve every author.
func TestAReviewApprovesOnlyTheNamedAuthorsPRs(t *testing.T) {
	cases := []struct {
		level   review.Verdicts
		approve []string
		author  string
		want    review.Verdicts
	}{
		{review.VerdictsAll, []string{"alice"}, "pm-sam", review.VerdictsRequestChangesOnly},
		{review.VerdictsApproveOnly, []string{"alice"}, "pm-sam", review.VerdictsCommentOnly},
		{review.VerdictsAll, []string{"alice"}, "alice", review.VerdictsAll},
		{review.VerdictsAll, []string{"Alice"}, "alice", review.VerdictsAll},
		{review.VerdictsAll, []string{"dependabot"}, "dependabot[bot]", review.VerdictsAll},
		{review.VerdictsAll, nil, "pm-sam", review.VerdictsAll},
	}
	for _, c := range cases {
		synctest.Test(t, func(t *testing.T) {
			list := func() (prs.Listing, error) {
				return prs.Listing{PRs: []prs.PR{{Number: 9, Author: c.author, Engagement: prs.New, CI: prs.CIPassing}}}, nil
			}
			edit := func(cfg *Config) { cfg.Verdicts, cfg.ApproveAuthors = c.level, c.approve }
			b := newBench(t, setup{list: list, edit: edit})
			synctest.Wait()
			argv := strings.Join(b.reviewer.command(t, 9).Args, " ")
			if c.want != review.VerdictsAll && !strings.Contains(argv, "--verdicts "+c.want.String()) {
				t.Errorf("%s by @%s: the prompt is %q", c.level, c.author, argv[max(len(argv)-60, 0):])
			}
			b.reviewer.finish(t, 9, 0, answer(9, "commented"))
			<-b.results
			if job := b.job(t, 9); job.Verdicts != c.want {
				t.Errorf("%s by @%s, approving %q: the job carries %s, want %s", c.level, c.author, c.approve, job.Verdicts, c.want)
			}
		})
	}
}
