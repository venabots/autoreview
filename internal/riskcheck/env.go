package riskcheck

import (
	"slices"
	"strconv"
	"strings"

	"github.com/venabots/autoreview/internal/prs"
)

// TokenVariable names a GitHub token of the operator's choosing that a risk
// script gets as $GH_TOKEN. autoreview's own token can approve PRs, so it
// never reaches the script: a script that reads GitHub, as one that reads
// CODEOWNERS does, needs a token of its own, best one that can only read.
// A Checker takes the token as Token and never passes this variable on from
// Environ, so a caller can keep it out of what everything else it starts
// inherits.
const TokenVariable = "AUTOREVIEW_RISK_GH_TOKEN"

// passed are the variables of autoreview's own environment that reach a
// risk script: what a program needs to find its tools, its home and its
// locale. autoreview runs with a token that can approve PRs and with the
// agents' credentials, and a script handed the whole environment would hold
// them all. The locale's variables are named one by one, not as every LC_
// name: the stock sshd_config of macOS, Debian and Ubuntu accepts LC_*
// from the client, so people carry their own values in names such as
// LC_GH_TOKEN.
var passed = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TZ", "LANG",
	"LC_ALL", "LC_COLLATE", "LC_CTYPE", "LC_MESSAGES", "LC_MONETARY", "LC_NUMERIC", "LC_TIME",
}

// env is the script's whole environment: the variables in passed, Token as
// $GH_TOKEN, and what autoreview tells the script. $GH_CONFIG_DIR points gh
// at ghConfig, an empty directory, because gh would otherwise sign in with
// the login that autoreview's own gh calls use, which it finds under $HOME.
// That stops gh from signing in by itself, not a script that goes looking:
// the script runs as autoreview's user, and the login stays where that user
// can reach it, in $HOME, in the system keyring, and through git where gh is
// git's credential helper.
func (c Checker) env(pr prs.PR, commit, ghConfig string) []string {
	var env []string
	for _, entry := range c.Environ {
		if name, _, ok := strings.Cut(entry, "="); ok && slices.Contains(passed, name) {
			env = append(env, entry)
		}
	}
	if c.Token != "" {
		env = append(env, "GH_TOKEN="+c.Token)
	}
	return append(env,
		"GH_CONFIG_DIR="+ghConfig,
		"AUTOREVIEW_REPO="+c.Repo.Slug(),
		"AUTOREVIEW_PR="+strconv.Itoa(pr.Number),
		"AUTOREVIEW_BASE="+pr.Base,
		"AUTOREVIEW_HEAD="+pr.Head,
		"AUTOREVIEW_TRUSTED_REF="+commit,
	)
}
