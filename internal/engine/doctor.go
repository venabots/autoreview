package engine

import (
	"slices"
	"strings"
	"time"
)

// DoctorEvery is how often a run asks the doctor again. The doctor asks
// each panelist a short question, which costs a little, and a panelist
// that ran out of credits stays down for hours.
const DoctorEvery = 30 * time.Minute

// Health is what `autoreview doctor` would say, asked in the background.
// A review that loses a panelist still ends, with a comment in place of a
// verdict, and nothing else on the screen says the panel is down.
type Health struct {
	// Seated is the panelists a review seats, and Answering how many of
	// them answered when asked.
	Seated, Answering int
	// Problems is each thing the doctor would call a problem.
	Problems []string
	// Blocking says reviews cannot approve or cannot start.
	Blocking bool
}

// doctorChecked is one answer from the doctor.
type doctorChecked struct{ health Health }

func (doctorChecked) input() {}

// startDoctor asks the doctor now and every DoctorEvery after, until the
// run ends. The doctor starts processes and reads the ledger, so it runs
// off the engine's goroutine.
func (e *Engine) startDoctor(w workers) {
	if e.deps.Doctor == nil {
		return
	}
	w.group.Go(func() {
		for {
			e.in.put(doctorChecked{health: e.deps.Doctor(w.ctx)})
			select {
			case <-w.ctx.Done():
				return
			case <-time.After(DoctorEvery):
			}
		}
	})
}

// doctorChecked takes the doctor's answer, and names each change in the
// log: the footer shows only the count, and the log says what is wrong.
func (e *Engine) doctorChecked(r run, in doctorChecked) run {
	before := r.snap.Health
	h := in.health
	r.snap.Health = &h
	was := Health{}
	if before != nil {
		was = *before
	}
	if !slices.Equal(was.Problems, h.Problems) || was.Blocking != h.Blocking {
		e.journal(HealthChanged{Before: was, After: h})
	}
	switch {
	case len(h.Problems) > 0 && (before == nil || !slices.Equal(before.Problems, h.Problems)):
		e.journal(Noted{Text: "doctor: " + strings.Join(h.Problems, "; ")})
	case len(h.Problems) == 0 && before != nil && len(before.Problems) > 0:
		e.journal(Noted{Text: "doctor: all clear again"})
	}
	return r
}
