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
	// intentAutofix, intentAutofixAll and intentFixAll
	// act on your own PRs. f is the fix there, and the focus on Review.
	intentAutofix
	intentAutofixAll
	intentFixAll
	// intentBots shows Dependabot's PRs on the Maintain tab, or hides them.
	intentBots
	intentWatch
	intentLog
	// intentFind is / and intentFindNumber a digit: both open the find
	// line, the digit with itself typed in it.
	intentFind
	intentFindNumber
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
	"a":         intentAutofix,
	"A":         intentAutofixAll,
	"F":         intentFixAll,
	"d":         intentBots,
	"w":         intentWatch,
	"l":         intentLog,
	"/":         intentFind,
	"0":         intentFindNumber,
	"1":         intentFindNumber,
	"2":         intentFindNumber,
	"3":         intentFindNumber,
	"4":         intentFindNumber,
	"5":         intentFindNumber,
	"6":         intentFindNumber,
	"7":         intentFindNumber,
	"8":         intentFindNumber,
	"9":         intentFindNumber,
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
	{"/", "find a PR by its number, or by words from its title, branch or author; typing a number starts it too"},
	{"^d/^u", "scroll the right pane (or space and the page keys)"},
	{"tab", "switch between Review and Maintain (or ←/→, or click one)"},
	{"R", "review the selected PR now, whatever holds it"},
	{"f", "Maintain: fix the selected PR once: its conflicts, review comments and CI, as it needs"},
	{"F", "Maintain: fix each of your PRs that needs it, once"},
	{"a", "Maintain: autofix the selected PR: fix it again each time it changes; an approved PR gets only its conflicts fixed"},
	{"A", "Maintain: autofix all your PRs, and any you open later; on when the view opens, A again turns it off"},
	{"d", "Maintain: show Dependabot's PRs too, to fix them or autofix them; d again hides them"},
	{"r", "resume the selected review, or your PR's last task, in a new terminal tab"},
	{"o", "open the selected PR in the browser"},
	{"x x", "stop the selected review or task; the first x asks"},
	{"w", "start or stop watching for new work"},
	{"f", "Review: type what the reviewers look at (the focus)"},
	{"l", "show the run log in the right pane; esc puts it away"},
	{"drag", "select text in the right pane and copy it"},
	{"m", "give the mouse back to the terminal, to select anywhere"},
	{"?", "show these keys; esc closes them"},
	{"q q", "quit, stopping any review or task that is running; the first q asks"},
	{"ctrl-C", "quit now, stopping any review that is running"},
}

// hint is one key in the footer.
type hint struct {
	keys string
	what string
}

// footerHints are the keys the footer names, most useful first. The footer
// shows as many as fit. Each tab names only the keys that act on it. The
// tab key names the tab it goes to: "tab review" on Maintain read as a key
// that reviews your PR.
func footerHints(shown tab) []hint {
	if shown == tabMine {
		return []hint{
			{"q", "quit"}, {"f", "fix"}, {"F", "fix all"}, {"a", "autofix"}, {"A", "autofix all"}, {"tab", "to Review"}, {"o", "open"}, {"j/k", "move"}, {"/", "find"}, {"r", "resume"}, {"x", "stop"}, {"d", "dependabot"},
			{"m", "mouse"}, {"l", "log"}, {"^d/^u", "scroll"},
		}
	}
	return []hint{
		{"q", "quit"}, {"j/k", "move"}, {"/", "find"}, {"r", "resume"}, {"o", "open"}, {"R", "review now"},
		{"tab", "to Maintain"}, {"w", "watch"}, {"m", "mouse"}, {"f", "focus"}, {"x", "stop"}, {"l", "log"}, {"^d/^u", "scroll"},
	}
}

// keysHint is always the last hint and is never cut: it is how a person
// finds every other key.
var keysHint = hint{"?", "keys"}
