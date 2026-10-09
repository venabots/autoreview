package panel

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/venabots/autoreview/internal/findings"
	"github.com/venabots/autoreview/internal/ledger"
	"github.com/venabots/autoreview/internal/worktree"
)

// slugTimeout bounds the one git call that names the repository for the
// ledger. The ledger is a note on the run, and must not hold its end.
const slugTimeout = 10 * time.Second

// panelRecord is the ledger's record of a run. Every panelist that started
// counts, answered or not: a panel that all failed, or one run with
// --no-synthesis, is still evidence about the models. synthesis is "" when
// there was none.
func panelRecord(base ledger.Panel, outcomes []Outcome, synthesis string) ledger.Record {
	p := base
	p.Panel = make([]ledger.Panelled, len(outcomes))
	for i, o := range outcomes {
		p.Panel[i] = panelled(o)
	}
	p.Risk, _ = findings.Risk(synthesis)
	p.Findings = findings.Parse(synthesis)
	p.Reviewed = findings.IsSynthesis(synthesis)
	return ledger.ForPanel(p)
}

func panelled(o Outcome) ledger.Panelled {
	entry := ledger.Panelled{Name: o.ID, Model: o.Model, Answered: o.Answered(), Elapsed: o.Elapsed}
	if o.Exited {
		code := o.Exit
		entry.Exit = &code
	}
	if !entry.Answered {
		return entry
	}
	if n, ok := findings.CountRaw(o.Stdout); ok {
		entry.Findings = &n
	}
	entry.Top, _ = findings.TopSeverity(o.Stdout)
	return entry
}

// repoSlug is owner/name from the origin remote, so a panel run and a review
// run of one repository count as one, and a run in a worktree does not
// record the worktree's directory as a new repository. With no remote it is
// the directory's name.
func repoSlug(git worktree.Git, root string) string {
	ctx, cancel := context.WithTimeout(context.Background(), slugTimeout)
	defer cancel()
	out, err := git(ctx, root, "config", "--get", "remote.origin.url")
	if err != nil {
		return filepath.Base(root)
	}
	if slug, ok := slugOfRemote(string(out)); ok {
		return slug
	}
	return filepath.Base(root)
}

// slugOfRemote reads `git@github.com:acme/widgets.git` and
// `https://github.com/acme/widgets` alike as `acme/widgets`.
func slugOfRemote(url string) (string, bool) {
	trimmed := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(url), "/"), ".git")
	cut := strings.LastIndexAny(trimmed, "/:")
	if cut <= 0 {
		return "", false
	}
	name, rest := trimmed[cut+1:], trimmed[:cut]
	owner := rest[strings.LastIndexAny(rest, "/:")+1:]
	if name == "" || owner == "" {
		return "", false
	}
	return owner + "/" + name, true
}
