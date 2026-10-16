package engine

import (
	"slices"
	"testing"
	"testing/synctest"
)

func TestAReviewSeatsThePanelTheRunNames(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, held(setup{prs: []int{9}, edit: func(c *Config) { c.Panelists = []string{"codex", "claude:opus-4.8"} }}))
		synctest.Wait()
		if env := b.reviewer.command(t, 9).Env; !slices.Contains(env, "PANEL_REVIEW_PANELISTS=codex claude:opus-4.8") {
			t.Errorf("the review's environment is %q, want the run's panel", env)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}
