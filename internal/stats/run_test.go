package stats

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/venabots/autoreview/internal/ledger"
)

// fixture is testdata/ledger.jsonl: invented reviews of invented repos. It
// has models spelled several ways, panelists that seldom answer, findings
// credited to names not on the panel, a name with an escape sequence in it,
// a duplicate id, a record from a newer format and a torn line.
const fixture = "testdata/ledger.jsonl"

// fixtureNow is a time after every review in the fixture.
var fixtureNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

type result struct {
	stdout string
	stderr string
	status int
}

func noEnv(string) string {
	return ""
}

func runStats(env func(string) string, now time.Time, onTerminal bool, args ...string) result {
	var stdout, stderr bytes.Buffer
	status := Run(args, env, now, onTerminal, &stdout, &stderr)
	return result{stdout: stdout.String(), stderr: stderr.String(), status: status}
}

var generatedAt = regexp.MustCompile(`"generated_at": \d+`)

// withoutClock hides the one value of the JSON report that is the time it
// was made.
func withoutClock(s string) string {
	return generatedAt.ReplaceAllString(s, `"generated_at": 0`)
}

// TestTheReportsAreV1Reports compares each report with the one v1 0.23.0
// wrote for the same flags. The files under testdata are v1's output from
// this directory:
//
//	autoreview stats --ledger testdata/ledger.jsonl <flags>
func TestTheReportsAreV1Reports(t *testing.T) {
	cases := []struct {
		golden string
		args   []string
	}{
		{"plain.txt", nil},
		{"report.json", []string{"--json"}},
		{"since.txt", []string{"--since", "2026-07-15"}},
		{"since.json", []string{"--since", "2026-07-15", "--json"}},
		{"repo.txt", []string{"--repo", "WIDGETS"}},
		{"repo.json", []string{"--repo=gadgets", "--json"}},
		{"both.txt", []string{"--since=2026-07-01", "--repo", "gadgets"}},
		{"none.txt", []string{"--repo", "nowhere"}},
		{"none.json", []string{"--repo", "nowhere", "--json"}},
	}
	for _, c := range cases {
		t.Run(c.golden, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", c.golden))
			if err != nil {
				t.Fatal(err)
			}
			got := runStats(noEnv, fixtureNow, false, append([]string{"--ledger", fixture}, c.args...)...)
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("status %d, stderr %q, want 0 and nothing", got.status, got.stderr)
			}
			if withoutClock(got.stdout) != withoutClock(string(want)) {
				t.Errorf("the report is not v1's.\ngot:\n%s\nwant:\n%s", got.stdout, want)
			}
		})
	}
}

func TestJSONIsTheSameOnATerminal(t *testing.T) {
	piped := runStats(noEnv, fixtureNow, false, "--ledger", fixture, "--json")
	onTerminal := runStats(noEnv, fixtureNow, true, "--ledger", fixture, "--json")
	if piped.stdout != onTerminal.stdout {
		t.Error("--json differs on a terminal: a program reads it either way")
	}
	if !strings.Contains(piped.stdout, `"generated_at": 1788264000,`) {
		t.Errorf("generated_at is not the time that was passed in:\n%.200s", piped.stdout)
	}
}

func TestOnATerminalTheReportIsATableWithTheSameWords(t *testing.T) {
	plainText := runStats(noEnv, fixtureNow, false, "--ledger", fixture)
	onTerminal := runStats(noEnv, fixtureNow, true, "--ledger", fixture)
	if onTerminal.status != 0 || !strings.Contains(onTerminal.stdout, "\x1b[") || !strings.Contains(onTerminal.stdout, "╭") {
		t.Fatalf("status %d, and no colored table in:\n%s", onTerminal.status, onTerminal.stdout)
	}
	drawn := ansi.Strip(onTerminal.stdout)
	for field := range strings.FieldsSeq(plainText.stdout) {
		if !strings.Contains(drawn, field) {
			t.Errorf("the table has no %q, which the plain text has", field)
		}
	}
	// The escape sequence in a model name of the fixture must not reach the
	// terminal: every sequence on the screen is a style of ours.
	if strings.Contains(onTerminal.stdout, "\x1b[31mred") || strings.Contains(onTerminal.stdout, "\x07") {
		t.Errorf("a model name's own escape sequence reached the terminal:\n%q", onTerminal.stdout)
	}
}

