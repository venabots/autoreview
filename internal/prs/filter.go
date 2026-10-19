package prs

import "strings"

// Hidden is why the sweep leaves a PR out. The PR stays in the listing either
// way: a view that shows an approved PR as approved is more use than one
// where it is missing, and "found 3 open PRs" on a repo showing 40 in the
// browser reads as a broken query rather than a working filter.
type Hidden int

const (
	NotHidden Hidden = iota
	// HiddenDraft is a PR its author has not asked anyone to read yet.
	HiddenDraft
	// HiddenOwn is a PR the person running the sweep opened. This tool is for
	// reviewing other people's work.
	HiddenOwn
	// HiddenBot is a bot's PR, shown only when Options.Dependabot asks.
	HiddenBot
	// HiddenApproved is a PR that already has its approval, shown only when
	// Options.All asks.
	HiddenApproved
	// HiddenAuthor is a PR that none of Options.Authors opened.
	HiddenAuthor
)

func (h Hidden) String() string {
	switch h {
	case HiddenDraft:
		return "draft"
	case HiddenOwn:
		return "own"
	case HiddenBot:
		return "bot"
	case HiddenApproved:
		return "approved"
	case HiddenAuthor:
		return "other author"
	default:
		return "not hidden"
	}
}

// Author logins treated as bots. An anchored prefix match: extend it as more
// coding bots appear.
const botLoginPrefix = "dependabot"

func isBot(login string) bool {
	return strings.HasPrefix(login, botLoginPrefix)
}

// hiddenReason is the first filter that removes the PR, in the order the
// sweep applies them: draft, own, author, bot, approved. The order decides
// what a view says about a PR that two filters would remove, and only the
// last two have a flag that lifts them, so a flag never uncovers a draft,
// the user's own PR, or a PR by someone --author leaves out.
func hiddenReason(n node, opts Options) Hidden {
	switch {
	case n.IsDraft:
		return HiddenDraft
	case n.Author.Login == opts.Me:
		return HiddenOwn
	case !byAuthor(n.Author.Login, opts.Authors):
		return HiddenAuthor
	case !opts.Dependabot && isBot(n.Author.Login):
		return HiddenBot
	case !opts.All && n.ReviewDecision == "APPROVED":
		return HiddenApproved
	default:
		return NotHidden
	}
}
