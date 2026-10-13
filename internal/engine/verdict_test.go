package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/rundir"
	"github.com/venabots/autoreview/internal/session"
)

func TestALookThatFindsNothingReadyEndsClean(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, setup{})
		result := <-b.results
		if result.Code != ExitOK || result.Final.Pass != 0 || len(result.Final.Jobs) != 0 {
			t.Errorf("code %d, pass %d, %d jobs", result.Code, result.Final.Pass, len(result.Final.Jobs))
		}
		if got := b.journal(); !slices.Equal(got, []string{"looked", "nothing"}) {
			t.Errorf("journal = %v", got)
		}
	})
}

func TestNoPostAsksGitHubNothingAndTakesNoVerdict(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, setup{prs: []int{9}, edit: func(c *Config) { c.NoPost = true }})
		if args := strings.Join(b.reviewer.command(t, 9).Args, " "); !strings.HasSuffix(args, "-- /panel-review 9") {
			t.Errorf("the command line ends %q, want the skill with no posting step", args[max(len(args)-40, 0):])
		}
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		<-b.results

		if nine := b.job(t, 9); nine.State != JobDone || nine.Verdict != "" || nine.Trailer == nil {
			t.Errorf("PR #9: state %d, verdict %q; want done, no verdict, and its trailer", nine.State, nine.Verdict)
		}
		if len(b.asked) != 0 {
			t.Error("GitHub was asked what landed when nothing was posted")
		}
	})
}

func TestTheVerdictIsGitHubsAndNotTheReviewersOwn(t *testing.T) {
	cases := []struct {
		name     string
		readback review.Readback
		claim    string
		want     string
		note     string
	}{
		{"GitHub outranks the claim", review.Readback{Kind: review.ReadbackLanded, Word: "changes requested"}, "approved", "changes requested", ""},
		{"nothing landed vetoes a claimed approval", review.Readback{Kind: review.ReadbackNothing}, "approved", "",
			`PR #9: the reviewer reported "approved", but GitHub shows no review from you; the verdict is left empty`},
		{"a comment survives the veto", review.Readback{Kind: review.ReadbackNothing}, "commented", "commented", ""},
		{"a failed call leaves the reviewer's word", review.Readback{Kind: review.ReadbackFailed}, "approved", "approved",
			"could not read PR #9's review back from GitHub; its verdict is the reviewer's own word"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				b := newBench(t, setup{prs: []int{9}, verdict: func(int) review.Readback { return c.readback }})
				b.reviewer.finish(t, 9, 0, answer(9, c.claim))
				<-b.results

				if got := b.job(t, 9).Verdict; got != c.want {
					t.Errorf("verdict %q, want %q", got, c.want)
				}
				notes := b.notes()
				if c.note == "" && len(notes) != 0 {
					t.Errorf("notes = %q, want none", notes)
				}
				if c.note != "" && !slices.Contains(notes, c.note) {
					t.Errorf("notes = %q, want %q", notes, c.note)
				}
			})
		})
	}
}

func TestOnlyAReviewThatReportedOnItselfIsRecorded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, setup{prs: []int{9, 8}, jobs: 2, record: func(Finished) error { return errors.New("disk full") }})
		b.reviewer.finish(t, 9, 0, answer(9, ""))
		b.reviewer.finish(t, 8, 0, answer(8, "approved"))
		result := <-b.results

		if len(b.recorded) != 1 || b.recorded[0].Job.PR != 8 || b.recorded[0].Job.Verdict != "approved" {
			t.Errorf("recorded %+v, want only PR #8", b.recorded)
		}
		if !slices.Contains(b.notes(), "could not record the review of PR #8 in the ledger: disk full") {
			t.Errorf("notes = %q", b.notes())
		}
		if result.Code != ExitOK {
			t.Errorf("code %d: a ledger that cannot be written must not fail the run", result.Code)
		}
	})
}

