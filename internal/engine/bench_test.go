package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/repo"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/rundir"
	"github.com/venabots/autoreview/internal/session"
)

const sessionOf9 = "0b1ce3c5-8f5f-5a0e-a3d4-1c2b3a4d5e6f"

// reviewer is a fake dash-p. Each started review blocks until the test
// ends it, and leaves the files a real one would.
type reviewer struct {
	mu       sync.Mutex
	commands []proc.Command
	procs    map[int]*fakeProc
	// refuse names the PRs whose process does not start.
	refuse []int
	// onStop is what a review exits with when it is stopped.
	onStop proc.Exit
}

type fakeProc struct {
	done   chan proc.Exit
	stops  atomic.Int32
	onStop proc.Exit
}

func (p *fakeProc) Wait() proc.Exit { return <-p.done }

func (p *fakeProc) Stop() {
	if p.stops.Add(1) == 1 {
		p.done <- p.onStop
	}
}

func prOf(c proc.Command) int {
	name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(c.Stdout), "pr-"), ".json")
	n, _ := strconv.Atoi(name)
	return n
}

func (r *reviewer) Start(c proc.Command) (proc.Proc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pr := prOf(c)
	r.commands = append(r.commands, c)
	if slices.Contains(r.refuse, pr) {
		return nil, errors.New("starting dash-p: executable file not found\x1b[2J")
	}
	p := &fakeProc{done: make(chan proc.Exit, 1), onStop: r.onStop}
	if r.procs == nil {
		r.procs = map[int]*fakeProc{}
	}
	r.procs[pr] = p
	return p, nil
}

func (r *reviewer) command(t *testing.T, pr int) proc.Command {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.commands {
		if prOf(c) == pr {
			return c
		}
	}
	t.Fatalf("no review of PR #%d was started", pr)
	return proc.Command{}
}

func (r *reviewer) started() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int, len(r.commands))
	for i, c := range r.commands {
		out[i] = prOf(c)
	}
	return out
}

// answer is what a review says, with a trailer when decision is not empty.
func answer(pr int, decision string) string {
	text := fmt.Sprintf("Reviewed PR %d.", pr)
	if decision == "" {
		return text
	}
	trailer := fmt.Sprintf(`{"decision":%q,"risk":"LOW","findings":{"must_fix":0,"should_fix":0,"polish":1},"panel":[{"name":"codex","model":"gpt-5.5","ok":true,"findings":1,"top":"LOW"},{"name":"claude","model":"opus","ok":true,"findings":0,"top":null}],"blockers":[]}`, decision)
	return text + "\n\n```autoreview\n" + trailer + "\n```"
}

