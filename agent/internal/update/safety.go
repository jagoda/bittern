package update

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// validateConfigFile refuses a live configuration path the engine must not
// follow or copy: a symlink, a non-regular file, a file owned by another user,
// or one that another user can modify. A missing optional path is allowed; a
// missing required path is left to the reader that needs it so the failure
// names the missing file.
func validateConfigFile(path string, required bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if required {
			return nil
		}
		return nil
	}
	if err != nil {
		return errorf("config.read", "read the live configuration %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errorf("config.symlink", "the live configuration %s is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return errorf("config.type", "the live configuration %s is not a regular file", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errorf("config.owner", "the owner of the live configuration %s cannot be determined", path)
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return errorf("config.owner", "the live configuration %s has owner uid %d, not %d", path, stat.Uid, os.Geteuid())
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errorf("config.mode", "the live configuration %s is group-writable or world-writable (%04o)", path, info.Mode().Perm())
	}
	return nil
}

// validateStateRoot refuses a state-root path whose existing components are a
// symlink, a non-directory, or writable by another user. The walk stops at the
// first component owned by another user, which is the root-owned anchor of the
// system tree; it does not demand that /tmp or /var be owned by the test user.
func validateStateRoot(root string) error {
	path, err := filepath.Abs(root)
	if err != nil {
		return errorf("state.path", "resolve the state root %s: %v", root, err)
	}
	for {
		info, err := os.Lstat(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return errorf("state.read", "read the state root component %s: %v", path, err)
			}
		} else {
			if info.Mode()&os.ModeSymlink != 0 {
				return errorf("state.symlink", "the state root component %s is a symlink", path)
			}
			if !info.IsDir() {
				return errorf("state.type", "the state root component %s is not a directory", path)
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return errorf("state.owner", "the owner of the state root component %s cannot be determined", path)
			}
			if stat.Uid != uint32(os.Geteuid()) {
				return nil
			}
			if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
				return errorf("state.mode", "the state root component %s is writable by another user (%04o)", path, info.Mode().Perm())
			}
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}
