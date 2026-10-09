package stats

import (
	"math"
	"slices"
)

// The answered runs it takes to call a cohort's numbers settled, and to show
// them with a caveat.
const (
	provenRuns   = 30
	emergingRuns = 10
)

// cohort is what the ledger says about one model on one backend.
type cohort struct {
	key key
	// runs is the times it was launched, answered or not.
	runs     uint64
	answered uint64
	failed   uint64
	// rawFindings is what the panelists reported themselves, before
	// synthesis.
	rawFindings uint64
	// rawRuns is the answered runs that reported a count, so rawFindings
	// has something to be divided by.
	rawRuns uint64
	// kept is the findings the synthesis kept that name this cohort.
	kept uint64
	// keptUnique is the kept findings only this cohort raised.
	keptUnique uint64
	// keptHigh is the kept findings at HIGH or CRITICAL.
	keptHigh uint64
	// dropped is the findings listed under Disagreements: surfaced to say
	// they did not hold up.
	dropped uint64
	// keepNum and keepDen are the keep rate's parts: run by run, the kept
	// findings that fit inside the reported count, over the reported count.
	keepNum uint64
	keepDen uint64
	// durations has one entry for each launch that recorded its time.
	durations []uint64
	firstAt   int64
	lastAt    int64
}

// plus adds another tally to this one and returns the sum. The key and the
// two times stay as they were.
func (c cohort) plus(d cohort) cohort {
	c.runs += d.runs
	c.answered += d.answered
	c.failed += d.failed
	c.rawFindings += d.rawFindings
	c.rawRuns += d.rawRuns
	c.kept += d.kept
	c.keptUnique += d.keptUnique
	c.keptHigh += d.keptHigh
	c.dropped += d.dropped
	c.keepNum += d.keepNum
	c.keepDen += d.keepDen
	c.durations = slices.Concat(c.durations, d.durations)
	return c
}

// seenAt widens the cohort's span to a run that finished at an epoch
// second. Zero means "no first run yet", as it does in v1.
func (c cohort) seenAt(at int64) cohort {
	if c.firstAt == 0 || at < c.firstAt {
		c.firstAt = at
	}
	c.lastAt = max(c.lastAt, at)
	return c
}

func (c cohort) availability() ratio {
	return ratioOf(c.answered, c.answered+c.failed)
}

func (c cohort) keepRate() ratio {
	return ratioOf(c.keepNum, c.keepDen)
}

// rawPerRun is the findings reported for each answered run. ok is false
// when no run reported a count.
func (c cohort) rawPerRun() (perRun float64, ok bool) {
	if c.rawRuns == 0 {
		return 0, false
	}
	return float64(c.rawFindings) / float64(c.rawRuns), true
}

// sample is how far to trust the numbers, in a word.
func (c cohort) sample() string {
	switch {
	case c.answered >= provenRuns:
		return "proven"
	case c.answered >= emergingRuns:
		return "emerging"
	}
	return "thin"
}

// medianSecs is the middle launch time. ok is false when no launch recorded
// one.
func (c cohort) medianSecs() (secs uint64, ok bool) {
	if len(c.durations) == 0 {
		return 0, false
	}
	sorted := slices.Sorted(slices.Values(c.durations))
	return sorted[len(sorted)/2], true
}

// ratio is a count over a count, with the interval that says how far to
// trust it. Ten runs and three hundred do not deserve the same confidence,
// and a bare percentage hides which is which.
type ratio struct {
	numerator   uint64
	denominator uint64
	// known is false when the denominator is zero. There is no value then,
	// and no interval.
	known bool
	value float64
	low   float64
	high  float64
}

func ratioOf(numerator, denominator uint64) ratio {
	if denominator == 0 {
		return ratio{numerator: numerator}
	}
	low, high := wilson(numerator, denominator)
	return ratio{
		numerator:   numerator,
		denominator: denominator,
		known:       true,
		value:       float64(numerator) / float64(denominator),
		low:         low,
		high:        high,
	}
}

// wilson is the 95% Wilson score interval: where the true rate plausibly
// is, given k of n. Unlike the normal approximation it stays inside 0..1 and
// does not shrink to a point at 0 of 3 or 3 of 3. n must not be zero.
//
// The steps are v1's, one float64 operation at a time. z is a variable so
// that z*z is rounded as v1 rounds it; a constant would be squared exactly.
// The two conversions stop the compiler from fusing a product with the sum
// or the difference that follows it: a fused multiply-add rounds once where
// v1 rounds twice, and the last digit of a bound would differ from v1's.
func wilson(k, n uint64) (low, high float64) {
	z := 1.96
	count := float64(n)
	p := float64(k) / count
	denom := 1.0 + z*z/count
	centre := p + z*z/(2.0*count)
	spread := float64(p*(1.0-p)) + z*z/(4.0*count)
	margin := float64(z * math.Sqrt(spread/count))
	return math.Max((centre-margin)/denom, 0), math.Min((centre+margin)/denom, 1)
}
