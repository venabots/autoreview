package panel

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/proc"
)

// act is what one fake process does: write its output and exit, hang until
// it is stopped, or fail to start at all.
type act struct {
	stdout, stderr string
	code           int
	hang           bool
	failStart      bool
}

// fakeStarter starts processes that do what plan says, by panelist id and
// attempt number, and keeps every command it was asked to start.
type fakeStarter struct {
	plan func(id string, attempt int) act

	mu       sync.Mutex
	attempts map[string]int
	started  []proc.Command
}

func newFakeStarter(plan func(id string, attempt int) act) *fakeStarter {
	return &fakeStarter{plan: plan, attempts: map[string]int{}}
}

func (f *fakeStarter) Start(c proc.Command) (proc.Proc, error) {
	id := strings.TrimSuffix(filepath.Base(c.Stdout), ".out")
	f.mu.Lock()
	f.attempts[id]++
	n := f.attempts[id]
	f.started = append(f.started, c)
	f.mu.Unlock()

	a := f.plan(id, n)
	if a.failStart {
		return nil, errors.New("fork/exec dash-p: no such file or directory")
	}
	if err := os.WriteFile(c.Stdout, []byte(a.stdout), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(c.Stderr, []byte(a.stderr), 0o600); err != nil {
		return nil, err
	}
	p := &fakeProc{exit: make(chan proc.Exit, 1), stopped: make(chan struct{})}
	if !a.hang {
		p.exit <- proc.Exit{Code: a.code}
	}
	return p, nil
}

func (f *fakeStarter) commands() []proc.Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]proc.Command(nil), f.started...)
}

type fakeProc struct {
	exit    chan proc.Exit
	once    sync.Once
	stopped chan struct{}
}

func (p *fakeProc) Wait() proc.Exit {
	select {
	case e := <-p.exit:
		return e
	case <-p.stopped:
		return proc.Exit{Signaled: true}
	}
}

func (p *fakeProc) Stop() { p.once.Do(func() { close(p.stopped) }) }

// answer is what a panelist that did its job prints.
func answer(model string) act {
	return act{stdout: "Model: " + model + "\nGoal (clear): x\n- [LOW] a.rs:1 — nit\n"}
}

// syncBuffer is a writer the run's goroutines may share.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// testRun is a fan-out over the fake starter, writing into buffers.
func testRun(t *testing.T, starter proc.Starter) (panelRun, *syncBuffer, *syncBuffer) {
	t.Helper()
	dir := t.TempDir()
	prompt := filepath.Join(dir, "review.prompt")
	if err := os.WriteFile(prompt, []byte("the prompt"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, log := &syncBuffer{}, &syncBuffer{}
	return panelRun{
		launch:     launch{starter: starter, dashp: "dash-p", outDir: dir, timeoutSecs: 600, grace: time.Second},
		isolated:   true,
		promptPath: prompt,
		status:     NewStatus(log, false),
		out:        out,
		log:        log,
	}, out, log
}

func panelOf(ids ...string) ([]Panelist, []string) {
	panel := make([]Panelist, len(ids))
	cwds := make([]string, len(ids))
	for i, id := range ids {
		panel[i] = Panelist{ID: id, Backend: id}
		cwds[i] = "/wt/" + id
	}
	return panel, cwds
}
