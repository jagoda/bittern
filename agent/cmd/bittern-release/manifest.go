package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bittern.invalid/agent/internal/release"
)

const manifestUsage = `usage: bittern-release manifest --payload ARCHIVE --version V --generation N
                          --source-commit HEX --key-id HEX
                          --os-generation-min N --os-generation-max N
                          --schema-min N --schema-max N --updater-min N --out PATH
                          [--channel stable|beta] [--kind app-bundle|os-image]
                          [--arch amd64] [--created RFC3339] [--dry-run]

Builds the canonical release manifest from a payload archive: the archive
digest and size, and the regular-file list with size, SHA-256 and mode, sorted
by path. Directories are not listed; the transaction creates parent directories.
The archive may be a plain tar or a gzip-compressed tar. The output is written
with the in-tree encoder and is byte-identical for the same inputs, so a second
run is a no-op.

  --payload ARCHIVE          payload archive the manifest describes
  --version V                release version, printable ASCII
  --generation N             app bundle generation, the anti-rollback counter
  --source-commit HEX        git commit the bundle was built from
  --key-id HEX               key id of the signer, from bittern-release key-id
  --os-generation-min N      oldest OS generation the bundle runs on
  --os-generation-max N      newest OS generation the bundle runs on
  --schema-min N             oldest appliance schema version the bundle can read
  --schema-max N             newest appliance schema version the bundle can read
  --updater-min N            oldest update helper version that can apply it
  --out PATH                 manifest output path, or - for stdout
  --channel stable|beta      release channel (default stable)
  --kind KIND                manifest kind (default app-bundle)
  --arch ARCH                architecture (default amd64)
  --created RFC3339          creation time; default now in UTC
  --dry-run                  build and report the manifest without writing it
`

func runManifest(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("manifest", manifestUsage, stderr)
	payloadPath := flags.String("payload", "", "payload archive")
	version := flags.String("version", "", "release version")
	generation := flags.Int64("generation", -1, "bundle generation")
	channel := flags.String("channel", release.ChannelStable, "release channel")
	sourceCommit := flags.String("source-commit", "", "source commit")
	keyID := flags.String("key-id", "", "signer key id")
	kind := flags.String("kind", release.KindAppBundle, "manifest kind")
	arch := flags.String("arch", release.ArchAMD64, "architecture")
	created := flags.String("created", "", "creation time")
	osGenerationMin := flags.Int64("os-generation-min", -1, "oldest OS generation")
	osGenerationMax := flags.Int64("os-generation-max", -1, "newest OS generation")
	schemaMin := flags.Int64("schema-min", -1, "oldest schema version")
	schemaMax := flags.Int64("schema-max", -1, "newest schema version")
	updaterMin := flags.Int64("updater-min", -1, "oldest updater version")
	outPath := flags.String("out", "", "manifest output path, or -")
	dryRun := flags.Bool("dry-run", false, "build without writing")
	flagArgs, positionals := splitArgs(flags, args)
	if proceed, code := parseFlags(flags, flagArgs); !proceed {
		return code
	}
	if !wantArgs(flags, positionals, 0) {
		return exitUsage
	}
	missing := make([]string, 0, 12)
	for _, required := range []struct {
		name  string
		value string
	}{
		{"--payload", *payloadPath},
		{"--version", *version},
		{"--source-commit", *sourceCommit},
		{"--key-id", *keyID},
		{"--out", *outPath},
	} {
		if required.value == "" {
			missing = append(missing, required.name)
		}
	}
	for _, required := range []struct {
		name  string
		value int64
	}{
		{"--generation", *generation},
		{"--os-generation-min", *osGenerationMin},
		{"--os-generation-max", *osGenerationMax},
		{"--schema-min", *schemaMin},
		{"--schema-max", *schemaMax},
		{"--updater-min", *updaterMin},
	} {
		if required.value < 0 {
			missing = append(missing, required.name)
		}
	}
	if len(missing) > 0 {
		flags.Usage()
		fmt.Fprintf(stderr, "manifest: missing required flag(s): %s\n", strings.Join(missing, ", "))
		return exitUsage
	}
	createdAt := *created
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}
	archive, err := readPayloadArchive(*payloadPath)
	if err != nil {
		return fail(stderr, "manifest", "%v", err)
	}
	manifest, canonical, err := buildManifest(archive, manifestSpec{
		Version:         *version,
		Generation:      *generation,
		Channel:         *channel,
		SourceCommit:    *sourceCommit,
		KeyID:           *keyID,
		Kind:            *kind,
		Arch:            *arch,
		Created:         createdAt,
		OSGenerationMin: *osGenerationMin,
		OSGenerationMax: *osGenerationMax,
		SchemaMin:       *schemaMin,
		SchemaMax:       *schemaMax,
		UpdaterMin:      *updaterMin,
	})
	if err != nil {
		return reportRejection(stderr, "manifest", err)
	}
	summary := stdout
	if *outPath == "-" {
		// stdout carries the exact manifest bytes, so the record goes to stderr.
		summary = stderr
	}
	fmt.Fprintf(summary, "manifest: payload %s (%d bytes, sha256 %s)\n",
		archive.path, archive.size, archive.digest)
	directories := "directories"
	if archive.directories == 1 {
		directories = "directory"
	}
	fmt.Fprintf(summary, "manifest: files %d (%d %s not listed)\n", len(manifest.Files), archive.directories, directories)
	fmt.Fprintf(summary, "manifest: %s %s generation %d channel %s key id %s\n",
		manifest.Kind, manifest.Version, manifest.Generation, manifest.Channel, manifest.Signing.KeyID)
	fmt.Fprintf(summary, "manifest: created %s, sha256 %s (%d bytes)\n",
		manifest.Created, manifestDigest(canonical), len(canonical))
	if *dryRun {
		fmt.Fprintf(summary, "manifest: dry run, no manifest written\n")
		return exitOK
	}
	if *outPath == "-" {
		if _, err := stdout.Write(canonical); err != nil {
			return fail(stderr, "manifest", "write the manifest to stdout: %v", err)
		}
		return exitOK
	}
	absoluteOut, err := filepath.Abs(*outPath)
	if err != nil {
		return fail(stderr, "manifest", "resolve the output path: %v", err)
	}
	existing, readErr := os.ReadFile(absoluteOut)
	if readErr == nil && bytes.Equal(existing, canonical) {
		fmt.Fprintf(summary, "manifest: unchanged %s\n", absoluteOut)
		return exitOK
	}
	if readErr != nil && !os.IsNotExist(readErr) {
		return fail(stderr, "manifest", "read the existing manifest %s: %v", absoluteOut, readErr)
	}
	if err := writeManifest(absoluteOut, canonical); err != nil {
		return fail(stderr, "manifest", "%v", err)
	}
	fmt.Fprintf(summary, "manifest: wrote %s\n", absoluteOut)
	return exitOK
}

