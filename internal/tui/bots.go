package tui

import (
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/mine"
)

// The Maintain tab can hold Dependabot's PRs beside yours. Each one says
// who opened it, so a bump is never read as your own work, and says when
// one of us pushed to it: Dependabot stops rebasing a PR after that, and
// nobody else will keep it up to date.

// botsPressed shows Dependabot's PRs on the Maintain tab, or hides them.
func (m Model) botsPressed() Model {
	if m.tab != tabMine {
		return m.flashed(notMine)
	}
	on := !m.snap.MineBots
	m.eng.Do(engine.IncludeBots{On: on})
	if on {
		return m.flashed("showing Dependabot's PRs beside yours; d hides them")
	}
	return m.flashed("showing only your PRs")
}

// mineWhat is the second line of a row: the branch, after its author when
// the PR is not yours.
func mineWhat(pr mine.PR) string {
	if pr.Bot {
		return "@dependabot " + pr.Branch
	}
	return pr.Branch
}

// botLines say who opened a bot's PR, and that Dependabot no longer keeps
// it up to date once one of us pushed to it.
func botLines(pr mine.PR) []line {
	if !pr.Bot {
		return nil
	}
	lines := []line{field("from", word{"Dependabot", plainStyle})}
	if pr.OthersPushed {
		lines = append(lines, field("rebase", word{"pushed to by us: Dependabot no longer rebases it", yellowStyle}))
	}
	return lines
}
