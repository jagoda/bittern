package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/release"
)

const (
	vectorDir              = "../../../tests/fixtures/release/manifest-v1"
	vectorPlaceholderKeyID = "2222222222222222222222222222222222222222222222222222222222222222"
)

// runCommand runs the tool in-process and captures its exit code and streams.
func runCommand(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func writeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func requireFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}

// outputLine returns the rest of the first line with the given prefix.
func outputLine(t *testing.T, output, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("output has no line with prefix %q:\n%s", prefix, output)
	return ""
}

// outputField returns the first whitespace-separated field after the prefix.
func outputField(t *testing.T, output, prefix string) string {
	t.Helper()
	rest := outputLine(t, output, prefix)
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		t.Fatalf("line with prefix %q has no value:\n%s", prefix, output)
	}
	return fields[0]
}

// newReleaseKey runs init and returns the key file, the trust directory and
// the key id it printed.
func newReleaseKey(t *testing.T) (keyPath, trustDir, keyID string) {
	t.Helper()
	base := t.TempDir()
	keyPath = filepath.Join(base, "release.key")
	trustDir = filepath.Join(base, "trust", "release-keys")
	code, stdout, stderr := runCommand(t, "", "init", "--key", keyPath, "--pub-dir", trustDir)
	if code != exitOK {
		t.Fatalf("init exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	return keyPath, trustDir, outputField(t, stdout, "init: key id ")
}

// vectorBytes reads the checked-in canonical manifest.
func vectorBytes(t *testing.T) []byte {
	t.Helper()
	return readFile(t, filepath.Join(vectorDir, "manifest.json"))
}

// vectorManifestWithKey writes the checked-in canonical manifest with the
// placeholder key id replaced by a real key id. The replacement keeps the
// document canonical: the value is still 64 lowercase hexadecimal characters.
func vectorManifestWithKey(t *testing.T, dir, keyID string) (path string, canonical []byte) {
	t.Helper()
	canonical = bytes.Replace(vectorBytes(t), []byte(vectorPlaceholderKeyID), []byte(keyID), 1)
	if !bytes.Contains(canonical, []byte(keyID)) {
		t.Fatal("the vector manifest does not contain the placeholder key id")
	}
	if _, err := release.ParseManifest(canonical); err != nil {
		t.Fatalf("the substituted vector is not a valid canonical manifest: %v", err)
	}
	path = filepath.Join(dir, "manifest.json")
	writeFile(t, path, canonical, 0o644)
	return path, canonical
}

// trustedPublicKey reads <key_id>.pub from a trust directory.
func trustedPublicKey(t *testing.T, trustDir, keyID string) ed25519.PublicKey {
	t.Helper()
	data := readFile(t, filepath.Join(trustDir, keyID+".pub"))
	if len(data) != ed25519.PublicKeySize {
		t.Fatalf("trust key %s/%s.pub is %d bytes", trustDir, keyID, len(data))
	}
	return ed25519.PublicKey(data)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// tarEntry describes one entry for the payload archive helpers.
type tarEntry struct {
	name     string
	mode     int64
	body     string
	typeflag byte
	linkname string
}

// writePayloadArchive writes a gzip-compressed tar and returns its bytes.
func writePayloadArchive(t *testing.T, path string, entries []tarEntry, compress bool) []byte {
	t.Helper()
	var buffer bytes.Buffer
	var target io.Writer = &buffer
	var gzipWriter *gzip.Writer
	if compress {
		gzipWriter = gzip.NewWriter(&buffer)
		target = gzipWriter
	}
	tarWriter := tar.NewWriter(target)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{
			Name:     entry.name,
			Mode:     entry.mode,
			Size:     int64(len(entry.body)),
			Typeflag: typeflag,
			Linkname: entry.linkname,
		}
		if typeflag != tar.TypeReg {
			header.Size = 0
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write tar header %s: %v", entry.name, err)
		}
		if typeflag == tar.TypeReg {
			if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
				t.Fatalf("write tar body %s: %v", entry.name, err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if gzipWriter != nil {
		if err := gzipWriter.Close(); err != nil {
			t.Fatalf("close gzip: %v", err)
		}
	}
	archive := buffer.Bytes()
	writeFile(t, path, archive, 0o644)
	return archive
}
