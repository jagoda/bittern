package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"bittern.invalid/agent/internal/release"
)

func loadTrustStore(options Options) (*release.TrustStore, error) {
	if options.TrustStore != nil {
		return options.TrustStore, nil
	}
	if options.TrustImageDir == "" {
		return nil, errorf("trust.missing", "the image trust store directory is empty")
	}
	return release.LoadTrustStore(options.TrustImageDir, options.TrustRotationDir)
}

// verifyManifest runs the release verifier and then the transaction-only
// predicates. A resume of the same candidate is accepted only when the active
// journal already names the same manifest digest and generation.
func verifyManifest(manifestBytes, signature []byte, store *release.TrustStore, committedGeneration int64, journal *Journal, deviceChannel string, platform Platform, runningSchema int64) (*release.Manifest, error) {
	if len(signature) == 0 {
		return nil, errorf("signature.missing", "the detached signature is missing")
	}
	if len(signature) != 64 {
		return nil, errorf("signature.length", "the detached signature is %d bytes, not 64", len(signature))
	}
	parsed, err := release.ParseManifest(manifestBytes)
	if err != nil {
		return nil, err
	}
	digest := sha256Hex(manifestBytes)
	if journal != nil && journal.CandidateManifestSHA256 != "" {
		if digest != journal.CandidateManifestSHA256 {
			return nil, errorf("journal.candidate-mismatch", "the staged manifest digest %s does not match the journal candidate %s", digest, journal.CandidateManifestSHA256)
		}
		if parsed.Generation != journal.CandidateGeneration {
			return nil, errorf("generation.resume-mismatch", "the staged generation %d does not match the journal candidate %d", parsed.Generation, journal.CandidateGeneration)
		}
	}
	manifest, err := release.VerifyManifest(manifestBytes, signature, store, release.KindAppBundle, committedGeneration)
	if err != nil {
		var releaseError *release.Error
		if journal != nil && errors.As(err, &releaseError) && releaseError.Rule == "generation.rollback" &&
			journal.CandidateManifestSHA256 == digest && journal.CandidateGeneration == parsed.Generation {
			manifest, err = release.VerifyManifest(manifestBytes, signature, store, release.KindAppBundle, parsed.Generation-1)
		}
		if err != nil {
			return nil, err
		}
	}
	if manifest.Channel != deviceChannel {
		return nil, errorf("channel.mismatch", "the manifest channel %q does not match the device channel %q", manifest.Channel, deviceChannel)
	}
	if err := verifyCompatibility(manifest, platform, runningSchema); err != nil {
		return nil, err
	}
	return manifest, nil
}

func verifyCompatibility(manifest *release.Manifest, platform Platform, runningSchema int64) error {
	if runtime.GOARCH != release.ArchAMD64 {
		return errorf("compatibility.arch", "the running architecture %q is not %q", runtime.GOARCH, release.ArchAMD64)
	}
	if platform.Generation < manifest.Compatibility.OSGenerationMin {
		return errorf("compatibility.os-generation", "the platform generation %d is older than the minimum %d", platform.Generation, manifest.Compatibility.OSGenerationMin)
	}
	if platform.Generation > manifest.Compatibility.OSGenerationMax {
		return errorf("compatibility.os-generation", "the platform generation %d is newer than the maximum %d", platform.Generation, manifest.Compatibility.OSGenerationMax)
	}
	if runningSchema < manifest.Compatibility.SchemaMin {
		return errorf("compatibility.schema", "the appliance schema %d is older than the minimum %d", runningSchema, manifest.Compatibility.SchemaMin)
	}
	if runningSchema > manifest.Compatibility.SchemaMax {
		return errorf("compatibility.schema", "the appliance schema %d is newer than the maximum %d", runningSchema, manifest.Compatibility.SchemaMax)
	}
	if UpdaterVersion < manifest.Compatibility.UpdaterMin {
		return errorf("compatibility.updater", "the updater version %d is older than the minimum %d", UpdaterVersion, manifest.Compatibility.UpdaterMin)
	}
	return nil
}

// verifyBundleFiles requires the exact manifest file set and verifies every
// path, type, size, digest and final mode. It is used for a promoted bundle and
// for the health gate.
func verifyBundleFiles(root string, manifest *release.Manifest) error {
	return verifyTree(root, manifest, nil, false)
}

// verifyStagedFiles verifies the extracted tree before promotion, compares the
// archive header mode with the manifest mode, then applies the final modes.
func verifyStagedFiles(root string, manifest *release.Manifest, headerModes map[string]fs.FileMode) error {
	return verifyTree(root, manifest, headerModes, true)
}