func TestNoColorIsHonoredOnATerminal(t *testing.T) {
	env := func(name string) string {
		return map[string]string{"NO_COLOR": "1"}[name]
	}
	got := runStats(env, fixtureNow, true, "--ledger", fixture)
	if strings.Contains(got.stdout, "\x1b") || !strings.Contains(got.stdout, "╭") {
		t.Errorf("want a table with no escape sequence, got:\n%q", got.stdout)
	}
}

func TestSinceASpanCountsBackFromNow(t *testing.T) {
	// The last review of the fixture finished on 2026-08-10.
	cases := []struct {
		name string
		now  time.Time
		args []string
		want string
	}{
		{"a span that reaches the last reviews", time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), []string{"--since", "7d"}, "3 reviews from 2026-08-06 to 2026-08-10 across 3 repos\n"},
		{"the same span in weeks", time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), []string{"--since=1w"}, "3 reviews from 2026-08-06 to 2026-08-10 across 3 repos\n"},
		{"a span from a later day reaches nothing", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), []string{"--since", "7d"}, "no reviews match (42 reviews recorded in testdata/ledger.jsonl)\n"},
		{"a date in the future matches nothing", fixtureNow, []string{"--since", "2999-01-01"}, "no reviews match (42 reviews recorded in testdata/ledger.jsonl)\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runStats(noEnv, c.now, false, append([]string{"--ledger", fixture}, c.args...)...)
			if got.status != 0 || !strings.HasPrefix(got.stdout, c.want) {
				t.Errorf("status %d, output starts %q, want %q", got.status, firstLine(got.stdout), c.want)
			}
		})
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line + "\n"
}

func TestAnEmptyLedgerIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "ledger.jsonl")
	env := func(name string) string {
		return map[string]string{ledger.EnvLedger: path}[name]
	}
	got := runStats(env, fixtureNow, false)
	want := "no reviews recorded yet in " + path + "\n" +
		"every autoreview pass and panel run is recorded from now on\n"
	if got.status != 0 || got.stdout != want || got.stderr != "" {
		t.Errorf("got status %d, stdout %q, stderr %q\nwant 0 and %q", got.status, got.stdout, got.stderr, want)
	}
	if _, err := os.Stat(filepath.Dir(path)); err == nil {
		t.Error("reading the report made the ledger's directory")
	}

	// Under --json it is still JSON: a tool must not get a sentence where it
	// expected an object.
	asJSON := runStats(env, fixtureNow, false, "--json")
	var doc struct {
		Ledger   string
		Overview struct{ Runs *int }
		Cohorts  []any
	}
	if err := json.Unmarshal([]byte(asJSON.stdout), &doc); err != nil {
		t.Fatalf("an empty ledger under --json is not JSON: %v\n%s", err, asJSON.stdout)
	}
	if asJSON.status != 0 || doc.Overview.Runs == nil || *doc.Overview.Runs != 0 || doc.Cohorts == nil || doc.Ledger != path {
		t.Errorf("status %d, report %s", asJSON.status, asJSON.stdout)
	}
}

func TestOneRecordedReviewIsReported(t *testing.T) {
	// What v1's end-to-end test records: codex found one LOW finding and
	// claude found nothing, on PR 8.
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	at := fixtureNow.Unix() - 60
	err := ledger.Append(path, ledger.Record{
		V: ledger.Version, ID: "autoreview:sid:1", At: at, Source: "autoreview",
		Repo: ptr("acme/widgets"), PR: ptr(uint64(8)), Decision: ptr("commented"), Risk: ptr("LOW"),
		Panel: []ledger.PanelEntry{
			entry("codex", "gpt-5.5", ptr(true), 1),
			entry("claude", "claude-opus-4.7", ptr(true), 0),
		},
		Findings: []ledger.Finding{finding("LOW", 1, false, source("codex", "gpt-5.5"))},
		Reviewed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	env := func(name string) string {
		return map[string]string{ledger.EnvLedger: path}[name]
	}
	want := "1 review on 2026-09-01 across 1 repo\n" +
		"\n" +
		"MODEL            BACKEND  RUNS  AVAIL          RAW/RUN  KEPT  UNIQUE  HIGH+  DROPPED  KEEP RATE      SAMPLE\n" +
		"gpt-5.5          codex    1     100% (21-100)  1.0      1     1       0      0        100% (21-100)  thin\n" +
		"claude-opus-4.7  claude   1     100% (21-100)  0.0      0     0       0      0        -              thin\n" +
		"\n" +
		"decisions: 1 commented\n" +
		"risk: 1 LOW\n" +
		"ledger: " + path + "\n"
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"with no flags", nil, want},
		{"a repo matches on a part of its name", []string{"--repo", "widgets"}, want},
		{"a span includes today's review", []string{"--since", "7d"}, want},
		{"another repo matches nothing", []string{"--repo", "gadgets"}, "no reviews match (1 review recorded in " + path + ")\n"},
		{"a date in the future matches nothing", []string{"--since", "2999-01-01"}, "no reviews match (1 review recorded in " + path + ")\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runStats(env, fixtureNow, false, c.args...)
			if got.status != 0 || got.stdout != c.want || got.stderr != "" {
				t.Errorf("status %d, stderr %q, stdout:\n%s\nwant:\n%s", got.status, got.stderr, got.stdout, c.want)
			}
		})
	}
}

