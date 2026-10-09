// Package marks puts an eyes reaction on a PR while its review runs.
//
// A review takes minutes and says nothing on the PR until it posts. Without
// a mark the author cannot tell a PR that is being read from one nobody has
// picked up, and nor can a second reviewer. The reaction goes on when the
// review starts and comes off when it ends, however it ends: a mark that
// outlived its review would say a review is running when none is.
//
// Every call to GitHub runs on its own goroutine. A slow GitHub must not
// stall the run, and a reaction that could not be set is a note, never a
// failed review.
package marks

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/venabots/autoreview/internal/gh"
)

const content = "eyes"

// Marks are the reactions one run has out.
type Marks struct {
	run  gh.Runner
	path func(pr int) string

	mu sync.Mutex
	// shown holds, for each PR with a mark on or on its way, a channel that
	// gives the reaction's id once the call has answered: zero when it
	// failed.
	shown map[int]chan int64
	// clearing waits for the marks on their way off.
	clearing sync.WaitGroup
	failed   []int
}

// New makes the marks for one repository.
func New(run gh.Runner, owner, name string) *Marks {
	return &Marks{
		run:   run,
		path:  func(pr int) string { return fmt.Sprintf("repos/%s/%s/issues/%d/reactions", owner, name, pr) },
		shown: map[int]chan int64{},
	}
}

// AddArgs is the call that puts the mark on. A PR is an issue to this
// endpoint, and --jq .id is what lets the removal name the reaction.
func (m *Marks) AddArgs(pr int) []string {
	return []string{"api", m.path(pr), "--method", "POST", "-f", "content=" + content, "--jq", ".id"}
}

// RemoveArgs is the call that takes one reaction off. It names the
// reaction: the collection with DELETE is not an endpoint.
func (m *Marks) RemoveArgs(pr int, id int64) []string {
	return []string{"api", m.path(pr) + "/" + strconv.FormatInt(id, 10), "--method", "DELETE"}
}

// Show marks a PR as being reviewed. It returns at once. A PR that is
// marked already is left alone: a review retried under the fallback is
// still the same review.
func (m *Marks) Show(pr int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.shown[pr]; ok {
		return
	}
	landed := make(chan int64, 1)
	m.shown[pr] = landed
	go func() { landed <- m.add(pr) }()
}

// Clear takes a mark off. It returns at once, and waits on its own
// goroutine for the mark to land first: a review that failed in its first
// second would otherwise remove a reaction that arrives a moment later and
// then stays.
//
// A mark whose call gave no id is asked for again before it is given up
// on. A call that timed out may still have landed, and GitHub answers a
// second add with the reaction that is already there.
func (m *Marks) Clear(pr int) {
	m.mu.Lock()
	landed, ok := m.shown[pr]
	delete(m.shown, pr)
	m.mu.Unlock()
	if !ok {
		return
	}
	m.clearing.Go(func() {
		id := <-landed
		if id == 0 {
			id = m.add(pr)
		}
		if id == 0 || !m.remove(pr, id) {
			m.mu.Lock()
			m.failed = append(m.failed, pr)
			m.mu.Unlock()
		}
	})
}

// Settle waits for every mark on its way off, and names the PRs whose mark
// could not be set or removed. The process may end right after it, and a
// call it did not wait for is a reaction left behind.
func (m *Marks) Settle() []int {
	m.clearing.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	failed := slices.Clone(m.failed)
	slices.Sort(failed)
	m.failed = nil
	return failed
}

func (m *Marks) add(pr int) int64 {
	out, err := m.run(context.Background(), m.AddArgs(pr)...)
	if err != nil {
		return 0
	}
	id, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func (m *Marks) remove(pr int, id int64) bool {
	_, err := m.run(context.Background(), m.RemoveArgs(pr, id)...)
	return err == nil
}
