package main

import (
	"os"
	"time"
)

// lateInput is how long the terminal gets to deliver what it had already
// sent when the view closed.
const lateInput = 50 * time.Millisecond

// discardInput throws away input nobody read. A terminal that was
// reporting the mouse can still have reports on the way when the view
// closes, and the shell would run them as if they had been typed.
func discardInput() {
	time.Sleep(lateInput)
	// Nothing can be done about a failure here, and nothing depends on it.
	_ = flushInput(int(os.Stdin.Fd()))
}
