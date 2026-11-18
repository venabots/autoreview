package panel

import (
	"io/fs"
	"strings"
	"testing"

	bundle "github.com/venabots/autoreview"
)

// skillText is a file of the bundled skills, by its path under skills/.
func skillText(t *testing.T, name string) string {
	t.Helper()
	text, err := fs.ReadFile(bundle.Skills(), name)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

// oneLine joins a wrapped paragraph, so a phrase is found across a line
// break.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// riskBlock is the Risk section of a panelist prompt, from its heading to
// the next one.
func riskBlock(t *testing.T, text string) string {
	t.Helper()
	start := strings.Index(text, "### 5. Risk")
	end := strings.Index(text, "### 6. Findings")
	if start < 0 || end < start {
		t.Fatal("the prompt has no Risk section before its findings")
	}
	return text[start:end]
}

// The approval of another author's PR stops at the synthesized Risk, and
// the panelists' ratings set its floor. A prompt that rated a diff and a
// PR on different lines would approve under one what it holds under the
// other.
func TestEveryPanelistRatesTheRiskOnTheSameLines(t *testing.T) {
	diff := riskBlock(t, skillText(t, "panel-review/prompts/review.md"))
	if pr := riskBlock(t, skillText(t, "panel-review/prompts/review-pr.md")); pr != diff {
		t.Error("the Risk section of review-pr.md differs from review.md")
	}
	for _, phrase := range []string{
		"`Risk (low):`", "`Risk (medium):`", "`Risk (high):`",
		"a revert does not undo it",
		"take the higher rating",
	} {
		if !strings.Contains(oneLine(diff), phrase) {
			t.Errorf("the Risk section lacks %q", phrase)
		}
	}
	synthesis := oneLine(skillText(t, "panel-review/SKILL.md"))
	for _, phrase := range []string{
		"The panelists' ratings set a floor.",
		"at least the highest one a panelist gave",
		"`Risk (unknown):` line, or no line, is no rating",
	} {
		if !strings.Contains(synthesis, phrase) {
			t.Errorf("the synthesis does not say %q", phrase)
		}
	}
}
