package setup

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// The summary's choices, as the accessible form numbers them.
const (
	save  = "1"
	back  = "2"
	leave = "3"
)

// every answers each page, then the summary with choice.
func every(choice string) []string {
	return []string{"codex:gpt-5.5", "0", "claude:opus-4.8 codex", "2", "4", "1h", "5m", "n", "5", "2", "3", choice}
}

func TestTheSummaryListsEveryAnswerBeforeItAsksToSave(t *testing.T) {
	_, out, err := ask(t, every(save)...)
	if err != nil {
		t.Fatalf("Ask: %v\n%s", err, out)
	}
	summary := out[strings.Index(out, "Step 12 of 12"):]
	for _, want := range []string{
		"The agent that runs each review: codex:gpt-5.5",
		"The models in the review panel: claude:opus-4.8 codex",
		"The verdicts a review may post: approve only",
		"Reviews one PR gets in a run: 4",
		"The rest before a reviewed PR is checked again: 1h",
		"The wait between two looks at the PR list: 5m",
		"Autofix your PRs when the view opens: no",
		"Fixes in a row on one of your PRs while nobody else acts: 5",
		"What autofix still fixes on one of your PRs once it is approved: conflicts and review comments",
		"Reviews that run at once: 3",
		"Save these settings?",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("the summary does not say %q:\n%s", want, summary)
		}
	}
}

func TestLeavingWithoutSavingWritesNothing(t *testing.T) {
	got, out, err := ask(t, every(leave)...)
	if !errors.Is(err, ErrNotSaved) {
		t.Fatalf("Ask = %+v, %v, want ErrNotSaved\n%s", got, err, out)
	}
}

func TestGoingBackAsksEachQuestionAgainFromTheAnswersGiven(t *testing.T) {
	answers := append(every(back), "claude", "0", "", "1", "2", "30m", "2m", "y", "3", "1", "2", save)
	got, out, err := ask(t, answers...)
	if err != nil {
		t.Fatalf("Ask: %v\n%s", err, out)
	}
	if strings.Count(out, "Step 1 of 12") != 2 {
		t.Errorf("the form did not start again at step 1:\n%s", out)
	}
	// The second round starts from the first one's answers: an empty
	// answer keeps the models added before, and the others change.
	if got.Orchestrator != "claude" || !reflect.DeepEqual(got.Panelists, []string{"claude:opus-4.8", "codex"}) ||
		got.MaxPasses != 2 || got.Jobs != 2 || got.Verdicts != "approve-and-request-changes" {
		t.Errorf("Ask = %+v, want the second round's answers", got)
	}
}

func TestThePanelLineSaysWhatABlankPanelSeats(t *testing.T) {
	a := answersOf(current())
	if text := summaryText(a, nil); !strings.Contains(text, "The models in the review panel: every backend on your PATH") {
		t.Errorf("summary:\n%s", text)
	}
}
