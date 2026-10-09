package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for the programs autoreview runs.
// A test links the binary into a directory under each name and puts that
// directory first on PATH. The code under test then starts a real process
// with a real argv, and no fake script has to be kept in step with it.
func TestMain(m *testing.M) {
	switch filepath.Base(os.Args[0]) {
	case "gh":
		os.Exit(fakeGh(os.Args[1:]))
	case "dash-p":
		os.Exit(fakeDashp(os.Args[1:]))
	case "claude", "codex":
		// Only looked for on PATH. A run that starts one has a bug.
		os.Exit(99)
	}
	os.Exit(m.Run())
}

// fakeGh answers the calls autoreview makes, from the files in
// $FAKE_GH_DIR. A call it does not know fails loudly: a new gh call needs
// an answer here before its test can pass.
func fakeGh(args []string) int {
	dir := os.Getenv("FAKE_GH_DIR")
	joined := strings.Join(args, " ")
	switch {
	case joined == "repo view --json owner,name":
		return serve(dir, "repo.json")
	case joined == "api user --jq .login":
		fmt.Println(os.Getenv("FAKE_GH_LOGIN"))
		return 0
	case strings.HasPrefix(joined, "api graphql ") && strings.Contains(joined, "search("):
		return serve(dir, "mine.json")
	case strings.HasPrefix(joined, "api graphql -F owner=acme -F name=widgets "):
		return serve(dir, "prs.json")
	case len(args) == 7 && joined == "pr view "+args[2]+" --repo acme/widgets --json latestReviews":
		fmt.Println(latestReviews(args[2]))
		return 0
	}
	fmt.Fprintf(os.Stderr, "fake gh: unhandled: %s\n", firstChars(joined, 80))
	return 1
}

// latestReviews is what GitHub says landed on a PR: the review named for it
// in $FAKE_GH_REVIEWS ("9:APPROVED 8:COMMENTED"), submitted now, or none.
func latestReviews(pr string) string {
	for _, entry := range strings.Fields(os.Getenv("FAKE_GH_REVIEWS")) {
		if number, state, ok := strings.Cut(entry, ":"); ok && number == pr {
			return fmt.Sprintf(`{"latestReviews":[{"author":{"login":%q},"state":%q,"submittedAt":%q}]}`,
				os.Getenv("FAKE_GH_LOGIN"), state, time.Now().UTC().Format(time.RFC3339))
		}
	}
	return `{"latestReviews":[]}`
}

func serve(dir, name string) int {
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake gh: %v\n", err)
		return 1
	}
	_, _ = os.Stdout.Write(raw)
	return 0
}

func firstChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

var firstNumber = regexp.MustCompile(`[0-9]+`)

// fakeDashp is a review that takes no time. It logs its argv on one line,
// writes the meta file dash-p writes, and prints dash-p's envelope.
//
//   - $FAKE_DASHP_FAIL lists the PRs whose review fails with exit 10.
//   - $FAKE_DASHP_DECISION is the decision in the trailer (commented).
//   - $FAKE_DASHP_NO_TRAILER lists the PRs whose review has no trailer.
//   - $FAKE_DASHP_SLEEP is how many seconds the review takes. The sandbox
//     sets it, so a person can watch a review run.
//   - $FAKE_DASHP_HANG names a file. The review writes its pid there and
//     then hangs for a minute.
func fakeDashp(args []string) int {
	prompt := args[len(args)-1]
	pr := firstNumber.FindString(prompt)
	if log := os.Getenv("FAKE_DASHP_LOG"); log != "" {
		f, err := os.OpenFile(log, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		if err == nil {
			_, _ = fmt.Fprintln(f, strings.ReplaceAll(strings.Join(args, " "), "\n", "\\n"))
			_ = f.Close()
		}
	}
	session := "00000000-0000-5000-a000-" + fmt.Sprintf("%012s", pr)
	for i, arg := range args {
		if (arg == "--session-id" || arg == "--resume") && i+1 < len(args) {
			session = args[i+1]
		}
	}
	if pidFile := os.Getenv("FAKE_DASHP_HANG"); pidFile != "" {
		// A review that never ends by itself. It says who it is, so a test
		// can see that a stop reached it.
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
		time.Sleep(time.Minute)
	}
	if seconds, err := strconv.Atoi(os.Getenv("FAKE_DASHP_SLEEP")); err == nil {
		time.Sleep(time.Duration(seconds) * time.Second)
	}
	failed := slices.Contains(strings.Fields(os.Getenv("FAKE_DASHP_FAIL")), pr)
	for i, arg := range args {
		if arg == "--meta-file" && i+1 < len(args) {
			meta := fmt.Sprintf(`{"session_id":%q,"total_cost_usd":0.42,"model_resolved":"claude-fable-5"}`, session)
			_ = os.WriteFile(args[i+1], []byte(meta), 0o600)
		}
	}
	if failed {
		printAnswer("You've hit your session limit · resets 12pm")
		return 10
	}
	said := "Reviewed PR " + pr + ".\n\n### should-fix\n\n- [MEDIUM] src/pay.rs:88 — a retried checkout charges the card twice\n  Flagged by: codex (gpt-5.5)"
	if !slices.Contains(strings.Fields(os.Getenv("FAKE_DASHP_NO_TRAILER")), pr) {
		said += "\n\n```autoreview\n" + trailer() + "\n```"
	}
	printAnswer(said)
	return 0
}

func trailer() string {
	decision := os.Getenv("FAKE_DASHP_DECISION")
	if decision == "" {
		decision = "commented"
	}
	return fmt.Sprintf(`{"decision":%q,"risk":"MEDIUM","findings":{"must_fix":0,"should_fix":1,"polish":0},`+
		`"panel":[{"name":"codex","model":"gpt-5.5","ok":true,"findings":1,"top":"MEDIUM"},{"name":"claude","model":"claude-opus-5.5","ok":true,"findings":0,"top":null}],`+
		`"blockers":[{"severity":"MEDIUM","domain":"money","reversible":false,"location":"src/pay.rs:88","gist":"a retried checkout charges the card twice"}]}`, decision)
}

func printAnswer(said string) {
	envelope, _ := json.Marshal(map[string]string{"answer": said})
	fmt.Println(string(envelope))
}

// dashpCalls is the argv of each review the run started, one line each.
func dashpCalls(t *testing.T, fixtures string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtures, "dash-p.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

var (
	sessionShape = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	elapsed      = regexp.MustCompile(`\b[0-9]+s\b`)
)

// steady replaces what differs from run to run: the directory the sandbox
// made, each session id, and how long a review took.
func steady(text, fixtures string) string {
	text = strings.ReplaceAll(text, fixtures, "<fixtures>")
	text = regexp.MustCompile(`<fixtures>/logs/run-[0-9]+`).ReplaceAllString(text, "<run>")
	text = sessionShape.ReplaceAllString(text, "<session>")
	return elapsed.ReplaceAllString(text, "Ns")
}
