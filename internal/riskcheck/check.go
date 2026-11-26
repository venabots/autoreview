// Package riskcheck runs a repository's own risk script on a PR before the
// review of the PR starts.
//
// A repository names the script in its REVIEW.md, on a line of its own:
//
//	Risk script: `bin/pr-risk`
//
// The run reads REVIEW.md and the script as GitHub has them on the
// repository's default branch, at one commit it asks for when the check
// begins, never from the PR or from the PR's base: a PR that edits the
// script to print LOW is judged by the copy before it, and so is a PR whose
// base is a branch that anyone who can push it could have changed. The
// script runs in the checkout's top directory. It gets the PR's changed
// paths on stdin, one to a line, with the old path of a rename too, and
// $AUTOREVIEW_REPO, $AUTOREVIEW_PR, $AUTOREVIEW_BASE, $AUTOREVIEW_HEAD and
// $AUTOREVIEW_TRUSTED_REF, the commit it was read at, from which it can
// read its companion files. Its environment is built from a short list of
// names, never autoreview's own, which holds a token that can approve PRs:
// see env. It prints LOW, MEDIUM, HIGH or CRITICAL alone on its first line,
// and its reasons on the lines after.
package riskcheck

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/venabots/autoreview/internal/gh"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/repo"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/sanitize"
)

// Timeout is how long a script may run. The look waits for every check, so
// a script that hangs must not hang the run; it holds the approval instead.
const Timeout = 60 * time.Second

// maxWhy is the most of a script's reasons that reach the log. A script
// that prints a page must not scroll the log away.
const maxWhy = 300

// Checker checks the PRs of one repository.
type Checker struct {
	GH   gh.Runner
	Repo repo.Context
	// Dir is where each script is written before it runs: a directory of
	// the run.
	Dir string
	// Environ is autoreview's own environment, as os.Environ gives it. The
	// script gets only the parts of it that env names.
	Environ []string
	// Run runs one script. Exec runs it for real.
	Run func(ctx context.Context, s Script) (stdout []byte, err error)
}

// Script is one run of a risk script.
type Script struct {
	Path string
	Dir  string
	// Env is the script's whole environment. Nothing else of autoreview's
	// reaches it.
	Env   []string
	Stdin []byte
}

// Check runs the script that REVIEW.md names on one head of the PR. Each
// failure on the way is a check that failed, which holds the approval: a
// GitHub that answers 503 for REVIEW.md must not read as a repository that
// names no script.
func (c Checker) Check(ctx context.Context, pr prs.PR) review.RiskCheck {
	if pr.Base == "" || pr.Head == "" {
		return failed("", "GitHub gave no base branch or head for the PR")
	}
	at, err := c.trusted(ctx)
	if err != nil {
		return failed("", err.Error())
	}
	path, named, err := c.scriptPath(ctx, at)
	switch {
	case err != nil:
		return failed("", err.Error())
	case !named:
		return review.RiskCheck{NoScript: true}
	}
	body, err := c.fetch(ctx, path, at.commit)
	if err != nil {
		return failed(path, fmt.Sprintf("reading %s from %s: %v", path, at, err))
	}
	paths, err := c.changed(ctx, pr.Number)
	if err != nil {
		return failed(path, err.Error())
	}
	out, err := c.run(ctx, pr, at, body, paths)
	if err != nil {
		return failed(path, fmt.Sprintf("%s failed: %v", path, err))
	}
	return rated(path, out)
}

// run writes the default branch's copy of the script into the run's
// directory and runs it there, so the copy in the checkout, which may be any
// branch, never runs. gh gets an empty directory of settings of its own for
// the one run.
func (c Checker) run(ctx context.Context, pr prs.PR, at trusted, body []byte, paths []string) ([]byte, error) {
	f, err := os.CreateTemp(c.Dir, "risk-script-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, err = f.Write(body)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o700)
	}
	if err != nil {
		return nil, err
	}
	ghConfig, err := os.MkdirTemp(c.Dir, "risk-gh-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(ghConfig) }()
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	return c.Run(ctx, Script{
		Path:  f.Name(),
		Dir:   c.Repo.Root,
		Env:   c.env(pr, at, ghConfig),
		Stdin: []byte(strings.Join(paths, "\n") + "\n"),
	})
}

// rated reads what the script printed: the rating alone on the first line,
// then its reasons.
func rated(path string, out []byte) review.RiskCheck {
	first, rest, _ := strings.Cut(string(out), "\n")
	risk, ok := review.ParseRating(first)
	if !ok {
		return failed(path, fmt.Sprintf("%s printed %q first, not LOW, MEDIUM, HIGH or CRITICAL", path, first))
	}
	var reasons []string
	for _, line := range strings.Split(rest, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			reasons = append(reasons, line)
		}
	}
	return review.RiskCheck{Script: path, Risk: risk, Why: cut(sanitize.Line(strings.Join(reasons, "; ")))}
}

func failed(path, why string) review.RiskCheck {
	return review.RiskCheck{Script: path, Why: cut(sanitize.Line(why))}
}

func cut(s string) string {
	if r := []rune(s); len(r) > maxWhy {
		return string(r[:maxWhy-1]) + "…"
	}
	return s
}
