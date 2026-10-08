package session

import "fmt"

// Flag is how a review attaches to a session.
type Flag int

const (
	// FlagNone passes no session flag. Claude allocates its own id.
	FlagNone Flag = iota
	// FlagPin passes `--session-id <id>`: a fresh session under a known id.
	FlagPin
	// FlagResume passes `--resume <id>`: an earlier review, reopened.
	FlagResume
)

// Plan is the session one review runs in.
type Plan struct {
	// ID is the session the review is filed under, when that is known
	// before it starts. It is known for a pin and for a resume.
	ID   string
	Flag Flag
	// Note is a message for the person, or empty. The caller shows it, so
	// it cannot land in the middle of a progress line.
	Note string
}

// Resumes says the review reopens an earlier session. A resumed review
// re-checks; it does not review from scratch.
func (p Plan) Resumes() bool {
	return p.Flag == FlagResume
}

// Probe answers the two questions a plan depends on, so the rules are
// tested without a session store or a process table.
type Probe struct {
	// Exists says a transcript for the id is on disk.
	Exists func(id string) bool
	// InUse says a process holds the session open now.
	InUse func(id string) bool
}

// PlanFor decides how PR pr attaches to its derived session id.
//
//   - No transcript: pin the id, so the session can be found again.
//   - A transcript, and resume not asked for: no flag. Passing --session-id
//     for an id that exists is a hard error in claude, and resuming without
//     being asked would be a surprise.
//   - A transcript, and resume asked for: resume it, unless a process holds
//     it open. Then the review runs fresh, and a note says why.
func PlanFor(id string, pr int, resume bool, probe Probe) Plan {
	switch {
	case !probe.Exists(id):
		return Plan{ID: id, Flag: FlagPin}
	case !resume:
		return Plan{ID: id, Flag: FlagNone}
	case probe.InUse(id):
		return Plan{
			ID:   id,
			Flag: FlagNone,
			Note: fmt.Sprintf("PR #%d has a review session open in another tab or process; reviewing fresh", pr),
		}
	}
	return Plan{ID: id, Flag: FlagResume}
}
