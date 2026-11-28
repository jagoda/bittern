package main

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/release"
)

// manifestArgs returns the full flag set for a manifest build.
func manifestArgs(payloadPath, outPath, keyID string) []string {
	return []string{
		"manifest",
		"--payload", payloadPath,
		"--version", "1.2.3",
		"--generation", "4",
		"--channel", "stable",
		"--source-commit", strings.Repeat("3", 40),
		"--key-id", keyID,
		"--os-generation-min", "1",
		"--os-generation-max", "1",
		"--schema-min", "1",
		"--schema-max", "2",
		"--updater-min", "1",
		"--created", "2026-11-21T00:00:00Z",
		"--out", outPath,
	}
}

// testPayloadEntries returns a tar with two files out of path order and one
// directory, so the test proves the tool sorts and skips directories.
func testPayloadEntries() []tarEntry {
	return []tarEntry{
		{name: "usr/lib/bittern/b", mode: 0o755, body: "#!/bin/sh\n"},
		{name: "usr/lib/bittern/", mode: 0o755, typeflag: tar.TypeDir},
		{name: "usr/lib/bittern/a", mode: 0o644, body: "hello\n"},
	}
}

func TestManifestBuildsCanonicalManifest(t *testing.T) {
	_, _, keyID := newReleaseKey(t)
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "bittern-example.tar.gz")
	archive := writePayloadArchive(t, payloadPath, testPayloadEntries(), true)
	outPath := filepath.Join(dir, "manifest.json")

	code, stdout, stderr := runCommand(t, "", manifestArgs(payloadPath, outPath, keyID)...)
	if code != exitOK {
		t.Fatalf("manifest exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "manifest: files 2 (1 directory not listed)") {
		t.Fatalf("manifest did not report the file and directory counts:\n%s", stdout)
	}
	canonical := readFile(t, outPath)
	if len(canonical) == 0 || canonical[len(canonical)-1] != '}' {
		t.Fatalf("the manifest does not end with the canonical object: %q", canonical)
	}
	manifest, err := release.ParseManifest(canonical)
	if err != nil {
		t.Fatalf("the built manifest does not parse: %v", err)
	}
	roundTrip, err := manifest.Canonical()
	if err != nil {
		t.Fatalf("re-encode the built manifest: %v", err)
	}
	if !bytes.Equal(roundTrip, canonical) {
		t.Fatal("the built manifest is not canonical")
	}
	if manifest.Product != release.Product || manifest.ManifestVersion != release.ManifestVersionV1 {
		t.Fatalf("product = %q, manifest_version = %d", manifest.Product, manifest.ManifestVersion)
	}
	if manifest.Kind != release.KindAppBundle || manifest.Version != "1.2.3" || manifest.Generation != 4 ||
		manifest.Channel != release.ChannelStable || manifest.SourceCommit != strings.Repeat("3", 40) ||
		manifest.Created != "2026-11-21T00:00:00Z" || manifest.Signing.KeyID != keyID {
		t.Fatalf("unexpected manifest fields: %+v", manifest)
	}
	if manifest.Compatibility != (release.Compatibility{
		Arch:            release.ArchAMD64,
		OSGenerationMin: 1,
		OSGenerationMax: 1,
		SchemaMin:       1,
		SchemaMax:       2,
		UpdaterMin:      1,
	}) {
		t.Fatalf("unexpected compatibility: %+v", manifest.Compatibility)
	}
	if manifest.Payload.Filename != "bittern-example.tar.gz" {
		t.Fatalf("payload.filename = %q", manifest.Payload.Filename)
	}
	if manifest.Payload.Size != int64(len(archive)) {
		t.Fatalf("payload.size = %d, want %d", manifest.Payload.Size, len(archive))
	}
	if manifest.Payload.SHA256 != sha256Hex(archive) {
		t.Fatalf("payload.sha256 = %s, want %s", manifest.Payload.SHA256, sha256Hex(archive))
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(manifest.Files))
	}
	first, second := manifest.Files[0], manifest.Files[1]
	if first.Path != "usr/lib/bittern/a" || second.Path != "usr/lib/bittern/b" {
		t.Fatalf("files are not sorted by path: %q, %q", first.Path, second.Path)
	}
	if first.Mode != "0644" || second.Mode != "0755" {
		t.Fatalf("modes = %s, %s", first.Mode, second.Mode)
	}
	if first.Size != int64(len("hello\n")) || first.SHA256 != sha256Hex([]byte("hello\n")) {
		t.Fatalf("unexpected first file: %+v", first)
	}
	if second.Size != int64(len("#!/bin/sh\n")) {
		t.Fatalf("unexpected second file: %+v", second)
	}
	requireFileMode(t, outPath, 0o644)

	// The same inputs produce the same canonical bytes, so a rerun is a no-op.
	before := readFile(t, outPath)
	code, stdout, stderr = runCommand(t, "", manifestArgs(payloadPath, outPath, keyID)...)
	if code != exitOK {
		t.Fatalf("the second manifest run exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "unchanged") {
		t.Fatalf("the second run did not report the unchanged manifest:\n%s", stdout)
	}
	if !bytes.Equal(readFile(t, outPath), before) {
		t.Fatal("the second run rewrote the manifest with different bytes")
	}
}

