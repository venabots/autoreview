package riskcheck

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// autoreviewsEnv is what a bot's environment holds: what any program needs,
// and the tokens and credentials that can approve a PR or spend a model.
func autoreviewsEnv(extra ...string) []string {
	return append([]string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/bot",
		"USER=bot",
		"LOGNAME=bot",
		"SHELL=/bin/bash",
		"TMPDIR=/tmp/bot",
		"TZ=UTC",
		"LANG=en_US.UTF-8",
		"LC_ALL=C.UTF-8",
		"GH_TOKEN=ghp_approving",
		"GITHUB_TOKEN=ghp_approving",
		"GH_ENTERPRISE_TOKEN=ghp_approving",
		"ANTHROPIC_API_KEY=sk-ant",
		"CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat",
		"CLAUDE_CONFIG_DIR=/home/bot/.claude",
		"OPENAI_API_KEY=sk-openai",
		"CODEX_HOME=/home/bot/.codex",
		"XDG_CONFIG_HOME=/home/bot/.config",
		"AUTOREVIEW_CONFIG=/etc/autoreview.toml",
		"AUTOREVIEW_CMD=sh -c reviewer",
		"PANEL_REVIEW_PANELISTS=claude,codex",
		"LC_GH_TOKEN=ghp_approving",
		"no equals sign",
	}, extra...)
}

// The script runs with a few of autoreview's variables by name, never with
// the whole of autoreview's environment, which holds a token that can
// approve PRs and the agents' credentials. A GitHub token reaches it only
// when the operator names one for it, and then as $GH_TOKEN, which gh
// reads. It arrives as Token only: the operator's variable left in
// Environ does not reach the script.
func TestAScriptGetsNoneOfAutoreviewsTokensUnlessOneIsNamedForIt(t *testing.T) {
	kept := []string{
		"PATH=/usr/bin:/bin", "HOME=/home/bot", "USER=bot", "LOGNAME=bot", "SHELL=/bin/bash",
		"TMPDIR=/tmp/bot", "TZ=UTC", "LANG=en_US.UTF-8", "LC_ALL=C.UTF-8",
	}
	told := []string{
		"AUTOREVIEW_REPO=acme/widgets", "AUTOREVIEW_PR=9", "AUTOREVIEW_BASE=main", "AUTOREVIEW_HEAD=abc123",
		"AUTOREVIEW_TRUSTED_REF=" + trustedCommit,
	}
	cases := []struct {
		name  string
		extra []string
		token string
		want  []string
	}{
		{"no token named", nil, "", nil},
		{"a token named", nil, "github_pat_read_only", []string{"GH_TOKEN=github_pat_read_only"}},
		{"the operator's variable left in the environment", []string{TokenVariable + "=github_pat_read_only"}, "", nil},
	}
	for _, c := range cases {
		s := &script{out: "LOW\n"}
		ck := checker(t, scripted(), s)
		ck.Environ = autoreviewsEnv(c.extra...)
		ck.Token = c.token
		before := slices.Clone(ck.Environ)
		ck.Check(context.Background(), pr9())
		if len(s.got) != 1 {
			t.Fatalf("%s: the script ran %d times", c.name, len(s.got))
		}
		env := slices.DeleteFunc(slices.Clone(s.got[0].Env), func(v string) bool { return strings.HasPrefix(v, "GH_CONFIG_DIR=") })
		want := slices.Concat(kept, c.want, told)
		if !slices.Equal(env, want) {
			t.Errorf("%s: the script's environment is\n%q\nwant\n%q", c.name, env, want)
		}
		if !slices.Equal(ck.Environ, before) {
			t.Errorf("%s: the check changed the environment it was handed", c.name)
		}
	}
}

// gh finds the login that autoreview's own gh calls use under $HOME, and a
// script that runs gh would sign in with it. The script's gh is pointed at
// an empty directory of the run's own for the one run, and the directory is
// gone after it, with the copy of the script beside it.
func TestTheScriptsGhHasEmptySettingsOfItsOwn(t *testing.T) {
	var dir string
	entries := -1
	ck := checker(t, scripted(), &script{})
	ck.Environ = autoreviewsEnv()
	ck.Run = func(_ context.Context, sc Script) ([]byte, error) {
		dir, _ = variable(sc.Env, "GH_CONFIG_DIR")
		if listed, err := os.ReadDir(dir); err == nil {
			entries = len(listed)
		}
		return []byte("LOW\n"), nil
	}
	ck.Check(context.Background(), pr9())
	if dir == "" || filepath.Dir(filepath.Dir(dir)) != ck.Dir {
		t.Fatalf("GH_CONFIG_DIR is %q, want a directory of the run's own in %s", dir, ck.Dir)
	}
	if entries != 0 {
		t.Errorf("the script's gh settings held %d entries, want an empty directory", entries)
	}
	if left, err := os.ReadDir(ck.Dir); err != nil || len(left) != 0 {
		t.Errorf("%s holds %v after the run (%v), want nothing", ck.Dir, left, err)
	}
}

// variable is the value env gives name, as a program would read it.
func variable(env []string, name string) (string, bool) {
	value, found := "", false
	for _, entry := range env {
		if n, v, cut := strings.Cut(entry, "="); cut && n == name {
			value, found = v, true
		}
	}
	return value, found
}
