package update

import "bittern.invalid/agent/internal/oplock"

// lockHandle is the shared operation lock. Apply takes the same file through
// the same package, so the two transactions cannot overlap.
type lockHandle = oplock.Lock

// acquireLock takes the exclusive, non-blocking cross-process operation lock.
func acquireLock(path string) (*lockHandle, error) {
	return oplock.Acquire(path)
}
