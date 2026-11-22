package update

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// RequestSchemaVersion is the request record schema version.
const RequestSchemaVersion = 1

// Request source kinds. A local-file request installs a downloaded bundle. A
// manual-rollback request reverts to the retained previous bundle or the
// baseline; it carries no source path.
const (
	RequestSourceLocalFile      = "local-file"
	RequestSourceManualRollback = "manual-rollback"
)

const (
	requestFileName = "request.json"
	requestLimit    = 4 << 10
)

var requestIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)

// Request is the fixed input record the update helper reads. It contains no
// generation, channel, key, digest, or verification result; the signed
// manifest remains the only trust input.
type Request struct {
	SchemaVersion int    `json:"schema_version"`
	RequestID     string `json:"request_id"`
	Source        string `json:"source"`
	SourcePath    string `json:"source_path,omitempty"`
	Reason        string `json:"reason,omitempty"`
	RequestedAt   string `json:"requested_at"`
}

// RequestPath returns the fixed request record path for a state root.
func RequestPath(stateRoot string) string {
	if stateRoot == "" {
		stateRoot = defaultStateRoot
	}
	return filepath.Join(stateRoot, "update", requestFileName)
}

// NewRequest builds a request for an existing local source directory. The
// caller writes it with WriteRequest.
func NewRequest(sourcePath string, now time.Time) (Request, error) {
	cleaned, err := validateSourceDirectory(sourcePath)
	if err != nil {
		return Request{}, err
	}
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Request{}, errorf("request.random", "read random bytes for the request id: %v", err)
	}
	return Request{
		SchemaVersion: RequestSchemaVersion,
		RequestID:     now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random[:]),
		Source:        RequestSourceLocalFile,
		SourcePath:    cleaned,
		RequestedAt:   now.UTC().Format(time.RFC3339),
	}, nil
}

// NewRollbackRequest builds a request for one manual rollback. The helper reads
// the reason into the rollback history; it is not a trust input.
func NewRollbackRequest(reason string, now time.Time) (Request, error) {
	if err := validateRollbackReason(reason); err != nil {
		return Request{}, err
	}
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Request{}, errorf("request.random", "read random bytes for the request id: %v", err)
	}
	return Request{
		SchemaVersion: RequestSchemaVersion,
		RequestID:     now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random[:]),
		Source:        RequestSourceManualRollback,
		Reason:        reason,
		RequestedAt:   now.UTC().Format(time.RFC3339),
	}, nil
}

func validateRollbackReason(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return errorf("request.reason", "the rollback reason is empty")
	}
	if len(reason) > 200 {
		return errorf("request.reason", "the rollback reason is %d bytes, over the 200 byte limit", len(reason))
	}
	for index := 0; index < len(reason); index++ {
		if reason[index] < 0x20 || reason[index] == 0x7f {
			return errorf("request.reason", "the rollback reason contains a control character")
		}
	}
	return nil
}

// Validate checks the fixed format. It does not require the source directory
// to still exist: a resume of a missing source fails at fetch-manifest with a
// clearer step error than a malformed request would.
func (r Request) Validate() error {
	if r.SchemaVersion != RequestSchemaVersion {
		return errorf("request.schema", "the request schema version %d is not %d", r.SchemaVersion, RequestSchemaVersion)
	}
	if !requestIDPattern.MatchString(r.RequestID) {
		return errorf("request.id", "the request id %q is not a valid request id", r.RequestID)
	}
	switch r.Source {
	case RequestSourceLocalFile:
		if err := validateSourcePath(r.SourcePath); err != nil {
			return err
		}
		if r.Reason != "" {
			return errorf("request.reason", "an install request cannot carry a rollback reason")
		}
	case RequestSourceManualRollback:
		if r.SourcePath != "" {
			return errorf("request.source", "a manual rollback request cannot carry a source path")
		}
		if err := validateRollbackReason(r.Reason); err != nil {
			return err
		}
	default:
		return errorf("request.source-kind", "the request source %q is not %q or %q", r.Source, RequestSourceLocalFile, RequestSourceManualRollback)
	}
	if _, err := time.Parse(time.RFC3339, r.RequestedAt); err != nil {
		return errorf("request.time", "the request time %q is not RFC 3339: %v", r.RequestedAt, err)
	}
	return nil
}

