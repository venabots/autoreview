package config

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func values() Values {
	return Values{
		Orchestrator: "codex:gpt-5.5", Fallback: "none", Jobs: 4, Timeout: 0, Budget: "2.50",
		Focus: `the "fast" path \ and a tab	here`, Verdicts: "approve-only",
		Watch: "5m", Rest: "1h", MaxPasses: 2, MaxIdle: 7, CIWait: "45m",
		Approved: true, Dependabot: false, Stacked: true, SkipCI: false,
		Autofix: false, AutofixCap: 5, AutofixApproved: "comments", Panelists: []string{"claude:opus-4.8", "codex"}, Synth: "claude",
		Set: allKeys(),
	}
}

func allKeys() map[string]bool {
	keys := map[string]bool{}
	for _, k := range KeyNames() {
		keys[k] = true
	}
	return keys
}

func rendered(t *testing.T, v Values) string {
	t.Helper()
	raw, err := Render(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestARenderedFileReadsBackAsTheSameValues(t *testing.T) {
	v := values()
	f, err := Load(fileWith(t, rendered(t, v)))
	if err != nil {
		t.Fatalf("the rendered file does not read back: %v\n%s", err, rendered(t, v))
	}
	got := Values{
		Orchestrator: *f.Review.Orchestrator, Fallback: *f.Review.Fallback, Jobs: *f.Review.Jobs, Timeout: *f.Review.Timeout,
		Budget: *f.Review.Budget, Focus: *f.Review.Focus, Verdicts: *f.Review.Verdicts,
		Watch: *f.Loop.Watch, Rest: *f.Loop.Rest, MaxPasses: *f.Loop.MaxPasses, MaxIdle: *f.Loop.MaxIdle, CIWait: *f.Loop.CIWait,
		Approved: *f.PRs.Approved, Dependabot: *f.PRs.Dependabot, Stacked: *f.PRs.Stacked, SkipCI: *f.PRs.SkipCI,
		Autofix: *f.Mine.Autofix, AutofixCap: *f.Mine.AutofixCap, AutofixApproved: *f.Mine.AutofixApproved, Panelists: f.Panel.Panelists, Synth: *f.Panel.Synth,
		Set: f.Keys(),
	}
	if !reflect.DeepEqual(got, v) {
		t.Errorf("read back %+v, want %+v", got, v)
	}
}

func TestEveryKeyOfTheRenderedFileHasACommentAbove(t *testing.T) {
	lines := strings.Split(rendered(t, values()), "\n")
	for i, line := range lines {
		if strings.Contains(line, " = ") && (i == 0 || !strings.HasPrefix(lines[i-1], "#")) {
			t.Errorf("line %d, %q, has no comment above it", i+1, line)
		}
	}
}

func TestAnEmptyPanelIsAnEmptyList(t *testing.T) {
	v := values()
	v.Panelists = nil
	if got := rendered(t, v); !strings.Contains(got, "\npanelists = []\n") {
		t.Errorf("an empty panel renders as:\n%s", got)
	}
}

// A key the person did not choose stays a comment that shows the default.
// Written as a value, it would hold the default of the day it was written,
// and a later default would never reach that person.
func TestAKeyThePersonDidNotChooseIsAComment(t *testing.T) {
	v := values()
	v.Set = map[string]bool{"review.jobs": true, "panel.panelists": true}
	text := rendered(t, v)
	for _, want := range []string{"\njobs = 4\n", "\npanelists = [\"claude:opus-4.8\", \"codex\"]\n", "\n# timeout = 0\n", "\n# verdicts = \"approve-only\"\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("the file lacks %q:\n%s", want, text)
		}
	}
	f, err := Load(fileWith(t, text))
	if err != nil || f.Review.Jobs == nil || f.Review.Timeout != nil || f.Review.Verdicts != nil {
		t.Errorf("read back: %+v, %v; want jobs set and timeout and verdicts unset", f.Review, err)
	}
}

func TestTheKeysAFileSetsAreTheOnesItNames(t *testing.T) {
	f, err := Load(fileWith(t, "[review]\njobs = 4\n[panel]\npanelists = []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Keys(); !reflect.DeepEqual(got, map[string]bool{"review.jobs": true, "panel.panelists": true}) {
		t.Errorf("Keys = %v", got)
	}
}

func TestTheKeysOfABuiltFileAreTheFieldsItSets(t *testing.T) {
	jobs, synth := 4, "codex"
	f := File{Review: Review{Jobs: &jobs}, Panel: Panel{Synth: &synth, Panelists: []string{"codex", "claude"}}}
	if got := f.Keys(); !reflect.DeepEqual(got, map[string]bool{"review.jobs": true, "panel.synth": true, "panel.panelists": true}) {
		t.Errorf("Keys = %v", got)
	}
}

// Each key the template marks must be one that a file can set, or a chosen
// value would be written as a comment.
func TestEveryKeyTheTemplateMarksIsASetting(t *testing.T) {
	f, err := Load(fileWith(t, rendered(t, values())))
	if err != nil {
		t.Fatal(err)
	}
	known := f.Keys()
	marked := regexp.MustCompile(`\.Mark "([^"]+)"`).FindAllStringSubmatch(fileTemplate.Root.String(), -1)
	for _, m := range marked {
		if !known[m[1]] {
			t.Errorf("the template marks %q, which a file does not set", m[1])
		}
	}
	if len(marked) != len(KeyNames()) || len(known) != len(KeyNames()) {
		t.Errorf("the template marks %d keys, a full file sets %d, and KeyNames lists %d", len(marked), len(known), len(KeyNames()))
	}
}
