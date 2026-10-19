package setup

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/review"
)

// orchestratorBeside checks the agent, and refuses to choose the one the
// settings name as the fallback: every run would refuse that pair. shown is
// the agent the form showed when it is no choice and is not written as one;
// it passes.
func orchestratorBeside(fallback, shown string) func(string) error {
	return func(raw string) error {
		o, err := review.ParseOrchestrator(strings.TrimSpace(raw))
		if err != nil || strings.TrimSpace(raw) == shown {
			return err
		}
		if f, err := review.ParseFallback(fallback); err == nil && f.Kind == review.FallbackSpec && f.Spec == o {
			return fmt.Errorf("%s is the fallback in the settings file too; choose another agent, or change review.fallback in the file", o.Label())
		}
		return nil
	}
}

// upTo is the check of a count from 1 to most, the limit the run itself
// uses.
func upTo(most int) func(string) error {
	return func(raw string) error {
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err != nil || n < 1 || n > most {
			return fmt.Errorf("a whole number from 1 to %d", most)
		}
		return nil
	}
}

func interval(raw string) error {
	_, err := cli.ParseInterval(strings.TrimSpace(raw), "wait")
	return err
}
