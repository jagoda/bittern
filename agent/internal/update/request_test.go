package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requestNow() time.Time {
	return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
}

func writeRequestJSON(t *testing.T, path string, request Request, mode os.FileMode) {
	t.Helper()
	data, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRequestRoundTripAndStrictFormat(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest(source, requestNow())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(request.RequestID, "20261122T120000Z-") {
		t.Fatalf("request id = %q", request.RequestID)
	}
	path := RequestPath(dir)
	if err := WriteRequest(path, request); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("request mode = %04o, want 0600", info.Mode().Perm())
	}
	read, err := ReadRequest(path)
	if err != nil {
		t.Fatal(err)
	}
	if read != request {
		t.Fatalf("read request = %+v, want %+v", read, request)
	}
}

func TestUpdateRequestRejectsNon0600Record(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest(source, requestNow())
	if err != nil {
		t.Fatal(err)
	}
	path := RequestPath(dir)
	writeRequestJSON(t, path, request, 0o644)
	_, err = ReadRequest(path)
	requireRule(t, err, "request.mode")
}

func TestUpdateRequestRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest(source, requestNow())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target.json")
	writeRequestJSON(t, target, request, 0o600)
	path := RequestPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	_, err = ReadRequest(path)
	requireRule(t, err, "request.type")
}

