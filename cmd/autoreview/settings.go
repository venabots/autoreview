package main

import "github.com/venabots/autoreview/internal/config"

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
