package cli

import (
	"strings"
	"testing"
	"time"
)

func TestParseIntervalAccepts(t *testing.T) {
	cases := []struct {
		raw, text string
		want      time.Duration
	}{
		{"30", "30m", 30 * time.Minute},
		{"30m", "30m", 30 * time.Minute},
		{"1h", "1h", time.Hour},
		{"2d", "2d", 48 * time.Hour},
		{"05", "05m", 5 * time.Minute},
		{"1", "1m", time.Minute},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			got, err := ParseInterval(c.raw, "watch")
			if err != nil {
				t.Fatalf("ParseInterval(%q): %v", c.raw, err)
			}
			if got.Text != c.text || got.Duration != c.want {
				t.Errorf("ParseInterval(%q) = %+v, want %q and %v", c.raw, got, c.text, c.want)
			}
		})
	}
}

func TestParseIntervalRefuses(t *testing.T) {
	cases := []struct {
		name, raw string
	}{
		{"zero would be a hot loop", "0"},
		{"zeros are still zero", "00"},
		{"zero with a unit", "0m"},
		{"seconds are under a minute", "30s"},
		{"a word", "soon"},
		{"nothing", ""},
		{"a unit and no number", "m"},
		{"a negative number", "-5"},
		{"a fraction", "1.5h"},
		{"a space", "5 m"},
		{"a value that would overflow", "99999999999999999999"},
		{"days that would overflow", "9223372036854775807d"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseInterval(c.raw, "watch")
			if err == nil {
				t.Fatalf("ParseInterval(%q) was accepted", c.raw)
			}
			if !strings.Contains(err.Error(), "invalid watch interval") {
				t.Errorf("the error does not name the flag: %v", err)
			}
		})
	}
}
