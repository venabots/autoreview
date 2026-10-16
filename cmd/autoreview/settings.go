package main

import (
	"fmt"

	"github.com/venabots/autoreview/internal/config"
	"github.com/venabots/autoreview/internal/panel"
)

// settings reads the person's settings file. With $AUTOREVIEW_CONFIG=off,
// or with no home to keep one in, there is none, and every value comes
// from a flag, a variable or the built-in default.
func settings(env func(string) string) (config.File, error) {
	path, ok := config.Path(env)
	if !ok {
		return config.File{}, nil
	}
	return config.Load(path)
}

// panelSettings is the panel the settings file names, for `autoreview
// panel`. Each spec is checked the way --panelist and --synth check theirs.
func panelSettings(env func(string) string) (panel.Settings, error) {
	file, err := settings(env)
	if err != nil {
		return panel.Settings{}, err
	}
	seated := make([]panel.Spec, 0, len(file.Panel.Panelists))
	for _, raw := range file.Panel.Panelists {
		spec, err := panel.ParseSpec(raw, "panelist")
		if err != nil {
			return panel.Settings{}, fmt.Errorf("panel.panelists in %s: %w", file.Path, err)
		}
		seated = append(seated, spec)
	}
	saved := panel.Settings{Panelists: seated}
	if file.Panel.Synth == nil || *file.Panel.Synth == "" {
		return saved, nil
	}
	synth, err := panel.ParseSpec(*file.Panel.Synth, "synth")
	if err != nil {
		return panel.Settings{}, fmt.Errorf("panel.synth in %s: %w", file.Path, err)
	}
	saved.Synth = &synth
	return saved, nil
}
