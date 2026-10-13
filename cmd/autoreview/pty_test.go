package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// These tests give the real binary a terminal. They look only at what is
// outside the view's frames: the alternate screen being entered and left,
// the exit status, what is printed after the view closes, and the state the
// terminal is left in. What a frame says is tested on the model, because
// the renderer sends a frame as cell updates and not as lines of text.

const (
	enterAltScreen = "\x1b[?1049h"
	leaveAltScreen = "\x1b[?1049l"
)

var (
	buildOnce sync.Once
	built     string
	buildErr  error
	// packageDir is where this package's source is: the directory the test
	// binary starts in.
	packageDir, _ = os.Getwd()
	// startEnv is the environment the test binary started with.
	startEnv = os.Environ()
)

// binary builds autoreview once for the tests that need the real program.
func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "autoreview-pty-")
		if err != nil {
			buildErr = err
			return
		}
		built = filepath.Join(dir, "autoreview")
		build := exec.Command("go", "build", "-o", built, ".")
		// A test changes directory into its sandbox before it gets here.
		build.Dir = packageDir
		// And it replaces HOME, where Go keeps its module cache. A build
		// with the sandbox's HOME would fill a temporary directory with
		// read-only files that the test cannot remove.
		build.Env = startEnv
		if out, err := build.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("%w\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatalf("building autoreview: %v", buildErr)
	}
	return built
}

// terminal is a shell line running under a pty, and everything it has
// drawn so far.
type terminal struct {
	t      *testing.T
	master *os.File
	cmd    *exec.Cmd
	mu     sync.Mutex
	drawn  bytes.Buffer
	done   chan struct{}
}

// script is run by sh after autoreview: it reports the exit status, counts
// the input autoreview left unread, and prints the terminal's settings.
const script = `"$AUTOREVIEW" "$@"; echo "exit=$?"; left=$(dd bs=64 count=1 2>/dev/null | wc -c | tr -d ' '); echo "leftover=$left"; stty -a`

// inTerminal starts autoreview under a pty of 120 by 30 in the sandbox.
func inTerminal(t *testing.T, args ...string) *terminal {
	t.Helper()
	cmd := exec.Command("sh", append([]string{"-c", script, "sh"}, args...)...)
	cmd.Env = append(os.Environ(), "AUTOREVIEW="+binary(t), "TERM=xterm-256color")
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 120, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	term := &terminal{t: t, master: master, cmd: cmd, done: make(chan struct{})}
	go func() {
		defer close(term.done)
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			term.mu.Lock()
			term.drawn.Write(buf[:n])
			term.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = master.Close()
	})
	return term
}

func (term *terminal) output() string {
	term.mu.Lock()
	defer term.mu.Unlock()
	return term.drawn.String()
}

