package term

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// sh runs a constant shell script through Exec. The arguments after it are
// the script's own $1, $2 and so on.
func sh(ctx context.Context, script string, args ...string) ([]byte, error) {
	return Exec(ctx, "sh", append([]string{"-c", script, "sh"}, args...)...)
}

func TestExecReturnsWhatTheProgramPrinted(t *testing.T) {
	out, err := sh(context.Background(), `printf '%s\n' "$@"`, "one word", `"two"`, "$(three)")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got, want := string(out), "one word\n\"two\"\n$(three)\n"; got != want {
		t.Errorf("Exec returned %q, want each argument as it was given: %q", got, want)
	}
}

func TestExecGivesTheProgramNoInput(t *testing.T) {
	out, err := sh(context.Background(), `if read line; then echo "read: $line"; else echo "no input"; fi`)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := string(out); got != "no input\n" {
		t.Errorf("the program saw %q, want an empty stdin", got)
	}
}

func TestWhatAProgramSaysOnStderrIsNotInItsOutput(t *testing.T) {
	out, err := sh(context.Background(), `echo "cmux: a warning" >&2; echo surface:1`)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got := string(out); got != "surface:1\n" {
		t.Errorf("Exec returned %q, want stdout alone", got)
	}
}

func TestAFailedProgramGivesItsFirstLineOfStderr(t *testing.T) {
	out, err := sh(context.Background(), `echo partial; echo "  Error: no such surface  " >&2; echo "second line" >&2; exit 1`)
	if err == nil {
		t.Fatal("Exec hid a failed program")
	}
	if got, want := err.Error(), "Error: no such surface"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	if out != nil {
		t.Errorf("Exec returned %q from a failed program, want nothing", out)
	}
}

func TestAFailedProgramWithNothingOnStderrGivesItsExit(t *testing.T) {
	_, err := sh(context.Background(), `exit 3`)
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 3 {
		t.Errorf("error = %v, want the exit status 3", err)
	}
}

func TestAProgramThatIsNotThereIsAnError(t *testing.T) {
	_, err := Exec(context.Background(), filepath.Join(t.TempDir(), "no-such-terminal"))
	if err == nil {
		t.Fatal("Exec hid a program that could not start")
	}
}

func TestADeadlineStopsTheProgramAndWhatItStarted(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-finished")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	began := time.Now()
	_, err := sh(ctx, `(sleep 1; touch "$1") & wait`, marker)

	if err == nil || err.Error() != "timed out" {
		t.Errorf("error = %v, want a timeout", err)
	}
	if took := time.Since(began); took > 800*time.Millisecond {
		t.Errorf("Exec returned after %v, want it to stop at the deadline", took)
	}
	time.Sleep(1300 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a process the program started outlived the deadline: the group was not killed")
	}
}

func TestACancelledCallSaysItWasCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	_, err := sh(ctx, `echo "a line that is not the cause" >&2; sleep 5`)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want the cancel", err)
	}
}

func TestACallWithNoDeadlineGetsTheDefault(t *testing.T) {
	ctx, cancel := withDefaultDeadline(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > DefaultTimeout || time.Until(deadline) < DefaultTimeout-time.Second {
		t.Errorf("deadline = %v, %v; want about %v from now", deadline, ok, DefaultTimeout)
	}

	short, cancelShort := context.WithTimeout(context.Background(), time.Second)
	defer cancelShort()
	kept, cancelKept := withDefaultDeadline(short)
	defer cancelKept()
	want, _ := short.Deadline()
	if got, _ := kept.Deadline(); !got.Equal(want) {
		t.Error("a caller's own deadline was replaced")
	}
}
