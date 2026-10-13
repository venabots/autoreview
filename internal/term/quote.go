package term

import (
	"fmt"
	"strings"
)

// safeBytes are the bytes, with the ASCII letters and digits, that a shell
// reads as plain text.
const safeBytes = "_./:=+,%@-"

// ShellQuote makes s one word for a POSIX shell. A tab is a new shell that
// the terminal starts, so the command reaches it as text, and a path with a
// space or a quote in it would split into two words or end the line early.
//
// A string of safe bytes is returned as it is. An empty string becomes a
// pair of single quotes, which is still one word and not nothing at all.
func ShellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, needsQuote) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// needsQuote says a character can mean something to a shell. Every
// character outside ASCII counts, as it does in v1.
func needsQuote(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	default:
		return !strings.ContainsRune(safeBytes, r)
	}
}

// ResumeCommand is the line a new tab runs to reopen a review: go to the
// review's repo, then run reopen there.
//
// The `&&` is the guard. When the directory is gone, the review must not
// open in whatever directory the new tab started in.
//
// dir is quoted here. reopen is a command line already, such as
// "claude --resume <id>", and is used as it is.
func ResumeCommand(dir, reopen string) string {
	return "cd " + ShellQuote(dir) + " && " + reopen
}

// ResumeLabel is the name the tab of a reopened review carries.
func ResumeLabel(pr int) string {
	return fmt.Sprintf("PR %d Resume", pr)
}
