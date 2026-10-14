package config

import (
	"reflect"
	"strings"
	"testing"
)

func values() Values {
	return Values{
		Orchestrator: "codex:gpt-5.5", Fallback: "none", Jobs: 4, Timeout: 0, Budget: "2.50",
		Focus: `the "fast" path \ and a tab	here`, Verdicts: "approve-only",
		Watch: "5m", Rest: "1h", MaxPasses: 2, MaxIdle: 7, CIWait: "45m",
		Approved: true, Dependabot: false, Stacked: true, SkipCI: false,
		AutofixCap: 5, Panelists: []string{"claude:opus-4.8", "codex"}, Synth: "claude",
	}
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
		AutofixCap: *f.Mine.AutofixCap, Panelists: f.Panel.Panelists, Synth: *f.Panel.Synth,
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
