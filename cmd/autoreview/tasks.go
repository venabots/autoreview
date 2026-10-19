package main

import (
	"context"
	"os"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/gh"
	"github.com/venabots/autoreview/internal/repo"
	"github.com/venabots/autoreview/internal/rundir"
	"github.com/venabots/autoreview/internal/session"
	"github.com/venabots/autoreview/internal/task"
	"github.com/venabots/autoreview/internal/worktree"
)

// withTasks lets a run under the view work on your own PRs. Each task runs
// in a worktree under the run directory, so a task never touches the
// checkout you work in, and its result is read back from GitHub.
func withTasks(d engine.Deps, cfg cli.Config, rc repo.Context, run rundir.Run) engine.Deps {
	request := func(r task.Request) worktree.Request {
		return worktree.Request{
			Root: rc.Root, Path: run.Worktree(r.PR), PR: r.PR, Branch: r.Branch, CrossRepo: r.CrossRepo, Keep: r.KeepWorktree,
		}
	}
	d.Prepare = func(ctx context.Context, r task.Request) (worktree.Prepared, error) {
		return worktree.Prepare(ctx, worktree.Exec, request(r))
	}
	d.Reopen = func(ctx context.Context, r task.Request) (worktree.Prepared, error) {
		return worktree.Reopen(ctx, worktree.Exec, request(r))
	}
	d.Finish = func(ctx context.Context, p worktree.Prepared) (string, error) {
		kept, _, err := worktree.Finish(ctx, worktree.Exec, rc.Root, p)
		return kept.Why, err
	}
	d.Pushed = func(ctx context.Context, pr int, before string) (string, bool) {
		return task.Readback(ctx, gh.Exec, rc.Slug(), pr, before)
	}
	roots := task.SkillRoots(cfg.Orchestrator, os.Getenv("HOME"), os.Getenv("CODEX_HOME"), session.ConfigDir(os.Getenv), rc.Root)
	d.TaskRefusal = func(k task.Kind) string {
		return task.NotInstalled(k, roots, cfg.Orchestrator)
	}
	return d
}
