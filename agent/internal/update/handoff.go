package update

import (
	"errors"
	"os"
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
		return Handoff{}, err
	}
	lock, err := oplock.Acquire(p.lock)
	if err != nil {
		return Handoff{}, err
	}
	defer func() { _ = lock.Release() }()

	journal, err := readJournal(p.journal)
	if err != nil {
		return Handoff{}, err
	}
	request, requestErr := ReadRequest(p.request)
	requestPresent := requestErr == nil
	if requestErr != nil && !errors.Is(requestErr, os.ErrNotExist) {
		return Handoff{}, requestErr
	}

	if journal != nil {
		if journal.SourcePath == "" {
			return Handoff{}, errorf("request.source-unknown", "the active journal %s does not record its source path; resolve it before starting a new request", journal.OperationID)
		}
		if journal.SourcePath != cleaned {
			return Handoff{}, errorf("request.busy", "an update operation %s is active from %s; finish it before starting %s", journal.OperationID, journal.SourcePath, cleaned)
		}
		if requestPresent {
			if request.SourcePath != cleaned {
				// The journal source is authoritative. A request record that
				// disagrees with it is a stale trigger, not a second source.
				if err := RemoveRequest(p.request); err != nil {
					return Handoff{}, err
				}
				request, err = NewRequest(cleaned, now)
				if err != nil {
					return Handoff{}, err
				}
				if err := WriteRequest(p.request, request); err != nil {
					return Handoff{}, err
				}
				return Handoff{Request: request, Interrupted: true, OperationID: journal.OperationID, WroteRequest: true}, nil
			}
			return Handoff{Request: request, Interrupted: true, OperationID: journal.OperationID}, nil
		}
		request, err = NewRequest(cleaned, now)
		if err != nil {
			return Handoff{}, err
		}
		if err := WriteRequest(p.request, request); err != nil {
			return Handoff{}, err
		}
		return Handoff{Request: request, Interrupted: true, OperationID: journal.OperationID, WroteRequest: true}, nil
	}

	if requestPresent {
		if request.SourcePath != cleaned {
			return Handoff{}, errorf("request.conflict", "the pending request %s names source %s, not %s; cancel it before starting %s", p.request, request.SourcePath, cleaned, cleaned)
		}
		return Handoff{Request: request}, nil
	}
	request, err = NewRequest(cleaned, now)
	if err != nil {
		return Handoff{}, err
	}
	if err := WriteRequest(p.request, request); err != nil {
		return Handoff{}, err
	}
	return Handoff{Request: request, WroteRequest: true}, nil
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
