//go:build unix

package ledger

import (
	"errors"
	"os"
	"syscall"
)

// lockExclusive waits for the ledger's write lock. flock is advisory, which
// is enough: every writer, v1 and v2, takes the same lock. A filesystem
// that does not support it returns an error, and the caller reports it
// instead of writing with no lock.
func lockExclusive(file *os.File) error {
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
		// A signal can end the wait early. The lock is not held then, so
		// ask again.
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}
