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
	if env(EnvPanelists) != "" || len(file.Panel.Panelists) == 0 {
		return nil, nil
	}
	for _, raw := range file.Panel.Panelists {
		if _, err := panel.ParseSpec(raw, "panelist"); err != nil {
			return nil, fmt.Errorf("panel.panelists in %s: %w", file.Path, err)
		}
	}
	return file.Panel.Panelists, nil
}
