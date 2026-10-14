package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

// File is the settings file as it is written. A nil field is a key the
// file does not set, so a key set to 0 or to false is still a value.
//
// The file holds values only. The checks each value needs are the checks
// of the flag it stands for, and they are made where the flags are read.
type File struct {
	// Path is where the file is, or would be. Found is false when it is
	// not there: the first run is that case.
	Path  string `toml:"-"`
	Found bool   `toml:"-"`

	Review Review `toml:"review"`
	Loop   Loop   `toml:"loop"`
	PRs    PRs    `toml:"prs"`
}

// Review is how each review runs.
type Review struct {
	Orchestrator *string `toml:"orchestrator"`
	Fallback     *string `toml:"fallback"`
	Jobs         *int    `toml:"jobs"`
	// Timeout is in seconds, as --timeout is.
	Timeout *int    `toml:"timeout"`
	Budget  *string `toml:"budget"`
	Focus   *string `toml:"focus"`
}

// Loop is when the run looks and when it reviews again. An interval is
// written as --watch takes it: a bare number of minutes, or 30m, 1h, 2d.
type Loop struct {
	Watch     *string `toml:"watch"`
	Rest      *string `toml:"rest"`
	MaxPasses *int    `toml:"max_passes"`
	MaxIdle   *int    `toml:"max_idle"`
	CIWait    *string `toml:"ci_wait"`
}

// PRs is which PRs the run takes.
type PRs struct {
	Approved   *bool `toml:"approved"`
	Dependabot *bool `toml:"dependabot"`
	Stacked    *bool `toml:"stacked"`
	SkipCI     *bool `toml:"skip_ci"`
}

// Load reads the settings file at path. A file that is not there has no
// settings. A key the file does not know is an error that names it.
func Load(path string) (File, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return File{Path: path}, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("reading the settings in %s: %w", path, err)
	}
	f := File{Path: path, Found: true}
	md, err := toml.Decode(string(raw), &f)
	if err != nil {
		return File{}, fmt.Errorf("reading the settings in %s: %w", path, err)
	}
	if unknown := md.Undecoded(); len(unknown) > 0 {
		return File{}, fmt.Errorf("%s sets %s, which is not a setting", path, unknown[0])
	}
	return f, nil
}
