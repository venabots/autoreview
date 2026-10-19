package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/venabots/autoreview/internal/panel"
	"github.com/venabots/autoreview/internal/proc"
)

// probePrompt is the smallest question that proves a model answers. The
// ledger only says a model failed. Asking it now gets the provider's own
// words: out of credits, a limit and when it resets, a flag the CLI no
// longer takes.
const probePrompt = "Reply with the single word OK.\n"

// exitTimeout is dash-p's status when its own timeout ended the run.
const exitTimeout = 20

// grace is how long past dash-p's timeout the probe waits before it stops
// the process itself.
const grace = 10 * time.Second

// Check is one panelist, asked now.
type Check struct {
	Spec    panel.Spec
	OK      bool
	Elapsed time.Duration
	// Detail is why the panelist did not answer, in its own words when it
	// left any. It is other people's text.
	Detail string
}

// Prober asks each panelist through dash-p, the way a review launches it.
// A probe that called the CLIs some other way would pass while the reviews
// fail.
type Prober struct {
	Starter proc.Starter
	Dashp   string
	// Dir holds the prompt and the output files, and is each panelist's
	// working directory.
	Dir     string
	Timeout time.Duration
}

// Probe asks every panelist at once and returns the checks in panel order.
func (p Prober) Probe(ctx context.Context, specs []panel.Spec) []Check {
	prompt := filepath.Join(p.Dir, "probe.prompt")
	if err := os.WriteFile(prompt, []byte(probePrompt), 0o600); err != nil {
		return failAll(specs, "could not write the prompt: "+err.Error())
	}
	seated := panel.ResolvePanel(specs)
	checks := make([]Check, len(seated))
	var wg sync.WaitGroup
	for i, s := range seated {
		wg.Go(func() { checks[i] = p.one(ctx, specs[i], s, prompt) })
	}
	wg.Wait()
	return checks
}

func failAll(specs []panel.Spec, why string) []Check {
	checks := make([]Check, len(specs))
	for i, s := range specs {
		checks[i] = Check{Spec: s, Detail: why}
	}
	return checks
}

func (p Prober) one(ctx context.Context, spec panel.Spec, seated panel.Panelist, prompt string) Check {
	stdout := filepath.Join(p.Dir, seated.ID+".out")
	stderr := filepath.Join(p.Dir, seated.ID+".err")
	started := time.Now()
	running, err := p.Starter.Start(proc.Command{
		Path:   p.Dashp,
		Args:   panel.DashpArgs(seated, p.Dir, int64(p.Timeout/time.Second), false),
		Stdin:  prompt,
		Stdout: stdout,
		Stderr: stderr,
	})
	if err != nil {
		return Check{Spec: spec, Detail: "could not start dash-p: " + err.Error()}
	}
	exit := wait(ctx, running, p.Timeout+grace)
	out, errText := readText(stdout), readText(stderr)
	why := failure(exit, out, errText)
	return Check{Spec: spec, OK: why == "", Elapsed: time.Since(started), Detail: why}
}

// wait is the exit of a process that may run no longer than limit. A probe
// that hung would hold the whole report until the person gave up on it.
func wait(ctx context.Context, running proc.Proc, limit time.Duration) proc.Exit {
	exited := make(chan proc.Exit, 1)
	go func() { exited <- running.Wait() }()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case e := <-exited:
		return e
	case <-timer.C:
	case <-ctx.Done():
	}
	running.Stop()
	<-exited
	return proc.Exit{Signaled: true}
}

func readText(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(raw)
}

// failure is why a probe did not answer, or "" when it did. dash-p puts a
// harness's refusal on stdout under an ERROR line, and a provider's error
// on stderr, so both are read.
func failure(exit proc.Exit, stdout, stderr string) string {
	switch {
	case exit.Signaled:
		return "did not answer in time and was stopped"
	case exit.Code == exitTimeout:
		return "timed out"
	case exit.Code == 0 && strings.TrimSpace(stdout) != "":
		return ""
	case exit.Code == 0:
		return "answered with nothing" + said(stderr, "")
	}
	return "exit " + strconv.Itoa(exit.Code) + said(stderr, stdout)
}

// said is ": " and the last line of the first text that has one.
func said(texts ...string) string {
	for _, text := range texts {
		if line := lastLine(text); line != "" {
			return ": " + line
		}
	}
	return ""
}

func lastLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
