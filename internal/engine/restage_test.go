package engine

import (
	"errors"
	"slices"
	"testing"
	"testing/synctest"
)

func TestEachReviewStagesTheSkillsAgainJustBeforeItsReviewerStarts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rev := &reviewer{}
		// startedBefore is how many reviewers had started at each restage.
		var startedBefore []int
		b := newBench(t, setup{prs: []int{9, 8}, jobs: 1, reviewer: rev, deps: func(d *Deps) {
			d.SkillsDir = "/run/agent"
			d.Restage = func() error {
				startedBefore = append(startedBefore, len(rev.started()))
				return nil
			}
		}})
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		synctest.Wait()
		b.reviewer.finish(t, 8, 0, answer(8, "approved"))
		if result := <-b.results; result.Code != ExitOK {
			t.Fatalf("code %d, want %d", result.Code, ExitOK)
		}

		if !slices.Equal(startedBefore, []int{0, 1}) {
			t.Errorf("reviewers started before each restage: %v, want [0 1]: one restage before each of the two", startedBefore)
		}
	})
}

func TestAReviewWhoseSkillsCannotBeStagedFailsWithoutStarting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		restages := 0
		b := newBench(t, setup{prs: []int{9, 8}, jobs: 1, deps: func(d *Deps) {
			d.SkillsDir = "/run/agent"
			d.Restage = func() error {
				restages++
				if restages == 1 {
					return errors.New("/my/skills no longer holds auto-review\x1b[2J")
				}
				return nil
			}
		}})

		nine := b.job(t, 9)
		if want := "staging the review skills again: /my/skills no longer holds auto-review[2J"; nine.State != JobFailed || nine.Error != want {
			t.Errorf("PR #9: state %d, error %q; want failed with %q", nine.State, nine.Error, want)
		}
		if got := b.reviewer.commandsFor(9); len(got) != 0 {
			t.Errorf("a reviewer started for PR #9 without its skills: %q", got)
		}
		b.reviewer.finish(t, 8, 0, answer(8, "approved"))
		if result := <-b.results; result.Code != ExitFailed || b.job(t, 8).State != JobDone {
			t.Errorf("code %d and PR #8 in state %d, want a failed run whose other review ran", result.Code, b.job(t, 8).State)
		}
	})
}

func TestAReviewThatReadsNoStagedSkillsStagesNothing(t *testing.T) {
	cases := []struct {
		name      string
		skillsDir string
		edit      func(*Config)
	}{
		{"a run that staged no skills", "", nil},
		{"a codex review, which never reads the staged directory", "/run/agent", func(c *Config) { c.Orchestrator = codex }},
		{"an override, which is the person's own command", "/run/agent", func(c *Config) { c.Override = "my-review" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				restages := 0
				b := newBench(t, setup{prs: []int{9}, edit: c.edit, deps: func(d *Deps) {
					d.SkillsDir = c.skillsDir
					d.Restage = func() error {
						restages++
						return nil
					}
				}})
				b.reviewer.finish(t, 9, 0, answer(9, "approved"))
				<-b.results

				if restages != 0 {
					t.Errorf("Restage ran %d times, want none", restages)
				}
				if got := b.reviewer.started(); !slices.Equal(got, []int{9}) {
					t.Errorf("reviews started: %v, want PR #9", got)
				}
			})
		})
	}
}