// manifestSpec is the manifest metadata the caller supplies. The payload facts
// and file list are read from the archive so the manifest and the payload
// cannot disagree.
type manifestSpec struct {
	Version         string
	Generation      int64
	Channel         string
	SourceCommit    string
	KeyID           string
	Kind            string
	Arch            string
	Created         string
	OSGenerationMin int64
	OSGenerationMax int64
	SchemaMin       int64
	SchemaMax       int64
	UpdaterMin      int64
}

// buildManifest returns the validated manifest and its canonical bytes.
func buildManifest(archive *payloadArchive, spec manifestSpec) (*release.Manifest, []byte, error) {
	manifest := &release.Manifest{
		ManifestVersion: release.ManifestVersion,
		Kind:            spec.Kind,
		Product:         release.Product,
		Version:         spec.Version,
		Generation:      spec.Generation,
		Channel:         spec.Channel,
		Created:         spec.Created,
		SourceCommit:    spec.SourceCommit,
		Signing:         release.Signing{KeyID: spec.KeyID},
		Compatibility: release.Compatibility{
			Arch:            spec.Arch,
			OSGenerationMin: spec.OSGenerationMin,
			OSGenerationMax: spec.OSGenerationMax,
			SchemaMin:       spec.SchemaMin,
			SchemaMax:       spec.SchemaMax,
			UpdaterMin:      spec.UpdaterMin,
		},
		Payload: release.Payload{
			Filename: archive.filename,
			Size:     archive.size,
			SHA256:   archive.digest,
		},
		Files: archive.files,
	}
	canonical, err := manifest.Canonical()
	if err != nil {
		return nil, nil, err
	}
	return manifest, canonical, nil
}

// payloadArchive is the archive facts the manifest records and the file list.
type payloadArchive struct {
	path        string
	filename    string
	size        int64
	digest      string
	files       []release.File
	directories int64
}

// countingReader counts the bytes read so the tool can prove the archive did
// not change size while it was hashed.
type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count += int64(n)
	return n, err
}

