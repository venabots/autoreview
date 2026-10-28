package main

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/notify"
)

// A channel the settings file can name and no sender for it would take
// the setting and drop every alert.
func TestEveryChannelTheSettingsNameHasASender(t *testing.T) {
	got := slices.Sorted(maps.Keys(senders(envOf(nil))))
	if want := slices.Sorted(slices.Values(cli.Channels)); !slices.Equal(got, want) {
		t.Errorf("senders for %q, want one for each of %q", got, want)
	}
}

func TestAHerdrAlertIsANotificationWithItsSound(t *testing.T) {
	cases := []struct {
		alert notify.Alert
		want  string
	}{
		{notify.Alert{Title: "bank: PR #2931 should close", Body: "superseded", Urgent: true},
			"notification show bank: PR #2931 should close --body superseded --sound request"},
		{notify.Alert{Title: "Doctor: all clear again"}, "notification show Doctor: all clear again --sound done"},
	}
	for _, c := range cases {
		if got := strings.Join(herdrArgs(c.alert), " "); got != c.want {
			t.Errorf("herdrArgs = %q, want %q", got, c.want)
		}
	}
}

// Outside Herdr there is no session to show the alert in. The sender does
// nothing, and says nothing failed.
func TestTheHerdrChannelDoesNothingOutsideHerdr(t *testing.T) {
	ran := false
	send := herdrSender(envOf(map[string]string{"HERDR_ENV": "0"}), func(context.Context, ...string) error { ran = true; return nil })
	if err := send(context.Background(), notify.Alert{Title: "x"}); err != nil || ran {
		t.Errorf("outside Herdr: ran %v, err %v", ran, err)
	}
	send = herdrSender(envOf(map[string]string{"HERDR_ENV": "1"}), func(context.Context, ...string) error { ran = true; return nil })
	if err := send(context.Background(), notify.Alert{Title: "x"}); err != nil || !ran {
		t.Errorf("inside Herdr: ran %v, err %v", ran, err)
	}
}

func TestARunWithNoChannelHasNoNotifier(t *testing.T) {
	if n := newNotifier(nil, "bank", envOf(nil), nil); n != nil {
		t.Error("a run with alerts off has a notifier")
	}
}
