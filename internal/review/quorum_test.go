package review

import "testing"

func count(n uint) *uint {
	return &n
}

// reviewed is a trailer with the given counts above polish, and a panel of
// launched reviewers of which the first answered ones returned a verdict.
func reviewed(mustFix, shouldFix uint, answered, launched int) *Trailer {
	panel := make([]Panelist, launched)
	for i := range panel {
		if i < answered {
			panel[i] = Panelist{OK: yes()}
		} else {
			panel[i] = Panelist{OK: no()}
		}
	}
	return &Trailer{
		Findings: &Findings{MustFix: count(mustFix), ShouldFix: count(shouldFix), Polish: count(2)},
		Panel:    panel,
	}
}

func TestTheGateIsThreeQuartersAndNeverFewerThanTwo(t *testing.T) {
	cases := []struct {
		launched, want int
		why            string
	}{
		{4, 3, "one of four can drop out"},
		{3, 3, "2.25 rounds up"},
		{5, 4, "3.75 rounds up"},
		{8, 6, "six of eight is the share exactly"},
		{2, 2, "both of two"},
		{1, 2, "one reviewer is never a quorum"},
		{0, 2, "nobody is never a quorum"},
	}
	for _, c := range cases {
		if got := Quorum(c.launched); got != c.want {
			t.Errorf("Quorum(%d) = %d, want %d: %s", c.launched, got, c.want, c.why)
		}
	}
}

func TestACleanReviewShortOfTheGateIsNamed(t *testing.T) {
	short, ok := CleanButShort("commented", reviewed(0, 0, 2, 4))
	if want := (Shortfall{Answered: 2, Launched: 4, Needed: 3}); !ok || short != want {
		t.Fatalf("CleanButShort = %+v, %v; want %+v", short, ok, want)
	}
	if got, want := short.Line(), "looks clean, but only 2 of 4 reviewers answered; approval needs 3"; got != want {
		t.Errorf("Line = %q, want %q", got, want)
	}
	if got, want := short.Short(), "clean 2/4"; got != want {
		t.Errorf("Short = %q, want %q", got, want)
	}
}

func TestCleanButShortIsOnlyForACleanReviewThatTheGateHeld(t *testing.T) {
	unreported := reviewed(0, 0, 2, 4)
	unreported.Findings = &Findings{MustFix: count(0)}
	noFindings := reviewed(0, 0, 2, 4)
	noFindings.Findings = nil
	silent := reviewed(0, 0, 2, 4)
	silent.Panel = []Panelist{{}, {}, {OK: yes()}, {OK: no()}}

	cases := []struct {
		name    string
		verdict string
		trailer *Trailer
		want    bool
	}{
		// The gate approves on polish, so polish is no reason to look.
		{"polish is still clean", "commented", reviewed(0, 0, 1, 4), true},
		{"a review that posted nothing can be clean too", "", reviewed(0, 0, 2, 4), true},
		{"a panelist that did not say counts as launched and not as answered", "commented", silent, true},
		{"a should-fix finding is not clean", "commented", reviewed(0, 1, 2, 4), false},
		{"a must-fix finding is not clean", "changes requested", reviewed(1, 0, 2, 4), false},
		// Clean and enough answered, but not approved: another gate held
		// it, and the blockers say which.
		{"a panel that met the gate has no shortfall", "commented", reviewed(0, 0, 3, 4), false},
		{"a full panel has no shortfall", "commented", reviewed(0, 0, 4, 4), false},
		// Nothing came back to be clean.
		{"nobody answered", "commented", reviewed(0, 0, 0, 4), false},
		{"no panel at all", "commented", reviewed(0, 0, 0, 0), false},
		{"an approved PR says nothing", "approved", reviewed(0, 0, 2, 4), false},
		// A count the reviewer did not report is not a zero.
		{"an unreported count is not zero", "commented", unreported, false},
		{"no findings reported", "commented", noFindings, false},
		{"no trailer", "commented", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			short, ok := CleanButShort(c.verdict, c.trailer)
			if ok != c.want {
				t.Errorf("CleanButShort = %+v, %v; want %v", short, ok, c.want)
			}
			if !ok && short != (Shortfall{}) {
				t.Errorf("CleanButShort gave %+v with no shortfall", short)
			}
		})
	}
}
