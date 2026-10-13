package prs

import "testing"

// Each row is one PR as GitHub answers it, so the JSON names of the three
// activity lists are under test as well as the ranking.
func TestEngagement(t *testing.T) {
	cases := []struct {
		name string
		json string
		want Engagement
	}{
		{
			name: "nothing has happened",
			json: `{"number":7,"comments":{"nodes":[]},"reviews":{"nodes":[]},"commits":{"nodes":[]}}`,
			want: New,
		},
		{
			name: "the activity lists are missing",
			json: `{"number":7}`,
			want: New,
		},
		{
			name: "only other people have acted",
			json: `{"number":7,
			  "comments":{"nodes":[{"author":{"login":"bob"},"updatedAt":"2026-08-08T10:00:00Z"}]},
			  "commits":{"nodes":[{"commit":{"committedDate":"2026-08-09T09:00:00Z","author":{"user":{"login":"carol"}}}}]}}`,
			want: New,
		},
		{
			// v1: updated_beats_seen. me commented, then carol pushed.
			name: "a push after my comment",
			json: `{"number":7,"title":"t","isDraft":false,
			  "updatedAt":"2026-08-09T10:00:00Z","reviewDecision":null,
			  "author":{"login":"carol"},
			  "comments":{"nodes":[{"author":{"login":"me"},"updatedAt":"2026-08-08T10:00:00Z"}]},
			  "reviews":{"nodes":[]},
			  "commits":{"nodes":[{"commit":{"committedDate":"2026-08-09T09:00:00Z","author":{"user":{"login":"carol"}}}}]}}`,
			want: Updated,
		},
		{
			name: "my comment after the push",
			json: `{"number":7,
			  "comments":{"nodes":[{"author":{"login":"me"},"updatedAt":"2026-08-09T10:00:00Z"}]},
			  "commits":{"nodes":[{"commit":{"committedDate":"2026-08-09T09:00:00Z","author":{"user":{"login":"carol"}}}}]}}`,
			want: Seen,
		},
		{
			name: "my review after the push",
			json: `{"number":7,
			  "reviews":{"nodes":[{"author":{"login":"me"},"submittedAt":"2026-08-09T10:00:00Z"}]},
			  "commits":{"nodes":[{"commit":{"committedDate":"2026-08-09T09:00:00Z","author":{"user":{"login":"carol"}}}}]}}`,
			want: Seen,
		},
		{
			name: "somebody else's review after mine",
			json: `{"number":7,
			  "reviews":{"nodes":[
			    {"author":{"login":"me"},"submittedAt":"2026-08-09T10:00:00Z"},
			    {"author":{"login":"bob"},"submittedAt":"2026-08-09T11:00:00Z"}]}}`,
			want: Updated,
		},
		{
			// UPDATED needs "other > mine" strictly.
			name: "a tie is mine",
			json: `{"number":7,
			  "comments":{"nodes":[{"author":{"login":"me"},"updatedAt":"2026-08-09T10:00:00Z"}]},
			  "commits":{"nodes":[{"commit":{"committedDate":"2026-08-09T10:00:00Z","author":{"user":{"login":"carol"}}}}]}}`,
			want: Seen,
		},
		{
			name: "one second later is not a tie",
			json: `{"number":7,
			  "comments":{"nodes":[{"author":{"login":"me"},"updatedAt":"2026-08-09T10:00:00Z"}]},
			  "commits":{"nodes":[{"commit":{"committedDate":"2026-08-09T10:00:01Z","author":{"user":{"login":"carol"}}}}]}}`,
			want: Updated,
		},
		{
			// The comparison is on the strings, and a later day with an
			// earlier hour is still later.
			name: "the date decides before the time",
			json: `{"number":7,
			  "comments":{"nodes":[{"author":{"login":"me"},"updatedAt":"2026-08-09T23:59:59Z"}]},
			  "commits":{"nodes":[{"commit":{"committedDate":"2026-08-10T00:00:00Z","author":{"user":{"login":"carol"}}}}]}}`,
			want: Updated,
		},
		{
			// An empty login counts as someone else.
			name: "a later commit no account claims",
			json: `{"number":7,
			  "comments":{"nodes":[{"author":{"login":"me"},"updatedAt":"2026-08-08T10:00:00Z"}]},
			  "commits":{"nodes":[{"commit":{"committedDate":"2026-08-09T09:00:00Z","author":{"user":null}}}]}}`,
			want: Updated,
		},
		{
			name: "a later comment from a deleted account",
			json: `{"number":7,
			  "comments":{"nodes":[
			    {"author":{"login":"me"},"updatedAt":"2026-08-08T10:00:00Z"},
			    {"author":null,"updatedAt":"2026-08-09T10:00:00Z"}]}}`,
			want: Updated,
		},
		{
			name: "only I have acted",
			json: `{"number":7,
			  "comments":{"nodes":[{"author":{"login":"me"},"updatedAt":"2026-08-08T10:00:00Z"}]}}`,
			want: Seen,
		},
		{
			// It cannot be placed after my comment, so it does not reopen
			// the PR.
			name: "an event with no date is left out",
			json: `{"number":7,
			  "comments":{"nodes":[
			    {"author":{"login":"me"},"updatedAt":"2026-08-08T10:00:00Z"},
			    {"author":{"login":"bob"},"updatedAt":null}]}}`,
			want: Seen,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := engagement(mustNode(t, c.json), "me"); got != c.want {
				t.Errorf("engagement = %s, want %s", got, c.want)
			}
		})
	}
}

func TestEngagementIsMeasuredAgainstTheLoginGiven(t *testing.T) {
	n := mustNode(t, `{"number":7,
	  "comments":{"nodes":[{"author":{"login":"me"},"updatedAt":"2026-08-08T10:00:00Z"}]}}`)
	if got := engagement(n, "someone-else"); got != New {
		t.Errorf("for another login the PR is %s, want %s", got, New)
	}
}
