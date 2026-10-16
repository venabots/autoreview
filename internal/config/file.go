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
	// panelistsNamed says the file names panel.panelists. An empty list and
	// a missing key decode alike, and only the first is a choice.
	panelistsNamed bool

	Review Review `toml:"review"`
	Loop   Loop   `toml:"loop"`
	PRs    PRs    `toml:"prs"`
	Mine   Mine   `toml:"mine"`
	Panel  Panel  `toml:"panel"`
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
	// Verdicts is approve-and-request-changes, approve-only or
	// comment-only.
	Verdicts *string `toml:"verdicts"`
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

// Mine is what is done to your own PRs.
type Mine struct {
	// AutofixCap is the most fixes a PR gets in a row while nobody else
	// acts on it.
	AutofixCap *int `toml:"autofix_cap"`
}

// Panel is the models that review together. Each one is written
// backend[:model], as `autoreview panel --panelist` takes it.
type Panel struct {
	// Panelists is the panel a review seats. Empty is every backend on
	// PATH.
	Panelists []string `toml:"panelists"`
	// Synth is the model that writes `autoreview panel`'s synthesis.
	Synth *string `toml:"synth"`
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
	return Parse(raw, path)
}

// Parse reads the text of a settings file that is, or will be, at path.
func Parse(raw []byte, path string) (File, error) {
	f := File{Path: path, Found: true}
	md, err := toml.Decode(string(raw), &f)
	if err != nil {
		return File{}, fmt.Errorf("reading the settings in %s: %w", path, err)
	}
	if unknown := md.Undecoded(); len(unknown) > 0 {
		return File{}, fmt.Errorf("%s sets %s, which is not a setting", path, unknown[0])
	}
	f.panelistsNamed = md.IsDefined("panel", "panelists")
	return f, nil
}

// keyed is each key with whether the file sets it, in the order the file
// lists them. A nil field is a key the file leaves out.
func (f File) keyed() []struct {
	name string
	set  bool
} {
	return []struct {
		name string
		set  bool
	}{
		{"review.orchestrator", f.Review.Orchestrator != nil}, {"review.fallback", f.Review.Fallback != nil},
		{"review.jobs", f.Review.Jobs != nil}, {"review.timeout", f.Review.Timeout != nil},
		{"review.budget", f.Review.Budget != nil}, {"review.focus", f.Review.Focus != nil},
		{"review.verdicts", f.Review.Verdicts != nil},
		{"loop.watch", f.Loop.Watch != nil}, {"loop.rest", f.Loop.Rest != nil},
		{"loop.max_passes", f.Loop.MaxPasses != nil}, {"loop.max_idle", f.Loop.MaxIdle != nil},
		{"loop.ci_wait", f.Loop.CIWait != nil},
		{"prs.approved", f.PRs.Approved != nil}, {"prs.dependabot", f.PRs.Dependabot != nil},
		{"prs.stacked", f.PRs.Stacked != nil}, {"prs.skip_ci", f.PRs.SkipCI != nil},
		{"mine.autofix_cap", f.Mine.AutofixCap != nil},
		{"panel.panelists", f.Panel.Panelists != nil || f.panelistsNamed}, {"panel.synth", f.Panel.Synth != nil},
	}
}

// KeyNames is every key a settings file can set, in the order the file
// lists them.
func KeyNames() []string {
	keys := File{}.keyed()
	names := make([]string, len(keys))
	for i, k := range keys {
		names[i] = k.name
	}
	return names
}

// Keys is the keys the file sets. A key it leaves out takes the built-in
// default, and the setup step writes it back as a comment.
func (f File) Keys() map[string]bool {
	set := map[string]bool{}
	for _, k := range f.keyed() {
		if k.set {
			set[k.name] = true
		}
	}
	return set
}
