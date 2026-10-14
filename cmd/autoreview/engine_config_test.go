package main

import (
	"testing"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/repo"
)

func TestTheViewAutofixesYourPRsUnlessOnceIsGiven(t *testing.T) {
	cases := []struct {
		name   string
		cfg    cli.Config
		screen bool
		want   bool
	}{
		{"the view", cli.Config{}, true, true},
		{"the view with --once", cli.Config{Once: true}, true, false},
		{"plain lines under --watch", cli.Config{WatchGiven: true}, false, false},
		{"plain lines under --babysit", cli.Config{BabysitGiven: true}, false, false},
		{"one plain pass", cli.Config{}, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := engineConfig(c.cfg, repo.Context{}, c.screen).Autofix; got != c.want {
				t.Errorf("Autofix = %t, want %t", got, c.want)
			}
		})
	}
}
