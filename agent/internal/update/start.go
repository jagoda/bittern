package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"bittern.invalid/agent/internal/oplock"
)

// Start failure kinds. They are stable, typed reasons for a request that never
// reached the fixed helper. The UI maps each kind to one plain sentence and one
// next action; it never classifies an error by matching its text.
const (
	StartStaleRequest     = "start.stale-request"
	StartIncomingLeftover = "start.incoming-leftover"
	StartBusy             = "start.busy"
	StartHelper           = "start.helper"
	StartSpace            = "start.space"
	StartRequestWrite     = "start.request-write"
	StartSourceRead       = "start.source-read"
	StartDownload         = "start.download"
)

// StartError is a typed failure from the update start path. Kind is stable;
// Message is the technical reason shown only in Technical details.
type StartError struct {
	Kind    string
	Message string
	Cause   error
}

func (e *StartError) Error() string { return e.Kind + ": " + e.Message }

func (e *StartError) Unwrap() error { return e.Cause }

// NewStartError builds a typed start failure. The cause is retained for
// errors.Is and errors.As, while the message stays the redacted technical text.
func NewStartError(kind string, cause error, format string, args ...any) *StartError {
	return &StartError{Kind: kind, Message: fmt.Sprintf(format, args...), Cause: cause}
}

const incomingCandidatePrefix = "candidate-"

// PrepareStart clears stale state that belongs to an earlier start attempt and
// refuses to begin while a journal is active. It takes the shared operation
// lock for the cleanup so it cannot remove a source an active transaction is
// reading. The caller downloads after this returns; PrepareHandoff takes the
// lock again for the request itself.
func PrepareStart(stateRoot string) error {
	options := normalizeOptions(Options{StateRoot: stateRoot})
	p := options.paths()
	lock, err := oplock.Acquire(p.lock)
	if err != nil {
		return startLockError(err)
	}
	defer func() { _ = lock.Release() }()

	journal, err := readJournal(p.journal)
	if err != nil {
		return NewStartError(StartStaleRequest, err, "read the active update journal: %v", err)
	}
	if journal != nil {
		return NewStartError(StartBusy, nil, "an update operation %s is active; wait for it to finish", journal.OperationID)
	}

	request, requestErr := ReadRequest(p.request)
	requestPresent := requestErr == nil
	if requestErr != nil && !errors.Is(requestErr, os.ErrNotExist) {
		return NewStartError(StartStaleRequest, requestErr, "read the pending update request: %v", requestErr)
	}
	if requestPresent && request.Source == RequestSourceLocalFile && IsIncomingSource(stateRoot, request.SourcePath) {
		// A channel request with no journal is a leftover from a start that did
		// not reach the helper. The source and record are both engine-written,
		// so a retry clears them. Cleanup verifies ownership before deleting.
		if err := cleanIncomingExcept(stateRoot, ""); err != nil {
			return err
		}
		if err := RemoveRequest(p.request); err != nil {
			return NewStartError(StartStaleRequest, err, "remove the stale update request %s: %v", p.request, err)
		}
		return nil
	}

	keep := ""
	if requestPresent && request.Source == RequestSourceLocalFile {
		keep = request.SourcePath
	}
	return cleanIncomingExcept(stateRoot, keep)
}

func startLockError(err error) error {
	if errors.Is(err, oplock.ErrBusy) {
		return NewStartError(StartBusy, err, "another update or configuration change holds the operation lock")
	}
	return NewStartError(StartStaleRequest, err, "take the operation lock: %v", err)
}

