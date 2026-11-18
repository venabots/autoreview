package engine

import (
	"slices"
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

// With a ceiling, the PR of an author the person does not name keeps the
// run's level and carries the ceiling to the review, so a clean small
// change merges on the review. A named author's PR carries no ceiling.
func TestAnotherAuthorsPRCarriesTheRiskCeilingInsteadOfLosingItsApproval(t *testing.T) {
	cases := []struct {
		author    string
		wantLevel review.Verdicts
		wantRisk  review.Risk
	}{
		{"pm-sam", review.VerdictsAll, review.RiskMedium},
		{"alice", review.VerdictsAll, review.RiskNone},
	}
	for _, c := range cases {
		synctest.Test(t, func(t *testing.T) {
			list := func() (prs.Listing, error) {
				return prs.Listing{PRs: []prs.PR{{Number: 9, Author: c.author, Engagement: prs.New, CI: prs.CIPassing}}}, nil
			}
			edit := func(cfg *Config) {
				cfg.ApproveAuthors, cfg.ApproveOthersUpTo = []string{"alice"}, review.RiskMedium
			}
			b := newBench(t, setup{list: list, edit: edit})
			synctest.Wait()
			argv := strings.Join(b.reviewer.command(t, 9).Args, " ")
			if carries := strings.Contains(argv, "--max-risk medium"); carries != (c.wantRisk != review.RiskNone) || strings.Contains(argv, "--verdicts") {
				t.Errorf("@%s: the prompt is %q", c.author, argv[max(len(argv)-60, 0):])
			}
			b.reviewer.finish(t, 9, 0, answer(9, "approved"))
			<-b.results
			if job := b.job(t, 9); job.Verdicts != c.wantLevel || job.MaxRisk != c.wantRisk {
				t.Errorf("@%s: the job carries %s up to %s, want %s up to %s", c.author, job.Verdicts, job.MaxRisk, c.wantLevel, c.wantRisk)
			}
		})
	}
}

// The approval has landed by the time the review reports its risk. When
// that risk is above the ceiling, the skill did not hold the approval, and
// the log says so, as it does for a verdict the level forbids.
func TestAnApprovalAboveTheRiskCeilingIsNoted(t *testing.T) {
	cases := []struct {
		risk  string
		noted bool
	}{
		{"HIGH", true},
		{"MEDIUM", false},
		{"", false},
	}
	for _, c := range cases {
		synctest.Test(t, func(t *testing.T) {
			list := func() (prs.Listing, error) {
				return prs.Listing{PRs: []prs.PR{{Number: 9, Author: "pm-sam", Engagement: prs.New, CI: prs.CIPassing}}}, nil
			}
			edit := func(cfg *Config) {
				cfg.ApproveAuthors, cfg.ApproveOthersUpTo = []string{"alice"}, review.RiskMedium
			}
			b := newBench(t, setup{list: list, edit: edit})
			synctest.Wait()
			b.reviewer.finish(t, 9, 0, strings.Replace(answer(9, "approved"), `"risk":"LOW"`, `"risk":"`+c.risk+`"`, 1))
			<-b.results
			want := "PR #9 was approved at risk high, above its ceiling of medium: the review skill did not follow --max-risk"
			if noted := slices.Contains(b.notes(), want); noted != c.noted {
				t.Errorf("approved at %q: notes %q", c.risk, b.notes())
			}
		})
	}
}
