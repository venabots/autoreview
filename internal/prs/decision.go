package prs

// Decision is what the PR's reviews add up to, as GitHub reports it. The
// sweep only reads "approved" (it leaves those alone); a view draws all of
// them, because an icon per PR is the fastest way to see where a repo stands.
type Decision int

const (
	// DecisionNone is a PR on a branch that asks for no review, or an answer
	// this code cannot read. Neither is a reason to leave a PR out.
	DecisionNone Decision = iota
	Approved
	ChangesRequested
	// ReviewRequired is a protected branch still waiting for its review.
	ReviewRequired
)

func decisionFromRaw(raw string) Decision {
	switch raw {
	case "APPROVED":
		return Approved
	case "CHANGES_REQUESTED":
		return ChangesRequested
	case "REVIEW_REQUIRED":
		return ReviewRequired
	default:
		return DecisionNone
	}
}

func (d Decision) String() string {
	switch d {
	case Approved:
		return "approved"
	case ChangesRequested:
		return "changes requested"
	case ReviewRequired:
		return "review required"
	default:
		return "none"
	}
}
