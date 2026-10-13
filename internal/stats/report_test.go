package stats

import (
	"strings"
	"testing"
)

func TestDatesFromEpochs(t *testing.T) {
	cases := []struct {
		epoch int64
		want  string
	}{
		{0, "1970-01-01"},
		{1_785_542_400, "2026-08-01"},
		{1_757_376_000, "2025-09-09"},
		{86_399, "1970-01-01"},
		{86_400, "1970-01-02"},
		{-1, "1969-12-31"},
		{1_709_164_800, "2024-02-29"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			if got := date(c.epoch); got != c.want {
				t.Errorf("date(%d) = %q, want %q", c.epoch, got, c.want)
			}
		})
	}
}

func TestARatioReadsAsAPointAndAnInterval(t *testing.T) {
	cases := []struct {
		name string
		k, n uint64
		want string
	}{
		{"most of many", 179, 185, "97% (93-99)"},
		{"nothing under it", 0, 0, "-"},
		{"none of a few", 0, 5, "0% (0-43)"},
		{"one of one", 1, 1, "100% (21-100)"},
		{"a half rounds to even", 1, 8, "12% (2-47)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ratioLabel(ratioOf(c.k, c.n)); got != c.want {
				t.Errorf("ratioLabel(%d of %d) = %q, want %q", c.k, c.n, got, c.want)
			}
		})
	}
}

