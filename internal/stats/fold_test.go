package stats

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/venabots/autoreview/internal/ledger"
)

func run(at int64, panel []ledger.PanelEntry, findings []ledger.Finding) ledger.Record {
	return ledger.Record{
		V:        ledger.Version,
		ID:       fmt.Sprintf("r%d", at),
		At:       at,
		Source:   "autoreview",
		Repo:     ptr("acme/widgets"),
		PR:       ptr(uint64(1)),
		Decision: ptr("commented"),
		Risk:     ptr("LOW"),
		Panel:    panel,
		Findings: findings,
		Reviewed: true,
	}
}

// entry is a panelist that reported a model. A negative count is no count.
func entry(name, model string, ok *bool, findings int) ledger.PanelEntry {
	e := ledger.PanelEntry{Name: name, Model: &model, OK: ok}
	if findings >= 0 {
		e.Findings = ptr(uint64(findings))
	}
	return e
}

func finding(severity string, count uint32, dropped bool, sources ...ledger.Source) ledger.Finding {
	return ledger.Finding{Severity: &severity, FlaggedBy: sources, Count: count, Dropped: dropped}
}

func cohortOn(t *testing.T, cohorts []cohort, backend string) cohort {
	t.Helper()
	for _, c := range cohorts {
		if c.key.backend == backend {
			return c
		}
	}
	t.Fatalf("no cohort on %s", backend)
	return cohort{}
}

func TestCohortsFoldTheRosterAndTheKeptFindings(t *testing.T) {
	yes, no := ptr(true), ptr(false)
	runs := []ledger.Record{
		run(100,
			[]ledger.PanelEntry{entry("codex", "gpt-5.5", yes, 3), entry("claude", "claude-opus-5", yes, 1)},
			[]ledger.Finding{
				finding("HIGH", 1, false, source("codex", "gpt-5.5")),
				finding("LOW", 2, false, source("codex", "gpt-5.5"), source("claude", "claude-opus-5")),
				// A bare backend name is the one claude on the panel.
				finding("MEDIUM", 1, true, source("claude")),
			},
		),
		run(200,
			[]ledger.PanelEntry{entry("codex", "gpt-5.5", no, -1), entry("claude", "claude-opus-5", yes, 0)},
			nil,
		),
	}
	cohorts := fold(runs).cohorts
	if len(cohorts) != 2 {
		t.Fatalf("fold gave %d cohorts, want 2", len(cohorts))
	}
	codex := cohortOn(t, cohorts, "codex")
	wantCodex := cohort{
		key: key{"codex", "gpt-5.5"}, runs: 2, answered: 1, failed: 1,
		rawFindings: 3, rawRuns: 1, kept: 2, keptUnique: 1, keptHigh: 1,
		keepNum: 2, keepDen: 3, firstAt: 100, lastAt: 200,
	}
	if !reflect.DeepEqual(codex, wantCodex) {
		t.Errorf("codex = %+v\n   want %+v", codex, wantCodex)
	}
	if got := codex.availability(); !got.known || got.value != 0.5 {
		t.Errorf("codex availability = %+v, want a half", got)
	}
	if got := codex.sample(); got != "thin" {
		t.Errorf("codex sample = %q, want thin", got)
	}

	claude := cohortOn(t, cohorts, "claude")
	// Kept in run 1 (1) against reported (1); reported 0 in run 2. The
	// disagreement counts against it.
	wantClaude := cohort{
		key: key{"claude", "claude-opus-5"}, runs: 2, answered: 2,
		rawFindings: 1, rawRuns: 2, kept: 1, dropped: 1,
		keepNum: 1, keepDen: 1, firstAt: 100, lastAt: 200,
	}
	if !reflect.DeepEqual(claude, wantClaude) {
		t.Errorf("claude = %+v\n    want %+v", claude, wantClaude)
	}
	if cohorts[0].key.backend != "codex" {
		t.Errorf("the cohort with the most kept findings is not first: %+v", cohorts[0].key)
	}
}

