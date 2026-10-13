package ledger_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/findings"
	"github.com/venabots/autoreview/internal/ledger"
	"github.com/venabots/autoreview/internal/review"
)

// Every wanted line in this file is what v1 0.23.0 writes for the same
// review: its autoreview_run, with `at` set to the time given here, written
// by its own serde_json.

func to[T any](v T) *T {
	return &v
}

// line is the record as Append writes it.
func line(t *testing.T, r ledger.Review) string {
	t.Helper()
	out, err := ledger.ForReview(r).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// bare is a review with a session and nothing else to report.
func bare() ledger.Review {
	return ledger.Review{
		Repo:       "acme/widgets",
		PR:         9,
		Session:    "sid",
		StartedAt:  time.Unix(100, 0),
		FinishedAt: time.Unix(160, 0),
	}
}

func TestAReviewBecomesTheRecordV1Writes(t *testing.T) {
	// v1's "an autoreview pass becomes one record", as the caller does it:
	// the trailer and the findings are both read from the review text.
	const text = "### Risk\nMEDIUM\n### should-fix\n" +
		"- [MEDIUM] src/a.rs:1 — issue. Flagged by 2: codex (gpt-5.5), claude (claude-opus-4.7)\n\n" +
		"```autoreview\n" +
		`{"decision":"commented","risk":"MEDIUM","findings":{"must_fix":0,"should_fix":1,"polish":0},` +
		`"panel":[{"name":"codex","model":"gpt-5.5","ok":true,"findings":1,"top":"MEDIUM"},` +
		`{"name":"claude","model":"claude-opus-4.7","ok":false}]}` + "\n```"
	trailer, ok := review.ParseTrailer(text)
	if !ok {
		t.Fatal("the text has no trailer")
	}
	r := bare()
	r.Verdict = "changes requested"
	r.Trailer = trailer
	r.Cost = to(0.42)
	r.Elapsed = 12 * time.Second
	r.Model = "claude-fable-5"
	r.Findings = findings.Parse(text)
	r.Reviewed = findings.IsSynthesis(text)

	record := ledger.ForReview(r)
	if record.ID != "autoreview:sid:100" || record.Source != "autoreview" {
		t.Errorf("ID = %q, Source = %q", record.ID, record.Source)
	}
	if record.Decision == nil || *record.Decision != "changes-requested" {
		t.Errorf("Decision = %v, want changes-requested: the readback outranks the trailer", record.Decision)
	}
	if len(record.Panel) != 2 || record.Panel[1].OK == nil || *record.Panel[1].OK {
		t.Errorf("Panel = %+v, want two panelists and the second not ok", record.Panel)
	}
	if len(record.Findings) != 1 || record.Findings[0].Count != 2 {
		t.Errorf("Findings = %+v, want one finding that two panelists raised", record.Findings)
	}

	want := `{"v":1,"id":"autoreview:sid:100","at":160,"source":"autoreview","repo":"acme/widgets","pr":9,` +
		`"session":"sid","decision":"changes-requested","risk":"MEDIUM",` +
		`"counts":{"must_fix":0,"should_fix":1,"polish":0},"cost_usd":0.42,"duration_secs":12,` +
		`"driver_model":"claude-fable-5",` +
		`"panel":[{"name":"codex","model":"gpt-5.5","ok":true,"findings":1,"top":"MEDIUM"},` +
		`{"name":"claude","model":"claude-opus-4.7","ok":false}],` +
		`"findings":[{"severity":"MEDIUM","location":"src/a.rs:1",` +
		`"flagged_by":[{"name":"codex","model":"gpt-5.5"},{"name":"claude","model":"claude-opus-4.7"}],"count":2}],` +
		`"reviewed":true}`
	if got := line(t, r); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestAReviewWithNoSessionIsKeyedByItsPR(t *testing.T) {
	// v1's "a run with no session is keyed by pr".
	r := bare()
	r.Session = ""
	record := ledger.ForReview(r)
	if record.ID != "autoreview:acme/widgets#9:100" {
		t.Errorf("ID = %q", record.ID)
	}
	if record.Decision != nil || len(record.Findings) != 0 {
		t.Errorf("Decision = %v, Findings = %v, want neither", record.Decision, record.Findings)
	}
	want := `{"v":1,"id":"autoreview:acme/widgets#9:100","at":160,"source":"autoreview","repo":"acme/widgets","pr":9,` +
		`"duration_secs":0,"panel":[],"findings":[],"reviewed":false}`
	if got := line(t, r); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestAFieldTheReviewDoesNotHaveIsLeftOut(t *testing.T) {
	const head = `{"v":1,"id":"autoreview:sid:100","at":160,"source":"autoreview","repo":"acme/widgets","pr":9,"session":"sid",`
	const none = `"panel":[],"findings":[],"reviewed":false}`
	cases := []struct {
		name string
		with func(*ledger.Review)
		want string
	}{
		{"a review with nothing to report", func(*ledger.Review) {}, `"duration_secs":0,` + none},
		{"a risk", func(r *ledger.Review) { r.Trailer.Risk = "LOW" }, `"risk":"LOW","duration_secs":0,` + none},
		{
			"counts with no count in them",
			func(r *ledger.Review) { r.Trailer.Findings = &review.Findings{} },
			`"counts":{},"duration_secs":0,` + none,
		},
		{
			"a count of zero is written and a missing count is not",
			func(r *ledger.Review) {
				r.Trailer.Findings = &review.Findings{MustFix: to(uint(0)), Polish: to(uint(2))}
			},
			`"counts":{"must_fix":0,"polish":2},"duration_secs":0,` + none,
		},
		{"a whole cost keeps its fraction", func(r *ledger.Review) { r.Cost = to(3.0) }, `"cost_usd":3.0,"duration_secs":0,` + none},
		{
			"a model",
			func(r *ledger.Review) { r.Model = "claude-fable-5[1m]" },
			`"duration_secs":0,"driver_model":"claude-fable-5[1m]",` + none,
		},
		{
			"the time it took is in whole seconds, cut and not rounded",
			func(r *ledger.Review) { r.Elapsed = 412*time.Second + 999*time.Millisecond },
			`"duration_secs":412,` + none,
		},
		{
			"a panelist with no name is named with a question mark",
			func(r *ledger.Review) { r.Trailer.Panel = []review.Panelist{{Model: "gpt-5.5"}} },
			`"duration_secs":0,"panel":[{"name":"?","model":"gpt-5.5"}],"findings":[],"reviewed":false}`,
		},
		{
			"a panelist with only a name",
			func(r *ledger.Review) { r.Trailer.Panel = []review.Panelist{{Name: "codex"}} },
			`"duration_secs":0,"panel":[{"name":"codex"}],"findings":[],"reviewed":false}`,
		},
		{
			"a panelist that failed and found nothing still has both facts",
			func(r *ledger.Review) {
				r.Trailer.Panel = []review.Panelist{
					{Name: "codex", Model: "gpt-5.5", OK: to(false), Findings: to(uint(0)), Top: "HIGH"},
				}
			},
			`"duration_secs":0,"panel":[{"name":"codex","model":"gpt-5.5","ok":false,"findings":0,"top":"HIGH"}],"findings":[],"reviewed":false}`,
		},
		{
			"a sign-off is a text with no findings and is not a review",
			func(r *ledger.Review) {
				const text = "Posted the review with gh."
				r.Findings, r.Reviewed = findings.Parse(text), findings.IsSynthesis(text)
			},
			`"duration_secs":0,` + none,
		},
		{
			"a synthesis that kept no finding is a review",
			func(r *ledger.Review) {
				const text = "### Overview\n**Reviewing:** PR #9"
				r.Findings, r.Reviewed = findings.Parse(text), findings.IsSynthesis(text)
			},
			`"duration_secs":0,"panel":[],"findings":[],"reviewed":true}`,
		},
		{
			"findings are written as they were read, reviewed or not",
			func(r *ledger.Review) {
				const text = "## Disagreements\n- `a.ts:1` — codex (gpt-5.5) claimed X. Verification falsified this."
				r.Findings, r.Reviewed = findings.Parse(text), findings.IsSynthesis(text)
			},
			`"duration_secs":0,"panel":[],"findings":[{"location":"a.ts:1","flagged_by":[{"name":"codex","model":"gpt-5.5"}],"count":1,"dropped":true}],"reviewed":false}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := bare()
			c.with(&r)
			if got, want := line(t, r), head+c.want; got != want {
				t.Errorf("got  %s\nwant %s", got, want)
			}
		})
	}
}

func TestADecisionHasOneSpelling(t *testing.T) {
	cases := []struct {
		name    string
		verdict string
		want    *string
	}{
		{"approved", "approved", to("approved")},
		{"the words of a summary become the token", "changes requested", to("changes-requested")},
		{"commented", "commented", to("commented")},
		{"capital letters are lowered", "Changes Requested", to("changes-requested")},
		{"space around the words is cut", "  approved\n", to("approved")},
		{"every space becomes a hyphen", "CHANGES  REQUESTED", to("changes--requested")},
		{"only a space does, and a tab stays", "changes\trequested", to("changes\trequested")},
		{"only the letters A to Z are lowered", "ÀPPROVED", to("Àpproved")},
		{"a verdict of only space is an empty decision", " ", to("")},
		{"nothing posted is no decision", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := bare()
			r.Verdict = c.verdict
			got := ledger.ForReview(r).Decision
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Decision for %q = %s, want %s", c.verdict, show(got), show(c.want))
			}
		})
	}
}

func show(s *string) string {
	if s == nil {
		return "none"
	}
	return strconv.Quote(*s)
}

func TestTheIDAndTheRepoKeepTheirBytes(t *testing.T) {
	r := bare()
	r.Session = ""
	r.Repo = "acme/<widgets>&co"
	want := `{"v":1,"id":"autoreview:acme/<widgets>&co#9:100","at":160,"source":"autoreview","repo":"acme/<widgets>&co","pr":9,` +
		`"duration_secs":0,"panel":[],"findings":[],"reviewed":false}`
	if got := line(t, r); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestANegativeNumberIsWrittenAsZero(t *testing.T) {
	// v1 cannot hold either number below zero. Converted as they are, they
	// would be written as numbers of twenty digits.
	r := bare()
	r.Session = ""
	r.PR = -1
	r.Elapsed = -time.Second
	record := ledger.ForReview(r)
	if *record.PR != 0 || *record.DurationSecs != 0 || record.ID != "autoreview:acme/widgets#0:100" {
		t.Errorf("PR = %d, DurationSecs = %d, ID = %q", *record.PR, *record.DurationSecs, record.ID)
	}
}

func TestForReviewDoesNotChangeTheReviewItIsGiven(t *testing.T) {
	sample := func() ledger.Review {
		r := bare()
		r.Verdict = "Changes Requested"
		r.Cost = to(0.42)
		r.Trailer = review.Trailer{
			Risk:     "LOW",
			Findings: &review.Findings{Polish: to(uint(1))},
			Panel:    []review.Panelist{{Model: "gpt-5.5", OK: to(true), Findings: to(uint(1))}},
		}
		r.Findings = findings.Parse("- [LOW] a.rs:1 — nit. Flagged by: codex (gpt-5.5)")
		return r
	}
	given := sample()
	ledger.ForReview(given)
	if want := sample(); !reflect.DeepEqual(given, want) {
		t.Errorf("the review changed:\n got  %+v\n want %+v", given, want)
	}
}

// TestEveryReviewInARealLedgerIsBuiltAgainWithTheSameBytes is skipped unless
// $AUTOREVIEW_LEDGER_ROUNDTRIP names a ledger file. It takes each record a
// review pass wrote, makes the review that record describes, and fails when
// ForReview does not write the line again byte for byte. That proves the id
// rule, the decision word and the present and absent fields on every review
// v1 has recorded. The findings are passed through: the review text is not
// in the ledger.
//
// Point it at a copy. A failure gives line numbers and no content: the file
// holds repo names, PR numbers and session ids.
func TestEveryReviewInARealLedgerIsBuiltAgainWithTheSameBytes(t *testing.T) {
	path := os.Getenv("AUTOREVIEW_LEDGER_ROUNDTRIP")
	if path == "" {
		t.Skip("set AUTOREVIEW_LEDGER_ROUNDTRIP to a copy of a ledger to run this")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n"))
	built := 0
	for i, written := range lines {
		var record ledger.Record
		if err := json.Unmarshal(written, &record); err != nil || record.Source != "autoreview" {
			continue
		}
		r, ok := described(record)
		if !ok {
			t.Errorf("line %d of %d is not a record a review pass writes", i+1, len(lines))
			continue
		}
		if got := line(t, r); got != string(written) {
			t.Errorf("line %d of %d was built again with other bytes (%d in, %d out)", i+1, len(lines), len(written), len(got))
			continue
		}
		built++
	}
	t.Logf("%d lines, %d reviews built again byte for byte", len(lines), built)
}

// described is the review that a record of a review pass describes.
func described(record ledger.Record) (ledger.Review, bool) {
	started, err := strconv.ParseInt(record.ID[strings.LastIndexByte(record.ID, ':')+1:], 10, 64)
	if err != nil || record.Repo == nil || record.PR == nil || record.DurationSecs == nil {
		return ledger.Review{}, false
	}
	trailer := review.Trailer{Risk: orEmpty(record.Risk)}
	if c := record.Counts; c != nil {
		trailer.Findings = &review.Findings{MustFix: narrow(c.MustFix), ShouldFix: narrow(c.ShouldFix), Polish: narrow(c.Polish)}
	}
	for _, p := range record.Panel {
		trailer.Panel = append(trailer.Panel, review.Panelist{
			Name: p.Name, Model: orEmpty(p.Model), OK: p.OK, Findings: narrow(p.Findings), Top: orEmpty(p.Top),
		})
	}
	return ledger.Review{
		Repo:       *record.Repo,
		PR:         int(*record.PR),
		Session:    orEmpty(record.Session),
		StartedAt:  time.Unix(started, 0),
		FinishedAt: time.Unix(record.At, 0),
		Verdict:    strings.ReplaceAll(orEmpty(record.Decision), "-", " "),
		Trailer:    trailer,
		Cost:       record.CostUSD,
		Elapsed:    time.Duration(*record.DurationSecs) * time.Second,
		Model:      orEmpty(record.DriverModel),
		Findings:   record.Findings,
		Reviewed:   record.Reviewed,
	}, true
}

func orEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func narrow(n *uint64) *uint {
	if n == nil {
		return nil
	}
	return to(uint(*n))
}
