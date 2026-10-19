package words

import (
	"fmt"

	"github.com/venabots/autoreview/internal/prs"
)

// The words for a PR that sits on another open PR. A layer of a stack is
// reviewed by itself, so its words say where it sits and nothing about a
// wait. A PR whose diff carries another PR's commits waits for that PR, and
// its words name what the two share, not a stack nobody declared.

// held is the row word for a PR held on the PR underneath it.
func held(parent *prs.Parent) string {
	switch {
	case parent == nil:
		return "stacked"
	case parent.Evidence == prs.CarriesTip:
		return fmt.Sprintf("built on #%d", parent.Number)
	}
	return fmt.Sprintf("shares commits with #%d", parent.Number)
}

// layerWord is where a ready PR sits in a stack, short enough for a row:
// "layer 3 of 5" in a GitHub stack, "layer on #2784" on another PR's branch.
// It is empty for a PR in no stack.
func layerWord(pr prs.PR) string {
	switch {
	case pr.Layer != nil:
		return fmt.Sprintf("layer %d of %d", pr.Layer.Position, pr.Layer.Size)
	case pr.Parent != nil && pr.Parent.Evidence == prs.Declared:
		return fmt.Sprintf("layer on #%d", pr.Parent.Number)
	}
	return ""
}

// Stack says in a line how the PR sits on another open PR, and whether
// that holds its review. It is empty for a PR that sits on none.
func Stack(pr prs.PR) string {
	if pr.Layer != nil {
		where := fmt.Sprintf("layer %d of %d in GitHub stack #%d", pr.Layer.Position, pr.Layer.Size, pr.Layer.Stack)
		if pr.Parent != nil {
			where += fmt.Sprintf(", on #%d", pr.Parent.Number)
		}
		return where + "; reviewed by itself"
	}
	parent := pr.Parent
	switch {
	case parent == nil:
		return ""
	case parent.Evidence == prs.Declared:
		return fmt.Sprintf("on #%d (its base is #%d's branch); reviewed by itself", parent.Number, parent.Number)
	case parent.Evidence == prs.CarriesTip:
		return fmt.Sprintf("built on #%d (%s also in #%d)", parent.Number, Count(parent.Shared, "commit"), parent.Number)
	}
	return fmt.Sprintf("shares %s with #%d", Count(parent.Shared, "commit"), parent.Number)
}
