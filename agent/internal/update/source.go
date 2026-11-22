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
	extra := make(map[string][]byte)
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, nil, nil, err
	}
	prefix := "manifest.json.sig."
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) {
			continue
		}
		keyID := strings.TrimPrefix(name, prefix)
		if keyID == "" || strings.ContainsAny(keyID, `/\`) {
			return nil, nil, nil, errorf("signature.name", "the signature file %s is not named manifest.json.sig.<key_id>", name)
		}
		data, err := readBoundedRegular(filepath.Join(s.Dir, name), signatureLimit)
		if err != nil {
			return nil, nil, nil, err
		}
		extra[keyID] = data
	}
	return manifest, signature, extra, nil
}

// OpenPayload opens the named archive after rejecting a symlink or a
// non-regular file. The caller bounds the copy with the manifest size.
func (s LocalSource) OpenPayload(filename string) (io.ReadCloser, error) {
	if filename == "" || filename == "." || filename == ".." ||
		strings.ContainsAny(filename, `/\`) || filepath.Base(filename) != filename {
		return nil, errorf("payload.filename", "the payload filename %q is not an archive file name", filename)
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