func TestContinueResumesAnEarlierSession(t *testing.T) {
	derived := session.ID("/sandbox/repo", "acme", "widgets", 9)
	cases := []struct {
		name            string
		exists, inUse   bool
		wantArg         string
		resumed         bool
		wantSkill, note string
	}{
		{"no earlier session", false, false, "--session-id " + derived, false, "/auto-review 9", ""},
		{"an earlier session", true, false, "--resume " + derived, true, "/recheck-pr 9", ""},
		{"an earlier session open elsewhere", true, true, "", false, "/auto-review 9",
			"PR #9 has a review session open in another tab or process; reviewing fresh"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				probe := session.Probe{Exists: func(string) bool { return c.exists }, InUse: func(string) bool { return c.inUse }}
				b := newBench(t, setup{prs: []int{9}, sessions: probe, edit: func(cfg *Config) { cfg.Continue = true }})
				args := strings.Join(b.reviewer.command(t, 9).Args, " ")
				resumed := b.job(t, 9).Resumed
				b.reviewer.finish(t, 9, 0, answer(9, "approved"))
				<-b.results

				if c.wantArg != "" && !strings.Contains(args, c.wantArg) {
					t.Errorf("the command line lacks %q: %s", c.wantArg, args)
				}
				if c.wantArg == "" && (strings.Contains(args, "--resume") || strings.Contains(args, "--session-id")) {
					t.Errorf("the command line names a session that is open elsewhere: %s", args)
				}
				if !strings.HasSuffix(args, "-- "+c.wantSkill) || resumed != c.resumed {
					t.Errorf("skill and resumed: %q, %v", args[max(len(args)-30, 0):], resumed)
				}
				if c.note != "" && !slices.Contains(b.notes(), c.note) {
					t.Errorf("notes = %q, want %q", b.notes(), c.note)
				}
			})
		})
	}
}

func TestAnOverrideIsJudgedByItsExitStatusAlone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, setup{prs: []int{9}, edit: func(c *Config) { c.Override = "my-review {}" }})
		c := b.reviewer.command(t, 9)
		if c.Path != "bash" || !slices.Equal(c.Args, []string{"-c", "my-review 9"}) {
			t.Errorf("command = %s %q", c.Path, c.Args)
		}
		write(t, c.Stdout, "The override's review, as prose.\n")
		b.reviewer.procs[9].done <- proc.Exit{}
		<-b.results

		nine := b.job(t, 9)
		if nine.State != JobDone || nine.Session != "" || nine.Cost != nil || nine.Trailer != nil {
			t.Errorf("PR #9: %+v, want done with no session, cost or trailer", nine)
		}
		text, _ := os.ReadFile(filepath.Join(b.run.Root, "pass-1", "pr-9.review.md"))
		if string(text) != "The override's review, as prose.\n" {
			t.Errorf("the review file holds %q", text)
		}
		if len(b.recorded) != 0 {
			t.Error("an override's review was recorded: it names no panel")
		}
	})
}

func TestARunThatReviewsFailsWhenTheListCannotBeRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		run, err := rundir.New(t.TempDir(), "")
		if err != nil {
			t.Fatal(err)
		}
		rev := &reviewer{}
		eng := New(Config{Review: true, Jobs: 2}, Deps{
			List:  func(context.Context) (prs.Listing, error) { return prs.Listing{}, errors.New("rate limited") },
			Mine:  func(context.Context) ([]mine.PR, error) { return nil, nil },
			Procs: rev,
			Run:   run,
		})
		result := eng.Run()
		if result.Code != ExitFailed || result.Final.Pass != 0 || len(rev.started()) != 0 {
			t.Errorf("code %d, pass %d, started %v", result.Code, result.Final.Pass, rev.started())
		}
	})
}

func TestAPublishedSnapshotDoesNotChangeWhenAJobDoes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, setup{prs: []int{9}})
		before := b.eng.Snapshot()
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		<-b.results
		if before.Jobs[0].State != JobRunning || before.Jobs[0].Verdict != "" {
			t.Errorf("a snapshot taken while PR #9 ran now shows %+v", before.Jobs[0])
		}
	})
}
