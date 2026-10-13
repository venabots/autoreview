package review

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

// ghAnswer is a gh that gives one answer and keeps the argv of each call.
type ghAnswer struct {
	calls [][]string
	out   string
	err   error
}

func (g *ghAnswer) run(_ context.Context, args ...string) ([]byte, error) {
	g.calls = append(g.calls, slices.Clone(args))
	return []byte(g.out), g.err
}

// oneReview is the answer of gh with one review in the list.
func oneReview(login, state, submittedAt string) string {
	return fmt.Sprintf(`{"latestReviews":[{"author":{"login":%q},"state":%q,"submittedAt":%q}]}`, login, state, submittedAt)
}

func started() time.Time {
	return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
}

func TestVerdictAsksGhForTheLatestReviewsOfOnePR(t *testing.T) {
	gh := &ghAnswer{out: `{"latestReviews":[]}`}
	Verdict(context.Background(), gh.run, "acme/widgets", 213, "me", started())

	// The repo is named, so the answer does not depend on the directory
	// the run was started in.
	want := []string{"pr", "view", "213", "--repo", "acme/widgets", "--json", "latestReviews"}
	if len(gh.calls) != 1 || !slices.Equal(gh.calls[0], want) {
		t.Errorf("gh was called with %q, want one call of %q", gh.calls, want)
	}
}

