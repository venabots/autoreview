package term

import (
	"context"
	"testing"
)

const sessionID = "7442b624-5cba-5d44-ae67-9c390cfe70a1"

var quoteCases = []struct {
	name, in, want string
}{
	{"a plain path is left as it is", "/plain/path", "/plain/path"},
	{"every safe byte is left as it is", "aZ09_./:=+,%@-", "aZ09_./:=+,%@-"},
	{"a session id is left as it is", sessionID, sessionID},
	{"a space is quoted", "/has space/repo", "'/has space/repo'"},
	{"a single quote is closed, escaped and opened again", "/it's here", `'/it'\''s here'`},
	{"an empty string is still one word", "", "''"},
	{"a second command cannot be added", "x; rm -rf ~", "'x; rm -rf ~'"},
	{"a substitution is not run", "$(id) `id` $HOME", "'$(id) `id` $HOME'"},
	{"a double quote and a backslash are kept", `a"b\c`, `'a"b\c'`},
	{"a newline is kept inside the word", "a\nb", "'a\nb'"},
	{"a letter outside ASCII is quoted", "/café", "'/café'"},
	{"only quotes", "''", `''\'''\'''`},
}

func TestShellQuoteGivesV1Output(t *testing.T) {
	for _, c := range quoteCases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShellQuote(c.in); got != c.want {
				t.Errorf("ShellQuote(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestAQuotedStringReachesAShellAsOneWord(t *testing.T) {
	for _, c := range quoteCases {
		t.Run(c.name, func(t *testing.T) {
			script := `printf '%s|%s' "$#" "$1"`
			out, err := Exec(context.Background(), "sh", "-c", "set -- "+ShellQuote(c.in)+"; "+script)
			if err != nil {
				t.Fatalf("sh: %v", err)
			}
			if got, want := string(out), "1|"+c.in; got != want {
				t.Errorf("the shell saw %q, want %q", got, want)
			}
		})
	}
}

func TestResumeCommandGoesToTheRepoThenReopens(t *testing.T) {
	cases := []struct {
		name, dir, reopen, want string
	}{
		{
			"a claude review reopens with claude in its repo",
			"/src/app", "claude --resume " + sessionID,
			"cd /src/app && claude --resume " + sessionID,
		},
		{
			"a codex review reopens with codex",
			"/src/app", "codex resume " + sessionID,
			"cd /src/app && codex resume " + sessionID,
		},
		{
			"a path with a space stays one word",
			"/src/my app", "claude --resume " + sessionID,
			"cd '/src/my app' && claude --resume " + sessionID,
		},
		{
			"a path with a quote cannot end the line",
			"/src/it's; rm -rf ~", "claude --resume " + sessionID,
			`cd '/src/it'\''s; rm -rf ~' && claude --resume ` + sessionID,
		},
		{
			"the reopen command is used as it is",
			"/src", "claude --resume " + ShellQuote("x; rm -rf ~"),
			"cd /src && claude --resume 'x; rm -rf ~'",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResumeCommand(c.dir, c.reopen); got != c.want {
				t.Errorf("ResumeCommand(%q, %q) = %q, want %q", c.dir, c.reopen, got, c.want)
			}
		})
	}
}

func TestAReopenedReviewsTabIsNamedForItsPR(t *testing.T) {
	if got, want := ResumeLabel(9), "PR 9 Resume"; got != want {
		t.Errorf("ResumeLabel(9) = %q, want %q", got, want)
	}
}
