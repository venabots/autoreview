package prs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The GraphQL answer, down to the two things this package reads. The PR nodes
// stay raw until the path to them is known to exist: a missing path and an
// empty list are different answers, and only the second means "no PRs".
type response struct {
	Errors []json.RawMessage `json:"errors"`
	Data   struct {
		Repository struct {
			DefaultBranchRef struct {
				Name string `json:"name"`
			} `json:"defaultBranchRef"`
			PullRequests struct {
				Nodes json.RawMessage `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	} `json:"data"`
}

// Parse reads a GraphQL answer into a listing. It touches nothing outside its
// arguments, so one answer can be read again with other options.
//
// A 200 carrying an `errors` array, or an answer missing the data path
// entirely, must not read as "no PRs": an unattended sweep would then exit 0
// having reviewed nothing, which is the lie the exit status exists to
// prevent. Both are errors here. The text carries no "error: " prefix, which
// is the caller's to add.
func Parse(raw []byte, opts Options) (Listing, error) {
	nodes, defaultBranch, err := decode(raw)
	if err != nil {
		return Listing{}, err
	}
	return classify(nodes, defaultBranch, opts), nil
}

func decode(raw []byte) ([]node, string, error) {
	var answer response
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, "", fmt.Errorf("parsing gh api graphql output: %w", err)
	}
	if len(answer.Errors) > 0 {
		return nil, "", refusal(answer.Errors)
	}
	list := answer.Data.Repository.PullRequests.Nodes
	if len(list) == 0 {
		return nil, "", errors.New("gh api graphql returned no pull request data")
	}
	nodes, err := decodeNodes(list)
	if err != nil {
		return nil, "", fmt.Errorf("parsing the pull request list: %w", err)
	}
	return nodes, answer.Data.Repository.DefaultBranchRef.Name, nil
}

// decodeNodes refuses the two answers encoding/json would otherwise accept in
// silence. A null list decodes to no PRs, and a null entry decodes to a PR
// numbered zero that every later step would treat as real.
func decodeNodes(list json.RawMessage) ([]node, error) {
	var nodes []node
	if err := json.Unmarshal(list, &nodes); err != nil {
		return nil, err
	}
	if nodes == nil {
		return nil, errors.New("the list is null")
	}
	for i, n := range nodes {
		if n.Number <= 0 {
			return nil, fmt.Errorf("entry %d has no number", i)
		}
	}
	return nodes, nil
}

// refusal is GitHub's own errors, one to a line, so that "rate limited"
// reaches the person who has to wait it out.
func refusal(entries []json.RawMessage) error {
	lines := make([]string, len(entries))
	for i, entry := range entries {
		lines[i] = "\n  " + errorMessage(entry)
	}
	return errors.New("gh api graphql returned errors:" + strings.Join(lines, ""))
}

// errorMessage is the entry's message, and the entry itself when it has none:
// an error in a shape nobody expected is still better shown than dropped.
func errorMessage(entry json.RawMessage) string {
	var named struct {
		Message *string `json:"message"`
	}
	if err := json.Unmarshal(entry, &named); err != nil || named.Message == nil {
		return string(entry)
	}
	return *named.Message
}
