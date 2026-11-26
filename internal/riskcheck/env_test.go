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
// reads.
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
		token []string
	}{
		{"no token named", nil, nil},
		{"an empty token", []string{TokenVariable + "="}, nil},
		{"a token named", []string{TokenVariable + "=github_pat_read_only"}, []string{"GH_TOKEN=github_pat_read_only"}},
	}
	for _, c := range cases {
		s := &script{out: "LOW\n"}
		ck := checker(t, scripted(), s)
		ck.Environ = autoreviewsEnv(c.extra...)
		before := slices.Clone(ck.Environ)
		ck.Check(context.Background(), pr9())
		if len(s.got) != 1 {
			t.Fatalf("%s: the script ran %d times", c.name, len(s.got))
		}
		env := slices.DeleteFunc(slices.Clone(s.got[0].Env), func(v string) bool { return strings.HasPrefix(v, "GH_CONFIG_DIR=") })
		want := slices.Concat(kept, c.token, told)
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
// gone after it.
func TestTheScriptsGhHasEmptySettingsOfItsOwn(t *testing.T) {
	s := &script{out: "LOW\n"}
	ck := checker(t, scripted(), s)
	ck.Environ = autoreviewsEnv()
	ck.Check(context.Background(), pr9())
	if len(s.got) != 1 {
		t.Fatalf("the script ran %d times", len(s.got))
	}
	dir, found := variable(s.got[0].Env, "GH_CONFIG_DIR")
	if !found || filepath.Dir(dir) != ck.Dir {
		t.Fatalf("GH_CONFIG_DIR is %q, want a directory in %s", dir, ck.Dir)
	}
	if s.got[0].ghConfig != 0 {
		t.Errorf("the script's gh settings held %d entries, want an empty directory", s.got[0].ghConfig)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("%s is still there after the run: %v", dir, err)
	}
}
