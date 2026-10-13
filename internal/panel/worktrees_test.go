package panel

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/venabots/autoreview/internal/worktree"
)

// recordingGit logs each call and fails the ones fail names.
type recordingGit struct {
	mu    sync.Mutex
	calls []string
	fail  func(args string) bool
	list  string
}

func (g *recordingGit) run(_ context.Context, _ string, args ...string) ([]byte, error) {
	joined := strings.Join(args, " ")
	g.mu.Lock()
	g.calls = append(g.calls, joined)
	g.mu.Unlock()
	if g.fail != nil && g.fail(joined) {
		return nil, errors.New("fatal: refused")
	}
	if joined == "worktree list --porcelain" {
		return []byte(g.list), nil
	}
	return nil, nil
}

func TestEachIDGetsADetachedWorktreeAndEveryStepIsSaid(t *testing.T) {
	git := &recordingGit{}
	var steps []string
	base := t.TempDir()
	wts, err := CreateWorktrees(context.Background(), git.run, "/repo", base, []string{"codex", "synthesis"}, "abc", func(s string) { steps = append(steps, s) })
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(steps, []string{"materializing worktree 1 of 2", "materializing worktree 2 of 2"}) {
		t.Errorf("steps %q", steps)
	}
	if len(wts.Dirs) != 2 || !strings.HasSuffix(wts.Dirs[0], "/worktree-codex") || !strings.HasSuffix(git.calls[1], "/worktree-synthesis abc") {
		t.Errorf("dirs %q, calls %q", wts.Dirs, git.calls)
	}
	if !strings.HasPrefix(git.calls[0], "worktree add --detach --quiet ") {
		t.Errorf("call %q", git.calls[0])
	}
}

func TestAFailedAddIsStillRemovedBecauseGitMayHaveRegisteredIt(t *testing.T) {
	git := &recordingGit{fail: func(args string) bool {
		return strings.Contains(args, "add") && strings.Contains(args, "worktree-claude")
	}}
	wts, err := CreateWorktrees(context.Background(), git.run, "/repo", t.TempDir(), []string{"codex", "claude", "opencode"}, "abc", func(string) {})
	if err == nil || !strings.HasPrefix(err.Error(), "could not create a worktree for 'claude': fatal: refused") {
		t.Fatalf("error %v", err)
	}
	if len(wts.Dirs) != 2 {
		t.Errorf("dirs %q: the failed one must be in it, and nothing after it", wts.Dirs)
	}
}

func TestAnInterruptStopsTheNextAdd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	git := &recordingGit{}
	wts, err := CreateWorktrees(ctx, git.run, "/repo", t.TempDir(), []string{"codex", "claude"}, "abc", func(s string) {
		if s == "materializing worktree 2 of 2" {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || len(wts.Dirs) != 1 || len(git.calls) != 1 {
		t.Errorf("error %v, dirs %q, calls %q", err, wts.Dirs, git.calls)
	}
}

func TestRemovePrunesOnlyARegistrationGitStillLists(t *testing.T) {
	cases := []struct {
		name      string
		refuse    bool
		listed    bool
		wantPrune bool
	}{
		{"a clean removal", false, false, false},
		{"a refusal for one never made", true, false, false},
		{"a refusal for one git still lists", true, true, true},
	}
	for _, c := range cases {
		git := &recordingGit{fail: func(args string) bool { return c.refuse && strings.HasPrefix(args, "worktree remove") }}
		if c.listed {
			git.list = "worktree /repo\nHEAD abc\n\nworktree /base/worktree-codex\ndetached\n"
		}
		Worktrees{root: "/repo", Dirs: []string{"/base/worktree-codex"}}.Remove(git.run)
		pruned := slices.Contains(git.calls, "worktree prune --expire now")
		if pruned != c.wantPrune || git.calls[0] != "worktree remove --force --force /base/worktree-codex" {
			t.Errorf("%s: calls %q", c.name, git.calls)
		}
	}
}

// `git worktree add` locks the worktree while it makes it, and unlocks it
// at the end. A ctrl-C ends the add with SIGKILL, and the lock stays. One
// --force does not remove a locked worktree and a prune does not clear it,
// so the person's repository would keep the registration.
func TestRemoveTakesOutAWorktreeThatAKilledAddLeftLocked(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := worktree.Exec(ctx, root, args...)
		if err != nil {
			t.Fatalf("git %s: %v", strings.Join(args, " "), err)
		}
		return string(out)
	}
	git("init", "--quiet")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "--quiet", "--allow-empty", "-m", "start")
	wts, err := CreateWorktrees(ctx, worktree.Exec, root, t.TempDir(), []string{"codex"}, "HEAD", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	git("worktree", "lock", "--reason", "initializing", wts.Dirs[0])

	wts.Remove(worktree.Exec)

	if list := git("worktree", "list", "--porcelain"); strings.Count(list, "worktree ") != 1 {
		t.Errorf("git still lists the locked worktree:\n%s", list)
	}
}

func TestRemovingNoWorktreesCallsNoGit(t *testing.T) {
	git := &recordingGit{}
	Worktrees{}.Remove(git.run)
	if len(git.calls) != 0 {
		t.Errorf("calls %q", git.calls)
	}
}