func TestTheLedgerFlagWinsOverTheEnvironment(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"over a ledger that is off", map[string]string{ledger.EnvLedger: "off"}},
		{"over another ledger", map[string]string{ledger.EnvLedger: "/nowhere/ledger.jsonl"}},
		{"with no home at all", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := func(name string) string { return c.env[name] }
			got := runStats(env, fixtureNow, false, "--ledger", fixture)
			if got.status != 0 || !strings.HasPrefix(got.stdout, "42 reviews from ") {
				t.Errorf("status %d, stderr %q, output starts %q", got.status, got.stderr, firstLine(got.stdout))
			}
		})
	}
}

func TestBadInputExitsOneAndSaysWhy(t *testing.T) {
	off := map[string]string{ledger.EnvLedger: "OFF", "HOME": "/h"}
	cases := []struct {
		name       string
		env        map[string]string
		args       []string
		wantStderr string
		wantHelp   bool
	}{
		{"an unknown flag", nil, []string{"--bogus"}, "error: unknown arg: --bogus\n", true},
		{"a bad since", nil, []string{"--since", "soon"}, "error: --since expects a span like 7d or 2w, or a date like 2026-08-01, got \"soon\"\n", false},
		{"a flag with no value", nil, []string{"--repo"}, "error: --repo expects a value\n", false},
		{"a flag where a value goes", nil, []string{"--repo", "--json"}, "error: --repo expects a value, but found the flag --json\n", false},
		{"a ledger that is off", off, nil, "error: the ledger is off ($AUTOREVIEW_LEDGER=off); pass --ledger PATH to read one anyway\n", false},
		{"no home to find the ledger in", nil, []string{"--json"}, "error: the ledger is off ($AUTOREVIEW_LEDGER=off); pass --ledger PATH to read one anyway\n", false},
		{"a ledger that cannot be read", nil, []string{"--ledger", "testdata"}, "error: could not read the ledger at testdata: ", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := func(name string) string { return c.env[name] }
			got := runStats(env, fixtureNow, false, c.args...)
			if got.status != 1 || got.stdout != "" {
				t.Errorf("status %d, stdout %q, want 1 and nothing", got.status, got.stdout)
			}
			if !strings.HasPrefix(got.stderr, c.wantStderr) {
				t.Errorf("stderr = %q, want it to start %q", got.stderr, c.wantStderr)
			}
			if hasHelp := strings.Contains(got.stderr, "Usage: autoreview stats"); hasHelp != c.wantHelp {
				t.Errorf("the help is on stderr: %v, want %v", hasHelp, c.wantHelp)
			}
		})
	}
}

func TestHelpExitsZeroAndExplainsTheColumns(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag, func(t *testing.T) {
			// Help needs no ledger, so it is given even when there is none.
			got := runStats(noEnv, fixtureNow, false, "--json", flag)
			if got.status != 0 || got.stdout != Help || got.stderr != "" {
				t.Errorf("status %d, stderr %q, stdout:\n%s", got.status, got.stderr, got.stdout)
			}
		})
	}
	for _, want := range []string{
		"Usage: autoreview stats [--since WHEN] [--repo NAME] [--json]",
		"KEEP RATE", "--ledger PATH", "A `Flagged by` name that matches no panelist",
	} {
		if !strings.Contains(Help, want) {
			t.Errorf("the help has no %q", want)
		}
	}
	if !strings.HasSuffix(Help, "the footer says how many.\n") {
		t.Error("the help does not end with one newline")
	}
}

func TestRunDoesNotChangeItsArguments(t *testing.T) {
	args := []string{"--ledger", fixture, "--since=2026-07-15", "--json"}
	before := strings.Join(args, "\x00")
	runStats(noEnv, fixtureNow, false, args...)
	if strings.Join(args, "\x00") != before {
		t.Errorf("Run changed its arguments: %q", args)
	}
}
