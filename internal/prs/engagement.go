package prs

// Engagement is where a PR stands with the person running the sweep. The
// order is the sweep's: the most actionable state has the lowest value.
type Engagement int

const (
	// New is a PR that person has never commented on, reviewed or pushed to.
	New Engagement = iota
	// Updated is a PR where somebody else acted after that person last did.
	Updated
	// Undecided is a PR where that person's last word was a review that
	// gave no verdict for a reason that can pass, such as a panelist that
	// did not answer. Read as Seen, it would wait for the author, who has
	// nothing to do, and nobody would review it again.
	Undecided
	// Seen is a PR where that person had the last word.
	Seen
)

func (e Engagement) String() string {
	switch e {
	case New:
		return "NEW"
	case Updated:
		return "UPDATED"
	case Undecided:
		return "UNDECIDED"
	default:
		return "SEEN"
	}
}

// One dated act on a PR, and the login behind it. An empty login is a deleted
// account or a commit no account claims.
type event struct {
	at  string
	who string
}

// engagement ranks a PR by the latest event from `me` against the latest from
// anyone else. The ISO timestamps are compared as strings, which is
// chronological for the one fixed shape GitHub emits.
//
// UPDATED needs the other event to be strictly later. A tie is SEEN: reading
// it as UPDATED would send the sweep back to a PR that `me` answered in the
// same second.
//
// An event with an empty login counts as someone else. A push from an address
// GitHub cannot match to an account is still a push, and a PR that changed
// must not stay SEEN because nobody could be named for the change.
func engagement(n node, me string) Engagement {
	all := events(n)
	mine := latest(all, func(e event) bool { return e.who == me })
	other := latest(all, func(e event) bool { return e.who != me })
	switch {
	case mine == "":
		return New
	case other > mine:
		return Updated
	case lastWordUndecided(n, me, mine):
		return Undecided
	default:
		return Seen
	}
}

// lastWordUndecided says the last word that `me` had, at the time mine, was
// a review that gave no verdict for a reason that can pass. When nobody
// acted after `me`, that review is the last one on the PR, so the query
// asks for the last review alone and not for a hundred bodies.
func lastWordUndecided(n node, me, mine string) bool {
	if len(n.LastReview.Nodes) == 0 {
		return false
	}
	r := n.LastReview.Nodes[0]
	return r.Author.Login == me && r.SubmittedAt == mine && undecided(r.Body)
}

// events is every comment, review and commit on the PR.
func events(n node) []event {
	found := make([]event, 0, len(n.Comments.Nodes)+len(n.Reviews.Nodes)+len(n.Commits.Nodes))
	for _, c := range n.Comments.Nodes {
		found = append(found, event{at: c.UpdatedAt, who: c.Author.Login})
	}
	for _, r := range n.Reviews.Nodes {
		found = append(found, event{at: r.SubmittedAt, who: r.Author.Login})
	}
	for _, c := range n.Commits.Nodes {
		found = append(found, event{at: c.Commit.CommittedDate, who: c.Commit.Author.User.Login})
	}
	return found
}

// latest is the newest timestamp among the events kept, and empty when none
// is kept. An event without a date never wins: it cannot be placed before or
// after anything, so it neither marks a PR as answered nor reopens it.
func latest(all []event, keep func(event) bool) string {
	newest := ""
	for _, e := range all {
		if keep(e) && e.at > newest {
			newest = e.at
		}
	}
	return newest
}