func TestARunWithNoReviewTextDoesNotDragTheKeepRate(t *testing.T) {
	// One reviewed run kept 1 of 2. Then a run whose review text was never
	// read reported 3. The rate is 1 of 2, not 1 of 5.
	reviewed := run(1,
		[]ledger.PanelEntry{entry("codex", "gpt-5.5", ptr(true), 2)},
		[]ledger.Finding{finding("LOW", 1, false, source("codex", "gpt-5.5"))},
	)
	textLess := run(2, []ledger.PanelEntry{entry("codex", "gpt-5.5", ptr(true), 3)}, nil)
	textLess.Reviewed = false
	c := fold([]ledger.Record{reviewed, textLess}).cohorts[0]
	if c.keepNum != 1 || c.keepDen != 2 {
		t.Errorf("keep rate = %d of %d, want 1 of 2", c.keepNum, c.keepDen)
	}
	if c.rawFindings != 5 {
		t.Errorf("rawFindings = %d, want 5: the reported counts still show under RAW/RUN", c.rawFindings)
	}
}

func TestKeptNeverExceedsReportedInTheRate(t *testing.T) {
	runs := []ledger.Record{run(1,
		[]ledger.PanelEntry{entry("codex", "gpt-5.5", ptr(true), 1)},
		[]ledger.Finding{
			finding("LOW", 1, false, source("codex", "gpt-5.5")),
			finding("LOW", 1, false, source("codex", "gpt-5.5")),
		},
	)}
	c := fold(runs).cohorts[0]
	if c.kept != 2 || c.keepNum != 1 || c.keepDen != 1 {
		t.Errorf("kept %d, rate %d of %d, want kept 2 and 1 of 1", c.kept, c.keepNum, c.keepDen)
	}
}

func TestTheFoldCountsEachPartOfARun(t *testing.T) {
	yes, no := ptr(true), ptr(false)
	gpt := key{"codex", "gpt-5.5"}
	cases := []struct {
		name string
		run  ledger.Record
		want cohort
	}{
		{
			name: "a panelist that never said whether it answered is launched and nothing more",
			run:  run(5, []ledger.PanelEntry{{Name: "codex", Model: ptr("gpt-5.5"), Findings: ptr(uint64(4))}}, nil),
			want: cohort{key: gpt, runs: 1, firstAt: 5, lastAt: 5},
		},
		{
			name: "a count from a panelist that failed is not a reported count",
			run:  run(5, []ledger.PanelEntry{entry("codex", "gpt-5.5", no, 4)}, nil),
			want: cohort{key: gpt, runs: 1, failed: 1, firstAt: 5, lastAt: 5},
		},
		{
			name: "an answer with no count has no keep rate",
			run: run(5, []ledger.PanelEntry{entry("codex", "gpt-5.5", yes, -1)},
				[]ledger.Finding{finding("LOW", 1, false, source("codex"))}),
			want: cohort{key: gpt, runs: 1, answered: 1, kept: 1, keptUnique: 1, firstAt: 5, lastAt: 5},
		},
		{
			name: "a finding that names one model twice is credited once",
			run: run(5, []ledger.PanelEntry{entry("codex", "gpt-5.5", yes, 2)},
				[]ledger.Finding{finding("CRITICAL", 1, false, source("codex"), source("gpt-5.5"))}),
			want: cohort{
				key: gpt, runs: 1, answered: 1, rawFindings: 2, rawRuns: 1,
				kept: 1, keptUnique: 1, keptHigh: 1, keepNum: 1, keepDen: 2, firstAt: 5, lastAt: 5,
			},
		},
		{
			name: "a finding more panelists raised than it names is not unique",
			run: run(5, []ledger.PanelEntry{entry("codex", "gpt-5.5", yes, 1)},
				[]ledger.Finding{finding("MEDIUM", 3, false, source("codex"))}),
			want: cohort{
				key: gpt, runs: 1, answered: 1, rawFindings: 1, rawRuns: 1,
				kept: 1, keepNum: 1, keepDen: 1, firstAt: 5, lastAt: 5,
			},
		},
		{
			name: "a dropped finding is not kept, not unique and not high",
			run: run(5, []ledger.PanelEntry{entry("codex", "gpt-5.5", yes, 1)},
				[]ledger.Finding{finding("HIGH", 1, true, source("codex"))}),
			want: cohort{
				key: gpt, runs: 1, answered: 1, rawFindings: 1, rawRuns: 1,
				dropped: 1, keepDen: 1, firstAt: 5, lastAt: 5,
			},
		},
		{
			name: "one cohort seated twice is launched twice and rated once",
			run: run(5,
				[]ledger.PanelEntry{entry("codex", "gpt-5.5", yes, 1), entry("codex-b", "gpt-5.5", yes, 2)},
				[]ledger.Finding{
					finding("LOW", 1, false, source("codex")),
					finding("LOW", 1, false, source("codex-b")),
					finding("LOW", 1, false, source("gpt-5.5")),
					finding("LOW", 1, false, source("codex", "gpt-5.5")),
				}),
			want: cohort{
				key: gpt, runs: 2, answered: 2, rawFindings: 3, rawRuns: 2,
				kept: 4, keptUnique: 4, keepNum: 3, keepDen: 3, firstAt: 5, lastAt: 5,
			},
		},
		{
			name: "a launch time is kept for the median",
			run: run(5, []ledger.PanelEntry{
				{Name: "codex", Model: ptr("gpt-5.5"), OK: no, DurationSecs: ptr(uint64(40)), ExitCode: ptr(int32(1))},
			}, nil),
			want: cohort{key: gpt, runs: 1, failed: 1, durations: []uint64{40}, firstAt: 5, lastAt: 5},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := fold([]ledger.Record{c.run}).cohorts
			if len(got) != 1 || !reflect.DeepEqual(got[0], c.want) {
				t.Errorf("fold = %+v\nwant  %+v", got, c.want)
			}
		})
	}
}

