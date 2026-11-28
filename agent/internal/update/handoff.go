package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bittern.invalid/agent/internal/oplock"
)

// Handoff is the agent side result of preparing a request. Interrupted means a
// journal for the same source already exists and the unit will resume it.
// WroteRequest means this call created the record; a failed start may remove
// that record again.
type Handoff struct {
	Request      Request
	Interrupted  bool
	OperationID  string
	WroteRequest bool
}

// PrepareHandoff decides whether a source starts a new request, resumes the
// active journal, or is refused. It takes the shared operation lock, so it
// cannot race an apply or an update that is already running, and it writes the
// request before returning. The caller starts the fixed unit after this
// returns; the helper takes the same lock for the whole transaction.
func PrepareHandoff(stateRoot, sourcePath string, now time.Time) (Handoff, error) {
	options := normalizeOptions(Options{StateRoot: stateRoot})
	p := options.paths()
	cleaned, err := validateSourceDirectory(sourcePath)
	if err != nil {
		return Handoff{}, NewStartError(StartSourceRead, err, "%v", err)
	}
	lock, err := oplock.Acquire(p.lock)
	if err != nil {
		return Handoff{}, startLockError(err)
	}
	defer func() { _ = lock.Release() }()

	journal, err := readJournal(p.journal)
	if err != nil {
		return Handoff{}, NewStartError(StartStaleRequest, err, "read the active update journal: %v", err)
	}
	request, requestErr := ReadRequest(p.request)
	requestPresent := requestErr == nil
	if requestErr != nil && !errors.Is(requestErr, os.ErrNotExist) {
		return Handoff{}, NewStartError(StartStaleRequest, requestErr, "read the pending update request: %v", requestErr)
	}

	if journal == nil {
		if requestPresent && request.SourcePath != cleaned {
			if !staleRequestIsIncoming(stateRoot, request) {
				cause := errorf("request.conflict", "the pending request %s names source %s, not %s; cancel it before starting %s", p.request, request.SourcePath, cleaned, cleaned)
				return Handoff{}, NewStartError(StartStaleRequest, cause, "%s", cause.Message)
			}
			if err := cleanIncomingExcept(stateRoot, cleaned); err != nil {
				return Handoff{}, err
			}
			if err := RemoveRequest(p.request); err != nil {
				return Handoff{}, NewStartError(StartStaleRequest, err, "remove the stale update request %s: %v", p.request, err)
			}
			requestPresent = false
		}
		if err := cleanIncomingExcept(stateRoot, cleaned); err != nil {
			return Handoff{}, err
		}
	}

	if journal != nil {
		if journal.SourcePath == "" {
			return Handoff{}, errorf("request.source-unknown", "the active journal %s does not record its source path; resolve it before starting a new request", journal.OperationID)
		}
		if journal.SourcePath != cleaned {
			cause := errorf("request.busy", "an update operation %s is active from %s; finish it before starting %s", journal.OperationID, journal.SourcePath, cleaned)
			return Handoff{}, NewStartError(StartBusy, cause, "%s", cause.Message)
		}
		if requestPresent {
			if request.SourcePath != cleaned {
				// The journal source is authoritative. A request record that
				// disagrees with it is a stale trigger, not a second source.
				if err := RemoveRequest(p.request); err != nil {
					return Handoff{}, NewStartError(StartStaleRequest, err, "remove the stale update request %s: %v", p.request, err)
				}
				request, err = NewRequest(cleaned, now)
				if err != nil {
					return Handoff{}, NewStartError(StartSourceRead, err, "%v", err)
				}
				if err := writeHandoffRequest(p.request, request); err != nil {
					return Handoff{}, err
				}
				return Handoff{Request: request, Interrupted: true, OperationID: journal.OperationID, WroteRequest: true}, nil
			}
			return Handoff{Request: request, Interrupted: true, OperationID: journal.OperationID}, nil
		}
		request, err = NewRequest(cleaned, now)
		if err != nil {
			return Handoff{}, NewStartError(StartSourceRead, err, "%v", err)
		}
		if err := writeHandoffRequest(p.request, request); err != nil {
			return Handoff{}, err
		}
		return Handoff{Request: request, Interrupted: true, OperationID: journal.OperationID, WroteRequest: true}, nil
	}

	if requestPresent {
		if request.SourcePath != cleaned {
			cause := errorf("request.conflict", "the pending request %s names source %s, not %s; cancel it before starting %s", p.request, request.SourcePath, cleaned, cleaned)
			return Handoff{}, NewStartError(StartStaleRequest, cause, "%s", cause.Message)
		}
		return Handoff{Request: request}, nil
	}
	request, err = NewRequest(cleaned, now)
	if err != nil {
		return Handoff{}, NewStartError(StartSourceRead, err, "%v", err)
	}
	if err := writeHandoffRequest(p.request, request); err != nil {
		return Handoff{}, err
	}
	return Handoff{Request: request, WroteRequest: true}, nil
}

