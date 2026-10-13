//go:build !unix

package ledger

import "os"

// lockExclusive does nothing where there is no flock, as in v1.
func lockExclusive(*os.File) error {
	return nil
}
