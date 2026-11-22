// Package oplock is the shared cross-process operation lock.
//
// Apply and update both take this lock before they write, so a configuration
// transaction and an app bundle transaction cannot overlap. The lock is an
// advisory flock on a regular file. The file is never deleted while a
// transaction is active and is harmless after a crash because the kernel
// releases the lock when the process exits.
package oplock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrBusy is returned when another process holds the lock.
var ErrBusy = errors.New("another operation is running")

// Error is a lock failure with a stable rule name.
type Error struct {
	Rule    string
	Message string
}

func (e *Error) Error() string { return e.Rule + ": " + e.Message }

func errorf(rule, format string, args ...any) *Error {
	return &Error{Rule: rule, Message: fmt.Sprintf(format, args...)}
}

// Path returns the shared lock path for a state root. Apply and update both
// use this constructor, so they cannot drift to different files.
func Path(stateRoot string) string {
	return filepath.Join(stateRoot, "update", "lock")
}

// checkNoSymlinkAncestor refuses a lock path whose existing parents include a
// symlink. A symlinked update directory could redirect the lock and the state
// root without changing the lock file itself.
func checkNoSymlinkAncestor(path string) error {
	for {
		info, err := os.Lstat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("oplock: inspect lock directory %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errorf("lock.parent", "the lock directory %s is a symlink", path)
		}
		if !info.IsDir() {
			return errorf("lock.parent", "the lock directory %s is not a directory", path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

// Lock is an acquired exclusive lock. Release must be called exactly once.
type Lock struct {
	file *os.File
	path string
}

// Acquire takes the exclusive, non-blocking lock at path. The parent directory
// is created if needed. The file is opened with O_NOFOLLOW, so a symlink is a
// hard failure rather than a redirect. An existing file without a holder is not
// stale: flock succeeds and the caller proceeds. A symlink, a non-regular file,
// a wrong owner, or a mode other than 0600 is a hard failure and is not
// repaired.
func Acquire(path string) (*Lock, error) {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("oplock: create lock directory %s: %w", parent, err)
	}
	if info, err := os.Lstat(parent); err != nil {
		return nil, fmt.Errorf("oplock: inspect lock directory %s: %w", parent, err)
	} else if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errorf("lock.parent", "the lock directory %s is not a regular directory", parent)
	}
	if err := checkNoSymlinkAncestor(parent); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, errorf("lock.type", "the lock path %s is a symlink", path)
		}
		return nil, fmt.Errorf("oplock: open lock %s: %w", path, err)
	}
	closeOnError := func(cause error) (*Lock, error) {
		_ = file.Close()
		return nil, cause
	}
	info, err := file.Stat()
	if err != nil {
		return closeOnError(fmt.Errorf("oplock: stat lock %s: %w", path, err))
	}
	if !info.Mode().IsRegular() {
		return closeOnError(errorf("lock.type", "the lock path %s is not a regular file", path))
	}
	if info.Mode().Perm() != 0o600 {
		return closeOnError(errorf("lock.mode", "the lock file %s has mode %04o, not 0600", path, info.Mode().Perm()))
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if stat.Uid != uint32(os.Geteuid()) {
			return closeOnError(errorf("lock.owner", "the lock file %s has owner uid %d, not %d", path, stat.Uid, os.Geteuid()))
		}
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrBusy
		}
		return nil, fmt.Errorf("oplock: lock %s: %w", path, err)
	}
	return &Lock{file: file, path: path}, nil
}

// Release unlocks and closes the file. It is safe on a nil lock.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return err
	}
	return closeErr
}