func staleRequestIsIncoming(stateRoot string, request Request) bool {
	return request.Source == RequestSourceLocalFile &&
		IsIncomingSource(stateRoot, request.SourcePath) &&
		strings.HasPrefix(filepath.Base(request.SourcePath), incomingCandidatePrefix)
}

func writeHandoffRequest(path string, request Request) error {
	if err := WriteRequest(path, request); err != nil {
		return NewStartError(StartRequestWrite, err, "write the update request %s: %v", path, err)
	}
	return nil
}

// CancelRequest removes a pending request that has no journal. It refuses while
// a transaction is active, because the request is the resume trigger.
func CancelRequest(stateRoot string) (bool, error) {
	options := normalizeOptions(Options{StateRoot: stateRoot})
	p := options.paths()
	lock, err := oplock.Acquire(p.lock)
	if err != nil {
		return false, err
	}
	defer func() { _ = lock.Release() }()

	journal, err := readJournal(p.journal)
	if err != nil {
		return false, err
	}
	if journal != nil {
		return false, errorf("request.busy", "an update operation %s is active; resume it instead of cancelling its request", journal.OperationID)
	}
	if _, err := os.Lstat(p.request); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := RemoveRequest(p.request); err != nil {
		return false, err
	}
	return true, nil
}

// PrepareRollbackHandoff writes the fixed manual-rollback request. It refuses
// while another journal is active and never starts a transaction itself. The
// helper takes the same lock for the rollback.
func PrepareRollbackHandoff(stateRoot, reason string, now time.Time) (Handoff, error) {
	options := normalizeOptions(Options{StateRoot: stateRoot})
	p := options.paths()
	lock, err := oplock.Acquire(p.lock)
	if err != nil {
		return Handoff{}, err
	}
	defer func() { _ = lock.Release() }()

	journal, err := readJournal(p.journal)
	if err != nil {
		return Handoff{}, err
	}
	if journal != nil {
		return Handoff{}, errorf("rollback.busy", "an update operation %s is active; finish it before rolling back", journal.OperationID)
	}
	request, requestErr := ReadRequest(p.request)
	requestPresent := requestErr == nil
	if requestErr != nil && !errors.Is(requestErr, os.ErrNotExist) {
		return Handoff{}, requestErr
	}
	if requestPresent {
		if request.Source != RequestSourceManualRollback {
			return Handoff{}, errorf("request.conflict", "the pending request %s is not a manual rollback; cancel it first", p.request)
		}
		return Handoff{Request: request}, nil
	}
	request, err = NewRollbackRequest(reason, now)
	if err != nil {
		return Handoff{}, err
	}
	if err := WriteRequest(p.request, request); err != nil {
		return Handoff{}, err
	}
	return Handoff{Request: request, WroteRequest: true}, nil
}
