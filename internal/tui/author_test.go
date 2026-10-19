package tui

import "testing"

// A run under --author shows fewer PRs than the repository has open. The
// Review tab names whose they are, so the short list does not read as
// every open PR.
func TestTheReviewTabNamesTheAuthorsItKeeps(t *testing.T) {
	m, hs := newModel(t, 200, 30)
	hs.eng.snap.Authors = []string{"alice", "bob"}
	m = send(m, snapshotMsg{snap: hs.eng.snap})
	wantOnScreen(t, m, " Review 4 · by @alice or @bob ")
}

func TestTheReviewTabNamesNoAuthorWhenItKeepsEveryone(t *testing.T) {
	m, hs := newModel(t, 200, 30)
	m = send(m, snapshotMsg{snap: hs.eng.snap})
	wantNotOnScreen(t, m, "by @")
}

func TestAFindForAPRThatIsNotListedNamesTheAuthorsItMayNotBeBy(t *testing.T) {
	m, hs := newModel(t, 200, 30)
	hs.eng.snap.Authors = []string{"alice"}
	m = send(m, snapshotMsg{snap: hs.eng.snap})
	m = typeText(t, press(t, m, "/"), "4242")
	wantOnScreen(t, m, "PR #4242 is not on either tab: it may be closed, a draft or a bot's, not by @alice, or older than the 50 newest open PRs")
}
