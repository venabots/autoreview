// Package config reads the person's settings file: the values a run uses
// when no flag and no variable gives one.
package config

import (
	"path/filepath"
	"strings"
)

// EnvConfig names the variable that moves the settings file, or turns it
// off. The tests and the sandbox turn it off or move it, so they never read
// the settings of the person who runs them, and never write them.
const EnvConfig = "AUTOREVIEW_CONFIG"

// Path is where the settings file is. ok is false when
// `$AUTOREVIEW_CONFIG=off` asked for no file, or when there is no home to
// put one in. An empty variable counts as unset.
func Path(env func(string) string) (path string, ok bool) {
	if named := env(EnvConfig); named != "" {
		if strings.EqualFold(named, "off") {
			return "", false
		}
		return named, true
	}
	dir := env("XDG_CONFIG_HOME")
	if dir == "" {
		home := env("HOME")
		if home == "" {
			return "", false
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autoreview", "config.toml"), true
}
