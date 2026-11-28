package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bittern.invalid/agent/internal/release"
)

// bundlePayloadName is the exact application bundle payload asset name from
// ADR 0031. The packer does not accept another name.
const bundlePayloadName = "bittern-app.tar.gz"

const packUsage = `usage: bittern-release pack --tree DIR --out DIR --version V --generation N
                          --source-commit HEX --key-id HEX
                          --os-generation-min N --os-generation-max N
                          --schema-min N --schema-max N --updater-min N
                          [--channel beta|stable] [--created RFC3339] [--epoch N]
                          [--dry-run]

Packs a built file tree into the application bundle payload and its canonical
manifest. The payload is a files-only gzip tar: entries are sorted by path, the
owner and group are 0, the mode comes from the tree, and the timestamp is the
epoch. Directories are not archived; the transaction creates parent directories.
The same tree and arguments produce byte-identical payload and manifest bytes.

The manifest is built from the archive bytes, so payload.filename, size,
sha256 and files describe exactly what the packer wrote. A regular file at
usr/lib/bittern/units/<name> is a declared bundle unit: the packer derives its
live path, digest, mode, owner, enablement, and reload flag, and emits manifest
version 2. Without a unit the manifest stays version 1. The packer never needs
a private key: sign the manifest with the existing sign command as a separate
step.

  --tree DIR                 built file tree to archive
  --out DIR                  output directory for bittern-app.tar.gz and manifest.json
  --version V                release version, printable ASCII
  --generation N             app bundle generation, the anti-rollback counter
  --source-commit HEX        git commit the bundle was built from
  --key-id HEX               key id of the signer, from bittern-release key-id
  --os-generation-min N      oldest OS generation the bundle runs on
  --os-generation-max N      newest OS generation the bundle runs on
  --schema-min N             oldest appliance schema version the bundle can read
  --schema-max N             newest appliance schema version the bundle can read
  --updater-min N            oldest update helper version that can apply it
  --channel beta|stable      release channel (default beta)
  --created RFC3339          creation time; default now in UTC
  --epoch N                  tar and gzip timestamp in seconds (default 0)
  --dry-run                  build and report without writing either output
`

func runPack(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("pack", packUsage, stderr)
	treePath := flags.String("tree", "", "built file tree")
	outDir := flags.String("out", "", "output directory")
	version := flags.String("version", "", "release version")
	generation := flags.Int64("generation", -1, "bundle generation")
	channel := flags.String("channel", release.ChannelBeta, "release channel")
	sourceCommit := flags.String("source-commit", "", "source commit")
	keyID := flags.String("key-id", "", "signer key id")
	created := flags.String("created", "", "creation time")
	osGenerationMin := flags.Int64("os-generation-min", -1, "oldest OS generation")
	osGenerationMax := flags.Int64("os-generation-max", -1, "newest OS generation")
	schemaMin := flags.Int64("schema-min", -1, "oldest schema version")
	schemaMax := flags.Int64("schema-max", -1, "newest schema version")
	updaterMin := flags.Int64("updater-min", -1, "oldest updater version")
	epoch := flags.Int64("epoch", 0, "tar and gzip timestamp")
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
		{"--tree", *treePath},
		{"--out", *outDir},
		{"--version", *version},
		{"--source-commit", *sourceCommit},
		{"--key-id", *keyID},
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
		fmt.Fprintf(stderr, "pack: missing required flag(s): %s\n", strings.Join(missing, ", "))
		return exitUsage
	}
	if *epoch < 0 {
		return fail(stderr, "pack", "--epoch must not be negative")
	}
	createdAt := *created
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}
	absoluteTree, err := filepath.Abs(*treePath)
	if err != nil {
		return fail(stderr, "pack", "resolve the tree path: %v", err)
	}
	absoluteOut, err := filepath.Abs(*outDir)
	if err != nil {
		return fail(stderr, "pack", "resolve the output directory: %v", err)
	}
	if inside(absoluteTree, absoluteOut) {
		return fail(stderr, "pack", "the output directory %s is inside the tree %s", absoluteOut, absoluteTree)
	}
	files, err := collectTree(absoluteTree)
	if err != nil {
		return fail(stderr, "pack", "%v", err)
	}
	spec := manifestSpec{
		Version:         *version,
		Generation:      *generation,
		Channel:         *channel,
		SourceCommit:    *sourceCommit,
		KeyID:           *keyID,
		Kind:            release.KindAppBundle,
		Arch:            release.ArchAMD64,
		Created:         createdAt,
		OSGenerationMin: *osGenerationMin,
		OSGenerationMax: *osGenerationMax,
		SchemaMin:       *schemaMin,
		SchemaMax:       *schemaMax,
		UpdaterMin:      *updaterMin,
	}
	if err := validateTreeAsManifest(absoluteTree, files, spec); err != nil {
		return fail(stderr, "pack", "%v", err)
	}
	targetOut := absoluteOut
	cleanup := func() {}
	if *dryRun {
		temporary, err := os.MkdirTemp("", "bittern-pack-")
		if err != nil {
			return fail(stderr, "pack", "create the dry-run directory: %v", err)
		}
		targetOut = temporary
		cleanup = func() { os.RemoveAll(temporary) }
	}
	defer cleanup()
	if !*dryRun {
		if err := os.MkdirAll(targetOut, 0o755); err != nil {
			return fail(stderr, "pack", "create the output directory %s: %v", targetOut, err)
		}
	}
	archivePath := filepath.Join(targetOut, bundlePayloadName)
	if err := writeBundleArchive(absoluteTree, archivePath, files, *epoch); err != nil {
		return fail(stderr, "pack", "%v", err)
	}
	archive, err := readPayloadArchive(archivePath)
	if err != nil {
		return fail(stderr, "pack", "%v", err)
	}
	manifest, canonical, err := buildManifest(archive, spec)
	if err != nil {
		return reportRejection(stderr, "pack", err)
	}
	manifestPath := filepath.Join(targetOut, "manifest.json")
	if err := writeManifest(manifestPath, canonical); err != nil {
		return fail(stderr, "pack", "%v", err)
	}
	// A signature describes one exact manifest. A signature left from an
	// earlier run cannot verify this manifest and must not travel beside it.
	if err := os.Remove(manifestPath + ".sig"); err != nil && !os.IsNotExist(err) {
		return fail(stderr, "pack", "remove the stale manifest signature: %v", err)
	}
	fmt.Fprintf(stdout, "pack: tree %s (%d files, epoch %d)\n", absoluteTree, len(files), *epoch)
	fmt.Fprintf(stdout, "pack: payload %s (%d bytes, sha256 %s)\n", archivePath, archive.size, archive.digest)
	fmt.Fprintf(stdout, "pack: manifest %s (sha256 %s, %d bytes)\n", manifestPath, manifestDigest(canonical), len(canonical))
	fmt.Fprintf(stdout, "pack: %s %s generation %d channel %s key id %s\n",
		manifest.Kind, manifest.Version, manifest.Generation, manifest.Channel, manifest.Signing.KeyID)
	if *dryRun {
		fmt.Fprintf(stdout, "pack: dry run, no payload or manifest written\n")
	}
	return exitOK
}