func TestANameFromTheLedgerIsMadeSafeAndShort(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"a plain name", "gpt-5.5", "gpt-5.5"},
		{"an escape sequence loses its escape", "\x1b[31mred\x07", "[31mred"},
		{"a bidi override and a zero-width space are dropped", "abc\u202edef\u200bghi", "abcdefghi"},
		{"a newline and a tab are dropped", "two\nlines\there", "twolineshere"},
		{"forty wide is kept", strings.Repeat("m", 40), strings.Repeat("m", 40)},
		{"forty-one wide is cut to forty", strings.Repeat("m", 41), strings.Repeat("m", 39) + "…"},
		{"wide characters count twice", strings.Repeat("月", 21), strings.Repeat("月", 19) + "…"},
		{"brackets are text", "claude-opus-5[1m]", "claude-opus-5[1m]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := label(c.in); got != c.want {
				t.Errorf("label(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func testCohort() cohort {
	return cohort{
		key:         key{backend: "codex", model: "gpt-5.5"},
		runs:        12,
		answered:    11,
		failed:      1,
		rawFindings: 22,
		rawRuns:     11,
		kept:        9,
		keptUnique:  4,
		keptHigh:    2,
		dropped:     1,
		keepNum:     9,
		keepDen:     22,
		durations:   []uint64{30, 90, 60},
		firstAt:     0,
		lastAt:      86_400,
	}
}

func TestThePlainReportIsGreppable(t *testing.T) {
	r := report{
		overview: overview{
			runs: 12, repos: 2, firstAt: ptr(int64(0)), lastAt: ptr(int64(86_400)),
			decisions: map[string]int{"commented": 8, "approved": 4},
		},
		cohorts:   []cohort{testCohort()},
		attention: []string{"fable on claude answered 0 of 5 launches"},
		ledger:    "/l/ledger.jsonl",
	}
	want := `12 reviews from 1970-01-01 to 1970-01-02 across 2 repos

MODEL    BACKEND  RUNS  AVAIL        RAW/RUN  KEPT  UNIQUE  HIGH+  DROPPED  KEEP RATE    SAMPLE
gpt-5.5  codex    12    92% (65-99)  2.0      9     4       2      1        41% (23-61)  emerging

decisions: 8 commented, 4 approved
median time per panelist (panel runs): gpt-5.5 1m00s
needs attention: fable on claude answered 0 of 5 launches
ledger: /l/ledger.jsonl
`
	if got := plain(r); got != want {
		t.Errorf("plain report:\n%s\nwant:\n%s", got, want)
	}
}

func TestTheHeadlineSaysWhatTheReviewsCover(t *testing.T) {
	day := int64(86_400)
	cases := []struct {
		name string
		o    overview
		want string
	}{
		{"one review on one day", overview{runs: 1, repos: 1, firstAt: ptr(day), lastAt: ptr(day + 60)}, "1 review on 1970-01-02 across 1 repo"},
		{"reviews over days", overview{runs: 3, repos: 2, firstAt: ptr(int64(0)), lastAt: ptr(day)}, "3 reviews from 1970-01-01 to 1970-01-02 across 2 repos"},
		{"reviews that name no repo", overview{runs: 2, firstAt: ptr(int64(0)), lastAt: ptr(int64(0))}, "2 reviews on 1970-01-01"},
		{"no reviews", overview{}, "0 reviews"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := headline(report{overview: c.o}); got != c.want {
				t.Errorf("headline = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTheFooterSaysWhatTheTableCannot(t *testing.T) {
	cases := []struct {
		name string
		r    report
		want []string
	}{
		{
			name: "only the ledger when there is nothing else",
			r:    report{ledger: "/l"},
			want: []string{"ledger: /l"},
		},
		{
			name: "the most frequent first, then by name",
			r: report{
				overview: overview{
					decisions: map[string]int{"commented": 2, "approved": 2, "unknown": 5},
					risks:     map[string]int{"LOW": 1},
				},
				ledger: "/l",
			},
			want: []string{"decisions: 5 unknown, 2 approved, 2 commented", "risk: 1 LOW", "ledger: /l"},
		},
		{
			name: "names not on a panel, the five most credited by name",
			r: report{
				unresolved: map[string]int{"a": 1, "b": 3, "c": 1, "d": 2, "e": 1, "f": 1, "g": 1},
				ledger:     "/l",
			},
			want: []string{
				"not scored: 10 findings credited to a name not on its panel (b x3, d x2, a x1, c x1, e x1, 2 more)",
				"ledger: /l",
			},
		},
		{
			name: "one name not on a panel",
			r:    report{unresolved: map[string]int{"coderabbitai": 1}, ledger: "/l"},
			want: []string{"not scored: 1 finding credited to a name not on its panel (coderabbitai x1)", "ledger: /l"},
		},
		{
			name: "text from the ledger is made safe",
			r: report{
				overview:   overview{decisions: map[string]int{"\x1b[2Jwiped": 1}, risks: map[string]int{"LOW\u202e": 1}},
				cohorts:    []cohort{{key: key{"codex", "\x1b]0;title\x07m"}, durations: []uint64{3700}}},
				attention:  []string{"bad\x1b[31m on codex answered 0 of 5 launches"},
				unresolved: map[string]int{"bot\r\n": 1},
				ledger:     "/l",
			},
			want: []string{
				"decisions: 1 [2Jwiped",
				"risk: 1 LOW",
				"median time per panelist (panel runs): ]0;titlem 1h01m",
				"needs attention: bad[31m on codex answered 0 of 5 launches",
				"not scored: 1 finding credited to a name not on its panel (bot x1)",
				"ledger: /l",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := footer(c.r)
			if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Errorf("footer:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(c.want, "\n"))
			}
		})
	}
}

func TestALaunchTimeTooLongForADurationDoesNotWrap(t *testing.T) {
	if got := seconds(^uint64(0)); got <= 0 {
		t.Errorf("seconds of the largest count = %v, want the longest duration", got)
	}
	if got := seconds(61).String(); got != "1m1s" {
		t.Errorf("seconds(61) = %s, want 1m1s", got)
	}
}

func TestColumnsAlignByWidthOnTheScreen(t *testing.T) {
	rows := [][]string{
		{"MODEL", "RUNS", "SAMPLE"},
		{"月之暗面", "7", "thin"},
		{"gpt", "12", ""},
	}
	want := "MODEL     RUNS  SAMPLE\n" +
		"月之暗面  7     thin\n" +
		"gpt       12\n"
	if got := align(rows); got != want {
		t.Errorf("align:\n%s\nwant:\n%s", got, want)
	}
}
