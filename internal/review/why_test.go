package review

import (
	"fmt"
	"slices"
	"testing"
)

func yes() *bool {
	value := true
	return &value
}

func no() *bool {
	value := false
	return &value
}

func blocker(severity, domain string, reversible *bool, location, gist string) Blocker {
	return Blocker{Severity: severity, Domain: domain, Reversible: reversible, Location: location, Gist: gist}
}

// blockers is n blockers that differ in their location only.
func blockers(n int) []Blocker {
	out := make([]Blocker, n)
	for i := range out {
		out[i] = blocker("MEDIUM", "ui", yes(), fmt.Sprintf("a.ts:%d", i), "wrong")
	}
	return out
}

func TestReasonsSayWhyAPRIsNotApprovedYet(t *testing.T) {
	charge := blocker("HIGH", "money", no(), "src/pay.rs:88", "a retried checkout charges twice")
	cases := []struct {
		name     string
		verdict  string
		blockers []Blocker
		want     []string
	}{
		{
			"a blocker names its severity, its domain and whether it can be undone",
			"commented", []Blocker{charge},
			[]string{"- [HIGH] (money, irreversible) src/pay.rs:88 — a retried checkout charges twice"},
		},
		{
			// "nothing posted" is the verdict that most needs a reason:
			// there is no review on the PR to go and read.
			"a review that posted nothing still explains itself",
			"", []Blocker{blocker("MEDIUM", "correctness", yes(), "src/q.rs:3", "off by one")},
			[]string{"- [MEDIUM] (correctness, reversible) src/q.rs:3 — off by one"},
		},
		{
			// The blockers are what the review fixed on its way to an
			// approval, or a field the reviewer forgot to clear. GitHub
			// says approved, and the summary does not argue with it.
			"an approved PR says nothing",
			"approved", []Blocker{blocker("LOW", "ui", yes(), "app/page.tsx:12", "the button sits two pixels low")},
			nil,
		},
		{
			"three blockers all show",
			"changes requested", blockers(3),
			[]string{
				"- [MEDIUM] (ui, reversible) a.ts:0 — wrong",
				"- [MEDIUM] (ui, reversible) a.ts:1 — wrong",
				"- [MEDIUM] (ui, reversible) a.ts:2 — wrong",
			},
		},
		{
			"past three blockers the rest are counted",
			"changes requested", blockers(5),
			[]string{
				"- [MEDIUM] (ui, reversible) a.ts:0 — wrong",
				"- [MEDIUM] (ui, reversible) a.ts:1 — wrong",
				"- [MEDIUM] (ui, reversible) a.ts:2 — wrong",
				"- 2 findings not shown",
			},
		},
		{
			"one hidden finding is singular",
			"commented", blockers(4),
			[]string{
				"- [MEDIUM] (ui, reversible) a.ts:0 — wrong",
				"- [MEDIUM] (ui, reversible) a.ts:1 — wrong",
				"- [MEDIUM] (ui, reversible) a.ts:2 — wrong",
				"- 1 finding not shown",
			},
		},
		{
			// Every field is optional, so a blocker can arrive with only a
			// sentence. A reader gets the sentence, and no empty bullet.
			"a blocker with only a gist still draws",
			"commented", []Blocker{{Gist: "the migration drops a column"}},
			[]string{"- the migration drops a column"},
		},
		{"a blocker with no gist still draws", "commented", []Blocker{{Severity: "HIGH", Location: "db/up.sql:4"}}, []string{"- [HIGH] db/up.sql:4"}},
		{"a domain alone is a tag", "commented", []Blocker{{Domain: "data", Gist: "rows are lost"}}, []string{"- (data) — rows are lost"}},
		{"reversibility alone is a tag", "commented", []Blocker{{Reversible: no(), Gist: "rows are lost"}}, []string{"- (irreversible) — rows are lost"}},
		{"a field of spaces is no field", "commented", []Blocker{{Severity: "  ", Domain: " ", Location: "", Gist: " tidy this "}}, []string{"- tidy this"}},
		{"a blocker that says nothing is dropped", "commented", []Blocker{{}}, nil},
		{
			"an empty blocker is not counted as hidden",
			"commented", slices.Concat([]Blocker{{}}, blockers(3)),
			[]string{
				"- [MEDIUM] (ui, reversible) a.ts:0 — wrong",
				"- [MEDIUM] (ui, reversible) a.ts:1 — wrong",
				"- [MEDIUM] (ui, reversible) a.ts:2 — wrong",
			},
		},
		{"a reviewer that reported no blockers says nothing", "commented", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			trailer := &Trailer{Blockers: c.blockers}
			before := slices.Clone(c.blockers)
			if got := Reasons(c.verdict, trailer); !slices.Equal(got, c.want) {
				t.Errorf("Reasons =\n%q\nwant\n%q", got, c.want)
			}
			if len(trailer.Blockers) != len(before) {
				t.Error("Reasons changed the trailer")
			}
		})
	}
}

func TestAReviewWithNoTrailerGivesNoReasons(t *testing.T) {
	// An older reviewer and a command override write no trailer. That costs
	// a missing list, and it is not an error.
	if got := Reasons("commented", nil); len(got) != 0 {
		t.Errorf("Reasons = %q with no trailer", got)
	}
}

func TestTheHeaderOfTheReasons(t *testing.T) {
	if ReasonsHeader != "not approved yet because:" {
		t.Errorf("ReasonsHeader = %q", ReasonsHeader)
	}
}
