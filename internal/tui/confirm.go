package tui

import "time"

// confirmFor is how long a first press stays armed.
const confirmFor = 3 * time.Second

type confirmKind int

const (
	confirmNone confirmKind = iota
	confirmStop
	confirmQuit
)

// confirm is a costly key that was pressed once. Stopping a review throws
// away what it has spent, and quitting stops every review, so one stray
// key must not do either: both act on the second press.
//
// The arming names the PR. The rows move as reviews finish, and a second x
// that lands on another row must not stop a review nobody asked to stop.
type confirm struct {
	kind confirmKind
	pr   int
	at   time.Time
}

// holds says this press is the second one: the same thing was armed, and
// not too long ago.
func (c confirm) holds(kind confirmKind, pr int, now time.Time) bool {
	return c.kind == kind && c.pr == pr && now.Sub(c.at) <= confirmFor
}
