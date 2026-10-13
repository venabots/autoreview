package stats

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTheJSONReportCarriesTheRatios(t *testing.T) {
	r := report{
		cohorts: []cohort{testCohort()},
		ledger:  "/l",
		since:   ptr(int64(5)),
	}
	doc, err := reportJSON(r, 77)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		GeneratedAt int64 `json:"generated_at"`
		Since       int64 `json:"since"`
		Cohorts     []struct {
			Key          struct{ Model string }
			Availability struct{ Numerator int }
			KeepRate     struct{ Denominator int } `json:"keep_rate"`
			Sample       string
			MedianSecs   int `json:"median_secs"`
		}
	}
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("the report is not JSON: %v\n%s", err, doc)
	}
	c := v.Cohorts[0]
	if v.GeneratedAt != 77 || v.Since != 5 || c.Key.Model != "gpt-5.5" || c.Availability.Numerator != 11 ||
		c.KeepRate.Denominator != 22 || c.Sample != "emerging" || c.MedianSecs != 60 {
		t.Errorf("the report reads back as %+v", v)
	}
}

func TestTheJSONReportIsSpelledAsV1SpellsIt(t *testing.T) {
	r := report{
		overview: overview{
			runs: 1, repos: 1, firstAt: ptr(int64(-5)), lastAt: ptr(int64(9)),
			decisions: map[string]int{"b": 1, "a<b>&\u2028": 2},
			risks:     map[string]int{},
			sources:   map[string]int{"panel": 1},
		},
		cohorts: []cohort{
			{key: key{"codex", "a\"b\x1b"}, runs: 2, answered: 2, rawFindings: 6, rawRuns: 2, durations: []uint64{4}},
			{key: key{"claude", "m"}, runs: 1},
		},
		unresolved: map[string]int{"z": 1, "a": 2},
		ledger:     "/l <x>",
	}
	want := `{
  "generated_at": 1,
  "since": null,
  "ledger": "/l <x>",
  "overview": {
    "runs": 1,
    "repos": 1,
    "first_at": -5,
    "last_at": 9,
    "decisions": {
      "a<b>&` + "\u2028" + `": 2,
      "b": 1
    },
    "risks": {},
    "sources": {
      "panel": 1
    }
  },
  "attention": [],
  "unresolved": {
    "a": 2,
    "z": 1
  },
  "cohorts": [
    {
      "key": {
        "backend": "codex",
        "model": "a\"b\u001b"
      },
      "runs": 2,
      "answered": 2,
      "failed": 0,
      "raw_findings": 6,
      "raw_runs": 2,
      "kept": 0,
      "kept_unique": 0,
      "kept_high": 0,
      "dropped": 0,
      "keep_num": 0,
      "keep_den": 0,
      "first_at": 0,
      "last_at": 0,
      "availability": {
        "numerator": 2,
        "denominator": 2,
        "value": 1.0,
        "low": 0.3423719528896193,
        "high": 1.0
      },
      "keep_rate": {
        "numerator": 0,
        "denominator": 0,
        "value": null,
        "low": null,
        "high": null
      },
      "raw_per_run": 3.0,
      "sample": "thin",
      "median_secs": 4
    },
    {
      "key": {
        "backend": "claude",
        "model": "m"
      },
      "runs": 1,
      "answered": 0,
      "failed": 0,
      "raw_findings": 0,
      "raw_runs": 0,
      "kept": 0,
      "kept_unique": 0,
      "kept_high": 0,
      "dropped": 0,
      "keep_num": 0,
      "keep_den": 0,
      "first_at": 0,
      "last_at": 0,
      "availability": {
        "numerator": 0,
        "denominator": 0,
        "value": null,
        "low": null,
        "high": null
      },
      "keep_rate": {
        "numerator": 0,
        "denominator": 0,
        "value": null,
        "low": null,
        "high": null
      },
      "raw_per_run": null,
      "sample": "thin",
      "median_secs": null
    }
  ]
}`
	got, err := reportJSON(r, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("JSON report:\n%s\nwant:\n%s", got, want)
	}
}

func TestTheJSONReportSendsNothingATerminalObeys(t *testing.T) {
	// DEL, a C1 control, a bidi override, a zero-width space and a byte
	// order mark. JSON escapes none of them by its own rules.
	model := "a\x7fb\u009bc\u202ed\u200be\ufeff"
	decision := "wiped\u202e"
	r := report{
		overview:   overview{runs: 1, decisions: map[string]int{decision: 1}},
		cohorts:    []cohort{{key: key{"codex", model}, runs: 1}},
		attention:  []string{model + " on codex answered 0 of 5 launches"},
		unresolved: map[string]int{"bot\u200f": 2},
		ledger:     "/l",
	}
	doc, err := reportJSON(r, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, risky := range []string{"\x7f", "\u009b", "\u202e", "\u200b", "\u200f", "\ufeff", "\x1b"} {
		if strings.Contains(doc, risky) {
			t.Errorf("the report holds %q as it is", risky)
		}
	}
	for _, want := range []string{
		`"model": "a\u007fb\u009bc\u202ed\u200be\ufeff"`,
		`"wiped\u202e": 1`,
		`"bot\u200f": 2`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the report has no %s:\n%s", want, doc)
		}
	}
	// The escapes keep the value: a program reads the names the ledger has.
	var v struct {
		Overview   struct{ Decisions map[string]int }
		Attention  []string
		Unresolved map[string]int
		Cohorts    []struct{ Key struct{ Model string } }
	}
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("the report is not JSON: %v", err)
	}
	if v.Cohorts[0].Key.Model != model || v.Overview.Decisions[decision] != 1 ||
		v.Unresolved["bot\u200f"] != 2 || !strings.HasPrefix(v.Attention[0], model) {
		t.Errorf("the names did not read back as the ledger has them: %+v", v)
	}
}

func TestACharacterPastTheBasicPlaneIsEscapedAsAPair(t *testing.T) {
	if got := string(appendEscape(nil, 0xE0001)); got != `\udb40\udc01` {
		t.Errorf("appendEscape(U+E0001) = %s, want a surrogate pair", got)
	}
	if got := string(terminalSafe([]byte("{\"a\":\"\U0001F642\u65e5 <>\"}"))); got != "{\"a\":\"\U0001F642\u65e5 <>\"}" {
		t.Errorf("terminalSafe changed text no terminal obeys: %q", got)
	}
}
