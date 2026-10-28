package doctor

// Tool is one program a run needs, and what it says its version is.
type Tool struct {
	Name     string
	Required bool
	Found    bool
	// Version is the first line of `NAME --version`. It is other people's
	// text.
	Version string
	// Min is the oldest version a run works with, or "" for any. TooOld
	// says the version installed is older; a version that cannot be read
	// is never too old, so the doctor does not guess.
	Min    string
	TooOld bool
}

// needed are the programs a run calls. gh and dash-p are required: without
// them no review starts. The agents are each optional, as long as one is
// there.
//
// dash-p must be 0.5.0 or later: an older one starts opencode 2.x outside
// the panelist's --cwd, so the panelist reviews the wrong directory and
// nothing fails.
var needed = []struct {
	name     string
	required bool
	min      string
}{
	{"gh", true, ""},
	{"dash-p", true, "0.5.0"},
	{"git", false, ""},
	{"claude", false, ""},
	{"codex", false, ""},
	{"opencode", false, ""},
}

// toolsOf asks version about each program. version returns false for a
// program that is not installed.
func toolsOf(version func(name string) (string, bool)) []Tool {
	tools := make([]Tool, len(needed))
	for i, n := range needed {
		v, found := version(n.name)
		tools[i] = Tool{Name: n.name, Required: n.required, Found: found, Version: v, Min: n.min, TooOld: found && older(versionIn(v), n.min)}
	}
	return tools
}
