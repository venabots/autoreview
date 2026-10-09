package stats

import (
	"slices"
	"testing"
)

func TestWilsonStaysInsideTheUnitInterval(t *testing.T) {
	cases := []struct {
		name             string
		k, n             uint64
		lowOver, lowTo   float64
		highOver, highTo float64
	}{
		{"3 of 3 is not certainty", 3, 3, 0.4, 0.5, 1, 1},
		{"0 of 3 is not impossibility", 0, 3, 0, 0, 0.5, 0.6},
		{"many runs give a narrow interval", 179, 185, 0.93, 0.97, 0.97, 0.99},
		{"1 of 1 is a wide interval", 1, 1, 0.2, 0.21, 1, 1},
		{"half of a few", 2, 4, 0.14, 0.16, 0.84, 0.86},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			low, high := wilson(c.k, c.n)
			if low < c.lowOver || low > c.lowTo {
				t.Errorf("low = %v, want %v to %v", low, c.lowOver, c.lowTo)
			}
			if high < c.highOver || high > c.highTo {
				t.Errorf("high = %v, want %v to %v", high, c.highOver, c.highTo)
			}
		})
	}
}

func TestWilsonGivesV1Digits(t *testing.T) {
	// v1 0.23.0 printed these bounds under --json. A fused multiply-add, or
	// a constant z squared exactly, changes the last digit of some of them.
	cases := []struct {
		name      string
		k, n      uint64
		low, high float64
	}{
		{"0 of 1", 0, 1, 0, 0.7934567085261071},
		{"0 of 8", 0, 8, 0, 0.3244156195108769},
		{"1 of 1", 1, 1, 0.20654329147389294, 1},
		{"1 of 14", 1, 14, 0.012721886112291648, 0.3146926955395781},
		{"3 of 5", 3, 5, 0.23071993220883702, 0.8823817688407467},
		{"4 of 36", 4, 36, 0.044065689087418335, 0.25315129015036325},
		{"7 of 14", 7, 14, 0.2679886791947528, 0.7320113208052473},
		{"12 of 33", 12, 33, 0.22186966167959873, 0.5338412737846591},
		{"20 of 41", 20, 41, 0.34254187239935696, 0.6351574068413481},
		{"24 of 36", 24, 36, 0.5033369709296683, 0.7978557522541395},
		{"36 of 36", 36, 36, 0.9035781695514236, 1},
		{"41 of 42", 41, 42, 0.8767851883475175, 0.9957847045008998},
		{"53 of 107", 53, 107, 0.40240905867272797, 0.5885690578467014},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			low, high := wilson(c.k, c.n)
			if low != c.low || high != c.high {
				t.Errorf("wilson(%d, %d) = %v, %v, want %v, %v", c.k, c.n, low, high, c.low, c.high)
			}
		})
	}
}

func TestARatioWithNothingUnderItHasNoValue(t *testing.T) {
	r := ratioOf(0, 0)
	if r.known || r.value != 0 || r.low != 0 || r.high != 0 {
		t.Errorf("ratioOf(0, 0) = %+v, want no value", r)
	}
	half := ratioOf(1, 2)
	if !half.known || half.value != 0.5 || half.numerator != 1 || half.denominator != 2 {
		t.Errorf("ratioOf(1, 2) = %+v, want a half", half)
	}
}

func TestTheSampleWordFollowsTheAnsweredRuns(t *testing.T) {
	cases := []struct {
		answered uint64
		want     string
	}{
		{0, "thin"},
		{9, "thin"},
		{10, "emerging"},
		{29, "emerging"},
		{30, "proven"},
		{300, "proven"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			// Launches that did not answer do not make a sample.
			if got := (cohort{runs: 500, answered: c.answered}).sample(); got != c.want {
				t.Errorf("sample() with %d answered = %q, want %q", c.answered, got, c.want)
			}
		})
	}
}

func TestTheMedianIsTheMiddleLaunch(t *testing.T) {
	cases := []struct {
		name      string
		durations []uint64
		want      uint64
		wantOK    bool
	}{
		{"no launch recorded a time", nil, 0, false},
		{"one launch", []uint64{40}, 40, true},
		{"an odd count", []uint64{30, 90, 60}, 60, true},
		{"an even count takes the upper middle", []uint64{30, 90, 60, 10}, 60, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := slices.Clone(c.durations)
			got, ok := (cohort{durations: c.durations}).medianSecs()
			if got != c.want || ok != c.wantOK {
				t.Errorf("medianSecs() = %d, %v, want %d, %v", got, ok, c.want, c.wantOK)
			}
			if !slices.Equal(c.durations, before) {
				t.Errorf("medianSecs sorted the cohort's own durations: %v", c.durations)
			}
		})
	}
}

func TestAddingTalliesLeavesBothAsTheyWere(t *testing.T) {
	a := cohort{runs: 1, kept: 2, durations: make([]uint64, 1, 8)}
	a.durations[0] = 10
	b := cohort{runs: 1, dropped: 1, durations: []uint64{20}}
	first := a.plus(b)
	second := a.plus(cohort{durations: []uint64{99}})
	if !slices.Equal(first.durations, []uint64{10, 20}) {
		t.Errorf("a later sum changed an earlier one: %v", first.durations)
	}
	if !slices.Equal(second.durations, []uint64{10, 99}) {
		t.Errorf("second sum = %v, want [10 99]", second.durations)
	}
	if first.runs != 2 || first.kept != 2 || first.dropped != 1 || a.runs != 1 || len(a.durations) != 1 {
		t.Errorf("sum = %+v from %+v", first, a)
	}
}

func TestTheFirstRunSeenIsTheEarliest(t *testing.T) {
	c := cohort{}.seenAt(200).seenAt(100).seenAt(300)
	if c.firstAt != 100 || c.lastAt != 300 {
		t.Errorf("span = %d to %d, want 100 to 300", c.firstAt, c.lastAt)
	}
}
