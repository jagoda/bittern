package update

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

type lockHandle struct {
	file *os.File
	path string
}

// acquireLock takes the exclusive, non-blocking cross-process lock. The lock
// file is never deleted. An existing file without a holder is not stale: flock
// succeeds and the transaction proceeds. A symlink, a non-regular file, a
// wrong owner, or a mode other than 0600 is a hard failure and is not repaired.
func acquireLock(path string) (*lockHandle, error) {
	if err := os.MkdirAll(parent(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("update: open lock %s: %w", path, err)
	}
	closeOnError := func(cause error) (*lockHandle, error) {
		_ = file.Close()
		return nil, cause
	}
	info, err := file.Stat()
	if err != nil {
		return closeOnError(err)
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
		return nil, fmt.Errorf("update: lock %s: %w", path, err)
	}
	return &lockHandle{file: file, path: path}, nil
}

func (l *lockHandle) release() error {
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

func parent(path string) string {
	for index := len(path) - 1; index >= 0; index-- {
		if path[index] == '/' {
			if index == 0 {
				return "/"
			}
			return path[:index]
		}
	}
	return "."
}
