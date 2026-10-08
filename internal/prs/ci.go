package prs

// CI is the state of the checks on a PR's head commit.
//
// A PR opened a minute ago has a linter still running, and a review posted
// while the checks are red is a review of code its author is about to change.
// So the sweep holds a PR until the checks on its head commit pass.
type CI int

const (
	// CINone is no checks on the head commit. A repo without CI is not a
	// repo that must wait for ever, so this reads as ready.
	CINone CI = iota
	CIPassing
	CIPending
	CIFailing
)

// CIFromState reads the StatusState on a head commit's rollup. The schema has
// exactly five values. Anything else, the empty string included, reads as no
// checks, because a value this code cannot read must not hold a PR for ever.
func CIFromState(state string) CI {
	switch state {
	case "SUCCESS":
		return CIPassing
	case "PENDING", "EXPECTED":
		return CIPending
	case "FAILURE", "ERROR":
		return CIFailing
	default:
		return CINone
	}
}

// ready says whether the sweep may review a PR in this state: passing, or
// nothing to pass.
func (c CI) ready() bool {
	return c == CINone || c == CIPassing
}

func (c CI) String() string {
	switch c {
	case CIPassing:
		return "passing"
	case CIPending:
		return "pending"
	case CIFailing:
		return "failing"
	default:
		return "none"
	}
}
