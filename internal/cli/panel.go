package cli

import (
	"fmt"

	"github.com/venabots/autoreview/internal/config"
	"github.com/venabots/autoreview/internal/panel"
)

// EnvPanelists names the variable panel-review.sh reads for its panel.
const EnvPanelists = "PANEL_REVIEW_PANELISTS"

// panelists is the panel the settings file names for a review. It is nil
// when the variable names one: panel-review.sh reads the variable itself,
// and a variable wins over the file.
func panelists(file config.File, env func(string) string) ([]string, error) {
	if _, _, err := PanelSpecs(file); err != nil {
		return nil, err
	}
	if env(EnvPanelists) != "" || len(file.Panel.Panelists) == 0 {
		return nil, nil
	}
	return file.Panel.Panelists, nil
}

// PanelSpecs reads the panel and the synthesizer the file names, checked
// the way --panelist and --synth check theirs. A review and `autoreview
// panel` both read the [panel] keys through it, so the two agree on them.
// synth is nil when the file names none.
func PanelSpecs(file config.File) (seated []panel.Spec, synth *panel.Spec, err error) {
	seated = make([]panel.Spec, 0, len(file.Panel.Panelists))
	for _, raw := range file.Panel.Panelists {
		spec, err := panel.ParseSpec(raw, "panelist")
		if err != nil {
			return nil, nil, fmt.Errorf("panel.panelists in %s: %w", file.Path, err)
		}
		seated = append(seated, spec)
	}
	if file.Panel.Synth == nil {
		return seated, nil, nil
	}
	spec, err := panel.ParseSpec(*file.Panel.Synth, "synth")
	if err != nil {
		return nil, nil, fmt.Errorf("panel.synth in %s: %w", file.Path, err)
	}
	return seated, &spec, nil
}
