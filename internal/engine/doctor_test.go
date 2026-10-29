package engine

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// doctors answers each check with the next health in a list, and the last
// one after that.
type doctors struct {
	mu     sync.Mutex
	said   []Health
	checks int
}

func (d *doctors) check(context.Context) Health {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := d.said[min(d.checks, len(d.said)-1)]
	d.checks++
	return h
}

func (d *doctors) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.checks
}

// The view shows what autoreview doctor would say. The run asks when it
// starts and every DoctorEvery after, off its own goroutine, and the log
// names each change.
func TestTheRunAsksTheDoctorAtTheStartAndAgainLater(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		down := Health{Seated: 4, Answering: 3, Problems: []string{"codex does not answer"}}
		d := &doctors{said: []Health{{Seated: 4, Answering: 4}, down, down, {Seated: 4, Answering: 4}}}
		b := newBench(t, held(setup{deps: func(deps *Deps) { deps.Doctor = d.check }}))
		synctest.Wait()
		if h := b.eng.Snapshot().Health; h == nil || h.Answering != 4 || d.count() != 1 {
			t.Fatalf("after the start: health %+v after %d checks", h, d.count())
		}

		time.Sleep(DoctorEvery)
		synctest.Wait()
		if h := b.eng.Snapshot().Health; h == nil || h.Answering != 3 {
			t.Errorf("after one interval: health %+v", h)
		}
		time.Sleep(2 * DoctorEvery)
		synctest.Wait()
		changes := []string{}
		b.mu.Lock()
		events := slices.Clone(b.events)
		b.mu.Unlock()
		for _, ev := range events {
			if c, ok := ev.(HealthChanged); ok {
				changes = append(changes, fmt.Sprintf("%d->%d", len(c.Before.Problems), len(c.After.Problems)))
			}
		}
		if !slices.Equal(changes, []string{"0->1", "1->0"}) {
			t.Errorf("HealthChanged events %q, want one to down and one back", changes)
		}
		want := []string{"doctor: codex does not answer", "doctor: all clear again"}
		if got := slices.DeleteFunc(b.notes(), func(n string) bool { return !slices.Contains(want, n) }); !slices.Equal(got, want) {
			t.Errorf("notes %q, want the change to down once and back once", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestARunWithNoDoctorShowsNoHealth(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, held(setup{}))
		if h := b.eng.Snapshot().Health; h != nil {
			t.Errorf("health %+v, want none", h)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}
