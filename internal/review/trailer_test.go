package review

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// fenced is a trailer block around one JSON text.
func fenced(json string) string {
	return "```autoreview\n" + json + "\n```"
}

// trailerOf parses a block that must parse.
func trailerOf(t *testing.T, json string) Trailer {
	t.Helper()
	trailer, ok := ParseTrailer(fenced(json))
	if !ok {
		t.Fatalf("ParseTrailer found no trailer in %s", json)
	}
	return trailer
}

func TestTheInstructionIsV1sByteForByte(t *testing.T) {
	// It travels as one argv token and one line in a call log. A newline
	// would split both.
	if strings.Contains(TrailerInstruction, "\n") {
		t.Error("the instruction is more than one line")
	}
	// The reviewers read these words. The hash is of v1's
	// TRAILER_INSTRUCTION in src/report.rs.
	sum := sha256.Sum256([]byte(TrailerInstruction))
	const want = "283372fdb8bd1f341ef548c710589264175e04db5500893ebc3a940b8218caeb"
	if got := hex.EncodeToString(sum[:]); got != want || len(TrailerInstruction) != 1399 {
		t.Errorf("the instruction is %d bytes with hash %s, want v1's 1399 bytes and %s", len(TrailerInstruction), got, want)
	}
}

func TestTheInstructionAsksForATrailerThatParses(t *testing.T) {
	// The example in the instruction is the shape the reviewers copy.
	_, example, _ := strings.Cut(TrailerInstruction, "shaped like ")
	example, _, _ = strings.Cut(example, ". decision is")
	for _, choices := range []string{"approved|commented|changes-requested|none", "LOW|MEDIUM|HIGH|CRITICAL"} {
		first, _, _ := strings.Cut(choices, "|")
		example = strings.Replace(example, choices, first, 1)
	}
	trailer := trailerOf(t, example)
	if trailer.Decision != "approved" || trailer.Risk != "LOW" || len(trailer.Panel) != 1 || len(trailer.Blockers) != 1 {
		t.Errorf("the example reads as %+v", trailer)
	}
}

func TestATrailerParsesFromAFencedBlock(t *testing.T) {
	answer := "The review is done.\n\n" + fenced(`{"decision":"commented","risk":"MEDIUM","findings":{"must_fix":0,"should_fix":1,"polish":2},"panel":[{"name":"codex","model":"gpt-5.5","ok":true,"findings":2,"top":"MEDIUM"}]}`)
	trailer, ok := ParseTrailer(answer)
	if !ok {
		t.Fatal("ParseTrailer found no trailer")
	}
	if trailer.Decision != "commented" || trailer.Risk != "MEDIUM" {
		t.Errorf("Decision, Risk = %q, %q", trailer.Decision, trailer.Risk)
	}
	f := trailer.Findings
	if f == nil || *f.MustFix != 0 || *f.ShouldFix != 1 || *f.Polish != 2 {
		t.Errorf("Findings = %+v, want 0, 1 and 2", f)
	}
	if len(trailer.Panel) != 1 {
		t.Fatalf("Panel has %d panelists, want 1", len(trailer.Panel))
	}
	p := trailer.Panel[0]
	if p.Name != "codex" || p.Model != "gpt-5.5" || p.OK == nil || !*p.OK || *p.Findings != 2 || p.Top != "MEDIUM" {
		t.Errorf("Panel[0] = %+v", p)
	}
	if len(trailer.Blockers) != 0 {
		t.Errorf("Blockers = %+v, want none", trailer.Blockers)
	}
}

func TestABlockerParsesWithBothFacts(t *testing.T) {
	trailer := trailerOf(t, `{"decision":"commented","blockers":[{"severity":"HIGH","domain":"money","reversible":false,"location":"src/pay.rs:88","gist":"a retried checkout charges twice"}]}`)
	b := trailer.Blockers[0]
	if b.Severity != "HIGH" || b.Domain != "money" || b.Location != "src/pay.rs:88" || b.Gist != "a retried checkout charges twice" {
		t.Errorf("Blockers[0] = %+v", b)
	}
	if b.Reversible == nil || *b.Reversible {
		t.Errorf("Reversible = %v, want a reported false", b.Reversible)
	}
}

