package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// script is a command that runs a constant shell script. The arguments
// after it are the script's own $1, $2 and so on.
func script(t *testing.T, body string, args ...string) Command {
	t.Helper()
	dir := t.TempDir()
	return Command{
		Path:   "sh",
		Args:   append([]string{"-c", body, "sh"}, args...),
		Dir:    dir,
		Stdout: filepath.Join(dir, "out"),
		Stderr: filepath.Join(dir, "err"),
	}
}

func start(t *testing.T, c Command) *osProc {
	t.Helper()
	p, err := OS{Grace: 50 * time.Millisecond}.Start(c)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return p.(*osProc)
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// groupGone waits for every process in the group to end.
func groupGone(pgid int) bool {
	for range 100 {
		if err := syscall.Kill(-pgid, 0); err == syscall.ESRCH {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func waitFor(t *testing.T, path string) {
	t.Helper()
	for range 250 {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never appeared", path)
}

func TestTheExitStatusIsReported(t *testing.T) {
	cases := []struct {
		body string
		want Exit
	}{
		{"exit 0", Exit{}},
		{"exit 10", Exit{Code: 10}},
		{"exit 20", Exit{Code: 20}},
		{"kill -9 $$", Exit{Signaled: true}},
	}
	for _, c := range cases {
		if got := start(t, script(t, c.body)).Wait(); got != c.want {
			t.Errorf("%q ended as %+v, want %+v", c.body, got, c.want)
		}
	}
}

func TestOutputGoesToTheFilesAndStdinIsEmpty(t *testing.T) {
	c := script(t, `echo to-stdout; echo to-stderr >&2; if read line; then echo "read $line"; else echo no-input; fi; pwd; echo "$EXTRA"`)
	c.Env = []string{"EXTRA=added"}
	if err := os.WriteFile(c.Stdout, []byte("from an earlier attempt\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := start(t, c).Wait(); got != (Exit{}) {
		t.Fatalf("exit = %+v", got)
	}

	dir, err := filepath.EvalSymlinks(c.Dir)
	if err != nil {
		t.Fatal(err)
	}
	wantOut := "to-stdout\nno-input\n" + dir + "\nadded\n"
	if got := read(t, c.Stdout); got != wantOut {
		t.Errorf("stdout file holds %q, want %q", got, wantOut)
	}
	if got := read(t, c.Stderr); got != "to-stderr\n" {
		t.Errorf("stderr file holds %q", got)
	}
}

func TestANamedStdinFileIsWhatTheProcessReads(t *testing.T) {
	c := script(t, `cat`)
	c.Stdin = filepath.Join(c.Dir, "prompt")
	if err := os.WriteFile(c.Stdin, []byte("a prompt\nof two lines\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := start(t, c).Wait(); got != (Exit{}) {
		t.Fatalf("exit = %+v", got)
	}
	if got := read(t, c.Stdout); got != "a prompt\nof two lines\n" {
		t.Errorf("the process read %q", got)
	}
}

func TestAStdinFileThatIsNotThereIsAnErrorBeforeAnythingStarts(t *testing.T) {
	c := script(t, `echo started`)
	c.Stdin = filepath.Join(c.Dir, "missing")
	if _, err := (OS{}).Start(c); err == nil {
		t.Fatal("Start succeeded with no stdin file to read")
	}
}

func TestEachProcessLeadsItsOwnGroup(t *testing.T) {
	c := script(t, `ps -o pgid= -p $$`)
	p := start(t, c)
	p.Wait()

	got := strings.TrimSpace(read(t, c.Stdout))
	if got != strconv.Itoa(p.pgid) {
		t.Errorf("the process ran in group %s, want a group of its own, %d", got, p.pgid)
	}
	if got == strconv.Itoa(syscall.Getpgrp()) {
		t.Error("the process ran in this test's own group: a stop would signal the caller too")
	}
}

func TestStopEndsAProcessThatObeysTERM(t *testing.T) {
	c := script(t, `touch ready; sleep 30`)
	p := start(t, c)
	waitFor(t, filepath.Join(c.Dir, "ready"))

	began := time.Now()
	p.Stop()
	exit := p.Wait()

	if !exit.Signaled {
		t.Errorf("exit = %+v, want a signal death", exit)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("the stop took %v", took)
	}
	if !groupGone(p.pgid) {
		t.Error("the group outlived the stop")
	}
}

func TestStopEndsAGrandchildThatIgnoresTERM(t *testing.T) {
	c := script(t, `(trap '' TERM; touch ready; sleep 30) & wait`)
	p := start(t, c)
	waitFor(t, filepath.Join(c.Dir, "ready"))

	p.Stop()
	p.Wait()

	if !groupGone(p.pgid) {
		t.Error("a child that ignores TERM outlived the stop: it would go on spending")
	}
}

func TestStopEndsALeaderThatIgnoresTERM(t *testing.T) {
	c := script(t, `trap '' TERM; touch ready; sleep 30; sleep 30`)
	p := start(t, c)
	waitFor(t, filepath.Join(c.Dir, "ready"))

	p.Stop()
	exit := p.Wait()

	if !exit.Signaled {
		t.Errorf("exit = %+v, want the KILL to end it", exit)
	}
	if !groupGone(p.pgid) {
		t.Error("the group outlived the stop")
	}
}

func TestStopIsSafeToRepeatAndAfterTheEnd(t *testing.T) {
	c := script(t, `touch ready; sleep 30`)
	p := start(t, c)
	waitFor(t, filepath.Join(c.Dir, "ready"))
	p.Stop()
	p.Stop()
	p.Wait()
	p.Stop()

	done := start(t, script(t, "exit 0"))
	done.Wait()
	done.Stop()
	if done.stopping {
		t.Error("a stop began for a process that was already collected: its group id is no longer its own")
	}
}

func TestStartSaysWhatItCouldNotStart(t *testing.T) {
	c := script(t, "exit 0")
	c.Path = filepath.Join(c.Dir, "no-such-program")
	if _, err := (OS{}).Start(c); err == nil || !strings.Contains(err.Error(), "no-such-program") {
		t.Errorf("Start error = %v, want one that names the program", err)
	}

	c = script(t, "exit 0")
	c.Stdout = filepath.Join(c.Dir, "missing-dir", "out")
	if _, err := (OS{}).Start(c); err == nil {
		t.Error("Start succeeded with nowhere to write stdout")
	}
}

func TestAWaitThatFailsStillGivesAnExit(t *testing.T) {
	if got := exitFrom(os.ErrClosed); got != (Exit{Code: exitNoStatus}) {
		t.Errorf("exitFrom(a wait error) = %+v, want code %d", got, exitNoStatus)
	}
}