func TestUpdateRequestRejectsUnknownFieldAndTrailingData(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest(source, requestNow())
	if err != nil {
		t.Fatal(err)
	}
	path := RequestPath(dir)
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	withExtra := append([]byte("{\"extra\":1,"), data[1:]...)
	if err := os.WriteFile(path, withExtra, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = ReadRequest(path)
	requireRule(t, err, "request.parse")

	if err := os.WriteFile(path, append(data, []byte("\n{}\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = ReadRequest(path)
	requireRule(t, err, "request.trailing")
}

func TestUpdateRequestRejectsRelativeSource(t *testing.T) {
	request := Request{
		SchemaVersion: RequestSchemaVersion,
		RequestID:     "20261122T120000Z-deadbeef",
		Source:        RequestSourceLocalFile,
		SourcePath:    "relative/source",
		RequestedAt:   "2026-11-22T12:00:00Z",
	}
	err := request.Validate()
	requireRule(t, err, "request.source")
}

func TestUpdateRequestRejectsUnknownSourceKind(t *testing.T) {
	request := Request{
		SchemaVersion: RequestSchemaVersion,
		RequestID:     "20261122T120000Z-deadbeef",
		Source:        "github-release",
		SourcePath:    "/srv/bundle",
		RequestedAt:   "2026-11-22T12:00:00Z",
	}
	err := request.Validate()
	requireRule(t, err, "request.source-kind")
}

func TestUpdateRequestRefusesToReplaceAnExistingRecord(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest(source, requestNow())
	if err != nil {
		t.Fatal(err)
	}
	path := RequestPath(dir)
	if err := WriteRequest(path, request); err != nil {
		t.Fatal(err)
	}
	if err := WriteRequest(path, request); err == nil {
		t.Fatal("a second request replaced the first")
	} else {
		requireRule(t, err, "request.exists")
	}
}

func TestUpdateHandoffRefusesSecondRequestWhileJournalExists(t *testing.T) {
	env := newTestEnv(t)
	sourceA := env.writeValidBundle(t, 1)
	sourceB := env.writeValidBundle(t, 2)
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepPreflight {
			return ErrPowerLoss
		}
		return nil
	}
	if _, err := env.install(t, sourceA); !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut install error = %v, want ErrPowerLoss", err)
	}
	journal := readJournalForTest(t, env.stateRoot)
	if journal == nil || journal.SourcePath != sourceA {
		t.Fatalf("journal = %+v, want source path %s", journal, sourceA)
	}

	if _, err := PrepareHandoff(env.stateRoot, sourceB, requestNow()); err == nil {
		t.Fatal("second source was accepted while a journal existed")
	} else {
		requireRule(t, err, "request.busy")
	}
	if _, err := os.Stat(RequestPath(env.stateRoot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused handoff wrote a request: %v", err)
	}

	resumed, err := PrepareHandoff(env.stateRoot, sourceA, requestNow())
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Interrupted || resumed.OperationID != journal.OperationID {
		t.Fatalf("handoff = %+v, want resume of %s", resumed, journal.OperationID)
	}
	if _, err := os.Stat(RequestPath(env.stateRoot)); err != nil {
		t.Fatalf("resume did not write the request: %v", err)
	}
}

func TestUpdateHandoffReusesPendingRequest(t *testing.T) {
	env := newTestEnv(t)
	source := env.writeValidBundle(t, 1)
	first, err := PrepareHandoff(env.stateRoot, source, requestNow())
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareHandoff(env.stateRoot, source, requestNow())
	if err != nil {
		t.Fatal(err)
	}
	if first.WroteRequest != true || second.WroteRequest != false {
		t.Fatalf("wrote request flags = %t and %t, want true and false", first.WroteRequest, second.WroteRequest)
	}
	if first.Request.RequestID != second.Request.RequestID {
		t.Fatalf("request id changed from %s to %s", first.Request.RequestID, second.Request.RequestID)
	}
}

func TestUpdateHandoffRefusesADifferentPendingSource(t *testing.T) {
	env := newTestEnv(t)
	sourceA := env.writeValidBundle(t, 1)
	sourceB := env.writeValidBundle(t, 2)
	if _, err := PrepareHandoff(env.stateRoot, sourceA, requestNow()); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareHandoff(env.stateRoot, sourceB, requestNow()); err == nil {
		t.Fatal("a different pending source was accepted")
	} else {
		requireRule(t, err, "request.conflict")
	}
}

func TestUpdateCancelRefusesWhileAJournalExists(t *testing.T) {
	env := newTestEnv(t)
	source := env.writeValidBundle(t, 1)
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepPreflight {
			return ErrPowerLoss
		}
		return nil
	}
	if _, err := env.install(t, source); !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut install error = %v, want ErrPowerLoss", err)
	}
	if removed, err := CancelRequest(env.stateRoot); err == nil || removed {
		t.Fatalf("cancel with a journal = (%t, %v), want a refusal", removed, err)
	} else {
		requireRule(t, err, "request.busy")
	}
}

func TestUpdateCancelRemovesAPendingRequest(t *testing.T) {
	env := newTestEnv(t)
	source := env.writeValidBundle(t, 1)
	if _, err := PrepareHandoff(env.stateRoot, source, requestNow()); err != nil {
		t.Fatal(err)
	}
	removed, err := CancelRequest(env.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("cancel did not remove the pending request")
	}
	if _, err := os.Stat(RequestPath(env.stateRoot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("request still exists after cancel: %v", err)
	}
}

func TestUpdateRequestPathsStayUnderTheStateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	path := RequestPath(root)
	if !strings.HasPrefix(path, root+string(filepath.Separator)) {
		t.Fatalf("request path %s is outside %s", path, root)
	}
	options := normalizeOptions(Options{StateRoot: root})
	if options.paths().request != path {
		t.Fatalf("request path = %s, want %s", options.paths().request, path)
	}
}

func TestUpdateHandoffReplacesAStaleRequestForTheJournalledSource(t *testing.T) {
	env := newTestEnv(t)
	sourceA := env.writeValidBundle(t, 1)
	sourceB := env.writeValidBundle(t, 2)
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepPreflight {
			return ErrPowerLoss
		}
		return nil
	}
	if _, err := env.install(t, sourceA); !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut install error = %v, want ErrPowerLoss", err)
	}
	stale, err := NewRequest(sourceB, requestNow())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRequest(RequestPath(env.stateRoot), stale); err != nil {
		t.Fatal(err)
	}
	handoff, err := PrepareHandoff(env.stateRoot, sourceA, requestNow())
	if err != nil {
		t.Fatal(err)
	}
	if !handoff.Interrupted || !handoff.WroteRequest {
		t.Fatalf("handoff = %+v, want a rewritten resume", handoff)
	}
	request, err := ReadRequest(RequestPath(env.stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	if request.SourcePath != sourceA {
		t.Fatalf("request source = %s, want the journalled %s", request.SourcePath, sourceA)
	}
}

func TestUpdateRequestManualRollbackRoundTrip(t *testing.T) {
	dir := t.TempDir()
	request, err := NewRollbackRequest("bench test", requestNow())
	if err != nil {
		t.Fatal(err)
	}
	if request.Source != RequestSourceManualRollback || request.SourcePath != "" || request.Reason != "bench test" {
		t.Fatalf("request = %+v", request)
	}
	path := RequestPath(dir)
	if err := WriteRequest(path, request); err != nil {
		t.Fatal(err)
	}
	read, err := ReadRequest(path)
	if err != nil {
		t.Fatal(err)
	}
	if read != request {
		t.Fatalf("read request = %+v, want %+v", read, request)
	}
}

func TestUpdateRequestManualRollbackRequiresAReason(t *testing.T) {
	if _, err := NewRollbackRequest("", requestNow()); err == nil {
		t.Fatal("an empty rollback reason was accepted")
	} else {
		requireRule(t, err, "request.reason")
	}
	request := Request{
		SchemaVersion: RequestSchemaVersion,
		RequestID:     "20261122T120000Z-deadbeef",
		Source:        RequestSourceManualRollback,
		SourcePath:    "/srv/bundle",
		Reason:        "bench test",
		RequestedAt:   "2026-11-22T12:00:00Z",
	}
	err := request.Validate()
	requireRule(t, err, "request.source")
}
