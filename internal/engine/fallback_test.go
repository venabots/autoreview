package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/review"
)

var codex = review.Orchestrator{Backend: "codex"}

func withFallback(s setup) setup {
	edit := s.edit
	s.edit = func(c *Config) {
		c.Fallback = &codex
		if edit != nil {
			edit(c)
		}
	}
	return s
}

func TestExit10IsRetriedOnceUnderTheFallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, withFallback(setup{prs: []int{9}}))
		b.reviewer.finish(t, 9, 10, "You've hit your session limit")
		synctest.Wait()

		calls := b.reviewer.commandsFor(9)
		if len(calls) != 2 {
			t.Fatalf("PR #9 was started %d times, want a retry", len(calls))
		}
		retry := calls[1]
		if !strings.HasPrefix(retry, "-H codex ") || strings.Contains(retry, "--session-id") || strings.Contains(retry, "--resume") {
			t.Errorf("the retry is %q, want a fresh codex review", retry[:min(len(retry), 120)])
		}
		job := b.job(t, 9)
		if job.State != JobRunning || job.Orchestrator != codex || job.First == nil {
			t.Fatalf("PR #9 during the retry: %+v", job)
		}
		if job.First.Orchestrator != review.Claude() || job.First.Outcome != "exit 10" || job.First.Error != "You've hit your session limit" {
			t.Errorf("the first attempt: %+v", *job.First)
		}
		if _, err := os.Stat(filepath.Join(b.run.Root, "pass-1", "pr-9.claude.json")); err != nil {
			t.Errorf("the failed attempt's answer was not set aside: %v", err)
		}

		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		result := <-b.results
		if result.Code != ExitOK || b.job(t, 9).State != JobDone {
			t.Errorf("code %d, PR #9 in state %d; want the fallback's review to count", result.Code, b.job(t, 9).State)
		}
		got := b.journal()
		if !slices.Contains(got, "retry 9") || slices.Index(got, "retry 9") > slices.Index(got, "finish 9") {
			t.Errorf("journal = %v, want the retry before the result", got)
		}
	})
}

// A codex review runs in a thread that only codex can open. Its id has the
// shape of a session id, so the next pass under claude would send it with
// --resume, fail, and fall back again at every rest.
func TestAReviewTheFallbackRanIsNotResumedUnderTheOrchestrator(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		b := newBench(t, withFallback(watchRun(g, nil)))
		b.reviewer.finish(t, 9, 10, "You've hit your session limit")
		synctest.Wait()
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		synctest.Wait()

		g.set(9, updated)
		time.Sleep(31 * time.Minute)
		synctest.Wait()
		calls := b.reviewer.commandsFor(9)
		if len(calls) != 3 {
			t.Fatalf("PR #9 was started %d times, want the review, its retry and a re-check", len(calls))
		}
		if !strings.HasPrefix(calls[2], "-H claude ") || strings.Contains(calls[2], "--resume") {
			t.Errorf("the re-check is %q, want a claude review that resumes nothing", calls[2][:min(len(calls[2]), 200)])
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestARetryThatFailsTooIsNotRetriedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, withFallback(setup{prs: []int{9}}))
		b.reviewer.finish(t, 9, 10, "usage limit")
		synctest.Wait()
		b.reviewer.finish(t, 9, 10, "codex is down")
		result := <-b.results

		job := b.job(t, 9)
		if result.Code != ExitFailed || job.State != JobFailed || job.Error != "codex is down" || job.First == nil {
			t.Errorf("code %d, PR #9: %+v", result.Code, job)
		}
		if n := len(b.reviewer.commandsFor(9)); n != 2 {
			t.Errorf("PR #9 was started %d times, want 2", n)
		}
	})
}

