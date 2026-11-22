package ui

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/secrets"
)

// secretsSnapshot is the secrets document as it was before a setup apply wrote
// a camera sign-in. Restoring it is how a run that did not commit leaves no
// credential behind: a sign-in belongs to the configuration generation that
// references it, not beside the transaction.
type secretsSnapshot struct {
	path    string
	data    []byte
	mode    fs.FileMode
	existed bool
}

// snapshotSecretsFile reads the document before the applier writes to it. A
// missing file is a snapshot of "no file", which restore removes again.
func snapshotSecretsFile(path string) (secretsSnapshot, error) {
	snapshot := secretsSnapshot{path: path, mode: 0o600}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot, nil
	}
	if err != nil {
		return secretsSnapshot{}, fmt.Errorf("ui: read secrets.yaml: %w", err)
	}
	snapshot.data = data
	snapshot.existed = true
	if info, statErr := os.Stat(path); statErr == nil {
		snapshot.mode = info.Mode().Perm()
	}
	return snapshot, nil
}

// restore puts the document back byte for byte. A file that did not exist is
// removed. A document that is already unchanged is left alone, so a run that
// wrote nothing rewrites nothing.
func (s secretsSnapshot) restore() error {
	if s.path == "" {
		return nil
	}
	current, err := os.ReadFile(s.path)
	if s.existed {
		if err == nil && bytes.Equal(current, s.data) {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("ui: read secrets.yaml before restoring it: %w", err)
		}
		if writeErr := secrets.WriteFileAtomic(s.path, s.data, s.mode); writeErr != nil {
			return fmt.Errorf("ui: restore secrets.yaml: %w", writeErr)
		}
		return nil
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("ui: read secrets.yaml before removing it: %w", err)
	}
	if removeErr := os.Remove(s.path); removeErr != nil {
		return fmt.Errorf("ui: remove the secrets.yaml this apply created: %w", removeErr)
	}
	return nil
}

// keepCameraSecrets reports whether the sign-ins written for this apply must
// stay. They stay when the transaction committed, when it stopped before
// finishing and can resume from its journal, and when it may have left the new
// configuration partly live and still reading them. Every other outcome
// restores the document, because a credential must not outlive the
// configuration that references it.
func keepCameraSecrets(result apply.Result, err error) bool {
	switch result.Status {
	case apply.StatusApplied:
		return err == nil
	case apply.StatusInterrupted:
		// The journal resumes this generation after a restart, and the
		// credential must be there when it commits.
		return true
	case apply.StatusRolledBack:
		return false
	}
	// No status, or a failed transaction: the credential stays only when a
	// live step ran, so the new configuration may be partly on the appliance.
	return result.LiveTouched
}