func validateSourcePath(sourcePath string) error {
	if sourcePath == "" {
		return errorf("request.source", "the request source path is empty")
	}
	if strings.ContainsAny(sourcePath, "\x00\n\r") {
		return errorf("request.source", "the request source path contains a control character")
	}
	if !filepath.IsAbs(sourcePath) {
		return errorf("request.source", "the request source path %q is not absolute", sourcePath)
	}
	if filepath.Clean(sourcePath) != sourcePath {
		return errorf("request.source", "the request source path %q is not clean", sourcePath)
	}
	return nil
}

func validateSourceDirectory(sourcePath string) (string, error) {
	if err := validateSourcePath(sourcePath); err != nil {
		return "", err
	}
	info, err := os.Lstat(sourcePath)
	if errors.Is(err, os.ErrNotExist) {
		return "", errorf("request.source", "the request source %s does not exist", sourcePath)
	}
	if err != nil {
		return "", errorf("request.source", "inspect the request source %s: %v", sourcePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", errorf("request.source", "the request source %s is a symlink", sourcePath)
	}
	if !info.IsDir() {
		return "", errorf("request.source", "the request source %s is not a directory", sourcePath)
	}
	return sourcePath, nil
}

// ReadRequest reads and validates the request record. A request that is not a
// regular file, is a symlink, has the wrong owner, or has any mode other than
// 0600 is rejected before it can start a transaction.
func ReadRequest(path string) (Request, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Request{}, fmt.Errorf("update: request %s: %w", path, os.ErrNotExist)
	}
	if err != nil {
		return Request{}, errorf("request.read", "inspect the request %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Request{}, errorf("request.type", "the request %s is a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return Request{}, errorf("request.type", "the request %s is not a regular file", path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Uid != uint32(os.Geteuid()) {
		return Request{}, errorf("request.owner", "the request %s has owner uid %d, not %d", path, stat.Uid, os.Geteuid())
	}
	if info.Mode().Perm() != 0o600 {
		return Request{}, errorf("request.mode", "the request %s has mode %04o, not 0600", path, info.Mode().Perm())
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return Request{}, errorf("request.read", "open the request %s: %v", path, err)
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return Request{}, errorf("request.read", "stat the request %s: %v", path, err)
	}
	if !os.SameFile(info, after) {
		return Request{}, errorf("request.type", "the request %s changed while it was opened", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, requestLimit+1))
	if err != nil {
		return Request{}, errorf("request.read", "read the request %s: %v", path, err)
	}
	if len(data) > requestLimit {
		return Request{}, errorf("request.size", "the request %s exceeds the %d byte limit", path, requestLimit)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, errorf("request.parse", "decode the request %s: %v", path, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Request{}, errorf("request.trailing", "the request %s has trailing data", path)
	}
	if err := request.Validate(); err != nil {
		return Request{}, err
	}
	return request, nil
}

// WriteRequest writes the request atomically at mode 0600 and refuses to
// replace an existing record. The hard-link publish gives no-overwrite
// semantics without writing the destination in place.
func WriteRequest(path string, request Request) error {
	if err := request.Validate(); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("update: create request directory: %w", err)
	}
	data, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(directory, ".tmp-request-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}
	if err := temp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	if err := os.Link(tempName, path); err != nil {
		_ = os.Remove(tempName)
		if errors.Is(err, os.ErrExist) {
			return errorf("request.exists", "the request %s already exists", path)
		}
		return fmt.Errorf("update: publish request %s: %w", path, err)
	}
	if err := os.Remove(tempName); err != nil {
		return err
	}
	return fsyncDir(directory)
}

// RemoveRequest removes the request record. A missing record is a no-op.
func RemoveRequest(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return fsyncDir(filepath.Dir(path))
}
