// Package proc starts the processes a run supervises and stops them.
//
// Each one gets a process group of its own, so stopping a job is one signal
// to everything it started: no walking a process tree, and no child that
// was reparented while the tree was being read.
//
// The engine reaches processes through Starter, so a test hands in a fake
// and the engine's rules are tested without a single real process.
package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Command is one process to start.
type Command struct {
	Path string
	Args []string
	// Env is added to this process's own environment.
	Env []string
	Dir string
	// Stdout and Stderr are files the process writes to. Each is created,
	// or emptied when it exists.
	Stdout string
	Stderr string
}

// Exit is how a process ended.
type Exit struct {
	// Code is the exit status. It means nothing when Signaled is true.
	Code int
	// Signaled is true when a signal ended the process: it was killed from
	// outside and left no status to read.
	Signaled bool
}

// Proc is a running process and the group it leads.
type Proc interface {
	// Wait blocks until the process ends. Call it once.
	Wait() Exit
	// Stop ends the whole group. It returns at once and is safe to call
	// more than once, and after the process has ended.
	Stop()
}

// Starter starts processes.
type Starter interface {
	Start(Command) (Proc, error)
}

// exitNoStatus is the code for a process whose status could not be read.
// It is the shell's "command not found", which is as near as a code gets
// to "there was nothing to run".
const exitNoStatus = 127

// OS starts real processes.
type OS struct {
	// Grace is the wait between the signals of a stop. Zero means
	// DefaultGrace.
	Grace time.Duration
}

// DefaultGrace is how long a process gets to act on one signal before the
// next one is sent.
const DefaultGrace = 200 * time.Millisecond

// Start starts the command in a new process group, with no stdin. A
// reviewer must not read the terminal, and must not inherit a handle on it.
func (o OS) Start(c Command) (Proc, error) {
	stdout, err := create(c.Stdout)
	if err != nil {
		return nil, err
	}
	stderr, err := create(c.Stderr)
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}
	cmd := exec.Command(c.Path, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, fmt.Errorf("starting %s: %w", c.Path, err)
	}
	grace := o.Grace
	if grace == 0 {
		grace = DefaultGrace
	}
	return &osProc{cmd: cmd, pgid: cmd.Process.Pid, files: []*os.File{stdout, stderr}, grace: grace}, nil
}

func create(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
}

type osProc struct {
	cmd   *exec.Cmd
	pgid  int
	files []*os.File
	grace time.Duration

	mu sync.Mutex
	// reaped is true once Wait has collected the process. After that the
	// group id may belong to nobody, or to somebody else.
	reaped bool
	// stopping is true once a stop has begun.
	stopping bool
}

func (p *osProc) Wait() Exit {
	err := p.cmd.Wait()
	p.mu.Lock()
	p.reaped = true
	p.mu.Unlock()
	for _, f := range p.files {
		_ = f.Close()
	}
	return exitFrom(err)
}

// exitFrom reads how the process ended out of what Wait returned. A wait
// that failed outright still gives an exit: a job with no exit would hold
// its slot forever.
func exitFrom(err error) Exit {
	if err == nil {
		return Exit{}
	}
	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		return Exit{Code: exitNoStatus}
	}
	if status, ok := exited.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return Exit{Signaled: true}
	}
	return Exit{Code: exited.ExitCode()}
}

// Stop sends TERM to the group now, then TERM again, then KILL, one grace
// apart. The first signal is sent before Stop returns, so a caller that
// stops many jobs has signalled every one before it does anything else.
//
// The later signals are sent whatever the first one did. The process that
// leads the group can exit on TERM while a child that ignores TERM lives
// on, and that child is still a reviewer spending money.
//
// A stop that begins after the process was collected does nothing: its
// group id is no longer this job's to signal.
func (p *osProc) Stop() {
	p.mu.Lock()
	if p.reaped || p.stopping {
		p.mu.Unlock()
		return
	}
	p.stopping = true
	p.signal(syscall.SIGTERM)
	p.mu.Unlock()

	go func() {
		time.Sleep(p.grace)
		p.signal(syscall.SIGTERM)
		time.Sleep(p.grace)
		p.signal(syscall.SIGKILL)
	}()
}

// signal sends to the whole group. An error means the group is gone, which
// is what a stop wants.
func (p *osProc) signal(sig syscall.Signal) {
	_ = syscall.Kill(-p.pgid, sig)
}
