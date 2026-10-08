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
	intentOpen
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
	"o":         intentOpen,
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
	{"tab", "switch between Review and My PRs"},
	{"o", "open the selected PR in the browser"},
	{"m", "give the mouse back to the terminal, to select text"},
	{"?", "show these keys; esc puts them away"},
	{"q", "quit"},
	{"ctrl-C", "quit now"},
}

// hint is one key in the footer.
type hint struct {
	keys string
	what string
}

// footerHints are the keys the footer names, most useful first. The footer
// shows as many as fit.
func footerHints(shown tab) []hint {
	other := "my PRs"
	if shown == tabMine {
		other = "review"
	}
	return []hint{
		{"q", "quit"},
		{"j/k", "move"},
		{"o", "open"},
		{"tab", other},
		{"m", "mouse"},
		{"^d/^u", "scroll"},
	}
}

// keysHint is always the last hint and is never cut: it is how a person
// finds every other key.
var keysHint = hint{"?", "keys"}
