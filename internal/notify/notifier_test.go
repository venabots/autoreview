package notify

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/task"
)

type inbox struct {
	mu   sync.Mutex
	got  []Alert
	fail error
}

func (b *inbox) send(_ context.Context, a Alert) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.got = append(b.got, a)
	return b.fail
}

func (b *inbox) alerts() []Alert {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.got)
}

var shouldClose = engine.JobFinished{Job: engine.Job{PR: 2931, Task: task.Conflicts, State: engine.JobDone,
	TaskTrailer: &review.TaskTrailer{Outcome: review.TaskShouldClose, Why: "superseded"}}}

// A new channel is one more Sender: each alert reaches every channel, and
// a channel that fails does not stop the others.
func TestEveryChannelGetsEachAlertAndOneThatFailsStopsNoOther(t *testing.T) {
	broken, working := &inbox{fail: errors.New("down")}, &inbox{}
	err := Fanout(broken.send, working.send)(context.Background(), Alert{Title: "x"})
	if len(broken.alerts()) != 1 || len(working.alerts()) != 1 {
		t.Errorf("broken got %d, working got %d; want 1 each", len(broken.alerts()), len(working.alerts()))
	}
	if err == nil {
		t.Error("the failure of one channel was not reported")
	}
}

func TestTheNotifierSendsTheAlertsOfEachEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &inbox{}
		n := New(b.send, "bank", nil)
		n.Emit(engine.Noted{Text: "nothing to say"})
		n.Emit(shouldClose)
		n.Close()
		want := []Alert{{Title: "bank: PR #2931 should close", Body: "superseded", Urgent: true}}
		if got := b.alerts(); !slices.Equal(got, want) {
			t.Errorf("sent %+v, want %+v", got, want)
		}
	})
}

// The engine calls Emit on its own goroutine. A channel that hangs must
// never make the run wait.
func TestEmitNeverWaitsForAChannel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hang := make(chan struct{})
		stuck := func(ctx context.Context, _ Alert) error {
			select {
			case <-hang:
			case <-ctx.Done():
			}
			return ctx.Err()
		}
		n := New(stuck, "", nil)
		for range 1000 {
			n.Emit(shouldClose)
		}
		close(hang)
		n.Close()
	})
}

func TestAnEventAfterCloseIsDropped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &inbox{}
		n := New(b.send, "", nil)
		n.Close()
		n.Emit(shouldClose)
		n.Close()
		if got := b.alerts(); len(got) != 0 {
			t.Errorf("sent %+v after Close", got)
		}
	})
}

func TestAChannelThatFailsIsSaidOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := &inbox{fail: errors.New("herdr: no session")}
		var said []string
		n := New(b.send, "", func(err error) { said = append(said, err.Error()) })
		n.Emit(shouldClose)
		n.Emit(shouldClose)
		n.Close()
		if !slices.Equal(said, []string{"herdr: no session"}) {
			t.Errorf("said %q, want the failure once", said)
		}
	})
}

func TestCloseGivesUpOnAChannelThatHangs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stuck := func(ctx context.Context, _ Alert) error { <-ctx.Done(); return ctx.Err() }
		n := New(stuck, "", nil)
		n.Emit(shouldClose)
		start := time.Now()
		n.Close()
		if waited := time.Since(start); waited > sendTimeout+time.Second {
			t.Errorf("Close waited %v", waited)
		}
	})
}