func TestACreditToANameNotOnThePanelIsCountedAndNotScored(t *testing.T) {
	runs := []ledger.Record{
		run(1, []ledger.PanelEntry{entry("codex", "gpt-5.5", ptr(true), 1)}, []ledger.Finding{
			finding("LOW", 2, false, source("codex"), source(" CodeRabbitAI ")),
			finding("LOW", 1, false, source("coderabbitai")),
			finding("LOW", 1, true, source("claude", "claude-opus-5")),
		}),
	}
	f := fold(runs)
	want := map[string]int{"coderabbitai": 2, "claude": 1}
	if !maps.Equal(f.unresolved, want) {
		t.Errorf("unresolved = %v, want %v", f.unresolved, want)
	}
	if len(f.cohorts) != 1 || f.cohorts[0].kept != 1 || f.cohorts[0].keptUnique != 0 {
		t.Errorf("cohorts = %+v, want codex alone with one kept finding that is not unique", f.cohorts)
	}
}

func TestCohortsAreOrderedByKeptThenRunsThenKey(t *testing.T) {
	yes := ptr(true)
	panel := func(names ...string) []ledger.PanelEntry {
		out := make([]ledger.PanelEntry, len(names))
		for i, n := range names {
			out[i] = entry(n, "m", yes, 1)
		}
		return out
	}
	runs := []ledger.Record{
		run(1, panel("zeta", "beta", "alpha", "busy"), []ledger.Finding{finding("LOW", 1, false, source("zeta"))}),
		run(2, panel("busy"), nil),
	}
	var got []string
	for _, c := range fold(runs).cohorts {
		got = append(got, c.key.backend)
	}
	if want := []string{"zeta", "busy", "alpha", "beta"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestTheFoldLeavesTheRunsAsTheyWere(t *testing.T) {
	runs := []ledger.Record{
		run(1, []ledger.PanelEntry{entry("codex", "gpt-5.5", ptr(true), 1)},
			[]ledger.Finding{finding("LOW", 1, false, source("codex"), source("nobody"))}),
		run(2, []ledger.PanelEntry{entry("claude", "claude-opus-5", ptr(false), -1)}, nil),
	}
	before, err := snapshot(runs)
	if err != nil {
		t.Fatal(err)
	}
	fold(runs)
	selectRuns(runs, ptr(int64(2)), "widgets")
	overviewOf(runs)
	after, err := snapshot(runs)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Errorf("the runs changed:\n before %s\n after  %s", before, after)
	}
}

func snapshot(runs []ledger.Record) (string, error) {
	var out []byte
	for _, r := range runs {
		line, err := r.MarshalJSON()
		if err != nil {
			return "", err
		}
		out = append(append(out, line...), '\n')
	}
	return string(out), nil
}

func TestSelectionByTimeAndRepo(t *testing.T) {
	widgets := run(100, nil, nil)
	gadgets := run(200, nil, nil)
	gadgets.Repo = ptr("acme/Gadgets")
	nowhere := run(300, nil, nil)
	nowhere.Repo = nil
	runs := []ledger.Record{widgets, gadgets, nowhere}
	cases := []struct {
		name  string
		since *int64
		repo  string
		want  []int64
	}{
		{"no filter", nil, "", []int64{100, 200, 300}},
		{"since is at or after", ptr(int64(200)), "", []int64{200, 300}},
		{"since between two runs", ptr(int64(150)), "", []int64{200, 300}},
		{"a repo in another case", nil, "gadgets", []int64{200}},
		{"a needle in another case", nil, "GADGETS", []int64{200}},
		{"a part of the owner", nil, "acme", []int64{100, 200}},
		{"a run with no repo matches no repo filter", nil, "a", []int64{100, 200}},
		{"both filters", ptr(int64(150)), "widgets", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []int64
			for _, r := range selectRuns(runs, c.since, c.repo) {
				got = append(got, r.At)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("selected the runs at %v, want %v", got, c.want)
			}
		})
	}
}

