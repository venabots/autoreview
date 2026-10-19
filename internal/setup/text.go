package setup

import (
	"strings"
)

// noteText is text for a note. The full-screen note reads _ * ` and \ as
// marks, and would eat the underscores of a variable's name, so each one
// is escaped there. The accessible form prints the text as it is.
func noteText(text string, accessible bool) string {
	if accessible {
		return text
	}
	return strings.NewReplacer(`\`, `\\`, "_", `\_`, "*", `\*`, "`", "\\`").Replace(text)
}

// sentences joins the sentences that are there.
func sentences(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
}

func defaultIs(value string) string {
	return "Default: " + value + "."
}

// shellWins says that a variable of the shell beats the answer, which the
// person would otherwise save and then not see take effect.
func shellWins(s Start, variable string) string {
	if getenv(s, variable) == "" {
		return ""
	}
	return "Your shell sets $" + variable + ", which wins over this answer. Remove it from your shell after you save."
}

// verdictWords names a verdict level in the words a summary reads.
func verdictWords(level string) string {
	switch level {
	case "approve-only":
		return "approve only"
	case "comment-only":
		return "comments only"
	}
	return "approve, and request changes"
}

// intro is the first page's note: where the answers go, and how the keys
// move through the steps.
func intro(path string, accessible bool) string {
	where := "These go to " + path + ", and every run reads them. A flag or a variable of your shell still wins for one run. Run autoreview setup to change them, or edit the file."
	if accessible {
		return where + " Each step asks one question. Type the answer, then press enter."
	}
	return where + " On each step, enter keeps the answer shown and goes to the next step. Type to change the answer. shift+tab goes back one step. ctrl+c stops, and writes nothing."
}
