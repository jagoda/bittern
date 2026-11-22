package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/release"
)

// writeBundleTree writes a small built tree with a nested path and executable
// and data modes.
func writeBundleTree(t *testing.T) string {
	t.Helper()
	tree := t.TempDir()
	writeFile(t, filepath.Join(tree, "usr", "bin", "bittern"), []byte("#!/bin/sh\nbittern\n"), 0o755)
	writeFile(t, filepath.Join(tree, "usr", "bin", "bitternd"), []byte("#!/bin/sh\nbitternd\n"), 0o755)
	writeFile(t, filepath.Join(tree, "usr", "lib", "bittern", "version"), []byte("generation 31\n"), 0o644)
	return tree
}

// packArgs returns the full flag set for a deterministic pack.
func packArgs(tree, outDir, keyID string) []string {
	return []string{
		"pack",
		"--tree", tree,
		"--out", outDir,
		"--version", "0.0.0-beta.31",
		"--generation", "31",
		"--channel", release.ChannelBeta,
		"--source-commit", strings.Repeat("3", 40),
		"--key-id", keyID,
		"--os-generation-min", "1",
		"--os-generation-max", "1",
		"--schema-min", "1",
		"--schema-max", "2",
		"--updater-min", "1",
		"--created", "2026-11-22T00:00:00Z",
		"--epoch", "0",
	}
}

