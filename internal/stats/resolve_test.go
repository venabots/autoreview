package stats

import (
	"slices"
	"testing"

	"github.com/venabots/autoreview/internal/ledger"
)

func source(name string, model ...string) ledger.Source {
	if len(model) == 0 {
		return ledger.Source{Name: name}
	}
	return ledger.Source{Name: name, Model: &model[0]}
}

func TestASourceIsMatchedToTheRosterHoweverItIsSpelled(t *testing.T) {
	var none key
	roster := []seat{
		{"opencode-glm", key{"opencode", "glm-5.3"}},
		{"opencode-grok", key{"opencode", "grok-4.6"}},
		{"codex", key{"codex", "gpt-5.6-sol"}},
	}
	twoClaude := []seat{
		{"claude", key{"claude", "claude-fable-5"}},
		{"claude-opus", key{"claude", "claude-opus-5"}},
	}
	fable := []seat{{"claude", key{"claude", "claude-fable-5"}}}
	noModelSeat := []seat{{"claude", key{"claude", "unknown"}}}
	twoWithNoModel := []seat{
		{"claude", key{"claude", "unknown"}},
		{"claude-b", key{"claude", "unknown"}},
		{"codex", key{"codex", "unknown"}},
	}
	sameCohortTwice := []seat{
		{"claude-a", key{"claude", "claude-opus-5"}},
		{"codex", key{"codex", "gpt-5.5"}},
		{"claude-b", key{"claude", "claude-opus-5"}},
	}
	cases := []struct {
		name   string
		source ledger.Source
		roster []seat
		want   key
	}{
		// v1's cases, in v1's order.
		{"by name, in any case", source("Opencode-Grok"), roster, key{"opencode", "grok-4.6"}},
		{"by model", source("glm-5.3"), roster, key{"opencode", "glm-5.3"}},
		{"by backend and model", source("opencode-glm-5.3"), roster, key{"opencode", "glm-5.3"}},
		{"the one codex", source("codex"), roster, key{"codex", "gpt-5.6-sol"}},
		{"by family", source("grok"), roster, key{"opencode", "grok-4.6"}},
		{
			"a backend and a model the run launched under a longer name",
			source("opencode", "glm-5.3"), roster, key{"opencode", "glm-5.3"},
		},
		{
			"however the model was spelled",
			source("opencode", "zai-coding-plan/glm-5.3"), roster, key{"opencode", "glm-5.3"},
		},
		{"a model no panelist ran is not scored", source("claude", "claude-opus-5"), roster, none},
		{"a credited name that is no panelist is not scored", source("ci-sdk", "v2"), roster, none},
		{
			"the model decides, not the name",
			source("claude", "claude-opus-5"), twoClaude, key{"claude", "claude-opus-5"},
		},
		{"two on one backend and no model is ambiguous", source("opencode"), roster, none},
		{"a bare backend is its one panelist", source("claude"), fable, key{"claude", "claude-fable-5"}},
		{"an outside bot is not the panelist", source("claude-code-bot"), fable, none},
		{"a bare model the panel did not run is not credited", source("claude-opus-5"), fable, none},
		{
			"a model credits the one panelist whose model was not recorded",
			source("claude", "claude-opus-5"), noModelSeat, key{"claude", "unknown"},
		},
		{"a known and different model is not credited", source("claude", "claude-opus-5"), fable, none},
		{"a review bot is not a panelist", source("coderabbitai"), roster, none},
		{"a review bot with no roster to check", source("coderabbitai"), nil, none},
		// The edges of the same rules.
		{"space round the name", source("  CODEX "), roster, key{"codex", "gpt-5.6-sol"}},
		{"a family before a slash", source("ollama"), []seat{{"opencode", key{"opencode", "ollama/grok-4.6"}}}, key{"opencode", "ollama/grok-4.6"}},
		{
			"two panelists with no model on one backend are one cohort",
			source("claude", "claude-opus-5"), twoWithNoModel, key{"claude", "unknown"},
		},
		{
			"one cohort seated twice is still one cohort",
			source("claude"), sameCohortTwice, key{"claude", "claude-opus-5"},
		},
		{
			"one cohort seated twice, named by its model",
			source("claude-opus-5"), sameCohortTwice, key{"claude", "claude-opus-5"},
		},
		{"a model with no name to go with it", source("", "gpt-5.6-sol"), roster, none},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := slices.Clone(c.roster)
			got, ok := resolve(c.source, c.roster)
			if ok != (c.want != none) || (ok && got != c.want) {
				t.Errorf("resolve(%q) = %+v, %v, want %+v", c.source.Name, got, ok, c.want)
			}
			if !slices.Equal(c.roster, before) {
				t.Error("resolve changed the roster")
			}
		})
	}
}

func TestTheRosterKeepsThePanelOrder(t *testing.T) {
	panel := []ledger.PanelEntry{
		{Name: " Codex ", Model: ptr("gpt-5.5")},
		{Name: "opencode-glm", Model: ptr("zai-coding-plan/glm-5.3")},
		{Name: "claude-fable"},
	}
	want := []seat{
		{"codex", key{"codex", "gpt-5.5"}},
		{"opencode-glm", key{"opencode", "glm-5.3"}},
		{"claude-fable", key{"claude", "fable"}},
	}
	if got := rosterOf(panel); !slices.Equal(got, want) {
		t.Errorf("rosterOf = %+v, want %+v", got, want)
	}
}