func verifyTree(root string, manifest *release.Manifest, headerModes map[string]fs.FileMode, applyModes bool) error {
	expected := make(map[string]release.File, len(manifest.Files))
	for _, file := range manifest.Files {
		expected[file.Path] = file
	}
	seen := make(map[string]bool, len(expected))
	var directories []string
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			directories = append(directories, current)
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errorf("file.type", "the extracted path %s is not a regular file", relative)
		}
		expectedFile, ok := expected[relative]
		if !ok {
			return errorf("file.extra", "the extracted file %s is not in the manifest", relative)
		}
		seen[relative] = true
		if info.Size() != expectedFile.Size {
			return errorf("file.size", "the extracted file %s is %d bytes, not %d", relative, info.Size(), expectedFile.Size)
		}
		digest, _, err := hashFile(current)
		if err != nil {
			return err
		}
		if digest != expectedFile.SHA256 {
			return errorf("file.digest", "the extracted file %s digest %s does not match %s", relative, digest, expectedFile.SHA256)
		}
		wantMode, err := parseMode(expectedFile.Mode)
		if err != nil {
			return errorf("file.mode", "the manifest mode for %s is invalid: %v", relative, err)
		}
		if wantMode&(os.ModeSetuid|os.ModeSetgid) != 0 {
			return errorf("file.mode", "the manifest mode for %s has a setuid or setgid bit", relative)
		}
		if headerModes != nil {
			headerMode, ok := headerModes[relative]
			if !ok {
				return errorf("file.missing", "the archive does not contain %s", relative)
			}
			if headerMode.Perm() != wantMode.Perm() || headerMode&(os.ModeSetuid|os.ModeSetgid) != 0 {
				return errorf("file.mode", "the archive mode %04o for %s does not match the manifest mode %04o", headerMode.Perm(), relative, wantMode.Perm())
			}
		}
		if applyModes {
			if err := os.Chmod(current, wantMode.Perm()); err != nil {
				return err
			}
		} else if info.Mode().Perm() != wantMode.Perm() {
			return errorf("file.mode", "the file %s has mode %04o, not %04o", relative, info.Mode().Perm(), wantMode.Perm())
		}
		return nil
	})
	if err != nil {
		return err
	}
	missing := make([]string, 0)
	for file := range expected {
		if !seen[file] {
			missing = append(missing, file)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return errorf("file.missing", "the extracted tree is missing %s", strings.Join(missing, ", "))
	}
	if applyModes {
		for _, directory := range directories {
			if err := os.Chmod(directory, 0o755); err != nil {
				return err
			}
		}
		if err := os.Chmod(root, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func parseMode(value string) (fs.FileMode, error) {
	if len(value) != 4 || value[0] != '0' {
		return 0, fmt.Errorf("mode %q is not four octal digits", value)
	}
	number, err := strconv.ParseUint(value, 8, 32)
	if err != nil {
		return 0, err
	}
	return fs.FileMode(number), nil
}

func readManifestFromBundle(directory string) ([]byte, *release.Manifest, error) {
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return nil, nil, err
	}
	manifest, err := release.ParseManifest(data)
	if err != nil {
		return nil, nil, err
	}
	return data, manifest, nil
}

// verifyBundle verifies a promoted bundle against the canonical manifest and
// signature supplied by the staged transaction.
func verifyBundle(directory string, manifestBytes, signature []byte, store *release.TrustStore, expected *release.Manifest) error {
	bundleManifest, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return errorf("bundle.manifest", "read the bundle manifest: %v", err)
	}
	if string(bundleManifest) != string(manifestBytes) {
		return errorf("bundle.conflict", "the bundle manifest differs from the verified manifest")
	}
	bundleSignature, err := os.ReadFile(filepath.Join(directory, "manifest.sig"))
	if err != nil {
		return errorf("bundle.signature", "read the bundle signature: %v", err)
	}
	if _, err := release.VerifyManifest(manifestBytes, bundleSignature, store, release.KindAppBundle, expected.Generation-1); err != nil {
		return err
	}
	extra, err := readExtraSignatures(directory)
	if err != nil {
		return err
	}
	if err := release.VerifyAdditionalSignatures(manifestBytes, extra, store); err != nil {
		return err
	}
	if expected.Channel == "" {
		return errorf("channel.mismatch", "the expected manifest has no channel")
	}
	filesRoot := filepath.Join(directory, "files")
	if info, err := os.Stat(filesRoot); err != nil || !info.IsDir() {
		return errorf("bundle.files", "the bundle has no files directory")
	}
	return verifyBundleFiles(filesRoot, expected)
}

func safeBundlePath(value string) (string, error) {
	if value == "" || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") {
		return "", errorf("archive.path", "the archive path %q is not a safe relative path", value)
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return "", errorf("archive.path", "the archive path %q contains a parent directory reference", value)
		}
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errorf("archive.path", "the archive path %q escapes the extraction root", value)
	}
	return clean, nil
}

func readExtraSignatures(directory string) (map[string][]byte, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	extra := make(map[string][]byte)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "manifest.sig.") {
			continue
		}
		keyID := strings.TrimPrefix(name, "manifest.sig.")
		if keyID == "" {
			return nil, errorf("signature.name", "the signature file %s has no key id", name)
		}
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return nil, err
		}
		if len(data) != 64 {
			return nil, errorf("signature.length", "the signature for key %s is %d bytes, not 64", keyID, len(data))
		}
		extra[keyID] = data
	}
	return extra, nil
}
