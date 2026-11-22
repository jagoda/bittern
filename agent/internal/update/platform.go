package update

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"syscall"
)

// PlatformMetadataPath is the image-owned platform metadata file. It lives
// next to the image trust store, so it is read-only and survives only as part
// of the running image. The path is provisional until the image ADR fixes the
// metadata layout; changing it is one constant.
const PlatformMetadataPath = "/usr/lib/bittern/platform.json"

const platformMetadataLimit = 4 << 10

type platformMetadata struct {
	SchemaVersion int   `json:"schema_version"`
	Generation    int64 `json:"generation"`
}

// FilePlatformProvider reads the running platform generation from a root-owned
// image metadata file. It fails closed when the file is missing, is a symlink,
// is not root-owned, or is writable by anyone else. It never reads the OS
// generation from appliance.yaml, an environment variable, or a unit property.
type FilePlatformProvider struct {
	Path string
	// UID is the expected owner. It defaults to the effective uid, which is
	// root in production. Tests set it to exercise the owner check.
	UID func() int
}

// Platform returns the frozen platform generation and its fixed source label.
func (p *FilePlatformProvider) Platform() (Platform, error) {
	path := p.Path
	if path == "" {
		path = PlatformMetadataPath
	}
	uid := os.Geteuid()
	if p.UID != nil {
		uid = p.UID()
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Platform{}, errorf("platform.missing", "the platform metadata %s is missing; the image must write it", path)
	}
	if err != nil {
		return Platform{}, errorf("platform.read", "inspect the platform metadata %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Platform{}, errorf("platform.type", "the platform metadata %s is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return Platform{}, errorf("platform.type", "the platform metadata %s is not a regular file", path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != uid {
		return Platform{}, errorf("platform.owner", "the platform metadata %s has owner uid %d, not %d", path, stat.Uid, uid)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return Platform{}, errorf("platform.mode", "the platform metadata %s has mode %04o, which is group- or other-writable", path, info.Mode().Perm())
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return Platform{}, errorf("platform.read", "open the platform metadata %s: %v", path, err)
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return Platform{}, errorf("platform.read", "stat the platform metadata %s: %v", path, err)
	}
	if !os.SameFile(info, after) {
		return Platform{}, errorf("platform.type", "the platform metadata %s changed while it was opened", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, platformMetadataLimit+1))
	if err != nil {
		return Platform{}, errorf("platform.read", "read the platform metadata %s: %v", path, err)
	}
	if len(data) > platformMetadataLimit {
		return Platform{}, errorf("platform.size", "the platform metadata %s exceeds the %d byte limit", path, platformMetadataLimit)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var metadata platformMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return Platform{}, errorf("platform.parse", "decode the platform metadata %s: %v", path, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Platform{}, errorf("platform.parse", "the platform metadata %s has trailing data", path)
	}
	if metadata.SchemaVersion != 1 {
		return Platform{}, errorf("platform.schema", "the platform metadata %s has schema version %d, not 1", path, metadata.SchemaVersion)
	}
	if metadata.Generation < 0 {
		return Platform{}, errorf("platform.generation", "the platform metadata %s has generation %d", path, metadata.Generation)
	}
	return Platform{Generation: metadata.Generation, Source: "image-metadata"}, nil
}

// WritePlatformMetadata is the image-side writer for the provider's file. It is
// used by tests and by an image preparation step, never by the transaction.
func WritePlatformMetadata(path string, generation int64) error {
	if generation < 0 {
		return errorf("platform.generation", "the platform generation %d is negative", generation)
	}
	data, err := json.Marshal(platformMetadata{SchemaVersion: 1, Generation: generation})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "" {
		path = PlatformMetadataPath
	}
	return writeFileAtomic(path, data, 0o644)
}