func TestPackBuildsDeterministicPayloadAndManifest(t *testing.T) {
	keyPath, trustDir, keyID := newReleaseKey(t)
	tree := writeBundleTree(t)
	firstOut := t.TempDir()
	secondOut := t.TempDir()

	code, stdout, stderr := runCommand(t, "", packArgs(tree, firstOut, keyID)...)
	if code != exitOK {
		t.Fatalf("pack exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	code, _, stderr = runCommand(t, "", packArgs(tree, secondOut, keyID)...)
	if code != exitOK {
		t.Fatalf("the second pack exited %d: %s", code, stderr)
	}

	firstArchive := readFile(t, filepath.Join(firstOut, bundlePayloadName))
	secondArchive := readFile(t, filepath.Join(secondOut, bundlePayloadName))
	if !bytes.Equal(firstArchive, secondArchive) {
		t.Fatal("the same tree and arguments produced different payload bytes")
	}
	firstManifest := readFile(t, filepath.Join(firstOut, "manifest.json"))
	secondManifest := readFile(t, filepath.Join(secondOut, "manifest.json"))
	if !bytes.Equal(firstManifest, secondManifest) {
		t.Fatal("the same tree and arguments produced different manifest bytes")
	}

	manifest, err := release.ParseManifest(firstManifest)
	if err != nil {
		t.Fatalf("the packed manifest does not parse: %v", err)
	}
	if manifest.Payload.Filename != bundlePayloadName {
		t.Fatalf("payload.filename = %q, want %q", manifest.Payload.Filename, bundlePayloadName)
	}
	if manifest.Payload.Size != int64(len(firstArchive)) {
		t.Fatalf("payload.size = %d, want %d", manifest.Payload.Size, len(firstArchive))
	}
	if manifest.Payload.SHA256 != sha256Hex(firstArchive) {
		t.Fatalf("payload.sha256 = %s, want %s", manifest.Payload.SHA256, sha256Hex(firstArchive))
	}
	if manifest.Generation != 31 || manifest.Channel != release.ChannelBeta ||
		manifest.Version != "0.0.0-beta.31" || manifest.SourceCommit != strings.Repeat("3", 40) {
		t.Fatalf("unexpected manifest identity: %+v", manifest)
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

	entries := readBundleArchive(t, firstArchive)
	if len(entries) != len(manifest.Files) {
		t.Fatalf("archive has %d entries, manifest lists %d files", len(entries), len(manifest.Files))
	}
	for index, file := range manifest.Files {
		entry := entries[index]
		if entry.name != file.Path {
			t.Fatalf("archive entry %d is %q, manifest file is %q", index, entry.name, file.Path)
		}
		if entry.size != file.Size {
			t.Fatalf("archive entry %s is %d bytes, manifest says %d", file.Path, entry.size, file.Size)
		}
		if entry.sha256 != file.SHA256 {
			t.Fatalf("archive entry %s digest %s does not match the manifest %s", file.Path, entry.sha256, file.SHA256)
		}
		if entry.mode != file.Mode {
			t.Fatalf("archive entry %s mode %s does not match the manifest %s", file.Path, entry.mode, file.Mode)
		}
	}
	if len(entries) != 3 {
		t.Fatalf("archive has %d entries, want 3", len(entries))
	}
	if entries[0].name != "usr/bin/bittern" || entries[0].mode != "0755" {
		t.Fatalf("the archive is not sorted or lost the executable mode: %+v", entries[0])
	}

	// The packer never receives the private key; signing is the separate step.
	if _, err := os.Lstat(filepath.Join(firstOut, "manifest.json.sig")); !os.IsNotExist(err) {
		t.Fatalf("pack wrote a signature without a key: %v", err)
	}
	manifestPath := filepath.Join(firstOut, "manifest.json")
	code, stdout, stderr = runCommand(t, "", "sign", manifestPath, "--key", keyPath, "--yes")
	if code != exitOK {
		t.Fatalf("sign exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	code, stdout, stderr = runCommand(t, "", "verify", manifestPath, manifestPath+".sig", "--trust-dir", trustDir)
	if code != exitOK {
		t.Fatalf("verify rejected the packed manifest: %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

func TestPackRejectsAnUnsafeTree(t *testing.T) {
	_, _, keyID := newReleaseKey(t)
	cases := []struct {
		name  string
		setup func(t *testing.T, tree string)
		want  string
	}{
		{
			name: "symlink",
			setup: func(t *testing.T, tree string) {
				target := filepath.Join(tree, "usr", "bin", "bittern")
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("bitternd", target); err != nil {
					t.Fatal(err)
				}
			},
			want: "symlink",
		},
		{
			name: "trust store path",
			setup: func(t *testing.T, tree string) {
				writeFile(t, filepath.Join(tree, "usr", "lib", "bittern", "trust", "release-keys", "x.pub"), []byte("x"), 0o644)
			},
			want: "trust",
		},
		{
			name: "empty tree",
			setup: func(t *testing.T, tree string) {
				if err := os.RemoveAll(filepath.Join(tree, "usr")); err != nil {
					t.Fatal(err)
				}
			},
			want: "no regular files",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			tree := writeBundleTree(t)
			test.setup(t, tree)
			out := t.TempDir()
			code, _, stderr := runCommand(t, "", packArgs(tree, out, keyID)...)
			if code == exitOK {
				t.Fatalf("pack accepted a %s tree", test.name)
			}
			if !strings.Contains(stderr, test.want) {
				t.Fatalf("stderr does not contain %q:\n%s", test.want, stderr)
			}
			if _, err := os.Lstat(filepath.Join(out, bundlePayloadName)); !os.IsNotExist(err) {
				t.Fatalf("a rejected tree still wrote a payload: %v", err)
			}
		})
	}
}

func TestPackDryRunWritesNothing(t *testing.T) {
	_, _, keyID := newReleaseKey(t)
	tree := writeBundleTree(t)
	out := filepath.Join(t.TempDir(), "missing-out")
	args := append(packArgs(tree, out, keyID), "--dry-run")
	code, stdout, stderr := runCommand(t, "", args...)
	if code != exitOK {
		t.Fatalf("pack --dry-run exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dry run") || !strings.Contains(stdout, "sha256") {
		t.Fatalf("pack --dry-run did not report the planned payload:\n%s", stdout)
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Fatalf("pack --dry-run created the output directory: %v", err)
	}
}

func TestPackRemovesAStaleSignature(t *testing.T) {
	_, _, keyID := newReleaseKey(t)
	tree := writeBundleTree(t)
	out := t.TempDir()
	code, _, stderr := runCommand(t, "", packArgs(tree, out, keyID)...)
	if code != exitOK {
		t.Fatalf("pack exited %d: %s", code, stderr)
	}
	signaturePath := filepath.Join(out, "manifest.json.sig")
	writeFile(t, signaturePath, bytes.Repeat([]byte{7}, 64), 0o644)
	code, _, stderr = runCommand(t, "", packArgs(tree, out, keyID)...)
	if code != exitOK {
		t.Fatalf("the second pack exited %d: %s", code, stderr)
	}
	if _, err := os.Lstat(signaturePath); !os.IsNotExist(err) {
		t.Fatalf("pack kept a signature that cannot describe the new manifest: %v", err)
	}
}

// archiveEntry is one regular file read back from the packed tar.
type archiveEntry struct {
	name   string
	mode   string
	size   int64
	sha256 string
}

// readBundleArchive reads the gzip tar and rejects any entry that is not a
// regular file, so the files-only rule is asserted rather than assumed.
func readBundleArchive(t *testing.T, data []byte) []archiveEntry {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("read the payload gzip stream: %v", err)
	}
	defer reader.Close()
	tarReader := tar.NewReader(reader)
	entries := make([]archiveEntry, 0)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read the payload tar: %v", err)
		}
		if header.Typeflag != tar.TypeReg {
			t.Fatalf("the payload contains a non-regular entry %s with type %d", header.Name, header.Typeflag)
		}
		digest := sha256.New()
		size, err := io.Copy(digest, tarReader)
		if err != nil {
			t.Fatalf("read the payload entry %s: %v", header.Name, err)
		}
		entries = append(entries, archiveEntry{
			name:   header.Name,
			mode:   "0" + strconvOctal(header.Mode),
			size:   size,
			sha256: hex.EncodeToString(digest.Sum(nil)),
		})
	}
	return entries
}

// strconvOctal returns three octal digits without a dependency on fmt.
func strconvOctal(mode int64) string {
	const digits = "01234567"
	value := mode & 0o777
	return string([]byte{
		digits[(value>>6)&0o7],
		digits[(value>>3)&0o7],
		digits[value&0o7],
	})
}
