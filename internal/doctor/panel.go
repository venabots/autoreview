package doctor

import (
	"fmt"
	"strings"

	"github.com/venabots/autoreview/internal/panel"
)

// Seated is the panel a review would seat now, and where it came from.
type Seated struct {
	Specs  []panel.Spec
	Source string
}

// panelOf picks the panel the way a review does: $PANEL_REVIEW_PANELISTS,
// then the settings file, then every backend on PATH. A doctor that asked
// a different panel would report on models no review runs.
func panelOf(env string, file []string, has func(string) bool) (Seated, error) {
	if raw := strings.Fields(strings.ReplaceAll(env, ",", " ")); len(raw) > 0 {
		specs, err := parseAll(raw, "$PANEL_REVIEW_PANELISTS")
		return Seated{Specs: specs, Source: "$PANEL_REVIEW_PANELISTS"}, err
	}
	if len(file) > 0 {
		specs, err := parseAll(file, "panel.panelists")
		return Seated{Specs: specs, Source: "the settings file"}, err
	}
	return Seated{Specs: panel.Autodetect(has), Source: "every backend on PATH"}, nil
}

func parseAll(raw []string, where string) ([]panel.Spec, error) {
	specs := make([]panel.Spec, 0, len(raw))
	for _, r := range raw {
		spec, err := panel.ParseSpec(r, "panelist")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", where, err)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// specName is a spec as a person writes it.
func specName(s panel.Spec) string {
	if s.Model == "" {
		return s.Backend
	}
	return s.Backend + ":" + s.Model
}