// finish ends a review the way dash-p does: the meta file, the envelope on
// stdout, then the exit status.
func (r *reviewer) finish(t *testing.T, pr int, code int, said string) {
	t.Helper()
	c := r.command(t, pr)
	meta := strings.TrimSuffix(c.Stdout, ".json") + ".meta.json"
	write(t, meta, fmt.Sprintf(`{"session_id":%q,"total_cost_usd":0.42,"model_resolved":"claude-fable-5"}`, sessionOf9))
	envelope, err := json.Marshal(map[string]string{"answer": said})
	if err != nil {
		t.Fatal(err)
	}
	write(t, c.Stdout, string(envelope))
	r.mu.Lock()
	p := r.procs[pr]
	r.mu.Unlock()
	p.done <- proc.Exit{Code: code}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// bench is one reviewing run and everything it talks to.
type bench struct {
	eng      *Engine
	reviewer *reviewer
	results  chan Result
	run      rundir.Run

	mu       sync.Mutex
	events   []Event
	recorded []Finished
	asked    []int
}

type setup struct {
	prs      []int
	jobs     int
	edit     func(*Config)
	verdict  func(pr int) review.Readback
	sessions session.Probe
	record   func(Finished) error
	reviewer *reviewer
	// marks is the reaction, or nil.
	marks Marks
	// deps changes the outside world further, for a test that needs it.
	deps func(*Deps)
	// list replaces the fixed list of prs, for a test whose PRs change
	// between looks.
	list func() (prs.Listing, error)
}

func newBench(t *testing.T, s setup) *bench {
	t.Helper()
	run, err := rundir.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	b := &bench{reviewer: s.reviewer, results: make(chan Result, 1), run: run}
	if b.reviewer == nil {
		b.reviewer = &reviewer{}
	}
	cfg := Config{
		Repo:         repo.Context{Owner: "acme", Name: "widgets", Root: "/sandbox/repo", Me: "me"},
		Gates:        bothGates,
		Review:       true,
		Jobs:         max(s.jobs, 1),
		Timeout:      time.Hour,
		Orchestrator: review.Claude(),
		Dashp:        "dash-p",
	}
	if s.edit != nil {
		s.edit(&cfg)
	}
	if s.sessions.Exists == nil {
		s.sessions = session.Probe{Exists: func(string) bool { return false }, InUse: func(string) bool { return false }}
	}
	deps := Deps{
		List: func(context.Context) (prs.Listing, error) {
			if s.list != nil {
				return s.list()
			}
			return listing(s.prs...), nil
		},
		Mine:     func(context.Context) ([]mine.PR, error) { return nil, nil },
		Procs:    b.reviewer,
		Marks:    s.marks,
		Sessions: s.sessions,
		Run:      run,
		Transcript: func(string) (string, bool) {
			return "", false
		},
		Verdict: func(_ context.Context, pr int, _ time.Time) review.Readback {
			b.mu.Lock()
			b.asked = append(b.asked, pr)
			b.mu.Unlock()
			if s.verdict == nil {
				return review.Readback{Kind: review.ReadbackLanded, Word: "approved"}
			}
			return s.verdict(pr)
		},
		Record: func(f Finished) error {
			b.mu.Lock()
			b.recorded = append(b.recorded, f)
			b.mu.Unlock()
			if s.record != nil {
				return s.record(f)
			}
			return nil
		},
		Journal: func(ev Event) {
			b.mu.Lock()
			b.events = append(b.events, ev)
			b.mu.Unlock()
		},
	}
	if s.deps != nil {
		s.deps(&deps)
	}
	b.eng = New(cfg, deps)
	go func() { b.results <- b.eng.Run() }()
	synctest.Wait()
	return b
}

func (b *bench) job(t *testing.T, pr int) Job {
	t.Helper()
	for _, j := range b.eng.Snapshot().Jobs {
		if j.PR == pr {
			return j
		}
	}
	t.Fatalf("the snapshot has no job for PR #%d", pr)
	return Job{}
}

// journal is the events so far, each as one word, with the PR where there
// is one.
func (b *bench) journal() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.events))
	for i, ev := range b.events {
		switch ev := ev.(type) {
		case Looked:
			out[i] = "looked"
		case NothingToReview:
			out[i] = "nothing"
		case PassStarted:
			out[i] = fmt.Sprintf("pass %d: %d at %d", ev.Pass, ev.Reviews, ev.Limit)
		case JobStarted:
			out[i] = fmt.Sprintf("start %d", ev.Job.PR)
		case JobFinished:
			out[i] = fmt.Sprintf("finish %d", ev.Job.PR)
		case Noted:
			out[i] = "note: " + ev.Text
		case Interrupted:
			out[i] = "interrupted"
		case JobRetrying:
			out[i] = fmt.Sprintf("retry %d", ev.Job.PR)
		}
	}
	return out
}

func (b *bench) notes() []string {
	notes := []string{}
	for _, line := range b.journal() {
		if text, ok := strings.CutPrefix(line, "note: "); ok {
			notes = append(notes, text)
		}
	}
	return notes
}

// commandsFor is the argv of every review of pr that was started, in order.
func (r *reviewer) commandsFor(pr int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []string{}
	for _, c := range r.commands {
		if prOf(c) == pr {
			out = append(out, strings.Join(c.Args, " "))
		}
	}
	return out
}