func TestARetryGoesAheadOfTheReviewsThatWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, withFallback(setup{prs: []int{9, 8, 7}, jobs: 1}))
		// PR #8 takes the slot the moment PR #9's process ends. The retry
		// is known only when PR #9's result is read, and goes ahead of PR #7.
		b.reviewer.finish(t, 9, 10, "usage limit")
		synctest.Wait()
		b.reviewer.finish(t, 8, 0, answer(8, "commented"))
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{9, 8, 9}) {
			t.Errorf("started %v, want the retry of PR #9 before PR #7", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestOnlyAFailedAgentIsRetried(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Config)
		run  func(t *testing.T, b *bench)
	}{
		{"another exit status", nil, func(t *testing.T, b *bench) { b.reviewer.finish(t, 9, 3, "boom") }},
		{"no fallback", func(c *Config) { c.Fallback = nil }, func(t *testing.T, b *bench) { b.reviewer.finish(t, 9, 10, "usage limit") }},
		{"an override", func(c *Config) { c.Override = "my-review" }, func(t *testing.T, b *bench) {
			b.reviewer.procs[9].done <- proc.Exit{Code: 10}
		}},
		{"a review a person stopped", nil, func(t *testing.T, b *bench) { b.eng.Do(StopJob{PR: 9}) }},
		{"a review the run stopped", nil, func(t *testing.T, b *bench) { b.eng.Do(Quit{}) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rev := &reviewer{onStop: proc.Exit{Code: 10}}
				b := newBench(t, withFallback(setup{prs: []int{9}, edit: c.edit, reviewer: rev}))
				c.run(t, b)
				<-b.results
				if n := len(b.reviewer.commandsFor(9)); n != 1 {
					t.Errorf("PR #9 was started %d times, want no retry", n)
				}
				if job := b.job(t, 9); job.State != JobFailed || job.First != nil {
					t.Errorf("PR #9: state %d, first %+v", job.State, job.First)
				}
			})
		})
	}
}

// reactions records what the run asked of the reaction.
type reactions struct {
	mu     sync.Mutex
	calls  []string
	failed []int
}

func (m *reactions) Show(pr int)  { m.add("show", pr) }
func (m *reactions) Clear(pr int) { m.add("clear", pr) }
func (m *reactions) Settle() []int {
	m.add("settle", 0)
	return m.failed
}

func (m *reactions) add(what string, pr int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pr == 0 {
		m.calls = append(m.calls, what)
		return
	}
	m.calls = append(m.calls, fmt.Sprintf("%s %d", what, pr))
}

func (m *reactions) log() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
}

func TestAReviewMarksItsPRWhileItRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &reactions{}
		b := newBench(t, setup{prs: []int{9, 8}, jobs: 1, marks: m})
		if got := m.log(); !slices.Equal(got, []string{"show 9"}) {
			t.Fatalf("while PR #9 runs: %v, want only its mark on", got)
		}
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		synctest.Wait()
		b.reviewer.finish(t, 8, 10, "usage limit")
		<-b.results
		// PR #8 starts in the slot PR #9's process freed, before PR #9's
		// result is read and its mark comes off.
		want := []string{"show 9", "show 8", "clear 9", "clear 8", "settle"}
		if got := m.log(); !slices.Equal(got, want) {
			t.Errorf("reactions = %v, want %v: on at the start, off at the end however it ended, settled before the run ends", got, want)
		}
	})
}

func TestARetryKeepsTheMarkUntilItEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &reactions{}
		b := newBench(t, withFallback(setup{prs: []int{9}, marks: m}))
		b.reviewer.finish(t, 9, 10, "usage limit")
		synctest.Wait()
		if got := m.log(); slices.Contains(got, "clear 9") {
			t.Fatalf("the mark came off for a review about to run again: %v", got)
		}
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		<-b.results
		if got := m.log(); !slices.Equal(got, []string{"show 9", "show 9", "clear 9", "settle"}) {
			t.Errorf("reactions = %v", got)
		}
	})
}

func TestQuitStopsTheReviewsThenTakesTheMarksOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &reactions{failed: []int{8}}
		rev := &reviewer{onStop: proc.Exit{Signaled: true}}
		b := newBench(t, setup{prs: []int{9, 8}, jobs: 2, marks: m, reviewer: rev})
		b.eng.Do(Quit{})
		<-b.results
		got := m.log()
		if len(got) != 5 || got[4] != "settle" || !slices.Contains(got, "clear 9") || !slices.Contains(got, "clear 8") {
			t.Errorf("reactions = %v, want both marks off and settled", got)
		}
		if !slices.Contains(b.notes(), "could not set or remove the eyes reaction on PR #8; the review is unaffected") {
			t.Errorf("notes = %q", b.notes())
		}
	})
}
