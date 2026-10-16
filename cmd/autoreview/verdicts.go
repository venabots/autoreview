package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/session"
	"github.com/venabots/autoreview/internal/skills"
	"github.com/venabots/autoreview/internal/task"
)

// verdictSkills are the skills that submit a verdict, and so read
// --verdicts.
var verdictSkills = []string{"auto-review", "recheck-pr"}

// verdictCopies refuses a verdict level that a skill the agent will run
// does not know. An older auto-review hands its options on to
// panel-review.sh, which stops on one it does not know, or it ignores the
// option and approves. Found at the first review, that is a review that
// posted the verdict the person turned off.
//
// The bundled copies know every option of this build. The copy that runs
// is the agent's installed one when there is one: Claude Code reads it
// before the staged copies, and codex reads only its own.
func verdictCopies(cfg cli.Config, o review.Orchestrator, source skills.Source, repoRoot string) error {
	// Under --no-post the review runs panel-review, which submits no
	// verdict and is given no --verdicts.
	if cfg.Verdicts == review.VerdictsAll || cfg.NoPost {
		return nil
	}
	roots := task.SkillRoots(o, os.Getenv("HOME"), os.Getenv("CODEX_HOME"), session.ConfigDir(os.Getenv), repoRoot)
	for _, name := range verdictSkills {
		path, ok := copyThatRuns(o, name, roots, source)
		if ok && !mentions(path, review.VerdictsFlag) {
			return fmt.Errorf("review.verdicts is %s, but %s does not know %s; update that copy of %s, or remove it so the bundled one runs",
				cfg.Verdicts, path, review.VerdictsFlag, name)
		}
	}
	return nil
}

// copyThatRuns is the SKILL.md the agent reads for name. It is false for
// the bundled copy, and for a skill that is not there at all, which the
// check for missing skills reports.
func copyThatRuns(o review.Orchestrator, name string, roots []string, source skills.Source) (string, bool) {
	for _, root := range roots {
		if path := filepath.Join(root, name, "SKILL.md"); exists(path) {
			return path, true
		}
	}
	if path := filepath.Join(source.Dir, name, "SKILL.md"); o.DiscoversStagedSkills() && source.Kind == skills.FromDir && exists(path) {
		return path, true
	}
	return "", false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func mentions(path, word string) bool {
	raw, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(raw), word)
}

// withVerdictCopies checks the copies the orchestrator and the fallback
// will run. A fallback the person named must know the option, as the
// orchestrator must. An automatic one that does not is dropped with the
// reason, as one that lacks the skills is.
func withVerdictCopies(p prepared, cfg cli.Config, source skills.Source, repoRoot string) (prepared, error) {
	if err := verdictCopies(cfg, cfg.Orchestrator, source, repoRoot); err != nil {
		return prepared{}, err
	}
	if p.fallback == nil {
		return p, nil
	}
	err := verdictCopies(cfg, *p.fallback, source, repoRoot)
	switch {
	case err == nil:
		return p, nil
	case cfg.Fallback.Kind == review.FallbackSpec:
		return prepared{}, fmt.Errorf("--fallback %s: %w", p.fallback.Label(), err)
	}
	p.fallback, p.fallbackLine = nil, "none ("+err.Error()+")"
	return p, nil
}
