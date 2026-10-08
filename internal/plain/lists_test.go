package plain

import (
	"errors"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/repo"
)

func snapshot() *engine.Snapshot {
	return &engine.Snapshot{
		Repo:   repo.Context{Owner: "acme", Name: "widgets"},
		Looked: true,
		Reviews: []queue.Row{
			{PR: prs.PR{Number: 9, Title: "Add retry logic", Author: "alice", Branch: "alice/retry"}},
			{PR: prs.PR{Number: 108, Title: "Fix the cache", Author: "bob", Branch: "bob/cache", CI: prs.CIPending}, Wait: queue.WaitChecks},
		},
		MineLooked: true,
		Mine:       []mine.PR{{Number: 4, Title: "My own work", Branch: "me/my-own-work"}},
	}
}

func lists(t *testing.T, s *engine.Snapshot) (string, string) {
	t.Helper()
	var out, errs strings.Builder
	if err := Lists(&out, &errs, s); err != nil {
		t.Fatalf("Lists: %v", err)
	}
	return out.String(), errs.String()
}

func TestListsPrintsOneLineForEachPR(t *testing.T) {
	out, errs := lists(t, snapshot())
	want := `acme/widgets
review list: 2 PRs
  #9    ready       @alice  alice/retry  Add retry logic
  #108  CI pending  @bob    bob/cache    Fix the cache
your PRs: 1 PR
  #4  awaiting review  me/my-own-work  My own work
`
	if out != want {
		t.Errorf("Lists printed\n%s\nwant\n%s", out, want)
	}
	if errs != "" {
		t.Errorf("Lists wrote %q to the error stream", errs)
	}
}

func TestListsSaysWhenGitHubHadMorePRsThanOneLookReads(t *testing.T) {
	s := snapshot()
	s.Truncated = true
	if out, _ := lists(t, s); !strings.Contains(out, "review list: 2+ PRs\n") {
		t.Errorf("Lists printed\n%s\nwant a count that says there are more", out)
	}
}

func TestAListThatCouldNotBeReadIsNotPrintedAsEmpty(t *testing.T) {
	s := snapshot()
	s.Looked, s.Reviews, s.ListErr = false, nil, "gh api graphql timed out"
	s.MineLooked, s.Mine, s.MineErr = false, nil, "search is down"

	out, errs := lists(t, s)

	if out != "acme/widgets\n" {
		t.Errorf("Lists printed %q, want only the repo: neither list was read", out)
	}
	want := "error: could not read the PR list: gh api graphql timed out\nnote: could not read your PRs: search is down\n"
	if errs != want {
		t.Errorf("the error stream got %q, want %q", errs, want)
	}
}

func TestAnEmptyListIsPrintedAsEmpty(t *testing.T) {
	s := snapshot()
	s.Reviews, s.Mine = nil, nil
	if out, _ := lists(t, s); out != "acme/widgets\nreview list: 0 PRs\nyour PRs: 0 PRs\n" {
		t.Errorf("Lists printed %q", out)
	}
}

func TestOtherPeoplesTextIsPrintedWithoutItsControlBytes(t *testing.T) {
	s := snapshot()
	s.Reviews[0].PR.Title = "nice\x1b[2Jtitle\nsecond line"
	s.Reviews[0].PR.Branch = "a\tb"
	out, _ := lists(t, s)
	if strings.Contains(out, "\x1b") || !strings.Contains(out, "nice[2Jtitlesecond line") || !strings.Contains(out, " ab ") {
		t.Errorf("Lists printed %q", out)
	}
}

type brokenPipe struct{}

func (brokenPipe) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestListsReportsAFailedWrite(t *testing.T) {
	var errs strings.Builder
	if err := Lists(brokenPipe{}, &errs, snapshot()); err == nil {
		t.Error("Lists hid a failed write")
	}
}
