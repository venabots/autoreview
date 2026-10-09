package tui

import tea "charm.land/bubbletea/v2"

// intent is what a key asks for.
type intent int

const (
	intentNone intent = iota
	intentUp
	intentDown
	intentTop
	intentBottom
	intentPageUp
	intentPageDown
	intentSwitchTab
	// intentFirstTab and intentSecondTab are the arrows. They name a side,
	// so the left arrow on the left tab does nothing: an arrow that wrapped
	// would move the way it does not point.
	intentFirstTab
	intentSecondTab
	intentOpen
	intentResume
	intentReviewNow
	intentStop
	intentFocus
	// intentComments, intentConflicts, intentBabysit and intentBabysitAll
	// act on your own PRs. f is the fix there, and the focus on Review.
	intentComments
	intentConflicts
	intentBabysit
	intentBabysitAll
	intentWatch
	intentLog
	intentMouse
	intentKeys
	intentBack
	intentQuit
	// intentInterrupt is ctrl-C: leave now, with no second press.
	intentInterrupt
)

// keyNames maps a key, as Bubble Tea names it, to what it asks for. It is
// one table, so the key help and the tests read the same source.
var keyNames = map[string]intent{
	"j":         intentDown,
	"down":      intentDown,
	"k":         intentUp,
	"up":        intentUp,
	"g":         intentTop,
	"home":      intentTop,
	"G":         intentBottom,
	"end":       intentBottom,
	"ctrl+d":    intentPageDown,
	"pgdown":    intentPageDown,
	"space":     intentPageDown,
	"ctrl+u":    intentPageUp,
	"pgup":      intentPageUp,
	"tab":       intentSwitchTab,
	"shift+tab": intentSwitchTab,
	"left":      intentFirstTab,
	"right":     intentSecondTab,
	"o":         intentOpen,
	"r":         intentResume,
	"R":         intentReviewNow,
	"x":         intentStop,
	"f":         intentFocus,
	"c":         intentComments,
	"u":         intentConflicts,
	"b":         intentBabysit,
	"B":         intentBabysitAll,
	"w":         intentWatch,
	"l":         intentLog,
	"m":         intentMouse,
	"?":         intentKeys,
	"esc":       intentBack,
	"q":         intentQuit,
	"ctrl+c":    intentInterrupt,
}

func intentFor(msg tea.KeyPressMsg) intent {
	return keyNames[msg.String()]
}

// keyHelp is one line of the key help.
type keyHelp struct {
	keys string
	what string
}

// helpLines lists every key this build answers to.
var helpLines = []keyHelp{
	{"j/k", "move between PRs (or the arrow keys)"},
	{"g/G", "first or last PR"},
	{"^d/^u", "scroll the right pane (or space and the page keys)"},
	{"tab", "switch between Review and My PRs (or ←/→, or click one)"},
	{"R", "review the selected PR now, whatever holds it"},
	{"f", "My PRs: fix the selected PR once (conflicts, comments, CI)"},
	{"b", "My PRs: babysit the selected PR: fix it again when it changes"},
	{"B", "My PRs: babysit all your PRs, and any you open later"},
	{"u", "My PRs: merge the base in and fix the conflicts"},
	{"c", "My PRs: answer the selected PR's review comments"},
	{"r", "resume the selected review, or your PR's last task, in a new terminal tab"},
	{"o", "open the selected PR in the browser"},
	{"x x", "stop the selected review or task"},
	{"w", "start or stop watching for new work"},
	{"f", "Review: type what the reviewers look at (the focus)"},
	{"l", "show the run log here; esc puts it away"},
	{"m", "give the mouse back to the terminal, to select text"},
	{"?", "show these keys; esc puts them away"},
	{"q", "quit; q again if reviews are running, which stops them"},
	{"ctrl-C", "quit now, stopping any review that is running"},
}

// hint is one key in the footer.
type hint struct {
	keys string
	what string
}

// footerHints are the keys the footer names, most useful first. The footer
// shows as many as fit. Each tab names only the keys that act on it.
func footerHints(shown tab) []hint {
	if shown == tabMine {
		return []hint{
			{"q", "quit"}, {"f", "fix"}, {"b", "babysit"}, {"u", "conflicts"}, {"c", "comments"}, {"tab", "review"},
			{"B", "babysit all"}, {"o", "open"}, {"j/k", "move"}, {"r", "resume"}, {"x", "stop"},
			{"m", "mouse"}, {"l", "log"}, {"^d/^u", "scroll"},
		}
	}
	return []hint{
		{"q", "quit"}, {"j/k", "move"}, {"r", "resume"}, {"o", "open"}, {"R", "review now"},
		{"tab", "my PRs"}, {"w", "watch"}, {"m", "mouse"}, {"f", "focus"}, {"x", "stop"}, {"l", "log"}, {"^d/^u", "scroll"},
	}
}

// keysHint is always the last hint and is never cut: it is how a person
// finds every other key.
var keysHint = hint{"?", "keys"}
