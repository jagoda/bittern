package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeSQLiteTool(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "sqlite3")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func snapshotPathFromArgument(argument, prefix string) string {
	return strings.TrimSuffix(strings.TrimPrefix(argument, prefix+" '"), "'")
}

func TestSQLiteSnapshotCreateUsesTheBackupCommand(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "media", "frigate.db")
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("live-database"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := fakeSQLiteTool(t, dir)
	destination := filepath.Join(dir, "backup", "frigate", "frigate.db")
	var gotName string
	var gotArgs []string
	adapter := &SQLiteSnapshot{
		LivePath: live,
		SQLite:   tool,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			gotName = name
			gotArgs = append([]string{}, args...)
			path := snapshotPathFromArgument(args[1], ".backup")
			return nil, os.WriteFile(path, []byte("snapshot-bytes"), 0o600)
		},
	}
	info, err := adapter.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	if gotName != tool {
		t.Fatalf("command = %s, want %s", gotName, tool)
	}
	if len(gotArgs) != 2 || gotArgs[0] != live || gotArgs[1] != ".backup '"+destination+"'" {
		t.Fatalf("args = %v, want the live database and the quoted .backup path", gotArgs)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("snapshot-bytes")))
	if info.SHA256 != digest || info.Size != int64(len("snapshot-bytes")) {
		t.Fatalf("snapshot info = %+v, want digest %s and size %d", info, digest, len("snapshot-bytes"))
	}
	if info.State != "snapshot" || info.Path != destination {
		t.Fatalf("snapshot info = %+v", info)
	}
}

func TestSQLiteSnapshotCreateFailsClosedWhenTheToolIsMissing(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "media", "frigate.db")
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("live-database"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	adapter := &SQLiteSnapshot{
		LivePath: live,
		SQLite:   filepath.Join(dir, "missing-sqlite3"),
		Command: func(context.Context, string, ...string) ([]byte, error) {
			called = true
			return nil, nil
		},
	}
	_, err := adapter.Create(filepath.Join(dir, "backup", "frigate.db"))
	if err == nil {
		t.Fatal("a missing sqlite3 tool was accepted")
	}
	if called {
		t.Fatal("the adapter ran a command without the sqlite3 tool")
	}
	if !strings.Contains(err.Error(), "sqlite3") || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("error = %v, want a specific missing sqlite3 error", err)
	}
	requireRule(t, err, "snapshot.sqlite-missing")
}

func TestSQLiteSnapshotRejectsRelativeToolPath(t *testing.T) {
	adapter := &SQLiteSnapshot{LivePath: "/tmp/frigate.db", SQLite: "sqlite3"}
	_, err := adapter.Create("/tmp/snapshot.db")
	requireRule(t, err, "snapshot.tool")
}

func TestSQLiteSnapshotRestoreReplacesDatabaseAndRemovesSidecars(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "media", "frigate.db")
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("candidate-database"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.WriteFile(live+suffix, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := filepath.Join(dir, "backup", "frigate.db")
	if err := os.MkdirAll(filepath.Dir(snapshot), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshot, []byte("previous-database"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := fakeSQLiteTool(t, dir)
	var gotArgs []string
	adapter := &SQLiteSnapshot{
		LivePath: live,
		SQLite:   tool,
		Command: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			gotArgs = append([]string{}, args...)
			return nil, os.WriteFile(live, []byte("previous-database"), 0o600)
		},
	}
	if err := adapter.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	if len(gotArgs) != 2 || gotArgs[0] != live || gotArgs[1] != ".restore '"+snapshot+"'" {
		t.Fatalf("args = %v, want the live database and the quoted .restore path", gotArgs)
	}
	data, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "previous-database" {
		t.Fatalf("live database = %q, want the restored bytes", data)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(live + suffix); !os.IsNotExist(err) {
			t.Fatalf("sidecar %s survived the restore: %v", suffix, err)
		}
	}
}

func TestSQLiteSnapshotRejectsMissingLiveDatabase(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "media", "frigate.db")
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := &SQLiteSnapshot{LivePath: live, SQLite: fakeSQLiteTool(t, dir)}
	_, err := adapter.Create(filepath.Join(dir, "backup", "frigate.db"))
	requireRule(t, err, "snapshot.database")
}

func TestSQLiteSnapshotRejectsUnquotableDestination(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "media", "frigate.db")
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("live"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := &SQLiteSnapshot{LivePath: live, SQLite: fakeSQLiteTool(t, dir)}
	_, err := adapter.Create(filepath.Join(dir, "backup", "fr'igate.db"))
	requireRule(t, err, "snapshot.path")
}

func TestPreparationKitInstallsSQLite3(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	install, err := os.ReadFile(filepath.Join(root, "packaging", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(install), "sqlite3") {
		t.Fatal("packaging/install.sh does not install sqlite3")
	}
	packages, err := os.ReadFile(filepath.Join(root, "image", "packages.txt"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(string(packages), "\n") {
		if strings.TrimSpace(line) == "sqlite3" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("image/packages.txt does not name sqlite3")
	}
}
