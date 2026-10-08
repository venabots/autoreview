package review

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/venabots/autoreview/internal/gh"
)

// ReadbackKind is what the readback from GitHub learned.
type ReadbackKind int

const (
	// ReadbackFailed means the gh call failed or timed out, or its answer
	// had no review list. Nothing was learned. It is the zero value.
	ReadbackFailed ReadbackKind = iota
	// ReadbackNothing means gh answered, and this login submitted no review
	// since the job started.
	ReadbackNothing
	// ReadbackLanded means gh answered, and the review is there.
	ReadbackLanded
)

// Readback is the answer to one question: did my login submit a review on
// this PR since the job started?
//
// Failed and Nothing differ on purpose. An empty answer contradicts an
// agent that says its review landed. A failed call contradicts nothing.
type Readback struct {
	Kind ReadbackKind
	// Word is approved, commented or changes requested. It is set for
	// ReadbackLanded only.
	Word string
}

// skewGrace absorbs a small difference between this machine's clock and
// GitHub's. It is short on purpose: it is also the window in which a review
// from the run before this one would read as this run's.
const skewGrace = 10 * time.Second

// latestReviews is the answer of `gh pr view --json latestReviews`. The
// list is a pointer, so an answer that has no list is told from an answer
// whose list is empty.
type latestReviews struct {
	LatestReviews *[]latestReview `json:"latestReviews"`
}

type latestReview struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	State       string `json:"state"`
	SubmittedAt string `json:"submittedAt"`
}

// Verdict asks GitHub for the review that the login me submitted on a PR
// since the job started.
//
// A reviewer is a model, and a model that was believed would show
// "approved" for an approval that never landed. This answer is the one a
// summary shows; the trailer is used only when GitHub has nothing to say.
//
// Only latestReviews is asked for. It holds the most recent review of each
// reviewer, so an approval from a run last week does not read as the
// verdict of this one.
//
// The PR is named with its repo, so the answer does not depend on the
// directory the run was started in.
func Verdict(ctx context.Context, run gh.Runner, repoSlug string, pr int, me string, since time.Time) Readback {
	out, err := run(ctx, "pr", "view", strconv.Itoa(pr), "--repo", repoSlug, "--json", "latestReviews")
	if err != nil {
		return Readback{Kind: ReadbackFailed}
	}
	var answer latestReviews
	if err := json.Unmarshal(out, &answer); err != nil || answer.LatestReviews == nil {
		// Only an answer that has the list means "GitHub says nothing
		// landed". An answer of another shape teaches nothing.
		return Readback{Kind: ReadbackFailed}
	}
	if word, ok := verdictFromReviews(*answer.LatestReviews, me, since); ok {
		return Readback{Kind: ReadbackLanded, Word: word}
	}
	return Readback{Kind: ReadbackNothing}
}

// verdictFromReviews is the verdict of the first review by me that is new
// enough. A review with no author is nobody's, and a review whose time
// cannot be read cannot be shown to be new.
func verdictFromReviews(reviews []latestReview, me string, since time.Time) (string, bool) {
	earliest := since.Unix() - int64(skewGrace/time.Second)
	for _, review := range reviews {
		if review.Author.Login == "" || review.Author.Login != me {
			continue
		}
		at, err := time.Parse(time.RFC3339, review.SubmittedAt)
		if err != nil {
			continue
		}
		if at.Unix() >= earliest {
			return verdictWord(review.State)
		}
	}
	return "", false
}

func verdictWord(state string) (string, bool) {
	switch state {
	case "APPROVED":
		return "approved", true
	case "CHANGES_REQUESTED":
		return "changes requested", true
	case "COMMENTED":
		return "commented", true
	}
	return "", false
}

// ResolveVerdict is the one verdict a summary shows, and false when there
// is none. t is the trailer, or nil when the review wrote none or its claim
// must not count.
//
// GitHub's readback outranks the agent's own decision. An empty readback
// vetoes a claim that a review landed: an agent that says its approval
// landed when GitHub says nothing did is the belief in itself that the
// verdict exists to catch.
//
// "commented" survives the veto. A plain comment never appears in
// latestReviews, an older skill and a command override can still post one,
// and a wrong "commented" claims no verdict at all.
func ResolveVerdict(rb Readback, t *Trailer) (string, bool) {
	if rb.Kind == ReadbackLanded {
		return rb.Word, true
	}
	if t == nil {
		return "", false
	}
	switch t.Decision {
	case "approved", "changes-requested":
		if rb.Kind == ReadbackNothing {
			return "", false
		}
		return claimWord(t.Decision), true
	case "commented":
		return "commented", true
	}
	return "", false
}

// claimWord is a trailer's decision in the words a readback uses.
func claimWord(decision string) string {
	if decision == "changes-requested" {
		return "changes requested"
	}
	return decision
}

// VetoedClaim is the claim that an empty readback vetoed, and false when it
// vetoed none. The caller shows it: to drop a false "approved" in silence
// would hide the lie the readback exists to catch.
func VetoedClaim(rb Readback, t *Trailer) (string, bool) {
	if rb.Kind != ReadbackNothing || t == nil {
		return "", false
	}
	switch t.Decision {
	case "approved", "changes-requested":
		return t.Decision, true
	}
	return "", false
}
