package riskcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// trustedQuery asks for the default branch and the commit it is at in one
// answer, so the name and the commit come from the same moment.
const trustedQuery = `
      query($owner:String!, $name:String!) {
        repository(owner:$owner, name:$name) {
          defaultBranchRef { name target { oid } }
        }
      }`

// commitID is a whole commit id, SHA-1 or SHA-256. Anything else would be
// read by GitHub as a branch name, which anyone who can push may move.
var commitID = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// trusted is where REVIEW.md and the script are read: one commit of the
// default branch, fixed for the whole check. A PR's base can be any branch,
// and anyone who can push to that branch could rewrite the rules the PR is
// judged by. The default branch is the one whose changes are reviewed.
type trusted struct {
	branch string
	commit string
}

// String names the commit for a person: the branch and a short id.
func (t trusted) String() string {
	return t.branch + " at " + t.commit[:12]
}

// trusted asks GitHub for the default branch's commit. A repository with
// no default branch, or an answer without a whole commit id, is an error:
// read by name instead, a move of the branch mid-check could hand REVIEW.md
// and the script from two different commits.
func (c Checker) trusted(ctx context.Context) (trusted, error) {
	raw, err := c.GH(ctx, "api", "graphql", "-F", "owner="+c.Repo.Owner, "-F", "name="+c.Repo.Name, "-f", "query="+trustedQuery)
	if err != nil {
		return trusted{}, fmt.Errorf("asking GitHub for the default branch: %w", err)
	}
	var answer struct {
		Data struct {
			Repository struct {
				DefaultBranchRef *struct {
					Name   string `json:"name"`
					Target struct {
						Oid string `json:"oid"`
					} `json:"target"`
				} `json:"defaultBranchRef"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return trusted{}, fmt.Errorf("reading GitHub's answer for the default branch: %w", err)
	}
	ref := answer.Data.Repository.DefaultBranchRef
	switch {
	case ref == nil || ref.Name == "":
		return trusted{}, errors.New("GitHub gave no default branch")
	case !commitID.MatchString(ref.Target.Oid):
		return trusted{}, fmt.Errorf("GitHub gave %q as the commit of %s", ref.Target.Oid, ref.Name)
	}
	return trusted{branch: ref.Name, commit: ref.Target.Oid}, nil
}
