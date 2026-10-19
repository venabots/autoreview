package rebase

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/venabots/autoreview/internal/gh"
	"github.com/venabots/autoreview/internal/prs"
)

// askedFilter keeps the comments that ask Dependabot to rebase, as one
// login and one date per line. The login is compared in Go: a login put
// into the filter would be text from outside in a program.
const askedFilter = `.[] | select(.body | test("^\\s*@dependabot rebase")) | [.user.login, .created_at] | @tsv`

// Check decides about one PR that Needs a rebase, and posts the comment
// when it should. post is false under --no-post.
//
// It asks GitHub at most three things, in this order, and stops at the
// first answer that settles it: whether the base moved on (only for a PR
// whose checks fail; a conflict is reason enough), whether a comment of
// yours already asks about this head, and then it posts. A person's commit
// on the branch settles it with no call: Dependabot would refuse.
func Check(ctx context.Context, run gh.Runner, slug, me string, pr prs.PR, post bool) (State, error) {
	if !pr.Conflicts {
		behind, err := behindBy(ctx, run, slug, pr)
		if err != nil {
			return Failed, err
		}
		if behind == 0 {
			return NotBehind, nil
		}
	}
	if pr.OthersCommitted {
		return OthersCommitted, nil
	}
	asked, err := alreadyAsked(ctx, run, slug, me, pr)
	switch {
	case err != nil:
		return Failed, err
	case asked:
		return AskedBefore, nil
	case !post:
		return WouldAsk, nil
	}
	if _, err := run(ctx, "api", "--method", "POST", issue(slug, pr.Number)+"/comments", "-f", "body="+Comment, "--silent"); err != nil {
		return Failed, fmt.Errorf("could not ask Dependabot to rebase PR #%d: %w", pr.Number, err)
	}
	return Asked, nil
}

// behindBy is how many commits the base has that the PR's head lacks. An
// unknown base or head is no proof the base moved: zero.
func behindBy(ctx context.Context, run gh.Runner, slug string, pr prs.PR) (int, error) {
	if pr.Base == "" || pr.Head == "" {
		return 0, nil
	}
	out, err := run(ctx, "api", fmt.Sprintf("repos/%s/compare/%s...%s", slug, pr.Base, pr.Head), "--jq", ".behind_by")
	if err != nil {
		return 0, fmt.Errorf("could not compare PR #%d with %s: %w", pr.Number, pr.Base, err)
	}
	behind, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("could not read how far PR #%d is behind %s: %q", pr.Number, pr.Base, strings.TrimSpace(string(out)))
	}
	return behind, nil
}

// alreadyAsked says a comment of yours asks Dependabot to rebase, written
// after the head commit. One written before it was answered by that head,
// or was about an older one.
func alreadyAsked(ctx context.Context, run gh.Runner, slug, me string, pr prs.PR) (bool, error) {
	out, err := run(ctx, "api", "--paginate", issue(slug, pr.Number)+"/comments", "--jq", askedFilter)
	if err != nil {
		return false, fmt.Errorf("could not read the comments on PR #%d: %w", pr.Number, err)
	}
	for line := range strings.Lines(string(out)) {
		login, at, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if ok && login == me && at >= pr.HeadDate {
			return true, nil
		}
	}
	return false, nil
}

func issue(slug string, number int) string {
	return fmt.Sprintf("repos/%s/issues/%d", slug, number)
}