// readPayloadArchive streams the archive once: it hashes every byte, lists the
// regular files with their modes, and rejects anything the manifest cannot
// describe, including symlinks, hardlinks, device nodes and FIFOs.
func readPayloadArchive(path string) (*payloadArchive, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve the payload path: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, fmt.Errorf("read the payload archive %s: %w", absolute, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("the payload archive %s is a symlink; pass the archive file itself", absolute)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("the payload archive %s is not a regular file", absolute)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, fmt.Errorf("open the payload archive %s: %w", absolute, err)
	}
	defer file.Close()

	counter := &countingReader{reader: file}
	hasher := sha256.New()
	buffered := bufio.NewReader(io.TeeReader(counter, hasher))
	magic, err := buffered.Peek(2)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("read the payload archive %s: %w", absolute, err)
	}
	compressed := len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b
	var stream io.Reader = buffered
	if compressed {
		reader, err := gzip.NewReader(buffered)
		if err != nil {
			return nil, fmt.Errorf("read the gzip payload archive %s: %w", absolute, err)
		}
		defer reader.Close()
		stream = reader
	}
	files, directories, err := readTar(stream)
	if err != nil {
		return nil, fmt.Errorf("read the payload archive %s: %w", absolute, err)
	}
	// Drain the rest of the archive and the file so the digest covers every
	// byte, including the gzip trailer and any tar padding.
	if _, err := io.Copy(io.Discard, stream); err != nil {
		return nil, fmt.Errorf("read the payload archive %s: %w", absolute, err)
	}
	if _, err := io.Copy(io.Discard, buffered); err != nil {
		return nil, fmt.Errorf("read the payload archive %s: %w", absolute, err)
	}
	if counter.count != info.Size() {
		return nil, fmt.Errorf("the payload archive %s changed while it was read (%d bytes, expected %d)",
			absolute, counter.count, info.Size())
	}
	sum := hasher.Sum(nil)
	return &payloadArchive{
		path:        absolute,
		filename:    filepath.Base(absolute),
		size:        counter.count,
		digest:      hex.EncodeToString(sum),
		files:       files,
		directories: directories,
	}, nil
}

// readTar collects the regular files from a tar stream, sorted by path.
func readTar(reader io.Reader) ([]release.File, int64, error) {
	files := make([]release.File, 0)
	directories := int64(0)
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, err
		}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			if header.Mode&^int64(0o777) != 0 {
				return nil, 0, fmt.Errorf("the payload entry %s has mode %04o with setuid, setgid or sticky bits; only 0644-style modes are allowed",
					header.Name, header.Mode)
			}
			digest := sha256.New()
			size, err := io.Copy(digest, tarReader)
			if err != nil {
				return nil, 0, err
			}
			if size != header.Size {
				return nil, 0, fmt.Errorf("the payload entry %s is %d bytes, not the %d bytes its header declares",
					header.Name, size, header.Size)
			}
			files = append(files, release.File{
				Path:   header.Name,
				Size:   size,
				SHA256: hex.EncodeToString(digest.Sum(nil)),
				Mode:   fmt.Sprintf("0%03o", header.Mode&0o777),
			})
		case tar.TypeDir:
			directories++
		default:
			return nil, 0, fmt.Errorf("the payload entry %s is not a regular file or directory; the manifest installs only regular files", header.Name)
		}
	}
	sort.Slice(files, func(left, right int) bool {
		return files[left].Path < files[right].Path
	})
	return files, directories, nil
}

// writeManifest writes the canonical bytes atomically with mode 0644.
func writeManifest(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create the output directory %s: %w", dir, err)
	}
	temporary, err := os.CreateTemp(dir, ".bittern-release-manifest-*")
	if err != nil {
		return fmt.Errorf("create a temporary manifest file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func(cause error) error {
		temporary.Close()
		os.Remove(temporaryPath)
		return cause
	}
	if err := temporary.Chmod(0o644); err != nil {
		return cleanup(fmt.Errorf("set the manifest mode: %w", err))
	}
	if _, err := temporary.Write(data); err != nil {
		return cleanup(fmt.Errorf("write the manifest: %w", err))
	}
	if err := temporary.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync the manifest: %w", err))
	}
	if err := temporary.Close(); err != nil {
		os.Remove(temporaryPath)
		return fmt.Errorf("close the manifest: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		os.Remove(temporaryPath)
		return fmt.Errorf("replace the manifest %s: %w", path, err)
	}
	return nil
}
