package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/venabots/autoreview/internal/doctor"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/sanitize"
)

// probeTimeout is how long one panelist has to say OK. A cold start of an
// agent CLI takes seconds; a minute and a half is a CLI that is stuck.
const probeTimeout = 90 * time.Second

// versionTimeout bounds one `--version`. A CLI that updates itself before
// it answers must not hold the report.
const versionTimeout = 10 * time.Second

// runDoctor is `autoreview doctor ...` with the real world handed in. The
// probe runs in a temporary directory, so no panelist reads the checkout
// it was started in.
func runDoctor(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	file, err := settings(os.Getenv)
	if err != nil {
		say(stderr, "error: %s\n", sanitize.Line(err.Error()))
		return doctor.ExitProblem
	}
	dir, err := os.MkdirTemp("", "autoreview-doctor.")
	if err != nil {
		say(stderr, "error: cannot make a temporary directory: %v\n", err)
		return doctor.ExitProblem
	}
	defer func() { _ = os.RemoveAll(dir) }()
	d := doctorDeps(ctx, file.Panel.Panelists, dir)
	d.Stdout, d.Stderr = stdout, stderr
	return doctor.Run(ctx, args, d)
}

// doctorDeps is the real world the doctor reads, with its probe's files in
// dir.
func doctorDeps(ctx context.Context, panelists []string, dir string) doctor.Deps {
	dashp, err := findDashp()
	if err != nil {
		dashp = "dash-p"
	}
	prober := doctor.Prober{Starter: proc.OS{}, Dashp: dashp, Dir: dir, Timeout: probeTimeout}
	return doctor.Deps{
		Env:       os.Getenv,
		Now:       time.Now(),
		Panelists: panelists,
		Has:       onPath,
		Probe:     prober.Probe,
		Version:   func(name string) (string, bool) { return versionOf(ctx, name, dashp) },
	}
}

// doctorHealth is what `autoreview doctor` would say now, for the view's
// footer. Each check gets a temporary directory of its own, as the
// subcommand does, so no panelist reads the checkout.
func doctorHealth(panelists []string) func(context.Context) engine.Health {
	return func(ctx context.Context) engine.Health {
		dir, err := os.MkdirTemp("", "autoreview-doctor.")
		if err != nil {
			return engine.Health{Problems: []string{"the doctor could not make a temporary directory"}}
		}
		defer func() { _ = os.RemoveAll(dir) }()
		s := doctor.Diagnose(ctx, doctorDeps(ctx, panelists, dir))
		return engine.Health{Seated: s.Seated, Answering: s.Answering, Problems: s.Problems, Blocking: s.Blocking}
	}
}

func onPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// versionOf is the first line a program prints for --version, and false
// when it is not installed. dash-p is the one the run would use, which
// $DASHP_BIN can move off PATH.
func versionOf(ctx context.Context, name, dashp string) (string, bool) {
	path := name
	if name == "dash-p" {
		path = dashp
	}
	if _, err := exec.LookPath(path); err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if err != nil && first == "" {
		return "installed, but --version failed: " + err.Error(), true
	}
	return first, true
}