// waitFor waits until the terminal has drawn text.
func (term *terminal) waitFor(text string) {
	term.t.Helper()
	for range 500 {
		if strings.Contains(term.output(), text) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	term.t.Fatalf("the terminal never drew %q; it drew:\n%q", text, term.output())
}

func (term *terminal) press(keys string) {
	term.t.Helper()
	if _, err := term.master.WriteString(keys); err != nil {
		term.t.Fatal(err)
	}
}

// finish waits for the shell line to end and returns everything drawn. The
// dd in the script waits for input, so finish closes its wait with a
// newline that the script then counts as nothing: dd reads zero bytes of
// leftover input only when none was pending before this point.
func (term *terminal) finish() string {
	term.t.Helper()
	term.waitFor("exit=")
	term.press("\x04")
	select {
	case <-term.done:
	case <-time.After(10 * time.Second):
		term.t.Fatalf("the shell never ended; the terminal drew:\n%q", term.output())
	}
	_ = term.cmd.Wait()
	return term.output()
}

// wantCooked fails unless the terminal was given back in the mode a shell
// expects: lines edited by the terminal, and typing echoed.
func wantCooked(t *testing.T, drawn string) {
	t.Helper()
	settings := drawn[strings.LastIndex(drawn, "leftover="):]
	// stty lists each flag as a word, with a minus sign when it is off.
	// Where a flag sits on its line differs between systems.
	flags := strings.FieldsFunc(settings, func(r rune) bool { return r == ' ' || r == ';' || r == '\n' || r == '\r' || r == '\t' })
	for _, flag := range []string{"icanon", "echo", "isig"} {
		if !slices.Contains(flags, flag) || slices.Contains(flags, "-"+flag) {
			t.Errorf("the terminal was left without %s:\n%s", flag, settings)
		}
	}
}

func TestOnATerminalTheViewOpensAndQGivesTheTerminalBack(t *testing.T) {
	fixtures := sandbox(t)
	term := inTerminal(t, "--jobs=1")
	term.waitFor(enterAltScreen)
	for range 500 {
		if len(dashpCalls(t, fixtures)) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(700 * time.Millisecond)
	term.press("qq")
	drawn := term.finish()

	if !strings.Contains(drawn, leaveAltScreen) {
		t.Fatalf("the view never left the alternate screen:\n%q", drawn)
	}
	after := drawn[strings.LastIndex(drawn, leaveAltScreen):]
	// The view watches, so q ends a run that would have gone on.
	for _, want := range []string{"2 reviews: 2 commented · summary in ", "/autoreview.log", "exit=130", "leftover=0"} {
		if !strings.Contains(after, want) {
			t.Errorf("after the view closed the terminal lacks %q:\n%s", want, after)
		}
	}
	if strings.Contains(drawn, "PR  RESULT") || strings.Contains(drawn, "skills: ") {
		t.Errorf("the view's run printed its summary table or its skills line on the screen:\n%q", drawn)
	}
	logs, _ := filepath.Glob(filepath.Join(fixtures, "logs", "run-*", "autoreview.log"))
	if len(logs) != 1 {
		t.Fatalf("run logs: %v", logs)
	}
	if log, _ := os.ReadFile(logs[0]); !strings.Contains(string(log), "skills: bundled (dev)") || !strings.Contains(string(log), "PR  RESULT  VERDICT") {
		t.Errorf("the run log lacks the skills line or the summary:\n%s", log)
	}
	wantCooked(t, drawn)
}

func TestHeadlessOnATerminalPrintsPlainLines(t *testing.T) {
	sandbox(t)
	term := inTerminal(t, "--headless", "9")
	drawn := term.finish()

	if strings.Contains(drawn, enterAltScreen) {
		t.Error("--headless opened the full-screen view")
	}
	for _, want := range []string{"start   #9 @alice (reviewing)", "done    #9", "PR  RESULT  VERDICT", "exit=0"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the terminal lacks %q:\n%s", want, drawn)
		}
	}
	wantCooked(t, drawn)
}

func TestQTwiceStopsARunningReviewAndExits130(t *testing.T) {
	fixtures := sandbox(t)
	pidFile := filepath.Join(fixtures, "review.pid")
	t.Setenv("FAKE_DASHP_HANG", pidFile)
	term := inTerminal(t, "9")
	term.waitFor(enterAltScreen)
	reviewer := waitForPid(t, pidFile)
	time.Sleep(500 * time.Millisecond)

	term.press("q")
	time.Sleep(300 * time.Millisecond)
	if strings.Contains(term.output(), "exit=") {
		t.Fatal("one press of q quit a run with a review alive")
	}
	term.press("q")
	drawn := term.finish()

	after := drawn[strings.LastIndex(drawn, leaveAltScreen):]
	for _, want := range []string{"1 review: 1 stopped · summary in ", "error: 1 of 1 review failed", "exit=130", "leftover=0"} {
		if !strings.Contains(after, want) {
			t.Errorf("after the view closed the terminal lacks %q:\n%s", want, after)
		}
	}
	wantGone(t, reviewer)
	wantCooked(t, drawn)
}

// The first q only asks, whatever the run is doing, and esc answers no.
func TestOneQNeverQuitsAndEscStaysInTheView(t *testing.T) {
	sandbox(t)
	term := inTerminal(t, "--once", "4")
	term.waitFor(enterAltScreen)
	time.Sleep(1500 * time.Millisecond)

	term.press("q")
	time.Sleep(300 * time.Millisecond)
	term.press("\x1b")
	time.Sleep(300 * time.Millisecond)
	if strings.Contains(term.output(), "exit=") {
		t.Fatal("one press of q quit a run with nothing running")
	}
	term.press("qq")
	drawn := term.finish()

	after := drawn[strings.LastIndex(drawn, leaveAltScreen):]
	if !strings.Contains(after, "exit=0") || !strings.Contains(after, "leftover=0") {
		t.Errorf("after q q:\n%s\nwant exit 0 and no input left for the shell", after)
	}
	wantCooked(t, drawn)
}

func TestCtrlCQuitsAtOnceAndLeavesNoInputForTheShell(t *testing.T) {
	fixtures := sandbox(t)
	pidFile := filepath.Join(fixtures, "review.pid")
	t.Setenv("FAKE_DASHP_HANG", pidFile)
	term := inTerminal(t, "9")
	term.waitFor(enterAltScreen)
	reviewer := waitForPid(t, pidFile)
	time.Sleep(500 * time.Millisecond)

	// ctrl-C, then two mouse reports of the kind a terminal still has on
	// the way when the view closes.
	term.press("\x03\x1b[<35;10;10M\x1b[<35;11;10M")
	drawn := term.finish()

	after := drawn[strings.LastIndex(drawn, leaveAltScreen):]
	if !strings.Contains(after, "exit=130") || !strings.Contains(after, "leftover=0") {
		t.Errorf("after ctrl-C:\n%s\nwant exit 130 and no input left for the shell to run", after)
	}
	wantGone(t, reviewer)
	wantCooked(t, drawn)
}

func TestWithOnceTheViewMakesOnePassAndQEndsWithItsStatus(t *testing.T) {
	fixtures := sandbox(t)
	term := inTerminal(t, "--once", "9")
	term.waitFor(enterAltScreen)
	for range 500 {
		if len(dashpCalls(t, fixtures)) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(700 * time.Millisecond)
	term.press("qq")
	drawn := term.finish()
	after := drawn[strings.LastIndex(drawn, leaveAltScreen):]
	if !strings.Contains(after, "1 review: 1 commented") || !strings.Contains(after, "exit=0") {
		t.Errorf("after the view closed:\n%s\nwant the pass's own status", after)
	}
	wantCooked(t, drawn)
}

func TestTheViewOpensOnARepoWithNothingToReview(t *testing.T) {
	sandbox(t)
	term := inTerminal(t, "--once", "4")
	term.waitFor(enterAltScreen)
	time.Sleep(1500 * time.Millisecond)
	term.press("qq")
	drawn := term.finish()
	after := drawn[strings.LastIndex(drawn, leaveAltScreen):]
	if !strings.Contains(after, "exit=0") || strings.Contains(after, "review") {
		t.Errorf("after the view closed:\n%s\nwant exit 0 and no summary of a run that reviewed nothing", after)
	}
	wantCooked(t, drawn)
}

func waitForPid(t *testing.T, pidFile string) int {
	t.Helper()
	for range 500 {
		if raw, err := os.ReadFile(pidFile); err == nil && len(raw) > 0 {
			pid, _ := strconv.Atoi(string(raw))
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the review never started")
	return 0
}

func wantGone(t *testing.T, pid int) {
	t.Helper()
	for range 150 {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("the reviewer, pid %d, outlived the run", pid)
}
