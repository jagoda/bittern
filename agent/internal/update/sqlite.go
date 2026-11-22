package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultSQLitePath is the fixed sqlite3 command line tool. The adapter never
// resolves sqlite3 through PATH while running as root.
const DefaultSQLitePath = "/usr/bin/sqlite3"

// DefaultSnapshotTimeout bounds one backup or restore command.
const DefaultSnapshotTimeout = 5 * time.Minute

// SQLiteSnapshot implements DataSnapshot with the sqlite3 command line tool.
// The .backup command uses SQLite's online backup API, so it produces a
// consistent copy while Frigate is still running; no Go SQLite driver is added.
type SQLiteSnapshot struct {
	LivePath string
	SQLite   string
	Timeout  time.Duration
	Command  func(context.Context, string, ...string) ([]byte, error)
}

// Create copies the live Frigate database into destination through the sqlite3
// online backup command. A missing tool is a specific, fail-closed error that
// names the tool and the package to install.
func (s *SQLiteSnapshot) Create(destination string) (SnapshotInfo, error) {
	tool, err := s.tool()
	if err != nil {
		return SnapshotInfo{}, err
	}
	if err := s.checkLiveDatabase(); err != nil {
		return SnapshotInfo{}, err
	}
	quoted, err := sqlitePathArgument(destination)
	if err != nil {
		return SnapshotInfo{}, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return SnapshotInfo{}, fmt.Errorf("update: create the snapshot directory: %w", err)
	}
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return SnapshotInfo{}, err
	}
	output, err := s.run(tool, s.LivePath, ".backup "+quoted)
	if err != nil {
		return SnapshotInfo{}, errorf("snapshot.backup", "the sqlite3 .backup command failed: %s", commandMessage(output, err))
	}
	info, err := os.Stat(destination)
	if err != nil {
		return SnapshotInfo{}, errorf("snapshot.backup", "the sqlite3 .backup command left no snapshot at %s: %v", destination, err)
	}
	if !info.Mode().IsRegular() {
		return SnapshotInfo{}, errorf("snapshot.backup", "the snapshot %s is not a regular file", destination)
	}
	digest, size, err := hashFileDigest(destination)
	if err != nil {
		return SnapshotInfo{}, errorf("snapshot.backup", "hash the snapshot %s: %v", destination, err)
	}
	return SnapshotInfo{
		State:  "snapshot",
		Path:   destination,
		SHA256: fmt.Sprintf("%x", digest),
		Size:   size,
	}, nil
}

// Restore replaces the live database from a verified snapshot. The engine
// stops Frigate before this call. Stale WAL, shared-memory, and journal
// sidecars are removed first, so a pre-stop write cannot replay over the
// restored image.
func (s *SQLiteSnapshot) Restore(snapshot string) error {
	tool, err := s.tool()
	if err != nil {
		return err
	}
	if err := s.checkLiveParent(); err != nil {
		return err
	}
	quoted, err := sqlitePathArgument(snapshot)
	if err != nil {
		return err
	}
	info, err := os.Lstat(snapshot)
	if errors.Is(err, os.ErrNotExist) {
		return errorf("snapshot.database", "the database snapshot %s is missing", snapshot)
	}
	if err != nil {
		return errorf("snapshot.database", "inspect the database snapshot %s: %v", snapshot, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errorf("snapshot.database", "the database snapshot %s is not a regular file", snapshot)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(s.LivePath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errorf("snapshot.sidecar", "remove the database sidecar %s%s: %v", s.LivePath, suffix, err)
		}
	}
	output, err := s.run(tool, s.LivePath, ".restore "+quoted)
	if err != nil {
		return errorf("snapshot.restore", "the sqlite3 .restore command failed: %s", commandMessage(output, err))
	}
	if _, err := os.Stat(s.LivePath); err != nil {
		return errorf("snapshot.restore", "the sqlite3 .restore command left no database at %s: %v", s.LivePath, err)
	}
	return fsyncDir(filepath.Dir(s.LivePath))
}

func (s *SQLiteSnapshot) tool() (string, error) {
	path := s.SQLite
	if path == "" {
		path = DefaultSQLitePath
	}
	if !filepath.IsAbs(path) {
		return "", errorf("snapshot.tool", "the sqlite3 path %q is not absolute", path)
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", errorf("snapshot.sqlite-missing", "the sqlite3 command line tool is not installed at %s; the preparation kit must install the sqlite3 package", path)
	}
	if err != nil {
		return "", errorf("snapshot.tool", "inspect the sqlite3 tool %s: %v", path, err)
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return "", errorf("snapshot.tool", "the sqlite3 path %s is not a regular file", path)
	}
	return path, nil
}

func (s *SQLiteSnapshot) checkLiveDatabase() error {
	if err := s.checkLiveParent(); err != nil {
		return err
	}
	info, err := os.Lstat(s.LivePath)
	if errors.Is(err, os.ErrNotExist) {
		return errorf("snapshot.database", "the Frigate database %s is missing; refusing to update without a database backup", s.LivePath)
	}
	if err != nil {
		return errorf("snapshot.database", "inspect the Frigate database %s: %v", s.LivePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errorf("snapshot.database", "the Frigate database %s is not a regular file", s.LivePath)
	}
	return nil
}

func (s *SQLiteSnapshot) checkLiveParent() error {
	if s.LivePath == "" {
		return errorf("snapshot.database", "the Frigate database path is empty")
	}
	if !filepath.IsAbs(s.LivePath) {
		return errorf("snapshot.database", "the Frigate database path %q is not absolute", s.LivePath)
	}
	parent := filepath.Dir(s.LivePath)
	info, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		return errorf("snapshot.database", "the Frigate media directory %s is missing", parent)
	}
	if err != nil {
		return errorf("snapshot.database", "inspect the Frigate media directory %s: %v", parent, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errorf("snapshot.database", "the Frigate media path %s is not a regular directory", parent)
	}
	return nil
}

func (s *SQLiteSnapshot) run(name string, args ...string) ([]byte, error) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultSnapshotTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := s.Command
	if command == nil {
		command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		}
	}
	return command(ctx, name, args...)
}

func sqlitePathArgument(path string) (string, error) {
	if path == "" {
		return "", errorf("snapshot.path", "the snapshot path is empty")
	}
	if !filepath.IsAbs(path) {
		return "", errorf("snapshot.path", "the snapshot path %q is not absolute", path)
	}
	if strings.ContainsAny(path, "'\"\\\x00\n\r") {
		return "", errorf("snapshot.path", "the snapshot path %q contains a character the sqlite3 dot command cannot quote", path)
	}
	return "'" + path + "'", nil
}

func commandMessage(output []byte, err error) string {
	message := strings.TrimSpace(string(output))
	if message == "" && err != nil {
		message = err.Error()
	}
	if message == "" {
		message = "the command failed without output"
	}
	if len(message) > 2000 {
		message = message[:2000] + "..."
	}
	return message
}
