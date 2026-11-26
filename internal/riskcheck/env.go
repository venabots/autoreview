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
const TokenVariable = "AUTOREVIEW_RISK_GH_TOKEN"

// passed are the variables of autoreview's own environment that reach a
// risk script, with every LC_ variable: what a program needs to find its
// tools, its home and its locale. autoreview runs with a token that can
// approve PRs and with the agents' credentials, and a script handed the
// whole environment would hold them all.
var passed = []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TZ", "LANG"}

// env is the script's whole environment: the variables in passed, the token
// that TokenVariable names as $GH_TOKEN, and what autoreview tells the
// script. $GH_CONFIG_DIR points gh at ghConfig, an empty directory, because
// gh would otherwise sign in with the login that autoreview's own gh calls
// use, which it finds under $HOME.
func (c Checker) env(pr prs.PR, at trusted, ghConfig string) []string {
	var env []string
	for _, entry := range c.Environ {
		name, value, ok := strings.Cut(entry, "=")
		switch {
		case !ok:
		case name == TokenVariable:
			if value != "" {
				env = append(env, "GH_TOKEN="+value)
			}
		case slices.Contains(passed, name) || strings.HasPrefix(name, "LC_"):
			env = append(env, entry)
		}
	}
	return append(env,
		"GH_CONFIG_DIR="+ghConfig,
		"AUTOREVIEW_REPO="+c.Repo.Owner+"/"+c.Repo.Name,
		"AUTOREVIEW_PR="+strconv.Itoa(pr.Number),
		"AUTOREVIEW_BASE="+pr.Base,
		"AUTOREVIEW_HEAD="+pr.Head,
		"AUTOREVIEW_TRUSTED_REF="+at.commit,
	)
}
