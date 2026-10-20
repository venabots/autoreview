package doctor

import (
	"reflect"
	"testing"

	"github.com/venabots/autoreview/internal/ledger"
)

// The settings form offers each model the machine has seated. It offers
// the spelling the model last ran under: the counts join spellings, but
// an opencode model with its provider cut off would not run.
func TestEachModelSeenIsOfferedUnderTheSpecItLastRanUnder(t *testing.T) {
	got := SeenModels([]ledger.Record{
		review(10, 1, "approved", claudeOK, glmOK, seat("codex", "gpt-5.6-sol", true)),
		review(20, 2, "commented", claudeOK, glmDown, seat("codex", "gpt-5.6-sol", false)),
		review(30, 3, "approved", seat("codex", "GPT-5.6-sol", true), ledger.PanelEntry{Name: "opencode", OK: ptr(true)}),
	})
	want := []Seen{
		{Spec: "codex:GPT-5.6-sol", Launched: 3, Answered: 2},
		{Spec: "claude:claude-opus-5-5", Launched: 2, Answered: 2},
		{Spec: "opencode:zai-coding-plan/glm-5.3", Launched: 2, Answered: 1},
		{Spec: "opencode", Launched: 1, Answered: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SeenModels =\n%+v\nwant\n%+v", got, want)
	}
}

func TestAModelThatReportedNoneIsSpelledFromItsPanelistName(t *testing.T) {
	got := SeenModels([]ledger.Record{
		review(10, 1, "approved", ledger.PanelEntry{Name: "claude-opus", Model: ptr("?"), OK: ptr(true)}),
	})
	if want := []Seen{{Spec: "claude:opus", Launched: 1, Answered: 1}}; !reflect.DeepEqual(got, want) {
		t.Errorf("SeenModels = %+v, want %+v", got, want)
	}
}
