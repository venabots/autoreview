package main

import (
	"context"
	"io"
	"os/exec"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/notify"
	"github.com/venabots/autoreview/internal/plain"
	"github.com/venabots/autoreview/internal/sanitize"
)

// senders is the sender of each channel, by the name the settings file
// uses. A new channel is one more entry here, and its name in cli.Channels.
func senders(env func(string) string) map[string]notify.Sender {
	return map[string]notify.Sender{
		"herdr": herdrSender(env, runHerdr),
	}
}

// newNotifier sends the run's alerts to channels, or is nil when there is
// none. A channel that fails is noted once in the log.
func newNotifier(channels []string, repo string, env func(string) string, log io.Writer) *notify.Notifier {
	if len(channels) == 0 {
		return nil
	}
	all := senders(env)
	chosen := make([]notify.Sender, 0, len(channels))
	for _, c := range channels {
		chosen = append(chosen, all[c])
	}
	return notify.New(notify.Fanout(chosen...), repo, func(err error) {
		if log != nil {
			say(log, "note: an alert was not sent: %s\n", sanitize.Line(err.Error()))
		}
	})
}

// herdrSender shows an alert as a Herdr notification, with a sound that
// asks for attention when it is urgent. Outside Herdr it does nothing:
// there is no session to show it in.
func herdrSender(env func(string) string, run func(ctx context.Context, args ...string) error) notify.Sender {
	return func(ctx context.Context, a notify.Alert) error {
		if env("HERDR_ENV") != "1" {
			return nil
		}
		return run(ctx, herdrArgs(a)...)
	}
}

func herdrArgs(a notify.Alert) []string {
	args := []string{"notification", "show", a.Title}
	if a.Body != "" {
		args = append(args, "--body", a.Body)
	}
	sound := "done"
	if a.Urgent {
		sound = "request"
	}
	return append(args, "--sound", sound)
}

func runHerdr(ctx context.Context, args ...string) error {
	return exec.CommandContext(ctx, "herdr", args...).Run()
}

// journal sends each event to the printer, and to the notifier when the
// run has one. Neither waits.
func journal(printer *plain.Printer, notifier *notify.Notifier) func(engine.Event) {
	if notifier == nil {
		return printer.Emit
	}
	return func(ev engine.Event) {
		printer.Emit(ev)
		notifier.Emit(ev)
	}
}
