package review

import "testing"

// The settings file names the ceiling in words. A word it does not know is
// an error: read as none, it would hold every approval, and read as a
// higher ceiling, it would let a migration through.
func TestTheRiskCeilingReadsAsTheSettingsFileWritesIt(t *testing.T) {
	for raw, want := range map[string]Risk{"none": RiskNone, "low": RiskLow, "medium": RiskMedium} {
		if got, err := ParseCeiling(raw); err != nil || got != want {
			t.Errorf("%q reads as %s, %v; want %s", raw, got, err, want)
		}
	}
	for _, bad := range []string{"", "high", "critical", "Medium", "med"} {
		if _, err := ParseCeiling(bad); err == nil {
			t.Errorf("%q reads as a ceiling", bad)
		}
	}
}

// A reviewer writes the rating as the synthesis does, in capitals and
// often with its reason after it. Only a known word, whole, is a rating: a
// word with more joined to it is no rating, so it can never read as LOW.
func TestAReportedRiskIsReadFromItsFirstWord(t *testing.T) {
	cases := []struct {
		raw  string
		want Risk
		ok   bool
	}{
		{"HIGH", RiskHigh, true},
		{"medium", RiskMedium, true},
		{"**LOW** — docs only", RiskLow, true},
		{"CRITICAL: drops a column", RiskCritical, true},
		{"", RiskNone, false},
		{"unknown", RiskNone, false},
		{"higher than usual", RiskNone, false},
		{"LOWISH", RiskNone, false},
		{"LOW-ish", RiskNone, false},
		{"low/medium", RiskNone, false},
		{"Low?", RiskNone, false},
	}
	for _, c := range cases {
		if got, ok := ParseRisk(c.raw); got != c.want || ok != c.ok {
			t.Errorf("%q reads as %s, %v; want %s, %v", c.raw, got, ok, c.want, c.ok)
		}
	}
}

func TestOnlyAnotherAuthorsPRGetsTheCeiling(t *testing.T) {
	cases := []struct {
		name        string
		named       bool
		run         Verdicts
		ceiling     Risk
		wantLevel   Verdicts
		wantCeiling Risk
	}{
		{"a named author", true, VerdictsAll, RiskMedium, VerdictsAll, RiskNone},
		{"another author, no ceiling", false, VerdictsAll, RiskNone, VerdictsRequestChangesOnly, RiskNone},
		{"another author, up to medium", false, VerdictsAll, RiskMedium, VerdictsAll, RiskMedium},
		{"another author, approve only, up to low", false, VerdictsApproveOnly, RiskLow, VerdictsApproveOnly, RiskLow},
		{"another author, a level that never approves", false, VerdictsCommentOnly, RiskMedium, VerdictsCommentOnly, RiskNone},
	}
	for _, c := range cases {
		level, ceiling := LevelFor(c.named, c.run, c.ceiling)
		if level != c.wantLevel || ceiling != c.wantCeiling {
			t.Errorf("%s: %s up to %s, want %s up to %s", c.name, level, ceiling, c.wantLevel, c.wantCeiling)
		}
	}
}

// The ceiling reaches the two skills that submit a verdict, after the
// level, and no other skill.
func TestTheRiskCeilingIsAnOptionOfTheSkillsThatGiveAVerdict(t *testing.T) {
	cases := []struct {
		name string
		r    Request
		want string
	}{
		{"no ceiling", with(func(r *Request) {}), "/auto-review 9"},
		{"up to medium", with(func(r *Request) { r.MaxRisk = RiskMedium }), "/auto-review 9 --max-risk medium"},
		{"after the level, before the focus", with(func(r *Request) { r.Verdicts, r.MaxRisk, r.Focus = VerdictsApproveOnly, RiskLow, "the migration" }),
			`/auto-review 9 --verdicts approve-only --max-risk low --focus "the migration"`},
		{"no post", with(func(r *Request) { r.NoPost, r.MaxRisk = true, RiskMedium }), "/panel-review 9"},
		{"a task", with(func(r *Request) { r.Task, r.MaxRisk = "babysit-pr", RiskMedium }), "/babysit-pr 9"},
	}
	for _, c := range cases {
		if got := Prompt(c.r); got != c.want {
			t.Errorf("%s: Prompt = %q, want %q", c.name, got, c.want)
		}
	}
}
