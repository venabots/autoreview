package marks

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
)

// github records each call, and answers an add with an id unless the test
// says otherwise.
type github struct {
	mu    sync.Mutex
	calls []string
	// addFails is how many adds fail before one succeeds.
	addFails int
	// removeFails makes every removal fail.
	removeFails bool
	// hold, when set, keeps the first add from answering until it closes.
	hold chan struct{}
}

func (g *github) run(_ context.Context, args ...string) ([]byte, error) {
	line := strings.Join(args, " ")
	g.mu.Lock()
	g.calls = append(g.calls, line)
	hold := g.hold
	g.hold = nil
	failAdd := strings.Contains(line, "POST") && g.addFails > 0
	if failAdd {
		g.addFails--
	}
	removeFails := g.removeFails
	g.mu.Unlock()
	if hold != nil && strings.Contains(line, "POST") {
		<-hold
	}
	switch {
	case failAdd:
		return nil, errors.New("timed out")
	case strings.Contains(line, "POST"):
		return []byte("77\n"), nil
	case removeFails:
		return nil, errors.New("HTTP 500")
	}
	return nil, nil
}

func (g *github) log() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.calls)
}

func TestTheCallsAreV1s(t *testing.T) {
	m := New(nil, "acme", "widgets")
	if got := strings.Join(m.AddArgs(9), " "); got != "api repos/acme/widgets/issues/9/reactions --method POST -f content=eyes --jq .id" {
		t.Errorf("add = %q", got)
	}
	if got := strings.Join(m.RemoveArgs(9, 77), " "); got != "api repos/acme/widgets/issues/9/reactions/77 --method DELETE" {
		t.Errorf("remove = %q", got)
	}
}

func TestAMarkGoesOnAndComesOff(t *testing.T) {
	g := &github{}
	m := New(g.run, "acme", "widgets")
	m.Show(9)
	m.Clear(9)
	if failed := m.Settle(); len(failed) != 0 {
		t.Errorf("failed = %v", failed)
	}
	want := []string{
		"api repos/acme/widgets/issues/9/reactions --method POST -f content=eyes --jq .id",
		"api repos/acme/widgets/issues/9/reactions/77 --method DELETE",
	}
	if got := g.log(); !slices.Equal(got, want) {
		t.Errorf("calls = %q, want %q", got, want)
	}
}

func TestTheRemovalWaitsForTheAddToLand(t *testing.T) {
	release := make(chan struct{})
	g := &github{hold: release}
	m := New(g.run, "acme", "widgets")
	m.Show(9)
	m.Clear(9)
	for range 50 {
		if len(g.log()) > 0 {
			break
		}
	}
	if got := g.log(); slices.ContainsFunc(got, func(c string) bool { return strings.Contains(c, "DELETE") }) {
		t.Fatalf("the removal ran before the add answered: %q", got)
	}
	close(release)
	if failed := m.Settle(); len(failed) != 0 {
		t.Errorf("failed = %v", failed)
	}
	if got := g.log(); len(got) != 2 || !strings.Contains(got[1], "/77 --method DELETE") {
		t.Errorf("calls = %q", got)
	}
}

func TestAnAddThatGaveNoIDIsAskedForAgain(t *testing.T) {
	g := &github{addFails: 1}
	m := New(g.run, "acme", "widgets")
	m.Show(9)
	m.Clear(9)
	if failed := m.Settle(); len(failed) != 0 {
		t.Errorf("failed = %v", failed)
	}
	got := g.log()
	if len(got) != 3 || !strings.Contains(got[1], "POST") || !strings.Contains(got[2], "DELETE") {
		t.Errorf("calls = %q, want a second add to learn the id, then the removal", got)
	}
}

func TestAMarkThatCouldNotBeSetOrRemovedIsNamed(t *testing.T) {
	g := &github{addFails: 2}
	m := New(g.run, "acme", "widgets")
	m.Show(9)
	m.Clear(9)
	g2 := &github{removeFails: true}
	m2 := New(g2.run, "acme", "widgets")
	m2.Show(8)
	m2.Clear(8)
	if failed := m.Settle(); !slices.Equal(failed, []int{9}) {
		t.Errorf("a mark that never landed: failed = %v", failed)
	}
	if failed := m2.Settle(); !slices.Equal(failed, []int{8}) {
		t.Errorf("a mark that would not come off: failed = %v", failed)
	}
}

func TestAPRIsMarkedOnceAndAnUnmarkedPRIsNotCleared(t *testing.T) {
	g := &github{}
	m := New(g.run, "acme", "widgets")
	m.Show(9)
	m.Show(9)
	m.Clear(8)
	m.Clear(9)
	m.Clear(9)
	m.Settle()
	if got := g.log(); len(got) != 2 {
		t.Errorf("calls = %q, want one add and one removal", got)
	}
}
