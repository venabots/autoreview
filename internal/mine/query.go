package mine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/venabots/autoreview/internal/gh"
	"github.com/venabots/autoreview/internal/prs"
)

// limit is how many of your PRs one look reads. More than anybody has open
// at once.
const limit = 50

// Query is the search that reads your PRs.
const Query = `
      query($q:String!) {
        search(query:$q, type:ISSUE, first:50) {
          nodes {
            ... on PullRequest {
              number
              title
              isDraft
              headRefName
              headRefOid
              updatedAt
              isCrossRepository
              reviewDecision
              mergeable
              latestReviews(first:20) { nodes { author { login } state } }
              reviewThreads(first:100) { nodes { id isResolved } }
              headCommit: commits(last:1) { nodes { commit { statusCheckRollup { state } } } }
            }
          }
        }
      }`

// SearchTerms is the search that finds your open PRs in one repo.
func SearchTerms(owner, name string) string {
	return fmt.Sprintf("repo:%s/%s is:pr is:open author:@me", owner, name)
}

// List reads your open PRs in the repo, most urgent first.
func List(ctx context.Context, run gh.Runner, owner, name string) ([]PR, error) {
	raw, err := run(ctx, "api", "graphql", "-f", "q="+SearchTerms(owner, name), "-f", "query="+Query)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

type response struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Data *struct {
		Search *struct {
			Nodes []node `json:"nodes"`
		} `json:"search"`
	} `json:"data"`
}

// node is one search result. Every field is optional: a result that is not
// a pull request comes back empty, and is dropped rather than refused.
type node struct {
	Number        *int   `json:"number"`
	Title         string `json:"title"`
	IsDraft       bool   `json:"isDraft"`
	HeadRefName   string `json:"headRefName"`
	HeadRefOid    string `json:"headRefOid"`
	UpdatedAt     string `json:"updatedAt"`
	IsCrossRepo   bool   `json:"isCrossRepository"`
	Decision      string `json:"reviewDecision"`
	Mergeable     string `json:"mergeable"`
	LatestReviews struct {
		Nodes []struct {
			Author *struct {
				Login string `json:"login"`
			} `json:"author"`
			State string `json:"state"`
		} `json:"nodes"`
	} `json:"latestReviews"`
	ReviewThreads struct {
		Nodes []struct {
			ID         string `json:"id"`
			IsResolved bool   `json:"isResolved"`
		} `json:"nodes"`
	} `json:"reviewThreads"`
	HeadCommit struct {
		Nodes []struct {
			Commit struct {
				Rollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"headCommit"`
}

// Parse is the PRs out of a search response, or why there are none. An
// errors array or a missing data path is a failed look, not an empty list:
// the tab would otherwise say you have no PRs.
func Parse(raw []byte) ([]PR, error) {
	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parsing your PRs: %w", err)
	}
	if len(r.Errors) > 0 {
		return nil, fmt.Errorf("gh api graphql returned errors: %s", messageOr(r.Errors[0].Message, "unknown error"))
	}
	if r.Data == nil || r.Data.Search == nil {
		return nil, errors.New("gh api graphql returned no search results")
	}
	list := make([]PR, 0, len(r.Data.Search.Nodes))
	for _, n := range r.Data.Search.Nodes {
		if n.Number == nil || len(list) == limit {
			continue
		}
		list = append(list, n.pr())
	}
	return Ranked(list), nil
}

func messageOr(msg, fallback string) string {
	if msg == "" {
		return fallback
	}
	return msg
}

func (n node) pr() PR {
	return PR{
		Number:    *n.Number,
		Title:     n.Title,
		Draft:     n.IsDraft,
		Branch:    n.HeadRefName,
		Head:      n.HeadRefOid,
		UpdatedAt: n.UpdatedAt,
		CrossRepo: n.IsCrossRepo,
		Review:    reviewFrom(n.Decision),
		Merge:     mergeFrom(n.Mergeable),
		CI:        prs.CIFromState(n.rollupState()),
		ThreadIDs: n.openThreads(),
		Reviewers: n.reviewers(),
	}
}

func (n node) rollupState() string {
	if len(n.HeadCommit.Nodes) == 0 || n.HeadCommit.Nodes[0].Commit.Rollup == nil {
		return ""
	}
	return n.HeadCommit.Nodes[0].Commit.Rollup.State
}

func (n node) openThreads() []string {
	ids := []string{}
	for _, t := range n.ReviewThreads.Nodes {
		if !t.IsResolved {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

// reviewers keeps the reviews that decided something. A comment decides
// nothing, and a review whose author GitHub no longer names has nobody to
// show.
func (n node) reviewers() []Reviewer {
	decided := []Reviewer{}
	for _, r := range n.LatestReviews.Nodes {
		review := reviewFrom(r.State)
		if r.Author == nil || r.Author.Login == "" {
			continue
		}
		if review == ReviewApproved || review == ReviewChangesRequested {
			decided = append(decided, Reviewer{Login: r.Author.Login, Review: review})
		}
	}
	return decided
}

func reviewFrom(raw string) Review {
	switch raw {
	case "APPROVED":
		return ReviewApproved
	case "CHANGES_REQUESTED":
		return ReviewChangesRequested
	case "REVIEW_REQUIRED":
		return ReviewRequired
	}
	return ReviewNone
}

func mergeFrom(raw string) Merge {
	switch raw {
	case "MERGEABLE":
		return MergeClean
	case "CONFLICTING":
		return MergeConflicts
	}
	return MergeUnknown
}
