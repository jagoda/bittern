package update

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Name returns the stable source label recorded in the journal and history.
func (s LocalSource) Name() string { return "local-file" }

// SourcePath returns the local directory a transaction started from. The
// journal records it so a resume reads the same source after a request record
// is replaced or removed.
func (s LocalSource) SourcePath() string { return s.Dir }

// LoadManifest reads manifest.json and the detached signature files from the
// local directory. It performs no network access and bounds every read.
func (s LocalSource) LoadManifest() ([]byte, []byte, map[string][]byte, error) {
	if s.Dir == "" {
		return nil, nil, nil, errorf("source.missing", "the local bundle directory is empty")
	}
	manifestPath := filepath.Join(s.Dir, "manifest.json")
	manifest, err := readBoundedRegular(manifestPath, manifestLimit)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil, errorf("manifest.missing", "the manifest %s is missing", manifestPath)
		}
		return nil, nil, nil, err
	}
	signature, err := readBoundedRegular(manifestPath+".sig", signatureLimit)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil, errorf("signature.missing", "the signature %s is missing", manifestPath+".sig")
		}
		return nil, nil, nil, err
	}
	extra, err := readAdditionalSignatures(s.Dir, "manifest.json.sig.")
	if err != nil {
		return nil, nil, nil, err
	}
	return manifest, signature, extra, nil
}

const (
	maxAdditionalSignatures     = 32
	maxAdditionalSignatureBytes = 1024
)

// readAdditionalSignatures reads manifest.json.sig.<key_id> files in bounded
// batches. The entry count and total bytes are enforced while reading, before
// staging or a journal entry can grow with attacker-controlled signatures.
func readAdditionalSignatures(directory, prefix string) (map[string][]byte, error) {
	handle, err := os.Open(directory)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	extra := make(map[string][]byte)
	count := 0
	total := 0
	for {
		entries, readErr := handle.ReadDir(64)
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) {
				continue
			}
			count++
			if count > maxAdditionalSignatures {
				return nil, errorf("signature.count", "the source has more than %d additional signature files", maxAdditionalSignatures)
			}
			keyID := strings.TrimPrefix(name, prefix)
			if keyID == "" || strings.ContainsAny(keyID, `/\`) {
				return nil, errorf("signature.name", "the signature file %s is not named manifest.json.sig.<key_id>", name)
			}
			data, err := readBoundedRegular(filepath.Join(directory, name), signatureLimit)
			if err != nil {
				return nil, err
			}
			total += len(data)
			if total > maxAdditionalSignatureBytes {
				return nil, errorf("signature.total", "the additional signature files exceed the %d byte limit", maxAdditionalSignatureBytes)
			}
			extra[keyID] = data
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return extra, nil
}

// OpenPayload opens the named archive after rejecting a symlink or a
// non-regular file. The caller bounds the copy with the manifest size.
func (s LocalSource) OpenPayload(filename string) (io.ReadCloser, error) {
	if filename == "" || filename == "." || filename == ".." ||
		strings.ContainsAny(filename, `/\`) || filepath.Base(filename) != filename {
		return nil, errorf("payload.filename", "the payload filename %q is not an archive file name", filename)
	}
	if err := validatePayloadFilename(filename); err != nil {
		return nil, err
	}
	path := filepath.Join(s.Dir, filename)
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errorf("payload.missing", "the payload %s is missing", path)
		}
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errorf("payload.symlink", "the payload %s is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return nil, errorf("payload.type", "the payload %s is not a regular file", path)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !os.SameFile(info, after) {
		_ = file.Close()
		return nil, errorf("payload.symlink", "the payload %s changed while it was opened", path)
	}
	return file, nil
}

func readBoundedRegular(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errorf("source.symlink", "%s is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return nil, errorf("source.type", "%s is not a regular file", path)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, after) {
		return nil, errorf("source.symlink", "%s changed while it was opened", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errorf("source.size", "%s exceeds the %d byte limit", path, limit)
	}
	return data, nil
}