// cleanIncomingExcept removes engine-owned channel download directories under
// the state root, except keep. It refuses any entry it cannot prove this agent
// wrote: an unexpected name, a symlink, a non-directory, another owner, or a
// mode other than 0700. A refusal leaves every entry in place.
func cleanIncomingExcept(stateRoot, keep string) error {
	root := filepath.Join(stateRoot, "update", "incoming")
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return NewStartError(StartIncomingLeftover, err, "inspect the channel download root %s: %v", root, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return NewStartError(StartIncomingLeftover, nil, "the channel download root %s is not a regular directory", root)
	}
	if !ownedByCurrentUser(info) {
		return NewStartError(StartIncomingLeftover, nil, "the channel download root %s is not owned by this agent", root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return NewStartError(StartIncomingLeftover, err, "read the channel download root %s: %v", root, err)
	}
	keep = filepath.Clean(keep)
	for _, entry := range entries {
		candidate := filepath.Join(root, entry.Name())
		if keep != "" && filepath.Clean(candidate) == keep {
			continue
		}
		if err := removeOwnedIncomingPath(root, candidate); err != nil {
			return err
		}
	}
	return nil
}

// RemoveIncomingSource removes one channel download only when it is proven to
// be an engine-written direct child of the incoming root. It never follows a
// symlink and never deletes a local source outside the incoming root.
func RemoveIncomingSource(stateRoot, sourcePath string) error {
	if stateRoot == "" {
		stateRoot = defaultStateRoot
	}
	if !IsIncomingSource(stateRoot, sourcePath) {
		return nil
	}
	root := filepath.Join(stateRoot, "update", "incoming")
	if filepath.Dir(filepath.Clean(sourcePath)) != filepath.Clean(root) {
		return NewStartError(StartIncomingLeftover, nil, "the incoming source %s is not a direct child of %s", sourcePath, root)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return NewStartError(StartIncomingLeftover, err, "inspect the channel download root %s: %v", root, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !ownedByCurrentUser(info) {
		return NewStartError(StartIncomingLeftover, nil, "the channel download root %s is not an owned regular directory", root)
	}
	return removeOwnedIncomingPath(root, sourcePath)
}

func removeOwnedIncomingPath(root, path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return NewStartError(StartIncomingLeftover, err, "inspect the channel download %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return NewStartError(StartIncomingLeftover, nil, "the channel download %s is not a regular directory", path)
	}
	if !strings.HasPrefix(filepath.Base(path), incomingCandidatePrefix) {
		return NewStartError(StartIncomingLeftover, nil, "the channel download %s does not have an engine-written name", path)
	}
	if !ownedByCurrentUser(info) {
		return NewStartError(StartIncomingLeftover, nil, "the channel download %s is not owned by this agent", path)
	}
	if info.Mode().Perm() != 0o700 {
		return NewStartError(StartIncomingLeftover, nil, "the channel download %s has mode %04o, not 0700", path, info.Mode().Perm())
	}
	if err := removeAll(path); err != nil {
		return NewStartError(StartIncomingLeftover, err, "remove the channel download %s: %v", path, err)
	}
	return nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return stat.Uid == uint32(os.Geteuid())
}

// DownloadRequiredBytes is the disk the incoming stage needs for one bundle.
// It scales with the payload and adds the bounded manifest and signature reads
// that are published beside it.
func DownloadRequiredBytes(payloadSize int64) uint64 {
	if payloadSize < 0 {
		payloadSize = 0
	}
	return uint64(payloadSize) + uint64(checkAssetLimit) + uint64(signatureLimit)
}

// PreflightDownload checks the free space under the state root before the
// incoming stage is written.
func PreflightDownload(stateRoot string, payloadSize int64) error {
	return PreflightDownloadWith(stateRoot, payloadSize, nil)
}

// PreflightDownloadWith is PreflightDownload with a free-space seam for tests.
func PreflightDownloadWith(stateRoot string, payloadSize int64, free func(string) (uint64, error)) error {
	if stateRoot == "" {
		stateRoot = defaultStateRoot
	}
	if free == nil {
		free = freeSpaceUnder
	}
	available, err := free(stateRoot)
	if err != nil {
		return NewStartError(StartSpace, err, "read free space under %s: %v", stateRoot, err)
	}
	required := DownloadRequiredBytes(payloadSize)
	if available < required {
		return NewStartError(StartSpace, nil, "the state root %s has %d bytes free, less than the %d bytes required for a %d byte bundle", stateRoot, available, required, payloadSize)
	}
	return nil
}

func freeSpaceUnder(path string) (uint64, error) {
	for {
		var stat syscall.Statfs_t
		err := syscall.Statfs(path, &stat)
		if err == nil {
			return stat.Bavail * uint64(stat.Bsize), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, err
		}
		path = parent
	}
}
