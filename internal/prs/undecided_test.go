package prs

import "testing"

func TestOnlyANoVerdictThatCanPassIsReadAsUndecided(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{"Reviewed with claude (all clear) and codex (all clear), but grok and glm-5.3 both failed, so I'm withholding approval for now.", true},
		{"Reviewed with claude (all clear), but codex failed, so I'm withholding approval for now.\n\n- a finding", true},
		{"No decision: only 2 of 4 reviewers answered.", true},
		{"No decision: some findings could not be filed.", true},
		{"No decision: the new commits need a panel review, and it did not run.", true},
		{"No decision: the head moved during the review. These comments are for 35b9069.", true},
		{"No decision: the head moved during the re-check. I looked at 35b9069.", true},
		{"  No decision: only 1 of 3 reviewers answered.  \n", true},
		{"Comments only. No decision.", false},
		{"No decision: this is my own PR.", false},
		{"No decision: this PR is a draft.", false},
		{"No decision: the panel was too small for a verdict.", false},
		{"No decision: 2 optional comments are still open.", false},
		{"LGTM", false},
		{"", false},
		{"- a finding\n\nReviewed with claude, but codex failed, so I'm withholding approval for now.", false},
	}
	for _, c := range cases {
		if got := undecided(c.body); got != c.want {
			t.Errorf("undecided(%q) = %v, want %v", c.body, got, c.want)
		}
	}
}
