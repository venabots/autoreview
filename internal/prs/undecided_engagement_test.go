package prs

import (
	"fmt"
	"testing"
)

// prWithMyLastReview is a PR that carol pushed to at 09:00, and whose last
// review, at 10:00, is lastAuthor's with body. `me` reviewed at 10:00 too.
// extraComments are more comments, as JSON list entries.
func prWithMyLastReview(body, lastAuthor, extraComments string) string {
	return fmt.Sprintf(`{"number":7,
	  "comments":{"nodes":[%s]},
	  "reviews":{"nodes":[{"author":{"login":"me"},"submittedAt":"2026-08-09T10:00:00Z"}]},
	  "lastReview":{"nodes":[{"author":{"login":%q},"submittedAt":"2026-08-09T10:00:00Z","body":%q}]},
	  "commits":{"nodes":[{"commit":{"committedDate":"2026-08-09T09:00:00Z","author":{"user":{"login":"carol"}}}}]}}`,
		extraComments, lastAuthor, body)
}

const shortPanel = "Reviewed with claude (all clear), but glm-5.3 failed, so I'm withholding approval for now."

func TestMyLastReviewWithNoVerdictLeavesThePRDueAgain(t *testing.T) {
	cases := []struct {
		name string
		json string
		want Engagement
	}{
		{"a short panel", prWithMyLastReview(shortPanel, "me", ""), Undecided},
		{"an older skill's short panel", prWithMyLastReview("No decision: only 2 of 4 reviewers answered.", "me", ""), Undecided},
		{"a head that moved during the review", prWithMyLastReview("No decision: the head moved during the review. These comments are for 35b9069.", "me", ""), Undecided},
		{"a deliberate comment review", prWithMyLastReview("Comments only. No decision.", "me", ""), Seen},
		{"an approval", prWithMyLastReview("LGTM", "me", ""), Seen},
		{"a panel too small because one agent is installed", prWithMyLastReview("No decision: the panel was too small for a verdict.", "me", ""), Seen},
		{
			"my own comment after the review is my last word",
			prWithMyLastReview(shortPanel, "me", `{"author":{"login":"me"},"updatedAt":"2026-08-09T11:00:00Z"}`),
			Seen,
		},
		{
			"somebody else's comment after it is still an update",
			prWithMyLastReview(shortPanel, "me", `{"author":{"login":"bob"},"updatedAt":"2026-08-09T11:00:00Z"}`),
			Updated,
		},
		{"a last review by somebody else at the same second", prWithMyLastReview(shortPanel, "bob", ""), Seen},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := engagement(mustNode(t, c.json), "me"); got != c.want {
				t.Errorf("engagement = %s, want %s", got, c.want)
			}
		})
	}
}

func TestAnUndecidedPRIsActionableAndSortsBeforeASeenOne(t *testing.T) {
	if !(PR{Engagement: Undecided}).Actionable() {
		t.Error("an undecided PR is not actionable, so nobody reviews it again")
	}
	if Undecided <= Updated || Undecided >= Seen {
		t.Error("undecided does not sort between updated and seen")
	}
}
