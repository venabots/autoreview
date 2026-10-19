package doctor

// Tool is one program a run needs, and what it says its version is.
type Tool struct {
	Name     string
	Required bool
	Found    bool
	// Version is the first line of `NAME --version`. It is other people's
	// text.
	Version string
}

// needed are the programs a run calls. gh and dash-p are required: without
// them no review starts. The agents are each optional, as long as one is
// there.
var needed = []struct {
	name     string
	required bool
}{
	{"gh", true},
	{"dash-p", true},
	{"git", false},
	{"claude", false},
	{"codex", false},
	{"opencode", false},
}

// toolsOf asks version about each program. version returns false for a
// program that is not installed.
func toolsOf(version func(name string) (string, bool)) []Tool {
	tools := make([]Tool, len(needed))
	for i, n := range needed {
		v, found := version(n.name)
		tools[i] = Tool{Name: n.name, Required: n.required, Found: found, Version: v}
	}
	return tools
}
