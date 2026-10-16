package doctor

import (
	"slices"
	"testing"

	"github.com/venabots/autoreview/internal/ledger"
)

func ptr[T any](v T) *T { return &v }

func seat(name, model string, ok bool) ledger.PanelEntry {
	return ledger.PanelEntry{Name: name, Model: ptr(model), OK: ptr(ok)}
}

func review(at int64, pr uint64, decision string, panel ...ledger.PanelEntry) ledger.Record {
	r := ledger.Record{At: at, Repo: ptr("acme/widgets"), PR: ptr(pr), Panel: panel, CostUSD: ptr(1.5)}
	if decision != "" {
		r.Decision = ptr(decision)
	}
	return r
}

var (
	claudeOK = seat("claude-opus", "claude-opus-5-5", true)
	grokOK   = seat("opencode-xai-grok-4.7", "xai/grok-4.7", true)
	grokDown = seat("opencode-xai-grok-4.7", "xai/grok-4.7", false)
	glmOK    = seat("opencode-zai-coding-plan-glm-5.3", "zai-coding-plan/glm-5.3", true)
	glmDown  = seat("opencode-zai-coding-plan-glm-5.3", "zai-coding-plan/glm-5.3", false)
)

func modelNamed(t *testing.T, h History, name string) Model {
	t.Helper()
	i := slices.IndexFunc(h.Models, func(m Model) bool { return m.Model == name })
	if i < 0 {
		t.Fatalf("no model %q in %+v", name, h.Models)
	}
	return h.Models[i]
}

func TestAModelIsCountedOnlyOnLaunchesInsideTheWindow(t *testing.T) {
	h := historyOf([]ledger.Record{
		review(50, 1, "approved", claudeOK, grokOK),
		review(150, 2, "commented", claudeOK, grokDown),
		review(160, 3, "commented", claudeOK, grokDown),
	}, 100)
	grok := modelNamed(t, h, "grok-4.7")
	if grok.Launched != 2 || grok.Answered != 0 || grok.Health() != Down {
		t.Errorf("grok = %+v (%v), want 0 of 2 and down", grok, grok.Health())
	}
	if grok.LastAnswer != 50 {
		t.Errorf("grok last answered at %d, want 50 from before the window", grok.LastAnswer)
	}
	if h.Reviews != 2 || h.SpendUSD != 3 {
		t.Errorf("window has %d reviews and $%.2f, want 2 and $3.00", h.Reviews, h.SpendUSD)
	}
}

func TestTheModelsAreSpelledAsStatsSpellsThem(t *testing.T) {
	h := historyOf([]ledger.Record{
		review(10, 1, "approved", seat("opencode-glm", "glm-5.3", true), glmDown),
	}, 0)
	glm := modelNamed(t, h, "glm-5.3")
	if glm.Backend != "opencode" || glm.Launched != 2 {
		t.Errorf("two spellings of glm-5.3 gave %+v, want one opencode row of 2", h.Models)
	}
}

func TestAModelUnderHalfIsFailingAndTheWorstComesFirst(t *testing.T) {
	cases := []struct {
		answered, launched int
		want               Health
	}{
		{0, 3, Down},
		{1, 3, Failing},
		{2, 4, Healthy},
		{3, 3, Healthy},
	}
	for _, c := range cases {
		if got := (Model{Answered: c.answered, Launched: c.launched}).Health(); got != c.want {
			t.Errorf("%d of %d is %v, want %v", c.answered, c.launched, got, c.want)
		}
	}
	h := historyOf([]ledger.Record{
		review(10, 1, "commented", claudeOK, grokDown, glmOK),
	}, 0)
	if h.Models[0].Model != "grok-4.7" {
		t.Errorf("the first model is %q, want the one that is down", h.Models[0].Model)
	}
}

func TestAPRIsStalledWhenItsLatestReviewHasNoVerdictAndAMissingPanelist(t *testing.T) {
	h := historyOf([]ledger.Record{
		review(10, 1, "commented", claudeOK, grokDown),
		review(20, 2, "commented", claudeOK, grokDown),
		review(30, 2, "approved", claudeOK, grokDown),
		review(40, 3, "commented", claudeOK, glmOK),
		review(50, 4, "", claudeOK, glmDown),
		review(60, 5, "none"),
	}, 0)
	var got []uint64
	for _, s := range h.Stalled {
		got = append(got, s.PR)
	}
	if want := []uint64{5, 4, 1}; !slices.Equal(got, want) {
		t.Errorf("stalled PRs = %v, want %v (newest first; #2 got a verdict, #3 had a full panel)", got, want)
	}
	if s := h.Stalled[2]; s.Seated != 2 || s.Answered != 1 || s.Decision != "commented" {
		t.Errorf("#1 = %+v, want commented with 1 of 2 answered", s)
	}
	if s := h.Stalled[1]; s.Decision != "none" {
		t.Errorf("#4 has no decision recorded, read as %q", s.Decision)
	}
}

func TestARecordWithNoPRIsNeverStalled(t *testing.T) {
	r := review(10, 1, "commented", grokDown)
	r.PR = nil
	if h := historyOf([]ledger.Record{r}, 0); len(h.Stalled) != 0 {
		t.Errorf("a panel run with no PR is stalled: %+v", h.Stalled)
	}
}

func TestHistoryDoesNotChangeItsRecords(t *testing.T) {
	records := []ledger.Record{review(10, 1, "approved", claudeOK), review(200, 2, "approved", claudeOK)}
	before := slices.Clone(records)
	historyOf(records, 100)
	if len(records) != 2 || records[0].At != before[0].At || records[1].At != before[1].At {
		t.Errorf("historyOf changed its argument: %+v", records)
	}
}
