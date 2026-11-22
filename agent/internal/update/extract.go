package update

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// extractArchive extracts a files-only gzip tar into directory. It accepts only
// regular files and directories, rejects duplicate entries and every special
// type, and writes files 0600 and directories 0700. The final modes are applied
// only after the digest check.
func extractArchive(archive, directory string) (map[string]fs.FileMode, error) {
	file, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, errorf("archive.gzip", "the payload is not a valid gzip stream: %v", err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	if err := removeAll(directory); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, err
	}
	modes := make(map[string]fs.FileMode)
	seen := make(map[string]bool)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errorf("archive.read", "read the payload archive: %v", err)
		}
		clean, err := safeBundlePath(header.Name)
		if err != nil {
			return nil, err
		}
		if seen[clean] {
			return nil, errorf("archive.duplicate", "the archive contains %s twice", clean)
		}
		seen[clean] = true
		target := filepath.Join(directory, filepath.FromSlash(clean))
		mode := header.FileInfo().Mode()
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return nil, err
			}
			if err := os.Chmod(target, 0o700); err != nil {
				return nil, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 {
				return nil, errorf("archive.size", "the archive entry %s has a negative size", clean)
			}
			if mode&(os.ModeSetuid|os.ModeSetgid) != 0 {
				return nil, errorf("file.mode", "the archive entry %s has a setuid or setgid bit", clean)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return nil, err
			}
			if err := os.Chmod(filepath.Dir(target), 0o700); err != nil {
				return nil, err
			}
			data, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
			if err != nil {
				return nil, err
			}
			if int64(len(data)) != header.Size {
				return nil, errorf("archive.size", "the archive entry %s is truncated", clean)
			}
			if err := writeFileAtomic(target, data, 0o600); err != nil {
				return nil, err
			}
			modes[clean] = mode
		case tar.TypeSymlink, tar.TypeLink:
			return nil, errorf("archive.type", "the archive entry %s is a link, not a regular file", clean)
		case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
			return nil, errorf("archive.type", "the archive entry %s is a device or FIFO", clean)
		default:
			return nil, errorf("archive.type", "the archive entry %s has unsupported type %q", clean, header.Typeflag)
		}
	}
	return modes, nil
}

// archiveModes re-reads the staged payload and returns the regular-file modes.
// The verify-files step uses it after a resume, when the extraction step's
// in-memory map is gone.
func archiveModes(archive string) (map[string]fs.FileMode, error) {
	file, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, errorf("archive.gzip", "the payload is not a valid gzip stream: %v", err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	modes := make(map[string]fs.FileMode)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		clean, err := safeBundlePath(header.Name)
		if err != nil {
			return nil, err
		}
		modes[clean] = header.FileInfo().Mode()
	}
	return modes, nil
}
