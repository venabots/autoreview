package ledger

import "strings"

// EnvLedger names the variable that moves the ledger, or turns it off.
const EnvLedger = "AUTOREVIEW_LEDGER"

// Path is where the ledger is. ok is false when `$AUTOREVIEW_LEDGER=off`
// asked for nothing to be recorded, or when there is no home to put it in.
// env looks a variable up, and an empty value counts as unset: a variable
// exported with no value must not send the ledger to a file named "".
func Path(env func(string) string) (path string, ok bool) {
	if named := env(EnvLedger); named != "" {
		if strings.EqualFold(named, "off") {
			return "", false
		}
		return named, true
	}
	state := env("XDG_STATE_HOME")
	if state == "" {
		home := env("HOME")
		if home == "" {
			return "", false
		}
		state = under(home, ".local/state")
	}
	return under(under(state, "autoreview"), "ledger.jsonl"), true
}

// under joins a name onto a directory the way v1 does. It does not clean the
// directory, so v1 and v2 print the same path for the same environment.
func under(dir, name string) string {
	if strings.HasSuffix(dir, "/") {
		return dir + name
	}
	return dir + "/" + name
}