func TestTheReadbackWantsMyNewReviewOnly(t *testing.T) {
	landed := func(word string) Readback { return Readback{Kind: ReadbackLanded, Word: word} }
	nothing := Readback{Kind: ReadbackNothing}
	cases := []struct {
		name string
		out  string
		want Readback
	}{
		// A review I submitted after the job started is this run's verdict.
		{"my approval", oneReview("me", "APPROVED", "2026-08-18T12:05:00Z"), landed("approved")},
		{"my request for changes", oneReview("me", "CHANGES_REQUESTED", "2026-08-18T12:05:00Z"), landed("changes requested")},
		{"my comment review", oneReview("me", "COMMENTED", "2026-08-18T12:05:00Z"), landed("commented")},
		// A review from before the run is some earlier engagement.
		{"a review from an hour before", oneReview("me", "APPROVED", "2026-08-18T11:00:00Z"), nothing},
		// The grace is for clock skew. It does not adopt the review of the
		// run just before this one.
		{"a review from half a minute before", oneReview("me", "APPROVED", "2026-08-18T11:59:30Z"), nothing},
		{"a review eleven seconds before", oneReview("me", "APPROVED", "2026-08-18T11:59:49Z"), nothing},
		// A small clock skew must not hide a review at the boundary.
		{"a review five seconds before", oneReview("me", "COMMENTED", "2026-08-18T11:59:55Z"), landed("commented")},
		{"a review exactly ten seconds before", oneReview("me", "APPROVED", "2026-08-18T11:59:50Z"), landed("approved")},
		{"a time with milliseconds", oneReview("me", "APPROVED", "2026-08-18T12:05:00.250Z"), landed("approved")},
		// Somebody else's approval is not my verdict.
		{"somebody else's approval", oneReview("alice", "APPROVED", "2026-08-18T12:05:00Z"), nothing},
		{"a login that differs in case", oneReview("Me", "APPROVED", "2026-08-18T12:05:00Z"), nothing},
		{"a review with no time", oneReview("me", "APPROVED", ""), nothing},
		{"a state that is no verdict", oneReview("me", "DISMISSED", "2026-08-18T12:05:00Z"), nothing},
		{"no reviews", `{"latestReviews":[]}`, nothing},
		{
			"mine among others",
			`{"latestReviews":[
				{"author":{"login":"alice"},"state":"CHANGES_REQUESTED","submittedAt":"2026-08-18T12:06:00Z"},
				{"author":null,"state":"APPROVED","submittedAt":"2026-08-18T12:06:00Z"},
				{"author":{"login":"me"},"state":"APPROVED","submittedAt":"2026-08-18T12:05:00Z"}]}`,
			landed("approved"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gh := &ghAnswer{out: c.out}
			if got := Verdict(context.Background(), gh.run, "acme/widgets", 9, "me", started()); got != c.want {
				t.Errorf("Verdict = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestTheStartOfTheJobIsComparedByTheSecond(t *testing.T) {
	// v1 knew the start as a whole second. A start of 12:00:00.900 is the
	// second 12:00:00, so a review at 11:59:50 is inside the grace.
	late := started().Add(900 * time.Millisecond)
	gh := &ghAnswer{out: oneReview("me", "APPROVED", "2026-08-18T11:59:50Z")}
	if got := Verdict(context.Background(), gh.run, "acme/widgets", 9, "me", late); got.Kind != ReadbackLanded {
		t.Errorf("Verdict = %+v, want the review to count", got)
	}
}

func TestAReviewWithNoAuthorIsNobodys(t *testing.T) {
	// A deleted account has no login. With an empty login of my own, every
	// such review would read as mine.
	gh := &ghAnswer{out: `{"latestReviews":[{"author":null,"state":"APPROVED","submittedAt":"2026-08-18T12:05:00Z"}]}`}
	if got := Verdict(context.Background(), gh.run, "acme/widgets", 9, "", started()); got.Kind != ReadbackNothing {
		t.Errorf("Verdict = %+v, want nothing landed", got)
	}
}

func TestACallThatFailedAndNothingLandedStayDistinct(t *testing.T) {
	cases := []struct {
		name string
		out  string
		err  error
		want ReadbackKind
	}{
		// GitHub answered, and nothing is there. That contradicts an agent
		// that says its review landed.
		{"an empty list is an answer", `{"latestReviews":[]}`, nil, ReadbackNothing},
		{"only somebody else's review is an answer", oneReview("alice", "APPROVED", "2026-08-18T12:05:00Z"), nil, ReadbackNothing},
		// Nothing was learned, so nothing is contradicted.
		{"gh failed", ``, errors.New("gh pr view timed out"), ReadbackFailed},
		{"gh failed after it printed a list", `{"latestReviews":[]}`, errors.New("gh pr view: exit status 1"), ReadbackFailed},
		{"not JSON", `HTTP 502`, nil, ReadbackFailed},
		{"no output", ``, nil, ReadbackFailed},
		{"an answer with no list", `{}`, nil, ReadbackFailed},
		{"a list that is null", `{"latestReviews":null}`, nil, ReadbackFailed},
		{"a list that is not a list", `{"latestReviews":{"nodes":[]}}`, nil, ReadbackFailed},
		{"a null answer", `null`, nil, ReadbackFailed},
		{"a review of the wrong shape", `{"latestReviews":[{"author":"me","state":"APPROVED"}]}`, nil, ReadbackFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gh := &ghAnswer{out: c.out, err: c.err}
			got := Verdict(context.Background(), gh.run, "acme/widgets", 9, "me", started())
			if got.Kind != c.want || got.Word != "" {
				t.Errorf("Verdict = %+v, want kind %d and no word", got, c.want)
			}
		})
	}
	if (Readback{}).Kind != ReadbackFailed {
		t.Error("a readback nobody made reads as an answer from GitHub")
	}
}

func claiming(decision string) *Trailer {
	return &Trailer{Decision: decision}
}

func TestTheVerdictPrefersGitHubToTheAgent(t *testing.T) {
	failed := Readback{Kind: ReadbackFailed}
	nothing := Readback{Kind: ReadbackNothing}
	cases := []struct {
		name     string
		readback Readback
		trailer  *Trailer
		want     string
	}{
		{"GitHub outranks the trailer", Readback{Kind: ReadbackLanded, Word: "approved"}, claiming("commented"), "approved"},
		{"GitHub needs no trailer", Readback{Kind: ReadbackLanded, Word: "changes requested"}, nil, "changes requested"},
		// A failed call contradicts nothing, so the trailer is the fallback.
		{"a failed call leaves the agent's approval", failed, claiming("approved"), "approved"},
		{"a failed call leaves the agent's comment", failed, claiming("commented"), "commented"},
		{"the trailer's hyphen becomes the readback's words", failed, claiming("changes-requested"), "changes requested"},
		{"a decision of none is no verdict", failed, claiming("none"), ""},
		{"an unknown decision is no verdict", failed, claiming("shipped"), ""},
		{"a trailer with no decision is no verdict", failed, &Trailer{Risk: "LOW"}, ""},
		{"no trailer is no verdict", failed, nil, ""},
		// GitHub answered and nothing landed. The agent's "approved" is the
		// belief in itself that the verdict exists to catch.
		{"an empty readback vetoes an approval", nothing, claiming("approved"), ""},
		{"an empty readback vetoes a request for changes", nothing, claiming("changes-requested"), ""},
		// A plain comment never appears in latestReviews.
		{"commented survives the veto", nothing, claiming("commented"), "commented"},
		{"an empty readback and no trailer", nothing, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ResolveVerdict(c.readback, c.trailer)
			if got != c.want || ok != (c.want != "") {
				t.Errorf("ResolveVerdict = %q, %v; want %q", got, ok, c.want)
			}
		})
	}
}

func TestAVetoedClaimIsNamedForTheNote(t *testing.T) {
	nothing := Readback{Kind: ReadbackNothing}
	cases := []struct {
		name     string
		readback Readback
		trailer  *Trailer
		want     string
	}{
		{"a false approval", nothing, claiming("approved"), "approved"},
		{"a false request for changes", nothing, claiming("changes-requested"), "changes-requested"},
		// A comment claim is never vetoed.
		{"a comment", nothing, claiming("commented"), ""},
		{"a decision of none", nothing, claiming("none"), ""},
		{"no trailer", nothing, nil, ""},
		// A failed readback contradicts nothing.
		{"a failed readback", Readback{Kind: ReadbackFailed}, claiming("approved"), ""},
		{"a review that landed", Readback{Kind: ReadbackLanded, Word: "approved"}, claiming("approved"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := VetoedClaim(c.readback, c.trailer)
			if got != c.want || ok != (c.want != "") {
				t.Errorf("VetoedClaim = %q, %v; want %q", got, ok, c.want)
			}
		})
	}
}
