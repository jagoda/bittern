package update

import (
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/release"
)

func runRejection(t *testing.T, spec bundleSpec, wantRule string) *testEnv {
	t.Helper()
	env := newTestEnv(t)
	dir := env.newSourceDir(t)
	if spec.files == nil {
		spec.files = defaultFiles()
	}
	env.writeBundle(t, dir, spec)
	result, err := env.install(t, dir)
	if err == nil {
		t.Fatalf("invalid bundle was accepted: %+v", result)
	}
	requireRule(t, err, wantRule)
	if result.Status != ResultRejected {
		t.Fatalf("result status = %s, want rejected", result.Status)
	}
	assertNoJournal(t, env.stateRoot)
	if current := readCurrentForTest(t, env.stateRoot); current != 0 {
		t.Fatalf("pointer = %d after rejection, want 0", current)
	}
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 0 {
		t.Fatalf("committed generation = %d after rejection, want 0", state.CommittedGeneration)
	}
	if len(env.system.calls) != 0 {
		t.Fatalf("rejection performed unit actions: %v", env.system.calls)
	}
	return env
}

func TestUpdateRejectsMissingManifest(t *testing.T) {
	env := newTestEnv(t)
	dir := env.newSourceDir(t)
	result, err := env.install(t, dir)
	if err == nil {
		t.Fatalf("missing manifest was accepted: %+v", result)
	}
	requireRule(t, err, "manifest.missing")
	if result.Status != ResultRejected {
		t.Fatalf("status = %s, want rejected", result.Status)
	}
}

func TestUpdateRejectsTruncatedSignature(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, signature: []byte("short")}, "signature.length")
}

func TestUpdateRejectsNonCanonicalManifest(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, noncanonical: true}, "canonical.syntax")
}

func TestUpdateRejectsUnknownSigner(t *testing.T) {
	_, otherKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	runRejection(t, bundleSpec{generation: 1, signer: otherKey}, "signature.unknown-key")
}

func TestUpdateRejectsWrongKind(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, mutate: func(m *release.Manifest) { m.Kind = release.KindOSImage }}, "manifest.kind")
}

func TestUpdateRejectsWrongProduct(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, rawMutate: func(data []byte) []byte {
		return []byte(strings.Replace(string(data), `"product":"bittern"`, `"product":"otherxx"`, 1))
	}}, "manifest.product")
}

func TestUpdateRejectsChannelMismatch(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, mutate: func(m *release.Manifest) { m.Channel = release.ChannelBeta }}, "channel.mismatch")
}

func TestUpdateRejectsLowerGeneration(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 2))
	source := env.writeValidBundle(t, 1)
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("lower generation was accepted: %+v", result)
	}
	requireRule(t, err, "generation.rollback")
	if result.Status != ResultRejected {
		t.Fatalf("status = %s, want rejected", result.Status)
	}
	assertPointerVerified(t, env, 2)
	if state := readStateForTest(t, env.stateRoot); state.CommittedGeneration != 2 {
		t.Fatalf("committed generation = %d, want 2", state.CommittedGeneration)
	}
}

func TestUpdateRejectsEqualGenerationAfterCommit(t *testing.T) {
	env := newTestEnv(t)
	source := env.writeValidBundle(t, 2)
	env.mustInstall(t, source)
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("equal generation was accepted: %+v", result)
	}
	requireRule(t, err, "generation.rollback")
	if result.Status != ResultRejected {
		t.Fatalf("status = %s, want rejected", result.Status)
	}
	assertPointerVerified(t, env, 2)
}

func TestUpdateRejectsResumeCandidateMismatch(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepVerifyFiles {
			return ErrPowerLoss
		}
		return nil
	}
	_, err := env.install(t, source)
	if !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("cut before verify-files did not interrupt: %v", err)
	}
	env.options.BeforeStep = nil
	journal := readJournalForTest(t, env.stateRoot)
	if journal == nil || journal.CandidateManifestSHA256 == "" {
		t.Fatalf("journal has no candidate: %+v", journal)
	}
	// Replace the staged manifest with a different valid manifest of the same
	// generation, and point the journal back at verify-manifest.
	alternate := filepath.Join(env.root, "alternate")
	if err := os.MkdirAll(alternate, 0o755); err != nil {
		t.Fatal(err)
	}
	env.writeBundle(t, alternate, bundleSpec{generation: 2, files: map[string]fileSpec{
		"share/other.txt": {data: []byte("other\n"), mode: 0o644},
	}})
	staging := filepath.Join(env.stateRoot, "app", "staging", journal.OperationID)
	for _, name := range []string{"manifest.json", "manifest.json.sig", "manifest.sig"} {
		sourceName := name
		if name == "manifest.sig" {
			sourceName = "manifest.json.sig"
		}
		data, err := os.ReadFile(filepath.Join(alternate, sourceName))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(staging, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	journal.NextStep = StepVerifyManifest
	journal.LastCompletedStep = StepFetchPayload
	if err := writeJournal(filepath.Join(env.stateRoot, "update", "journal.json"), journal); err != nil {
		t.Fatal(err)
	}
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("candidate mismatch was accepted: %+v", result)
	}
	requireRule(t, err, "journal.candidate-mismatch")
	if result.Status != ResultRejected {
		t.Fatalf("status = %s, want rejected", result.Status)
	}
	assertPointerVerified(t, env, 1)
}

