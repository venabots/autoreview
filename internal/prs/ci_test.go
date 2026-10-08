package prs

import "testing"

func TestTheFiveStatesAndTheAbsenceOfOne(t *testing.T) {
	cases := []struct {
		state string
		want  CI
	}{
		{"SUCCESS", CIPassing},
		{"PENDING", CIPending},
		{"EXPECTED", CIPending},
		{"FAILURE", CIFailing},
		{"ERROR", CIFailing},
		// No checks at all.
		{"", CINone},
		// A value outside the schema must not hold a PR for ever.
		{"SOMETHING_NEW", CINone},
		// The schema is upper case, and a near miss is still unknown.
		{"success", CINone},
	}
	for _, c := range cases {
		if got := CIFromState(c.state); got != c.want {
			t.Errorf("CIFromState(%q) = %s, want %s", c.state, got, c.want)
		}
	}
}

func TestOnlyGreenAndAbsentChecksAreReady(t *testing.T) {
	cases := []struct {
		ci   CI
		want bool
	}{
		{CIPassing, true},
		// A repo without CI is not held.
		{CINone, true},
		{CIPending, false},
		{CIFailing, false},
	}
	for _, c := range cases {
		if got := c.ci.ready(); got != c.want {
			t.Errorf("%s ready = %v, want %v", c.ci, got, c.want)
		}
	}
}
