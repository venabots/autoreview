package panel

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// tickEvery is how often the wait for the panel redraws its count.
const tickEvery = 250 * time.Millisecond

// launch is what every dash-p start in a run shares.
type launch struct {
	starter     proc.Starter
	dashp       string
	outDir      string
	timeoutSecs int64
	// grace is how long past dash-p's own timeout the run waits before it
	// stops the group itself.
	grace time.Duration
}

// limit is how long one process may run before the run stops it.
func (l launch) limit() time.Duration {
	return time.Duration(l.timeoutSecs)*time.Second + l.grace
}

// files is where one process's stdout and stderr go.
func (l launch) files(id string) (stdout, stderr string) {
	return filepath.Join(l.outDir, id+".out"), filepath.Join(l.outDir, id+".err")
}

// attempt is one start of one panelist.
type attempt struct {
	idx      int
	panelist Panelist
	cwd      string
	// started is when this panelist's work began. It carries over to a
	// retry, so the time reported is what the person waited.
	started time.Time
	retried bool
}

// finished is an attempt that ended, and what it left.
type finished struct {
	attempt attempt
	outcome Outcome
	stopped string
}

// panelRun is one fan-out: the panel, where each one runs, and where its
// words go.
type panelRun struct {
	launch
	isolated   bool
	promptPath string
	status     *Status
	out, log   io.Writer
}

// start starts one attempt and watches it on a goroutine of its own, which
// reports on results when it ends.
func (r panelRun) start(ctx context.Context, a attempt, results chan<- finished) error {
	stdout, stderr := r.files(a.panelist.ID)
	p, err := r.starter.Start(proc.Command{
		Path:   r.dashp,
		Args:   DashpArgs(a.panelist, a.cwd, r.timeoutSecs, r.isolated),
		Stdin:  r.promptPath,
		Stdout: stdout,
		Stderr: stderr,
	})
	if err != nil {
		return err
	}
	go func() {
		e := supervise(ctx, p, r.limit())
		results <- finished{attempt: a, outcome: r.outcome(a, e), stopped: e.stopped}
	}()
	return nil
}

// outcome reads what an ended attempt left on disk.
func (r panelRun) outcome(a attempt, e ended) Outcome {
	stdoutPath, stderrPath := r.files(a.panelist.ID)
	stdout, stderr := readText(stdoutPath), readText(stderrPath)
	failure := e.stopped
	if failure == "" {
		failure = failureReason(!e.exit.Signaled, e.exit.Code, stdout, stderr)
	}
	model, ok := ExtractModel(stdout)
	if !ok {
		model = unknownModel
	}
	return Outcome{
		ID:      a.panelist.ID,
		Model:   model,
		Exit:    e.exit.Code,
		Exited:  !e.exit.Signaled,
		Stdout:  stdout,
		Failure: failure,
		Elapsed: time.Since(a.started),
		Retried: a.retried,
	}
}

// readText is a file's text, or "" for a file that cannot be read: a
// panelist that left no output file contributed nothing.
func readText(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(raw)
}

// report prints one outcome the moment it lands. Waiting for the slowest is
// how a person ends up staring at nothing for ten minutes while three
// reviews sit finished on disk.
func (r panelRun) report(o Outcome) {
	r.status.Suspend(func() { writeSection(r.out, r.log, o) })
}

// fanOut runs every panelist at once, prints each report as it lands,
// retries a transient failure once, and returns the outcomes in panel order,
// so two runs of the same panel compare.
//
// No pool: a panel is a few processes, decided by what is installed, and
// holding one back would only make the slowest later.
func (r panelRun) fanOut(ctx context.Context, panel []Panelist, cwds []string) []Outcome {
	results := make(chan finished)
	running := map[int]time.Time{}
	var done []Outcome
	for i, p := range panel {
		a := attempt{idx: i, panelist: p, cwd: cwds[i], started: time.Now()}
		if o, ok := r.startFirst(ctx, a, results); !ok {
			r.report(o)
			done = append(done, o)
			continue
		}
		running[i] = a.started
	}
	done = append(done, r.collect(ctx, results, running)...)
	return inPanelOrder(done, panel)
}

// startFirst starts a panelist's first attempt. When it cannot start, the
// outcome says why and ok is false. A signal that arrived while the
// worktrees were being made stops every start that follows.
func (r panelRun) startFirst(ctx context.Context, a attempt, results chan<- finished) (Outcome, bool) {
	id := a.panelist.ID
	if ctx.Err() != nil {
		r.status.Say("panel: interrupted before " + id + " started")
		return neverRan(a.panelist, "interrupted before it started", 0), false
	}
	if err := r.start(ctx, a, results); err != nil {
		// One panelist that cannot start costs one voice, not the run.
		why := "could not start: " + sanitize.Line(err.Error())
		r.status.Say("panel: " + id + " " + why)
		return neverRan(a.panelist, why, time.Since(a.started)), false
	}
	r.status.Say(fmt.Sprintf("panel: %s started (cwd=%s)", id, a.cwd))
	return Outcome{}, true
}

// collect waits for every running attempt. running maps each panelist's
// place in the panel to when its work began; collect owns it from here.
func (r panelRun) collect(ctx context.Context, results chan finished, running map[int]time.Time) []Outcome {
	var done []Outcome
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	interrupted := ctx.Done()
	for len(running) > 0 {
		select {
		case f := <-results:
			delete(running, f.attempt.idx)
			if r.retry(ctx, f, results) {
				running[f.attempt.idx] = f.attempt.started
				continue
			}
			r.report(f.outcome)
			done = append(done, f.outcome)
		case <-interrupted:
			// Each watcher stops its own panelist on the same signal. What
			// they wrote is still printed: a review stopped mid-run may hold
			// most of a report already paid for.
			r.status.Say("\npanel: interrupted; stopping " + words.Count(len(running), "panelist"))
			interrupted = nil
		case <-ticker.C:
			r.status.Tick(fmt.Sprintf("%s still reviewing, %s", words.Count(len(running), "panelist"), words.Duration(longestWait(running))))
		}
	}
	return done
}

// retry starts a second attempt when the first is worth one, and says so.
func (r panelRun) retry(ctx context.Context, f finished, results chan<- finished) bool {
	if f.stopped != "" || ctx.Err() != nil || !worthRetrying(f.outcome) {
		return false
	}
	id := f.attempt.panelist.ID
	r.status.Say(fmt.Sprintf("panel: %s produced nothing (%s); trying once more", id, sanitize.Line(f.outcome.Failure)))
	again := f.attempt
	again.retried = true
	if err := r.start(ctx, again, results); err != nil {
		r.status.Say("panel: " + id + " could not be retried: " + sanitize.Line(err.Error()))
		return false
	}
	return true
}

func longestWait(running map[int]time.Time) time.Duration {
	var longest time.Duration
	for _, started := range running {
		longest = max(longest, time.Since(started))
	}
	return longest
}

// inPanelOrder sorts outcomes into the order of the panel, as a new slice.
func inPanelOrder(outcomes []Outcome, panel []Panelist) []Outcome {
	place := map[string]int{}
	for i, p := range panel {
		place[p.ID] = i
	}
	sorted := slices.Clone(outcomes)
	slices.SortStableFunc(sorted, func(a, b Outcome) int { return place[a.ID] - place[b.ID] })
	return sorted
}
