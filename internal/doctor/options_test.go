package doctor

import (
	"errors"
	"testing"
)

func TestTheOptionsReadEachFlagInBothSpellings(t *testing.T) {
	const now = 1_000_000
	cases := []struct {
		name string
		args []string
		want options
	}{
		{"nothing is the last day", nil, options{since: now - 86_400, window: "1d"}},
		{"a spaced value", []string{"--since", "2w"}, options{since: now - 14*86_400, window: "2w"}},
		{"an inline value and no probe", []string{"--since=1d", "--no-probe", "--ledger=/x"}, options{since: now - 86_400, window: "1d", noProbe: true, ledger: "/x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parse(c.args, now)
			if err != nil || got != c.want {
				t.Errorf("parse(%v) = %+v, %v, want %+v", c.args, got, err, c.want)
			}
		})
	}
}

func TestAForgottenValueIsAnErrorAndNotAWindow(t *testing.T) {
	for _, args := range [][]string{{"--since", "--no-probe"}, {"--since="}, {"--ledger"}, {"--since", "soon"}} {
		if _, err := parse(args, 1_000_000); err == nil {
			t.Errorf("parse(%v) took it", args)
		}
	}
}

func TestAnUnknownArgumentAsksForTheHelp(t *testing.T) {
	_, err := parse([]string{"--json"}, 0)
	if !errors.Is(err, errUnknownArg) {
		t.Errorf("err = %v, want errUnknownArg", err)
	}
}