func TestWhatAReviewerDidNotReportIsAbsentAndNotZero(t *testing.T) {
	// Every reviewer older than a field reports nothing for it, and the
	// instruction says to use null for anything unknown.
	cases := []struct {
		name string
		json string
	}{
		{"an old reviewer", `{"decision":"approved"}`},
		{"nulls for everything unknown", `{"decision":"approved","risk":null,"findings":null,"panel":null,"blockers":null}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			trailer := trailerOf(t, c.json)
			if trailer.Decision != "approved" || trailer.Risk != "" {
				t.Errorf("Decision, Risk = %q, %q", trailer.Decision, trailer.Risk)
			}
			if trailer.Findings != nil || len(trailer.Panel) != 0 || len(trailer.Blockers) != 0 {
				t.Errorf("trailer = %+v, want no findings, panel or blockers", trailer)
			}
		})
	}

	partial := trailerOf(t, `{"findings":{"must_fix":0,"should_fix":null},"panel":[{"name":"codex","ok":null}],"blockers":[{"gist":"x","reversible":null}]}`)
	if f := partial.Findings; f == nil || f.MustFix == nil || *f.MustFix != 0 || f.ShouldFix != nil || f.Polish != nil {
		t.Errorf("Findings = %+v, want a reported 0 and two counts that are absent", f)
	}
	if p := partial.Panel[0]; p.OK != nil || p.Findings != nil {
		t.Errorf("Panel[0] = %+v, want ok and findings absent", p)
	}
	if partial.Blockers[0].Reversible != nil {
		t.Error("a null reversible reads as a reported value")
	}
}

func TestTheLastBlockThatParsesWins(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		// A review that quotes an earlier block reports the newer state.
		{"two blocks", fenced(`{"decision":"none"}`) + "\ntext\n" + fenced(`{"decision":"approved"}`), "approved"},
		{"a mention of the tag after the block does not hide it", fenced(`{"decision":"approved"}`) + "\nsee the ```autoreview block above", "approved"},
		{"a last block that does not parse does not hide the one before", fenced(`{"decision":"commented"}`) + "\n\n" + fenced(`{"decision":`), "commented"},
		{"a last block with no closing fence does not hide the one before", fenced(`{"decision":"commented"}`) + "\n\n```autoreview\n{\"decision\":\"approved\"}", "commented"},
		{"an unknown field is ignored", fenced(`{"decision":"approved","surprise":42}`), "approved"},
		{"the block can sit on one line", "```autoreview {\"decision\":\"approved\"} ```", "approved"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			trailer, ok := ParseTrailer(c.text)
			if !ok || trailer.Decision != c.want {
				t.Errorf("ParseTrailer = %q, %v; want %q", trailer.Decision, ok, c.want)
			}
		})
	}
}

func TestABlockThatIsMissingOrMalformedIsNoTrailer(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"no block", "no block here"},
		{"nothing", ""},
		{"not JSON", fenced("not json")},
		{"a block that never closes", "```autoreview\n{\"decision\":"},
		{"an empty block", fenced("")},
		// The decoder takes a bare null for a struct and changes nothing,
		// which would read as a trailer that says nothing.
		{"a null", fenced("null")},
		{"a list", fenced(`["approved"]`)},
		{"a string", fenced(`"approved"`)},
		{"a field of the wrong type", fenced(`{"decision":42}`)},
		{"a count below zero", fenced(`{"findings":{"must_fix":-1}}`)},
		{"a count that is not whole", fenced(`{"findings":{"must_fix":1.5}}`)},
		{"two objects", fenced(`{"decision":"approved"} {"decision":"none"}`)},
		{"another fence tag", "```json\n{\"decision\":\"approved\"}\n```"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if trailer, ok := ParseTrailer(c.text); ok {
				t.Errorf("ParseTrailer read %+v", trailer)
			}
			if got := StripTrailer(c.text); got != c.text {
				t.Errorf("StripTrailer changed a text with no trailer: %q", got)
			}
		})
	}
}

