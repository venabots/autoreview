package doctor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/panel"
	"github.com/venabots/autoreview/internal/proc"
)

// reply is what a fake dash-p leaves for one panelist.
type reply struct {
	stdout, stderr string
	exit           proc.Exit
	hang           bool
}

type fakeStarter struct {
	replies map[string]reply
	mu      sync.Mutex
	started []proc.Command
}

func (f *fakeStarter) Start(c proc.Command) (proc.Proc, error) {
	f.mu.Lock()
	f.started = append(f.started, c)
	f.mu.Unlock()
	r := f.replies[strings.TrimSuffix(filepath.Base(c.Stdout), ".out")]
	if err := os.WriteFile(c.Stdout, []byte(r.stdout), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(c.Stderr, []byte(r.stderr), 0o600); err != nil {
		return nil, err
	}
	return &fakeProc{reply: r, stopped: make(chan struct{})}, nil
}

type fakeProc struct {
	reply   reply
	once    sync.Once
	stopped chan struct{}
}

func (p *fakeProc) Wait() proc.Exit {
	if !p.reply.hang {
		return p.reply.exit
	}
	<-p.stopped
	return proc.Exit{Signaled: true}
}

func (p *fakeProc) Stop() { p.once.Do(func() { close(p.stopped) }) }

func probeWith(t *testing.T, replies map[string]reply, timeout time.Duration, specs ...panel.Spec) ([]Check, *fakeStarter) {
	t.Helper()
	starter := &fakeStarter{replies: replies}
	p := Prober{Starter: starter, Dashp: "dash-p", Dir: t.TempDir(), Timeout: timeout}
	return p.Probe(context.Background(), specs), starter
}

var (
	claude = panel.Spec{Backend: "claude", Model: "opus"}
	grok   = panel.Spec{Backend: "opencode", Model: "xai/grok-4.7"}
)

func TestAProbeReportsEachPanelistInPanelOrderWithItsOwnWords(t *testing.T) {
	checks, _ := probeWith(t, map[string]reply{
		"claude-opus":           {stdout: "OK\n"},
		"opencode-xai-grok-4.7": {stdout: "ERROR\n  Unrecognized flag: --dir in command opencode run\n", exit: proc.Exit{Code: 10}},
	}, time.Minute, claude, grok)
	if len(checks) != 2 || !checks[0].OK || checks[0].Spec != claude {
		t.Fatalf("checks = %+v, want claude ok first", checks)
	}
	want := "exit 10: Unrecognized flag: --dir in command opencode run"
	if checks[1].OK || checks[1].Detail != want {
		t.Errorf("grok = %+v, want down with %q", checks[1], want)
	}
}

func TestAProbeLaunchesThePanelistAsAReviewDoesAndReadOnly(t *testing.T) {
	_, starter := probeWith(t, map[string]reply{"claude-opus": {stdout: "OK"}}, time.Minute, claude)
	args := starter.started[0].Args
	for _, want := range []string{"--harness=claude", "--model=opus", "--output-format=text", "--timeout=60", "--perms=read-only"} {
		if !slices.Contains(args, want) {
			t.Errorf("args %v lack %s", args, want)
		}
	}
	prompt, err := os.ReadFile(starter.started[0].Stdin)
	if err != nil || string(prompt) != probePrompt {
		t.Errorf("stdin = %q, %v, want the probe prompt", prompt, err)
	}
}

func TestAProbeThatHangsIsStoppedAndSaysSo(t *testing.T) {
	checks, _ := probeWith(t, map[string]reply{"claude-opus": {hang: true}}, -grace+10*time.Millisecond, claude)
	if checks[0].OK || !strings.Contains(checks[0].Detail, "stopped") {
		t.Errorf("a hung probe = %+v, want down and stopped", checks[0])
	}
}

func TestFailureNamesTheReasonAPersonCanActOn(t *testing.T) {
	cases := []struct {
		name           string
		exit           proc.Exit
		stdout, stderr string
		want           string
	}{
		{"an answer", proc.Exit{}, "OK", "", ""},
		{"a provider error on stderr", proc.Exit{Code: 1}, "", "warn\nError: insufficient credits\n\n", "exit 1: Error: insufficient credits"},
		{"stderr wins over stdout", proc.Exit{Code: 1}, "ERROR\n  bad flag", "limit reached", "exit 1: limit reached"},
		{"dash-p's timeout", proc.Exit{Code: exitTimeout}, "", "", "timed out"},
		{"a silent success", proc.Exit{}, "  \n", "rate limited", "answered with nothing: rate limited"},
		{"a kill", proc.Exit{Signaled: true}, "", "", "did not answer in time and was stopped"},
		{"nothing said", proc.Exit{Code: 3}, "", "", "exit 3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := failure(c.exit, c.stdout, c.stderr); got != c.want {
				t.Errorf("failure = %q, want %q", got, c.want)
			}
		})
	}
}
