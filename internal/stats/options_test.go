package stats

import (
	"errors"
	"strings"
	"testing"
)

func ptr[T any](v T) *T {
	return &v
}

func TestSinceIsASpanOrADate(t *testing.T) {
	const day = 86_400
	cases := []struct {
		name string
		raw  string
		now  int64
		want int64
	}{
		{"days", "7d", 1_000_000, 1_000_000 - 7*day},
		{"weeks", "2w", 1_000_000, 1_000_000 - 14*day},
		{"a span with a plus sign", "+7d", 1_000_000, 1_000_000 - 7*day},
		{"a date is midnight UTC", "2026-08-01", 0, 1_785_542_400},
		{"the day the epoch began", "1970-01-01", 5, 0},
		{"the leap day of a leap year", "2024-02-29", 0, 1_709_164_800},
		{"a date before the epoch", "1969-12-31", 0, -day},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseSince(c.raw, c.now)
			if err != nil || got != c.want {
				t.Errorf("parseSince(%q) = %d, %v, want %d", c.raw, got, err, c.want)
			}
		})
	}
}

func TestABadSinceIsRefused(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		now  int64
	}{
		{"no days", "0d", 1},
		{"negative days", "-3d", 1},
		{"a word", "soon", 1},
		{"nothing", "", 1},
		{"a unit alone", "d", 1},
		{"a number alone", "7", 1},
		{"a unit that is not days or weeks", "7m", 1},
		{"a fraction", "1.5d", 1},
		{"month thirteen", "2026-13-01", 1},
		{"February the thirtieth", "2026-02-30", 1},
		{"April the thirty-first", "2026-04-31", 1},
		{"the leap day of a year with none", "2025-02-29", 1},
		{"a date with slashes", "2026/08/01", 1},
		{"ten characters that are not a date", "123456789d", 1},
		{"one multibyte character", "日", 1},
		{"a multibyte unit", "7日", 1},
		{"a multibyte unit of two bytes", "7é", 1},
		{"a date that ends in a multibyte character", "2026-08-0é", 1},
		{"more days than fit", "9223372036854775807d", 0},
		{"more weeks than fit", "9223372036854775807w", 0},
		{"a span that reaches past the first second there is", "106751991167300d", -9_000_000_000_000_000_000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseSince(c.raw, c.now)
			if err == nil {
				t.Fatalf("parseSince(%q) = %d, want an error", c.raw, got)
			}
			if !strings.Contains(err.Error(), "--since expects a span like 7d or 2w") {
				t.Errorf("the error does not say what was wanted: %v", err)
			}
		})
	}
}

func TestFlagsAreReadInBothSpellings(t *testing.T) {
	const now = 1_000_000
	cases := []struct {
		name string
		args []string
		want options
	}{
		{
			name: "a value after a space",
			args: []string{"--json", "--since", "7d", "--repo", "widgets", "--ledger", "/l"},
			want: options{json: true, since: ptr(int64(now - 7*86_400)), repo: "widgets", ledger: "/l"},
		},
		{
			name: "a value after an equals sign",
			args: []string{"--since=2w", "--repo=acme", "--ledger=/x"},
			want: options{since: ptr(int64(now - 14*86_400)), repo: "acme", ledger: "/x"},
		},
		{
			name: "a value with an equals sign in it",
			args: []string{"--ledger=/x=y"},
			want: options{ledger: "/x=y"},
		},
		{
			name: "a value that starts with one hyphen",
			args: []string{"--repo", "-widgets"},
			want: options{repo: "-widgets"},
		},
		{name: "the last of a flag given twice wins", args: []string{"--repo", "a", "--repo=b"}, want: options{repo: "b"}},
		{name: "no arguments", args: nil, want: options{}},
		{name: "help", args: []string{"--help"}, want: options{help: true}},
		{name: "help in short", args: []string{"-h"}, want: options{help: true}},
		{name: "help after other flags", args: []string{"--json", "--repo", "x", "-h"}, want: options{help: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parse(c.args, now)
			if err != nil {
				t.Fatal(err)
			}
			if !sameOptions(got, c.want) {
				t.Errorf("parse(%q) = %+v, want %+v", c.args, got, c.want)
			}
		})
	}
}

func sameOptions(a, b options) bool {
	sameSince := (a.since == nil) == (b.since == nil) && (a.since == nil || *a.since == *b.since)
	return sameSince && a.repo == b.repo && a.json == b.json && a.ledger == b.ledger && a.help == b.help
}

func TestABadArgumentIsRefused(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		want        string
		wantUnknown bool
	}{
		{"an unknown flag", []string{"--bogus"}, "unknown arg: --bogus", true},
		{"a positional argument", []string{"widgets"}, "unknown arg: widgets", true},
		{"a value given to a flag that takes none", []string{"--json=true"}, "unknown arg: --json=true", true},
		{"a flag that only starts like one", []string{"--sincerely=7d"}, "unknown arg: --sincerely=7d", true},
		{"an unknown flag before help", []string{"--bogus", "--help"}, "unknown arg: --bogus", true},
		{"a flag with no value", []string{"--repo"}, "--repo expects a value", false},
		{"a flag with an empty value", []string{"--repo="}, "--repo expects a value", false},
		{"an empty value after a space", []string{"--ledger", ""}, "--ledger expects a value", false},
		{
			"a flag where the value goes",
			[]string{"--repo", "--json"},
			"--repo expects a value, but found the flag --json",
			false,
		},
		{
			"a flag after the equals sign",
			[]string{"--ledger=--json"},
			"--ledger expects a value, but found the flag --json",
			false,
		},
		{"a since with no value", []string{"--since"}, "--since expects a value", false},
		{"a bad since", []string{"--since", "soon"}, `--since expects a span like 7d or 2w, or a date like 2026-08-01, got "soon"`, false},
		{"a bad since before help", []string{"--since=soon", "--help"}, `got "soon"`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parse(c.args, 0)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("parse(%q) error = %v, want one that says %q", c.args, err, c.want)
			}
			if got := errors.Is(err, errUnknownArg); got != c.wantUnknown {
				t.Errorf("the error is an unknown argument: %v, want %v", got, c.wantUnknown)
			}
		})
	}
}

func TestParseDoesNotChangeItsArguments(t *testing.T) {
	args := []string{"--since", "7d", "--repo=acme", "--json"}
	before := strings.Join(args, " ")
	if _, err := parse(args, 1_000_000); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(args, " "); got != before {
		t.Errorf("parse changed its arguments to %q", got)
	}
}