func TestTrailerStringsCannotDrawOnTheScreen(t *testing.T) {
	trailer := trailerOf(t, `{"decision":"appr\u0007oved","risk":"L\u001b[31mOW","panel":[{"name":"co\u001b]0;pwned\u0007dex","model":"gpt\u001b[0m-5.5","top":"HI\u202eGH"}],"blockers":[{"severity":"HI\u0000GH","domain":"mo\u001b[31mney","location":"a.go\n:3","gist":"one\ttwo\u200b"}]}`)
	// The escape byte goes. What it would have driven stays as text.
	got := []string{
		trailer.Decision, trailer.Risk,
		trailer.Panel[0].Name, trailer.Panel[0].Model, trailer.Panel[0].Top,
		trailer.Blockers[0].Severity, trailer.Blockers[0].Domain, trailer.Blockers[0].Location, trailer.Blockers[0].Gist,
	}
	want := []string{
		"approved", "L[31mOW",
		"co]0;pwneddex", "gpt[0m-5.5", "HIGH",
		"HIGH", "mo[31mney", "a.go:3", "onetwo",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTrailerStringsAndListsAreCut(t *testing.T) {
	long := strings.Repeat("x", 500)
	// Two bytes for each character: a cut that counted bytes would stop at
	// half the room, or in the middle of a character.
	wide := strings.Repeat("é", 500)
	panel := make([]string, 40)
	for i := range panel {
		panel[i] = fmt.Sprintf(`{"name":"p%d","model":%q,"top":%q}`, i, long, wide)
	}
	blockers := make([]string, 40)
	for i := range blockers {
		blockers[i] = fmt.Sprintf(`{"severity":%q,"domain":%q,"location":%q,"gist":%q}`, long, long, long, wide)
	}
	trailer := trailerOf(t, fmt.Sprintf(`{"decision":%q,"risk":%q,"panel":[%s],"blockers":[%s]}`,
		long, long, strings.Join(panel, ","), strings.Join(blockers, ",")))

	if len(trailer.Panel) != 16 {
		t.Errorf("Panel has %d panelists, want the list cut at 16", len(trailer.Panel))
	}
	if len(trailer.Blockers) != 8 {
		t.Errorf("Blockers has %d, want the list cut at 8", len(trailer.Blockers))
	}
	if trailer.Panel[15].Name != "p15" {
		t.Errorf("the last panelist kept is %q, want the first 16 in order", trailer.Panel[15].Name)
	}
	p, b := trailer.Panel[0], trailer.Blockers[0]
	for name, value := range map[string]string{
		"decision": trailer.Decision, "risk": trailer.Risk,
		"model": p.Model, "top": p.Top,
		"severity": b.Severity, "domain": b.Domain, "location": b.Location,
	} {
		if n := utf8.RuneCountInString(value); n != 80 {
			t.Errorf("%s is %d characters, want 80", name, n)
		}
	}
	// A gist is a sentence, so it gets more room than a label.
	if n := utf8.RuneCountInString(b.Gist); n != 120 || !utf8.ValidString(b.Gist) {
		t.Errorf("gist is %d characters, valid = %v; want 120 whole ones", n, utf8.ValidString(b.Gist))
	}
}

func TestTheTrailerLeavesTheReviewWhereverItSits(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			// The review, the block, then the sign-off. A rule that the
			// block comes last left its raw JSON in the middle of the file.
			"between the review and a sign-off",
			"## Overview\n\nThe review.\n\n" + fenced(`{"decision":"none"}`) + "\n\nThe script exited cleanly.",
			"## Overview\n\nThe review.\n\nThe script exited cleanly.",
		},
		{
			// A resumed session holds one block for each pass.
			"one block for each pass",
			"First pass.\n\n" + fenced(`{"decision":"none"}`) + "\n\nSecond pass.\n\n" + fenced(`{"decision":"approved"}`),
			"First pass.\n\nSecond pass.",
		},
		{"at the end", "Looks fine.\n\n" + fenced(`{"decision":"none"}`), "Looks fine."},
		{"at the start", fenced(`{"decision":"none"}`) + "\n\nThe review.", "\n\nThe review."},
		{"alone", fenced(`{}`), ""},
		{
			// Only a block that parses is a trailer.
			"prose that names the fence keeps what is under it",
			"The reviewer emits a ```autoreview block.\n\nReal finding here.",
			"The reviewer emits a ```autoreview block.\n\nReal finding here.",
		},
		{
			"a block that does not parse stays, and the one that does goes",
			"A.\n\n" + fenced(`{"decision":"none"}`) + "\n\nB.\n\n" + fenced(`{"decision":`),
			"A.\n\nB.\n\n" + fenced(`{"decision":`),
		},
		{"no block", "just prose", "just prose"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StripTrailer(c.text); got != c.want {
				t.Errorf("StripTrailer = %q, want %q", got, c.want)
			}
		})
	}
}
