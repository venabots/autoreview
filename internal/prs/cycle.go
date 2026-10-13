package prs

import "slices"

// breakCircles frees one PR in every circle of holds. A relation that points
// in a circle would leave every PR in it waiting on another, and none of them
// reviewed. The lowest-numbered member is the one freed: something in the
// circle has to be reviewed, and the oldest PR is the one to review. Two PRs
// on the same tip are the way this happens in practice.
//
// The answer is a new slice. Each circle is looked for in the holds as they
// stand after the circles before it were broken, so one circle is never
// broken twice.
func breakCircles(branches []branch, on []int) []int {
	freed := slices.Clone(on)
	for start := range freed {
		circle := circleFrom(freed, start)
		if len(circle) == 0 {
			continue
		}
		freed[lowestNumbered(branches, circle)] = none
	}
	return freed
}

// circleFrom is the members of the circle `start` leads into, and empty when
// it leads into none.
func circleFrom(on []int, start int) []int {
	path := []int{}
	for at := start; at != none; at = on[at] {
		if first := slices.Index(path, at); first >= 0 {
			return slices.Clone(path[first:])
		}
		path = append(path, at)
	}
	return nil
}

func lowestNumbered(branches []branch, members []int) int {
	return slices.MinFunc(members, func(a, b int) int {
		return branches[a].number - branches[b].number
	})
}