func TestManifestDryRunAndStdout(t *testing.T) {
	_, _, keyID := newReleaseKey(t)
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.tar.gz")
	writePayloadArchive(t, payloadPath, testPayloadEntries(), true)

	dryOut := filepath.Join(dir, "dry.json")
	args := append(manifestArgs(payloadPath, dryOut, keyID), "--dry-run")
	code, stdout, stderr := runCommand(t, "", args...)
	if code != exitOK {
		t.Fatalf("manifest --dry-run exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run") {
		t.Fatalf("manifest --dry-run did not report the plan:\n%s", stdout)
	}
	if _, err := os.Lstat(dryOut); !os.IsNotExist(err) {
		t.Fatalf("manifest --dry-run wrote a file: %v", err)
	}

	code, stdout, stderr = runCommand(t, "", manifestArgs(payloadPath, "-", keyID)...)
	if code != exitOK {
		t.Fatalf("manifest --out - exited %d: %s", code, stderr)
	}
	if !strings.HasPrefix(stdout, `{"channel":`) {
		t.Fatalf("manifest --out - did not put the manifest on stdout:\n%s", stdout)
	}
	if _, err := release.ParseManifest([]byte(stdout)); err != nil {
		t.Fatalf("the stdout manifest does not parse: %v", err)
	}
	if !strings.Contains(stderr, "manifest: payload") {
		t.Fatalf("manifest --out - did not put the record on stderr:\n%s", stderr)
	}
}

func TestManifestAcceptsPlainTar(t *testing.T) {
	_, _, keyID := newReleaseKey(t)
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.tar")
	writePayloadArchive(t, payloadPath, testPayloadEntries(), false)
	outPath := filepath.Join(dir, "manifest.json")
	code, _, stderr := runCommand(t, "", manifestArgs(payloadPath, outPath, keyID)...)
	if code != exitOK {
		t.Fatalf("manifest exited %d: %s", code, stderr)
	}
	manifest, err := release.ParseManifest(readFile(t, outPath))
	if err != nil {
		t.Fatalf("the plain-tar manifest does not parse: %v", err)
	}
	if len(manifest.Files) != 2 || manifest.Payload.Filename != "payload.tar" {
		t.Fatalf("unexpected plain-tar manifest: %+v", manifest)
	}
}

func TestManifestRejectsUnsupportedArchiveEntries(t *testing.T) {
	cases := []struct {
		name    string
		entries []tarEntry
		want    string
	}{
		{
			name:    "symlink",
			entries: []tarEntry{{name: "usr/lib/bittern/link", mode: 0o777, typeflag: tar.TypeSymlink, linkname: "../../etc/passwd"}},
			want:    "not a regular file or directory",
		},
		{
			name:    "hardlink",
			entries: []tarEntry{{name: "usr/lib/bittern/link", mode: 0o644, typeflag: tar.TypeLink, linkname: "usr/lib/bittern/a"}},
			want:    "not a regular file or directory",
		},
		{
			name:    "setuid file",
			entries: []tarEntry{{name: "usr/lib/bittern/root", mode: 0o4755, body: "binary"}},
			want:    "setuid, setgid or sticky bits",
		},
		{
			name:    "absolute path",
			entries: []tarEntry{{name: "/usr/lib/bittern/a", mode: 0o644, body: "hello\n"}},
			want:    "manifest.path",
		},
		{
			name:    "parent path",
			entries: []tarEntry{{name: "../usr/lib/bittern/a", mode: 0o644, body: "hello\n"}},
			want:    "manifest.path",
		},
		{
			name: "duplicate path",
			entries: []tarEntry{
				{name: "usr/lib/bittern/a", mode: 0o644, body: "one"},
				{name: "usr/lib/bittern/a", mode: 0o644, body: "two"},
			},
			want: "manifest.duplicate-file",
		},
		{
			name:    "trust store path",
			entries: []tarEntry{{name: "usr/lib/bittern/trust/release-keys/x.pub", mode: 0o644, body: "x"}},
			want:    "manifest.trust-path",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, keyID := newReleaseKey(t)
			dir := t.TempDir()
			payloadPath := filepath.Join(dir, "payload.tar.gz")
			writePayloadArchive(t, payloadPath, test.entries, true)
			outPath := filepath.Join(dir, "manifest.json")
			code, _, stderr := runCommand(t, "", manifestArgs(payloadPath, outPath, keyID)...)
			if code == exitOK {
				t.Fatalf("manifest accepted a %s archive", test.name)
			}
			if !strings.Contains(stderr, test.want) {
				t.Fatalf("stderr does not contain %q:\n%s", test.want, stderr)
			}
			if _, err := os.Lstat(outPath); !os.IsNotExist(err) {
				t.Fatalf("a rejected archive still wrote a manifest: %v", err)
			}
		})
	}
}

