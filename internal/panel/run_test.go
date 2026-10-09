package panel

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// testDeps is a run on fakes: every program installed, git answering from
// the table, panelists from the starter, and the ledger off.
func testDeps(t *testing.T, git fakeGit, starter *fakeStarter) (Deps, *syncBuffer, *syncBuffer) {
	t.Helper()
	out, errs := &syncBuffer{}, &syncBuffer{}
	return Deps{
		Env:      func(name string) string { return map[string]string{"AUTOREVIEW_LEDGER": "off"}[name] },
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		Git:      git.run,
		Starter:  starter,
		Stdout:   out,
		Stderr:   errs,
		Cwd:      "/repo",
		TempDir:  t.TempDir(),
		Version:  "1.2.3",
	}, out, errs
}

func uncommittedRepo() fakeGit {
	return fakeGit{
		"rev-parse --show-toplevel":               "/repo\n",
		"rev-parse --abbrev-ref HEAD":             "main\n",
		"diff HEAD":                               "diff --git a/a.go b/a.go\n+x\n",
		"ls-files --others --exclude-standard -z": "",
	}
}

func TestARunPrintsTheReviewsThenTheSynthesis(t *testing.T) {
	starter := newFakeStarter(func(id string, _ int) act {
		if id == "synthesis" {
			return act{stdout: "### Overview\n\nall good\n"}
		}
		return answer(id + "-model")
	})
	d, out, errs := testDeps(t, uncommittedRepo(), starter)

	code := Run(context.Background(), []string{"--panelist", "codex", "--panelist", "claude:opus"}, d)

	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, errs.String())
	}
	got := out.String()
	for _, want := range []string{
		"# Panel review\n\n- Target: uncommitted changes on main\n- Panelists: codex, claude-opus\n- Outputs: `",
		"## codex / codex-model (exit 0)", "## claude-opus / claude-opus-model (exit 0)",
		"# Synthesis\n\n### Overview\n\nall good\n\n---\n2 of 2 answered · outputs: `",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stdout lacks %q:\n%s", want, got)
		}
	}
	if !strings.HasPrefix(errs.String(), "reading the repo\nbuilding the diff\n") || !strings.Contains(errs.String(), "synthesizing with claude\n") {
		t.Errorf("stderr:\n%s", errs.String())
	}
	cmds := starter.commands()
	if len(cmds) != 3 || cmds[0].Path != "dash-p" || !strings.Contains(strings.Join(cmds[1].Args, " "), "--model=opus --perms=read-only") {
		t.Errorf("commands %+v", cmds)
	}
}

func TestDashpBinNamesTheDashpToRun(t *testing.T) {
	starter := newFakeStarter(func(string, int) act { return answer("m") })
	d, _, _ := testDeps(t, uncommittedRepo(), starter)
	d.Env = func(name string) string {
		return map[string]string{"AUTOREVIEW_LEDGER": "off", EnvDashp: "/opt/dash-p"}[name]
	}
	Run(context.Background(), []string{"--panelist", "codex", "--no-synthesis"}, d)
	if cmds := starter.commands(); len(cmds) != 1 || cmds[0].Path != "/opt/dash-p" {
		t.Errorf("commands %+v", cmds)
	}
}

func TestHelpAndVersionNeedNoRepository(t *testing.T) {
	d, out, _ := testDeps(t, fakeGit{}, newFakeStarter(nil))
	if code := Run(context.Background(), []string{"-h"}, d); code != ExitOK || out.String() != Help {
		t.Errorf("help: exit %d", code)
	}
	d, out, _ = testDeps(t, fakeGit{}, newFakeStarter(nil))
	if code := Run(context.Background(), []string{"-V"}, d); code != ExitOK || out.String() != "autoreview panel 1.2.3\n" {
		t.Errorf("version: exit %d, %q", code, out.String())
	}
}

func TestAnUnknownArgumentPrintsTheHelpAfterTheError(t *testing.T) {
	d, out, errs := testDeps(t, fakeGit{}, newFakeStarter(nil))
	code := Run(context.Background(), []string{"--bogus"}, d)
	if code != ExitFailed || out.String() != "" || errs.String() != "error: unknown arg: --bogus\n"+Help {
		t.Errorf("exit %d, stderr %q", code, errs.String())
	}
}

func TestOutsideACheckoutTheRunSaysSo(t *testing.T) {
	d, _, errs := testDeps(t, fakeGit{}, newFakeStarter(nil))
	code := Run(context.Background(), nil, d)
	if code != ExitFailed || !strings.Contains(errs.String(), "error: not inside a git checkout: ") {
		t.Errorf("exit %d, stderr %q", code, errs.String())
	}
}

func TestACtrlCWhileTheTargetIsReadExits130(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	git := fakeGit{"rev-parse --show-toplevel": "/repo\n"}
	d, _, errs := testDeps(t, git, newFakeStarter(nil))
	d.Git = func(c context.Context, dir string, args ...string) ([]byte, error) {
		if args[0] == "diff" {
			cancel()
			return nil, errors.New("signal: killed")
		}
		return git.run(c, dir, args...)
	}
	code := Run(ctx, []string{"--panelist", "codex"}, d)
	if code != ExitInterrupted || !strings.HasSuffix(errs.String(), "panel: interrupted\n") {
		t.Errorf("exit %d, stderr %q", code, errs.String())
	}
}

func TestACtrlCDuringThePanelSkipsTheSynthesisAndExits130(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	starter := newFakeStarter(func(string, int) act {
		cancel()
		return act{hang: true}
	})
	d, _, errs := testDeps(t, uncommittedRepo(), starter)
	code := Run(ctx, []string{"--panelist", "codex"}, d)
	if code != ExitInterrupted || !strings.HasSuffix(errs.String(), "panel: interrupted; the worktrees are being removed\n") {
		t.Errorf("exit %d, stderr %q", code, errs.String())
	}
	if len(starter.commands()) != 1 {
		t.Errorf("started %d processes after the signal", len(starter.commands()))
	}
}

func TestAFailedSynthesisFailsTheRunAfterThePanelIsPrinted(t *testing.T) {
	starter := newFakeStarter(func(id string, _ int) act {
		if id == "synthesis" {
			return act{code: 10, stderr: "usage limit\n"}
		}
		return answer("m")
	})
	d, out, errs := testDeps(t, uncommittedRepo(), starter)
	code := Run(context.Background(), []string{"--panelist", "codex"}, d)
	if code != ExitFailed || !strings.Contains(out.String(), "## codex / m (exit 0)") || !strings.Contains(errs.String(), "error: the synthesis failed (exit 10): usage limit. The panelist reports above") {
		t.Errorf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errs.String())
	}
}