// treeFile is one regular file from the built tree.
type treeFile struct {
	path string
	mode fs.FileMode
	size int64
}

// collectTree lists the regular files in the tree, sorted by slash-separated
// path, and rejects symlinks, special files and unsafe modes. It does not
// follow a symlink, and an empty tree is a failure because it cannot be a
// useful application bundle.
func collectTree(root string) ([]treeFile, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("read the tree %s: %w", root, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("the tree %s is a symlink; pass the tree directory itself", root)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("the tree %s is not a directory", root)
	}
	files := make([]treeFile, 0)
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if entryInfo.IsDir() {
			return nil
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("the tree entry %s is a symlink; the payload holds regular files only", relative)
		}
		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("the tree entry %s is not a regular file; the payload holds regular files only", relative)
		}
		if entryInfo.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return fmt.Errorf("the tree entry %s has a setuid, setgid or sticky bit; only 0644-style modes are allowed", relative)
		}
		files = append(files, treeFile{path: relative, mode: entryInfo.Mode().Perm(), size: entryInfo.Size()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read the tree %s: %w", root, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("the tree %s has no regular files", root)
	}
	sort.Slice(files, func(left, right int) bool {
		return files[left].path < files[right].path
	})
	return files, nil
}

// validateTreeAsManifest applies the manifest path and mode rules before the
// archive is written, so an unsafe tree cannot leave a payload behind.
func validateTreeAsManifest(root string, files []treeFile, spec manifestSpec) error {
	manifestFiles := make([]release.File, len(files))
	for index, file := range files {
		manifestFiles[index] = release.File{
			Path:   file.path,
			Size:   file.size,
			SHA256: strings.Repeat("0", 64),
			Mode:   fmt.Sprintf("0%03o", file.mode.Perm()),
		}
	}
	units := make([]release.Unit, 0)
	for index, file := range files {
		if pathpkg.Dir(file.path) != bundleUnitSourceDir {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.path)))
		if err != nil {
			return fmt.Errorf("read the unit source %s: %w", file.path, err)
		}
		name := pathpkg.Base(file.path)
		enabled, err := unitEnabled(name, string(content))
		if err != nil {
			return fmt.Errorf("unit %s: %w", file.path, err)
		}
		digest := sha256.Sum256(content)
		manifestFiles[index].SHA256 = hex.EncodeToString(digest[:])
		reload := true
		units = append(units, release.Unit{
			Name:    name,
			Source:  file.path,
			Path:    "/etc/systemd/system/" + name,
			Mode:    fmt.Sprintf("0%03o", file.mode.Perm()),
			Owner:   "root:root",
			SHA256:  hex.EncodeToString(digest[:]),
			Enabled: enabled,
			Reload:  &reload,
		})
	}
	version := int64(release.ManifestVersionV1)
	var subsystems *release.Subsystems
	if len(units) > 0 && spec.Kind == release.KindAppBundle {
		version = release.ManifestVersionV2
		subsystems = &release.Subsystems{Units: units}
	}
	provisional := &release.Manifest{
		ManifestVersion: version,
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
			Filename: bundlePayloadName,
			Size:     0,
			SHA256:   strings.Repeat("0", 64),
		},
		Files:      manifestFiles,
		Subsystems: subsystems,
	}
	if err := provisional.Validate(); err != nil {
		return fmt.Errorf("the tree is not a valid application bundle: %w", err)
	}
	return nil
}

