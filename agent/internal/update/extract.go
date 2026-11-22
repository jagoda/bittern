package update

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

const (
	maxArchiveEntries    = 10000
	maxArchiveEntryBytes = 64 << 20
	maxArchiveBytes      = 128 << 20
)

// extractArchive extracts a files-only gzip tar into directory. Directories are
// structural: the manifest lists files only, so explicit and implicit
// directories, including empty ones, are allowed and preserved. The extraction
// bounds the entry count, each entry size, and the total uncompressed bytes
// before writing, and refuses a name containing a control character. It accepts
// only regular files and directories, rejects duplicate entries and every
// special type, and writes files 0600 and directories 0700. The final modes are
// applied only after the digest check.
func extractArchive(archive, directory string) (map[string]fs.FileMode, error) {
	return extractArchiveWithSpace(archive, directory, defaultMinFreeBytes)
}

func extractArchiveWithSpace(archive, directory string, minFree uint64) (map[string]fs.FileMode, error) {
	free, err := extractionFreeSpace(directory)
	if err != nil {
		return nil, err
	}
	if free < minFree {
		return nil, errorf("archive.space", "the extraction target has %d bytes free, less than required %d", free, minFree)
	}
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
	entries := 0
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errorf("archive.read", "read the payload archive: %v", err)
		}
		entries++
		if entries > maxArchiveEntries {
			return nil, errorf("archive.entries", "the archive has more than %d entries", maxArchiveEntries)
		}
		clean, err := safeBundlePath(header.Name)
		if err != nil {
			return nil, err
		}
		if seen[clean] {
			return nil, errorf("archive.duplicate", "the archive contains %s twice", clean)
		}
		seen[clean] = true
		if header.Size < 0 {
			return nil, errorf("archive.size", "the archive entry %s has a negative size", clean)
		}
		if header.Size > maxArchiveEntryBytes {
			return nil, errorf("archive.size", "the archive entry %s is %d bytes, over the %d byte limit", clean, header.Size, maxArchiveEntryBytes)
		}
		total += header.Size
		if total > maxArchiveBytes {
			return nil, errorf("archive.total-size", "the archive expands to more than %d bytes", maxArchiveBytes)
		}
		if uint64(total) > free-minFree {
			return nil, errorf("archive.space", "the archive would leave less than %d bytes free under %s", minFree, directory)
		}
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
			if mode&(os.ModeSetuid|os.ModeSetgid) != 0 {
				return nil, errorf("file.mode", "the archive entry %s has a setuid or setgid bit", clean)
			}
			if err := copyReaderAtomic(target, reader, header.Size, 0o600); err != nil {
				return nil, errorf("archive.size", "write the archive entry %s: %v", clean, err)
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

func extractionFreeSpace(directory string) (uint64, error) {
	path := filepath.Dir(directory)
	for {
		var stat syscall.Statfs_t
		err := syscall.Statfs(path, &stat)
		if err == nil {
			return stat.Bavail * uint64(stat.Bsize), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return 0, errorf("archive.space", "read free space under %s: %v", path, err)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, errorf("archive.space", "read free space under %s: %v", path, err)
		}
		path = parent
	}
}

// archiveModes re-reads the staged payload and returns the regular-file modes.
// The verify-files step uses it after a resume, when the extraction step's
// in-memory map is gone. It applies the same entry and total bounds so a resume
// cannot rebuild an unbounded map.
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
	entries := 0
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		entries++
		if entries > maxArchiveEntries {
			return nil, errorf("archive.entries", "the archive has more than %d entries", maxArchiveEntries)
		}
		if header.Size < 0 {
			return nil, errorf("archive.size", "the archive entry %q has a negative size", header.Name)
		}
		if header.Size > maxArchiveEntryBytes {
			return nil, errorf("archive.size", "the archive entry %q is %d bytes, over the %d byte limit", header.Name, header.Size, maxArchiveEntryBytes)
		}
		total += header.Size
		if total > maxArchiveBytes {
			return nil, errorf("archive.total-size", "the archive expands to more than %d bytes", maxArchiveBytes)
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
