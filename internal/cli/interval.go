package cli

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Interval is a validated wait between two looks: the text a person reads
// ("30m", "1h") and its length.
type Interval struct {
	Text     string
	Duration time.Duration
}

// ParseInterval reads an interval. A bare number is minutes, and a value
// with an m, h or d suffix is taken as written.
//
// Anything else is refused here and never reaches the loop. Zero would look
// again with no delay, which is a hot loop running reviews unattended.
// Units under a minute are refused for the same reason. An absurd value is
// refused too, so it cannot wrap into a near-zero wait. kind names the flag
// in the error, so a person who typed --watch is not sent to another flag.
func ParseInterval(raw, kind string) (Interval, error) {
	text := raw
	if text != "" && allDigits(text) {
		text += "m"
	}
	if len(text) < 2 {
		return Interval{}, rejectInterval(raw, kind)
	}
	digits, unit := text[:len(text)-1], unitOf(text[len(text)-1])
	if unit == 0 || !allDigits(digits) {
		return Interval{}, rejectInterval(raw, kind)
	}
	// Leading zeros are fine ("05" is plainly five). A value of zero is not.
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n == 0 || n > math.MaxInt64/int64(unit) {
		return Interval{}, rejectInterval(raw, kind)
	}
	return Interval{Text: text, Duration: time.Duration(n) * unit}, nil
}

func unitOf(suffix byte) time.Duration {
	switch suffix {
	case 'm':
		return time.Minute
	case 'h':
		return time.Hour
	case 'd':
		return 24 * time.Hour
	}
	return 0
}

func allDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

func rejectInterval(raw, kind string) error {
	return fmt.Errorf("invalid %s interval: %q (expected a positive duration, e.g. 30, 30m, 1h)", kind, raw)
}
