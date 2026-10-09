package panel

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSpecsParseWithAndWithoutAModel(t *testing.T) {
	cases := []struct {
		raw     string
		want    Spec
		wantErr string
	}{
		{"claude", Spec{Backend: "claude"}, ""},
		{"opencode:zai/glm-5.3", Spec{Backend: "opencode", Model: "zai/glm-5.3"}, ""},
		{"gpt4", Spec{}, "unknown panelist backend"},
		{"claude:", Spec{}, "names no model"},
	}
	for _, c := range cases {
		got, err := ParseSpec(c.raw, "panelist")
		if c.wantErr == "" && (err != nil || got != c.want) {
			t.Errorf("%q: %+v, %v", c.raw, got, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%q: error %v, want one containing %q", c.raw, err, c.wantErr)
		}
	}
}

func TestAutodetectFindsWhatIsInstalledInV1sOrder(t *testing.T) {
	has := func(name string) bool { return name == "opencode" || name == "codex" }
	want := []Spec{{Backend: "codex"}, {Backend: "opencode"}}
	if got := Autodetect(has); !reflect.DeepEqual(got, want) {
		t.Errorf("Autodetect = %+v, want %+v", got, want)
	}
	if got := Autodetect(func(string) bool { return false }); len(got) != 0 {
		t.Errorf("nothing installed found %+v", got)
	}
}

func TestIDsStayUniqueAndPathSafe(t *testing.T) {
	specs := []Spec{
		{Backend: "claude"},
		{Backend: "opencode", Model: "zai/glm-5.3"},
		{Backend: "claude"},
		{Backend: "claude", Model: "opus-4.8"},
		{Backend: "claude"},
	}
	before := slices.Clone(specs)
	var ids []string
	for _, p := range ResolvePanel(specs) {
		ids = append(ids, p.ID)
	}
	want := []string{"claude", "opencode-zai-glm-5.3", "claude-2", "claude-opus-4.8", "claude-3"}
	if !slices.Equal(ids, want) {
		t.Errorf("ids %q, want %q", ids, want)
	}
	if !slices.Equal(specs, before) {
		t.Errorf("ResolvePanel changed its argument: %+v", specs)
	}
	// A model that tries to climb out of the output directory cannot: the
	// separators are gone, so the whole thing is one directory name.
	escaped := ResolvePanel([]Spec{{Backend: "codex", Model: "../../etc"}})[0].ID
	if escaped != "codex-..-..-etc" || strings.Contains(escaped, "/") {
		t.Errorf("id %q is a path component with a separator in it", escaped)
	}
}

func TestThePromptNeverRidesInTheArgv(t *testing.T) {
	got := DashpArgs(Panelist{ID: "claude", Backend: "claude"}, "/repo", 600, false)
	want := []string{"--harness=claude", "--output-format=text", "--timeout=600", "--cwd=/repo", "--perms=read-only"}
	if !slices.Equal(got, want) {
		t.Errorf("argv %q, want %q", got, want)
	}
}

func TestAnIsolatedPanelistMayRunThings(t *testing.T) {
	got := DashpArgs(Panelist{ID: "codex", Backend: "codex", Model: "gpt-5"}, "/wt", 900, true)
	want := []string{"--harness=codex", "--output-format=text", "--timeout=900", "--cwd=/wt", "--model=gpt-5", "--dangerously-skip-permissions"}
	if !slices.Equal(got, want) {
		t.Errorf("argv %q, want %q", got, want)
	}
}

// dash-p forwards a flag it does not know only as one token, and a value
// split from its flag would be dropped in silence.
func TestEveryDashpFlagWithAValueIsOneToken(t *testing.T) {
	argvs := [][]string{
		DashpArgs(Panelist{ID: "x", Backend: "opencode", Model: "m"}, "/a b", 1, true),
		synthArgs(Spec{Backend: "claude", Model: "opus"}, "/c", 5),
	}
	for _, argv := range argvs {
		for _, arg := range argv {
			if !strings.HasPrefix(arg, "--") || (!strings.Contains(arg, "=") && arg != "--dangerously-skip-permissions") {
				t.Errorf("%q in %q is not one --flag=value token", arg, argv)
			}
		}
	}
}

func TestTheModelLineIsReadFromTheFirstLineOnly(t *testing.T) {
	cases := []struct {
		stdout string
		want   string
		ok     bool
	}{
		{"Model: gpt-5.6\nGoal (clear): ...", "gpt-5.6", true},
		{"Model:   claude-opus-5  \n", "claude-opus-5", true},
		{"Model: m\r\nrest", "m", true},
		{"Goal: ...\nModel: quoted-later", "", false},
		{"Model:\n", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := ExtractModel(c.stdout)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: %q, %v; want %q, %v", c.stdout, got, ok, c.want, c.ok)
		}
	}
	// Bounded: a section heading is not the place for the characters a
	// panelist chose.
	long, _ := ExtractModel("Model: " + strings.Repeat("é", 500))
	if utf8.RuneCountInString(long) != maxModelRunes || !utf8.ValidString(long) {
		t.Errorf("a long model kept %d runes", utf8.RuneCountInString(long))
	}
}
