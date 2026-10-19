package main

import (
	"strings"
	"testing"
)

func TestDoctorAsksEachPanelistThroughDashpAndNamesTheOneThatIsDown(t *testing.T) {
	b := panelSandbox(t)
	t.Setenv("FAKE_PANEL_ALWAYS_EMPTY", "opencode")
	t.Setenv("PANEL_REVIEW_PANELISTS", "codex opencode:xai/grok-4.7")
	stdout, stderr, code := autoreview(t, "doctor")

	if code != 1 || stderr != "" {
		t.Errorf("exit %d, stderr %q, want 1 and nothing", code, stderr)
	}
	contains(t, "stdout", stdout,
		"Panel, asked now (from $PANEL_REVIEW_PANELISTS)\n",
		"  ok    codex                  answered in ",
		"  DOWN  opencode:xai/grok-4.7  exit 1: quota exceeded\n",
		"Last 1d in the ledger\n  0 reviews, $0.00\n",
		"  dash-p    dash-p fake\n",
		"  gh        not installed\n",
		"  - opencode:xai/grok-4.7 does not answer\n",
		"  - gh is not installed\n")
	// The probe is read-only and runs outside the person's checkout.
	calls := b.calls()
	contains(t, "the dash-p calls", calls, "--harness=opencode --output-format=text --timeout=90", "--model=xai/grok-4.7", "--perms=read-only")
	lacks(t, "the dash-p calls", calls, b.repo, "--dangerously-skip-permissions")
}

func TestDoctorWithNoProbeAsksNobody(t *testing.T) {
	b := panelSandbox(t)
	t.Setenv("PANEL_REVIEW_PANELISTS", "")
	stdout, _, _ := autoreview(t, "doctor", "--no-probe")
	if !strings.Contains(stdout, "Panel, from every backend on PATH: not asked (--no-probe)") || b.calls() != "" {
		t.Errorf("stdout:\n%s\ncalls: %q", stdout, b.calls())
	}
}

func TestDoctorIsASubcommandOnlyAsTheFirstArgument(t *testing.T) {
	stdout, _, code := autoreview(t, "doctor", "--help")
	if code != 0 || !strings.HasPrefix(stdout, "autoreview doctor: is the panel answering") {
		t.Errorf("doctor --help: exit %d, stdout %q", code, firstChars(stdout, 80))
	}
	_, stderr, code := autoreview(t, "--all", "doctor")
	if code != 1 || !strings.Contains(stderr, "doctor is a subcommand and goes first: autoreview doctor") {
		t.Errorf("--all doctor: exit %d, stderr %q", code, stderr)
	}
}