func TestManifestRequiredFlags(t *testing.T) {
	_, _, keyID := newReleaseKey(t)
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.tar.gz")
	writePayloadArchive(t, payloadPath, testPayloadEntries(), true)
	args := manifestArgs(payloadPath, filepath.Join(dir, "manifest.json"), keyID)
	trimmed := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		if args[index] == "--schema-min" {
			index++
			continue
		}
		trimmed = append(trimmed, args[index])
	}
	code, _, stderr := runCommand(t, "", trimmed...)
	if code != exitUsage {
		t.Fatalf("manifest exited %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "--schema-min") || !strings.Contains(stderr, "missing required flag") {
		t.Fatalf("stderr does not name the missing flag:\n%s", stderr)
	}
}

func TestManifestRejectsInvalidFields(t *testing.T) {
	_, _, keyID := newReleaseKey(t)
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.tar.gz")
	writePayloadArchive(t, payloadPath, testPayloadEntries(), true)

	t.Run("non-ASCII version", func(t *testing.T) {
		args := manifestArgs(payloadPath, filepath.Join(dir, "manifest.json"), keyID)
		for index := range args {
			if args[index] == "--version" {
				args[index+1] = "1.2.3-caf\u00e9"
			}
		}
		code, _, stderr := runCommand(t, "", args...)
		requireRejection(t, code, exitNonCanonical, stderr, "manifest.version")
	})
	t.Run("unknown kind", func(t *testing.T) {
		args := append(manifestArgs(payloadPath, filepath.Join(dir, "manifest.json"), keyID), "--kind", "container")
		code, _, stderr := runCommand(t, "", args...)
		requireRejection(t, code, exitManifestRejected, stderr, "manifest.kind")
	})
}

func TestManifestCreatedDefaultsToNow(t *testing.T) {
	_, _, keyID := newReleaseKey(t)
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.tar.gz")
	writePayloadArchive(t, payloadPath, testPayloadEntries(), true)
	args := manifestArgs(payloadPath, filepath.Join(dir, "manifest.json"), keyID)
	trimmed := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		if args[index] == "--created" {
			index++
			continue
		}
		trimmed = append(trimmed, args[index])
	}
	before := time.Now().UTC().Add(-time.Minute)
	code, _, stderr := runCommand(t, "", trimmed...)
	if code != exitOK {
		t.Fatalf("manifest exited %d: %s", code, stderr)
	}
	manifest, err := release.ParseManifest(readFile(t, filepath.Join(dir, "manifest.json")))
	if err != nil {
		t.Fatalf("parse the manifest: %v", err)
	}
	created, err := time.Parse(time.RFC3339, manifest.Created)
	if err != nil {
		t.Fatalf("created is not RFC 3339: %v", err)
	}
	if created.Before(before) {
		t.Fatalf("created %s is not the current time", manifest.Created)
	}
}
