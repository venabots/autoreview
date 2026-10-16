package doctor

import (
	"slices"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/panel"
)

func hasOnly(names ...string) func(string) bool {
	return func(n string) bool { return slices.Contains(names, n) }
}

func TestThePanelIsPickedAsAReviewPicksIt(t *testing.T) {
	cases := []struct {
		name       string
		env        string
		file       []string
		wantSpecs  []panel.Spec
		wantSource string
	}{
		{
			"the variable wins, split on spaces and commas",
			"claude:opus, opencode:xai/grok-4.7", []string{"codex"},
			[]panel.Spec{{Backend: "claude", Model: "opus"}, {Backend: "opencode", Model: "xai/grok-4.7"}},
			"$PANEL_REVIEW_PANELISTS",
		},
		{
			"then the settings file",
			"", []string{"codex:gpt-5.6-sol"},
			[]panel.Spec{{Backend: "codex", Model: "gpt-5.6-sol"}},
			"the settings file",
		},
		{
			"then every backend on PATH",
			"  ", nil,
			[]panel.Spec{{Backend: "claude"}},
			"every backend on PATH",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := panelOf(c.env, c.file, hasOnly("claude"))
			if err != nil || !slices.Equal(got.Specs, c.wantSpecs) || got.Source != c.wantSource {
				t.Errorf("panelOf = %+v, %v, want %+v from %s", got, err, c.wantSpecs, c.wantSource)
			}
		})
	}
}

func TestABadPanelistNamesWhereItWasWritten(t *testing.T) {
	_, err := panelOf("gemini", nil, hasOnly())
	if err == nil || !strings.Contains(err.Error(), "$PANEL_REVIEW_PANELISTS") {
		t.Errorf("err = %v, want one that names the variable", err)
	}
}