func TestTheOverviewCountsWhatTheRunsConcluded(t *testing.T) {
	second := run(200, nil, nil)
	second.Repo = ptr("Acme/Widgets")
	third := run(50, nil, nil)
	third.Repo, third.Decision, third.Risk, third.Source = nil, nil, nil, "panel"
	o := overviewOf([]ledger.Record{run(100, nil, nil), second, third})
	if o.runs != 3 || o.repos != 1 {
		t.Errorf("runs %d, repos %d, want 3 runs in 1 repo: a repo is one repo in any case", o.runs, o.repos)
	}
	if o.firstAt == nil || *o.firstAt != 50 || o.lastAt == nil || *o.lastAt != 200 {
		t.Errorf("span = %v to %v, want 50 to 200", o.firstAt, o.lastAt)
	}
	for name, got := range map[string]map[string]int{"decisions": o.decisions, "risks": o.risks, "sources": o.sources} {
		want := map[string]map[string]int{
			"decisions": {"commented": 2, "unknown": 1},
			"risks":     {"LOW": 2, "unknown": 1},
			"sources":   {"autoreview": 2, "panel": 1},
		}[name]
		if !maps.Equal(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	empty := overviewOf(nil)
	if empty.runs != 0 || empty.firstAt != nil || empty.lastAt != nil || len(empty.decisions) != 0 {
		t.Errorf("the overview of no runs = %+v, want nothing", empty)
	}
}

func TestACohortThatNeverAnswersIsCalledOut(t *testing.T) {
	var runs []ledger.Record
	for i := range 5 {
		runs = append(runs, run(int64(i), []ledger.PanelEntry{entry("claude-fable", "fable", ptr(false), -1)}, nil))
	}
	want := []string{"fable on claude answered 0 of 5 launches"}
	if got := attention(fold(runs).cohorts); !slices.Equal(got, want) {
		t.Errorf("attention = %q, want %q", got, want)
	}
	if got := attention(fold(runs[:4]).cohorts); len(got) != 0 {
		t.Errorf("attention = %q, want none: four launches is too few to say", got)
	}
}

func TestACohortThatAnswersHalfTheTimeIsNotCalledOut(t *testing.T) {
	cases := []struct {
		name             string
		answered, failed uint64
		want             int
	}{
		{"exactly half", 3, 3, 0},
		{"under half", 2, 3, 1},
		{"launches that never said do not count", 0, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cohorts := []cohort{{key: key{"codex", "m"}, runs: 50, answered: c.answered, failed: c.failed}}
			if got := attention(cohorts); len(got) != c.want {
				t.Errorf("attention = %q, want %d notes", got, c.want)
			}
		})
	}
}
