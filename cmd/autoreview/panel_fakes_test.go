package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// isPanelCall tells a panelist from a review: a panel asks dash-p for plain
// text, and a review asks for its JSON envelope.
func isPanelCall(args []string) bool {
	return slices.Contains(args, "--output-format=text")
}

// fakePanelist is one panelist, or the synthesis, that takes no time. It
// logs its argv to $FAKE_PANEL_DIR/calls and keeps the prompt it read on
// stdin, so a test can check what it was asked. The knobs list backends:
//
//   - $FAKE_PANEL_EMPTY: nothing on the first attempt, a review on the
//     second.
//   - $FAKE_PANEL_ALWAYS_EMPTY: nothing, with "quota exceeded" on stderr.
//   - $FAKE_PANEL_HANG: writes its pid to hang-pid and sleeps two minutes,
//     past every deadline.
//   - $FAKE_PANEL_DIRTY_EXIT: a review, then exit 3.
func fakePanelist(args []string) int {
	dir := os.Getenv("FAKE_PANEL_DIR")
	backend := ""
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--harness="); ok {
			backend = value
		}
	}
	appendLine(filepath.Join(dir, "calls"), strings.Join(args, " "))
	prompt, _ := io.ReadAll(os.Stdin)
	kept := filepath.Join(dir, fmt.Sprintf("stdin-%s-%d", backend, os.Getpid()))
	_ = os.WriteFile(kept, prompt, 0o600)
	attempt := nextAttempt(filepath.Join(dir, "tries-"+backend))

	if strings.Contains(string(prompt), "Panel synthesis request") {
		_ = os.WriteFile(filepath.Join(dir, "synthesis-prompt-path"), []byte(kept), 0o600)
		fmt.Print("### Overview\n\n**Reviewing:** synthesized\n")
		return 0
	}
	listed := func(knob string) bool { return slices.Contains(strings.Fields(os.Getenv(knob)), backend) }
	switch {
	case listed("FAKE_PANEL_EMPTY") && attempt == 1:
		fmt.Fprintln(os.Stderr, "transient blip")
		return 1
	case listed("FAKE_PANEL_ALWAYS_EMPTY"):
		fmt.Fprintln(os.Stderr, "quota exceeded")
		return 1
	case listed("FAKE_PANEL_HANG"):
		_ = os.WriteFile(filepath.Join(dir, "hang-pid"), []byte(strconv.Itoa(os.Getpid())), 0o600)
		time.Sleep(2 * time.Minute)
		return 0
	case listed("FAKE_PANEL_DIRTY_EXIT"):
		fmt.Printf("Model: %s-model\nGoal (clear): x\n- [LOW] a.rs:1 — nit\n", backend)
		return 3
	}
	fmt.Printf("Model: %s-model\nGoal (clear): x\nApproach (sound): y\n- [LOW] a.rs:1 — nit\n", backend)
	return 0
}

func appendLine(path, line string) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(f, line)
	_ = f.Close()
}

// nextAttempt counts the attempts of one backend in a file. Each backend
// has a file of its own, so panelists running at once do not share one.
func nextAttempt(path string) int {
	raw, _ := os.ReadFile(path)
	n, _ := strconv.Atoi(string(raw))
	_ = os.WriteFile(path, []byte(strconv.Itoa(n+1)), 0o600)
	return n + 1
}