func TestUpdateRejectsOSGenerationTooOld(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, mutate: func(m *release.Manifest) {
		m.Compatibility.OSGenerationMin = 2
		m.Compatibility.OSGenerationMax = 3
	}}, "compatibility.os-generation")
}

func TestUpdateRejectsOSGenerationTooNew(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, mutate: func(m *release.Manifest) {
		m.Compatibility.OSGenerationMin = 0
		m.Compatibility.OSGenerationMax = 0
	}}, "compatibility.os-generation")
}

func TestUpdateRejectsSchemaTooNew(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, mutate: func(m *release.Manifest) {
		m.Compatibility.SchemaMin = 1
		m.Compatibility.SchemaMax = 1
	}}, "compatibility.schema")
}

func TestUpdateRejectsSchemaTooOld(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, mutate: func(m *release.Manifest) {
		m.Compatibility.SchemaMin = 3
		m.Compatibility.SchemaMax = 3
	}}, "compatibility.schema")
}

func TestUpdateRejectsUpdaterTooOld(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, mutate: func(m *release.Manifest) { m.Compatibility.UpdaterMin = 2 }}, "compatibility.updater")
}

func TestUpdateRejectsPayloadSizeMismatch(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, mutate: func(m *release.Manifest) { m.Payload.Size++ }}, "payload.size")
}

func TestUpdateRejectsPayloadDigestMismatch(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, mutate: func(m *release.Manifest) {
		m.Payload.SHA256 = strings.Repeat("0", 64)
	}}, "payload.digest")
}

func TestUpdateRejectsExtraArchiveFile(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, archiveExtra: []tarEntry{{
		name: "share/extra.txt", data: []byte("extra\n"), mode: 0o644, typeflag: '0',
	}}}, "file.extra")
}

func TestUpdateRejectsMissingArchiveFile(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, archiveRemove: []string{"share/app.txt"}}, "file.missing")
}

func TestUpdateRejectsFileDigestMismatch(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, mutate: func(m *release.Manifest) {
		for index := range m.Files {
			if m.Files[index].Path == "share/app.txt" {
				m.Files[index].SHA256 = strings.Repeat("0", 64)
			}
		}
	}}, "file.digest")
}

func TestUpdateRejectsFileModeMismatch(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, mutate: func(m *release.Manifest) {
		for index := range m.Files {
			if m.Files[index].Path == "share/app.txt" {
				m.Files[index].Mode = "0755"
			}
		}
	}}, "file.mode")
}

func TestUpdateRejectsSymlinkArchiveEntry(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, archiveExtra: []tarEntry{{
		name: "share/link", typeflag: '2', linkname: "/etc/passwd", mode: 0o777,
	}}}, "archive.type")
}

func TestUpdateRejectsTrustStorePath(t *testing.T) {
	runRejection(t, bundleSpec{generation: 1, mutate: func(m *release.Manifest) {
		m.Files[0].Path = "aaaaaaaaaaaaaaaaaa"
	}, rawMutate: func(data []byte) []byte {
		return []byte(strings.Replace(string(data), "aaaaaaaaaaaaaaaaaa", "etc/bittern/trust", 1))
	}}, "manifest.trust-path")
}

func TestUpdateRejectsSecretInHistory(t *testing.T) {
	env := newTestEnv(t)
	env.data.createErr = errors.New("snapshot failed: password=super-secret-value")
	source := env.writeValidBundle(t, 1)
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("secret-bearing failure was accepted: %+v", result)
	}
	if result.Status != ResultRejected {
		t.Fatalf("status = %s, want rejected", result.Status)
	}
	historyDir := filepath.Join(env.stateRoot, "update", "history")
	entries, err := os.ReadDir(historyDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("history records = %d, want 1", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(historyDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "super-secret-value") {
		t.Fatalf("history leaked the secret: %s", data)
	}
	if !strings.Contains(string(data), "[REDACTED]") {
		t.Fatalf("history did not record a redacted failure: %s", data)
	}
}
