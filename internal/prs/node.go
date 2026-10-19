package prs

// One PR as the query returns it. Every field GitHub may leave out or answer
// with null is a plain value here, so an absent one reads as its zero value:
// an empty string is "GitHub did not say", never a name or a SHA.
type node struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	IsDraft        bool   `json:"isDraft"`
	UpdatedAt      string `json:"updatedAt"`
	ReviewDecision string `json:"reviewDecision"`
	// The SHA at the tip of the PR branch. This is what tells a push from a
	// comment: both make a PR actionable again, and only one is new code.
	HeadRefOid string `json:"headRefOid"`
	// The branch this PR merges into, and the branch this PR is. A PR whose
	// base is another open PR's branch is stacked on it by design.
	BaseRefName string `json:"baseRefName"`
	HeadRefName string `json:"headRefName"`
	// The head branch lives in a fork, so its name means nothing to another
	// PR's base. Absent reads as "same repo": the ordinary case, and the one
	// the branch names are comparable in.
	IsCrossRepository bool `json:"isCrossRepository"`
	// Mergeable is MERGEABLE, CONFLICTING or UNKNOWN. GitHub works it out
	// in the background, so UNKNOWN is common and is no claim either way.
	Mergeable string      `json:"mergeable"`
	Author    actor       `json:"author"`
	Comments  commentList `json:"comments"`
	Reviews   reviewList  `json:"reviews"`
	Commits   commitList  `json:"commits"`
	// The tip of the branch alone, for its checks.
	HeadCommit headCommitList `json:"headCommit"`
	// The GitHub stack the PR is a layer of, and its place in it. Both are
	// null for a PR in no stack, and absent on a server that has no stacks.
	Stack      *nativeStack `json:"stack"`
	StackEntry *nativeEntry `json:"stackEntry"`
}

type nativeStack struct {
	Number int `json:"number"`
	Size   int `json:"size"`
}

// Position 1 is the layer nearest the base branch.
type nativeEntry struct {
	Position int `json:"position"`
}

// A deleted account answers with a null author, which reads as an empty
// login.
type actor struct {
	Login string `json:"login"`
}

type commentList struct {
	Nodes []commentNode `json:"nodes"`
}

type commentNode struct {
	Author    actor  `json:"author"`
	UpdatedAt string `json:"updatedAt"`
}

type reviewList struct {
	Nodes []reviewNode `json:"nodes"`
}

type reviewNode struct {
	Author      actor  `json:"author"`
	SubmittedAt string `json:"submittedAt"`
}

type commitList struct {
	Nodes []commitNode `json:"nodes"`
}

type commitNode struct {
	Commit commit `json:"commit"`
}

// The oid is optional so that an answer without it reads as "no overlap
// anyone can prove" rather than failing the whole list.
type commit struct {
	Oid           string       `json:"oid"`
	CommittedDate string       `json:"committedDate"`
	Author        commitAuthor `json:"author"`
}

// A commit by an address no account claims has no user, which reads as an
// empty login.
type commitAuthor struct {
	User actor `json:"user"`
}

type headCommitList struct {
	Nodes []headCommitNode `json:"nodes"`
}

type headCommitNode struct {
	Commit headCommit `json:"commit"`
}

// The rollup is null when the commit has no checks and no statuses at all.
type headCommit struct {
	StatusCheckRollup rollup `json:"statusCheckRollup"`
}

type rollup struct {
	State string `json:"state"`
}

// headDate is when the newest commit was made, and empty with no commits.
// The list is oldest first, so the newest is the last.
func (n node) headDate() string {
	if len(n.Commits.Nodes) == 0 {
		return ""
	}
	return n.Commits.Nodes[len(n.Commits.Nodes)-1].Commit.CommittedDate
}

// othersCommitted says a person other than the author made one of the
// commits. A bot's commit has no account GitHub names, so it reads as
// nobody: only a named person counts, and another bot does not.
func (n node) othersCommitted() bool {
	for _, c := range n.Commits.Nodes {
		login := c.Commit.Author.User.Login
		if login != "" && login != n.Author.Login && !isBot(login) {
			return true
		}
	}
	return false
}

// rollupState is the raw state of the checks on the head commit. A missing
// node and a null rollup both read as empty: neither is a reason to hold a
// PR.
func (n node) rollupState() string {
	if len(n.HeadCommit.Nodes) == 0 {
		return ""
	}
	return n.HeadCommit.Nodes[0].Commit.StatusCheckRollup.State
}