// writeBundleArchive writes the files-only gzip tar atomically. The header
// timestamp, owner and group are fixed, and the gzip header carries no name.
func writeBundleArchive(root, archivePath string, files []treeFile, epoch int64) error {
	directory := filepath.Dir(archivePath)
	temporary, err := os.CreateTemp(directory, ".bittern-pack-*")
	if err != nil {
		return fmt.Errorf("create a temporary payload file in %s: %w", directory, err)
	}
	temporaryPath := temporary.Name()
	fail := func(cause error) error {
		temporary.Close()
		os.Remove(temporaryPath)
		return cause
	}
	if err := temporary.Chmod(0o644); err != nil {
		return fail(fmt.Errorf("set the payload mode: %w", err))
	}
	gzipWriter, err := gzip.NewWriterLevel(temporary, gzip.BestCompression)
	if err != nil {
		return fail(fmt.Errorf("create the gzip writer: %w", err))
	}
	gzipWriter.Header.ModTime = time.Unix(epoch, 0).UTC()
	gzipWriter.Header.Name = ""
	gzipWriter.Header.Comment = ""
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	modified := time.Unix(epoch, 0).UTC()
	for _, file := range files {
		header := &tar.Header{
			Name:     file.path,
			Mode:     int64(file.mode.Perm()),
			Size:     file.size,
			Typeflag: tar.TypeReg,
			ModTime:  modified,
			Format:   tar.FormatPAX,
			Uid:      0,
			Gid:      0,
			Uname:    "",
			Gname:    "",
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			tarWriter.Close()
			gzipWriter.Close()
			return fail(fmt.Errorf("write the payload header for %s: %w", file.path, err))
		}
		source, err := os.Open(filepath.Join(root, filepath.FromSlash(file.path)))
		if err != nil {
			tarWriter.Close()
			gzipWriter.Close()
			return fail(fmt.Errorf("open the tree file %s: %w", file.path, err))
		}
		written, copyErr := io.CopyN(tarWriter, source, file.size)
		closeErr := source.Close()
		if copyErr != nil {
			tarWriter.Close()
			gzipWriter.Close()
			return fail(fmt.Errorf("write the tree file %s: %w", file.path, copyErr))
		}
		if closeErr != nil {
			tarWriter.Close()
			gzipWriter.Close()
			return fail(fmt.Errorf("close the tree file %s: %w", file.path, closeErr))
		}
		if written != file.size {
			tarWriter.Close()
			gzipWriter.Close()
			return fail(fmt.Errorf("the tree file %s is %d bytes, not the %d bytes read from the tree", file.path, written, file.size))
		}
	}
	if err := tarWriter.Close(); err != nil {
		gzipWriter.Close()
		return fail(fmt.Errorf("finish the payload tar: %w", err))
	}
	if err := gzipWriter.Close(); err != nil {
		return fail(fmt.Errorf("finish the payload gzip stream: %w", err))
	}
	if err := temporary.Sync(); err != nil {
		return fail(fmt.Errorf("sync the payload: %w", err))
	}
	if err := temporary.Close(); err != nil {
		os.Remove(temporaryPath)
		return fmt.Errorf("close the payload: %w", err)
	}
	if err := os.Rename(temporaryPath, archivePath); err != nil {
		os.Remove(temporaryPath)
		return fmt.Errorf("replace the payload %s: %w", archivePath, err)
	}
	return nil
}

// inside reports whether candidate is the root or a path below it.
func inside(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
