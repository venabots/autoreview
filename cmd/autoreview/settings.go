package main

import (
	"github.com/venabots/autoreview/internal/cli"
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
// panel`, read as a review reads it.
func panelSettings(env func(string) string) (panel.Settings, error) {
	file, err := settings(env)
	if err != nil {
		return panel.Settings{}, err
	}
	seated, synth, err := cli.PanelSpecs(file)
	if err != nil {
		return panel.Settings{}, err
	}
	return panel.Settings{Panelists: seated, Synth: synth}, nil
}
