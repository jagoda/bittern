package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/launcher"
)

func recoveryOptions(stateRoot string) RecoveryOptions {
	return RecoveryOptions{
		StateRoot: stateRoot,
		UnitState: func() (RecoveryUnitState, error) {
			return RecoveryUnitState{Known: true}, nil
		},
		StartUnit: func() error { return nil },
		ResetUnit: func() error { return nil },
		ResolveHelper: func() (launcher.Choice, error) {
			return launcher.Choice{Path: "/bundle/usr/bin/bittern", Generation: 7}, nil
		},
		RunHelper:     func(context.Context, string) error { return nil },
		FreeSpace:     func(string) (uint64, error) { return 1 << 40, nil },
		Now:           time.Now,
		Sleep:         func(time.Duration) {},
		VerifyTimeout: 10 * time.Millisecond,
	}
}

func writeRecoveryRequest(t *testing.T, env *testEnv) {
	t.Helper()
	source := env.newSourceDir(t)
	if _, err := PrepareHandoff(env.stateRoot, source, env.options.Now()); err != nil {
		t.Fatal(err)
	}
}

func writeRecoveryJournal(t *testing.T, env *testEnv) {
	t.Helper()
	journal := &Journal{
		SchemaVersion: SchemaVersion,
		OperationID:   "20261128T120000Z-g000001-abcdef01",
		Phase:         PhaseInstall,
		State:         StateRunning,
		Steps:         []Step{{Key: StepPreflight, Kind: kindPreflight}},
		NextStep:      StepPreflight,
	}
	if err := writeJournal(env.options.paths().journal, journal); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryDetectsEachStuckState(t *testing.T) {
	t.Run("request with no unit running", func(t *testing.T) {
		env := newTestEnv(t)
		writeRecoveryRequest(t, env)
		status := DetectRecovery(recoveryOptions(env.stateRoot))
		if status.State != RecoveryRequestPending || status.Action != RecoveryActionStart {
			t.Fatalf("status = %+v, want request-pending/start", status)
		}
	})
	t.Run("request with a failed unit", func(t *testing.T) {
		env := newTestEnv(t)
		writeRecoveryRequest(t, env)
		options := recoveryOptions(env.stateRoot)
		options.UnitState = func() (RecoveryUnitState, error) {
			return RecoveryUnitState{Known: true, Failed: true, Result: "exit-code"}, nil
		}
		status := DetectRecovery(options)
		if status.State != RecoveryRequestFailed || status.Action != RecoveryActionActiveBundle {
			t.Fatalf("status = %+v, want request-failed/active-bundle", status)
		}
	})
	t.Run("journal with no unit running", func(t *testing.T) {
		env := newTestEnv(t)
		writeRecoveryJournal(t, env)
		status := DetectRecovery(recoveryOptions(env.stateRoot))
		if status.State != RecoveryJournalPending || status.Action != RecoveryActionResume {
			t.Fatalf("status = %+v, want journal-pending/resume", status)
		}
	})
	t.Run("journal with a failed unit", func(t *testing.T) {
		env := newTestEnv(t)
		writeRecoveryJournal(t, env)
		options := recoveryOptions(env.stateRoot)
		options.UnitState = func() (RecoveryUnitState, error) {
			return RecoveryUnitState{Known: true, Failed: true}, nil
		}
		status := DetectRecovery(options)
		if status.State != RecoveryJournalFailed || status.Action != RecoveryActionActiveBundle {
			t.Fatalf("status = %+v, want journal-failed/active-bundle", status)
		}
	})
	t.Run("a failed helper with a classified reason", func(t *testing.T) {
		env := newTestEnv(t)
		writeRecoveryRequest(t, env)
		if err := writeJSON(HelperFailurePath(env.stateRoot), HelperFailure{
			SchemaVersion: helperFailureSchemaVersion,
			Rule:          "config.schema-newer",
			Message:       "appliance.yaml schema version 4 is newer than this helper (3)",
			RecordedAt:    "2026-11-28T12:00:00Z",
		}, 0o600); err != nil {
			t.Fatal(err)
		}
		options := recoveryOptions(env.stateRoot)
		options.UnitState = func() (RecoveryUnitState, error) {
			return RecoveryUnitState{Known: true, Failed: true}, nil
		}
		status := DetectRecovery(options)
		if status.State != RecoveryHelperFailure || status.Action != RecoveryActionActiveBundle {
			t.Fatalf("status = %+v, want helper-failure/active-bundle", status)
		}
		if status.Reason != "config.schema-newer" {
			t.Fatalf("reason = %q, want the classified rule", status.Reason)
		}
	})
	t.Run("an incoming source with no request", func(t *testing.T) {
		env := newTestEnv(t)
		incoming := filepath.Join(env.stateRoot, "update", "incoming", incomingCandidatePrefix+"20261128")
		if err := os.MkdirAll(incoming, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(incoming, "manifest.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		status := DetectRecovery(recoveryOptions(env.stateRoot))
		if status.State != RecoveryOrphanIncoming || status.Action != RecoveryActionQuarantine {
			t.Fatalf("status = %+v, want orphan-incoming/quarantine", status)
		}
	})
	t.Run("a failed unit with no input", func(t *testing.T) {
		env := newTestEnv(t)
		options := recoveryOptions(env.stateRoot)
		options.UnitState = func() (RecoveryUnitState, error) {
			return RecoveryUnitState{Known: true, Failed: true}, nil
		}
		status := DetectRecovery(options)
		if status.State != RecoveryFailedUnit || status.Action != RecoveryActionResetUnit {
			t.Fatalf("status = %+v, want failed-unit/reset-unit", status)
		}
	})
	t.Run("insufficient space", func(t *testing.T) {
		env := newTestEnv(t)
		if err := writeCheck(env.stateRoot, Check{
			Result:    CheckUpdateAvailable,
			Available: &CheckCandidate{Generation: 2, Version: "0.0.0-beta.2", PayloadSize: 100 << 20},
		}); err != nil {
			t.Fatal(err)
		}
		options := recoveryOptions(env.stateRoot)
		options.FreeSpace = func(string) (uint64, error) { return 1 << 20, nil }
		status := DetectRecovery(options)
		if status.State != RecoveryInsufficientSpace || status.Action != RecoveryActionRecheck {
			t.Fatalf("status = %+v, want insufficient-space/recheck", status)
		}
	})
	t.Run("a live transaction", func(t *testing.T) {
		env := newTestEnv(t)
		writeRecoveryJournal(t, env)
		options := recoveryOptions(env.stateRoot)
		options.UnitState = func() (RecoveryUnitState, error) {
			return RecoveryUnitState{Known: true, Active: true}, nil
		}
		status := DetectRecovery(options)
		if status.State != RecoveryLive || status.Action != RecoveryActionNone {
			t.Fatalf("status = %+v, want live/no action", status)
		}
	})
	t.Run("an unknown unit state", func(t *testing.T) {
		env := newTestEnv(t)
		options := recoveryOptions(env.stateRoot)
		options.UnitState = func() (RecoveryUnitState, error) { return RecoveryUnitState{}, errors.New("systemctl unavailable") }
		status := DetectRecovery(options)
		if status.State != RecoveryUnknown || status.Action != RecoveryActionNone {
			t.Fatalf("status = %+v, want unknown/no action", status)
		}
	})
}

func TestRecoveryRefusesALiveTransaction(t *testing.T) {
	env := newTestEnv(t)
	writeRecoveryJournal(t, env)
	started := 0
	options := recoveryOptions(env.stateRoot)
	options.UnitState = func() (RecoveryUnitState, error) {
		return RecoveryUnitState{Known: true, Active: true}, nil
	}
	options.StartUnit = func() error { started++; return nil }
	_, err := Recover(context.Background(), options, RecoveryActionResume)
	requireRule(t, err, "recovery.live")
	if started != 0 {
		t.Fatalf("StartUnit calls = %d, want 0 for a live transaction", started)
	}
	if _, err := os.Stat(env.options.paths().journal); err != nil {
		t.Fatalf("the live journal was removed: %v", err)
	}
}

func TestRecoveryQuarantineLeavesTheStateRootIntact(t *testing.T) {
	env := newTestEnv(t)
	if err := env.options.paths().ensure(); err != nil {
		t.Fatal(err)
	}
	if err := writeState(env.options.paths().state, &State{SchemaVersion: SchemaVersion, CommittedGeneration: 3, CommittedVersion: "0.0.0-beta.3"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(env.options.paths().history, 0o755); err != nil {
		t.Fatal(err)
	}
	historyPath := env.options.paths().historyPath("20261128T120000Z-g000003-abcdef01")
	if err := os.WriteFile(historyPath, []byte(`{"result":"committed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	incoming := filepath.Join(env.stateRoot, "update", "incoming", incomingCandidatePrefix+"20261128")
	if err := os.MkdirAll(incoming, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(incoming, "manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateBefore := digestFile(t, env.options.paths().state)
	historyBefore := digestFile(t, historyPath)

	result, err := Recover(context.Background(), recoveryOptions(env.stateRoot), RecoveryActionQuarantine)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RecoveryRecovered {
		t.Fatalf("result = %+v, want recovered", result)
	}
	if digestFile(t, env.options.paths().state) != stateBefore {
		t.Fatal("state.json changed during quarantine")
	}
	if digestFile(t, historyPath) != historyBefore {
		t.Fatal("a history record changed during quarantine")
	}
	entries, err := os.ReadDir(filepath.Join(env.stateRoot, "update", "incoming"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("incoming still has %d entries", len(entries))
	}
	quarantined, err := os.ReadDir(filepath.Join(env.stateRoot, "update", "quarantine"))
	if err != nil {
		t.Fatal(err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantine = %d entries, want 1", len(quarantined))
	}
	moved := filepath.Join(env.stateRoot, "update", "quarantine", quarantined[0].Name(), incomingCandidatePrefix+"20261128")
	if _, err := os.Stat(filepath.Join(moved, "manifest.json")); err != nil {
		t.Fatalf("the moved download is missing: %v", err)
	}
}

func TestRecoveryActiveBundleCompletesAPendingUpdate(t *testing.T) {
	env := newTestEnv(t)
	source := env.writeValidBundle(t, 1)
	if _, err := PrepareHandoff(env.stateRoot, source, env.options.Now()); err != nil {
		t.Fatal(err)
	}
	options := recoveryOptions(env.stateRoot)
	options.StateRoot = env.stateRoot
	options.VerifyTimeout = 5 * time.Second
	options.UnitState = func() (RecoveryUnitState, error) {
		return RecoveryUnitState{Known: true, Failed: true}, nil
	}
	options.RunHelper = func(ctx context.Context, path string) error {
		if path != "/bundle/usr/bin/bittern" {
			t.Fatalf("helper path = %q, want the resolved active bundle binary", path)
		}
		go func() {
			_, _ = RunHelper(helperOptionsFromEnv(env))
		}()
		return nil
	}
	result, err := Recover(context.Background(), options, RecoveryActionActiveBundle)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RecoveryRecovered {
		t.Fatalf("result = %+v, want recovered", result)
	}
	if _, err := os.Stat(env.options.paths().request); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the request remains after recovery: %v", err)
	}
	if _, err := os.Stat(env.options.paths().journal); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the journal remains after recovery: %v", err)
	}
	record, err := LatestHistory(env.stateRoot)
	if err != nil || record == nil {
		t.Fatalf("history: %v, %v", record, err)
	}
	if record.Result != ResultCommitted {
		t.Fatalf("history result = %q, want committed", record.Result)
	}
}

func TestRecoveryReportsTheUnderlyingFailure(t *testing.T) {
	env := newTestEnv(t)
	writeRecoveryRequest(t, env)
	options := recoveryOptions(env.stateRoot)
	options.VerifyTimeout = 20 * time.Millisecond
	options.UnitState = func() (RecoveryUnitState, error) {
		return RecoveryUnitState{Known: true, Failed: true}, nil
	}
	options.RunHelper = func(context.Context, string) error {
		return writeJSON(HelperFailurePath(env.stateRoot), HelperFailure{
			SchemaVersion: helperFailureSchemaVersion,
			Rule:          "config.invalid",
			Message:       "the appliance configuration is invalid: schema_version value must be 2",
			RecordedAt:    "2026-11-28T12:00:01Z",
		}, 0o600)
	}
	result, err := Recover(context.Background(), options, RecoveryActionActiveBundle)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != RecoveryFailed || result.Reason != "config.invalid" {
		t.Fatalf("result = %+v, want failed with config.invalid", result)
	}
	if !strings.Contains(result.Detail, "schema_version value must be 2") {
		t.Fatalf("detail = %q, want the underlying reason", result.Detail)
	}
}

func TestRecoveryNeverDeletesStateHistoryOrAJournal(t *testing.T) {
	actions := []RecoveryAction{
		RecoveryActionStart, RecoveryActionResume, RecoveryActionActiveBundle,
		RecoveryActionQuarantine, RecoveryActionResetUnit, RecoveryActionRecheck,
	}
	for _, action := range actions {
		t.Run(string(action), func(t *testing.T) {
			env := newTestEnv(t)
			if err := env.options.paths().ensure(); err != nil {
				t.Fatal(err)
			}
			if err := writeState(env.options.paths().state, &State{SchemaVersion: SchemaVersion, CommittedGeneration: 2}); err != nil {
				t.Fatal(err)
			}
			historyPath := env.options.paths().historyPath("20261128T120000Z-g000002-abcdef01")
			if err := os.WriteFile(historyPath, []byte(`{"result":"committed"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			writeRecoveryJournal(t, env)
			if _, err := Recover(context.Background(), recoveryOptions(env.stateRoot), action); err == nil {
				// A mismatch refusal is the expected result for most actions.
			}
			for _, path := range []string{env.options.paths().state, historyPath, env.options.paths().journal} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("%s was removed by %s: %v", path, action, err)
				}
			}
		})
	}
}

func TestRecoveryTransientUnitMirrorsTheFixedUnit(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "bittern-update.service"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, property := range recoverySandboxProperties {
		value := strings.TrimPrefix(property, "--property=")
		if !strings.Contains(text, value) {
			t.Fatalf("the fixed unit does not carry %q; the transient recovery unit drifted", value)
		}
	}
}

func helperOptionsFromEnv(env *testEnv) HelperOptions {
	return HelperOptions{
		StateRoot:        env.options.StateRoot,
		ConfigPath:       env.options.ConfigPath,
		FrigateConfig:    env.options.FrigateConfig,
		TrustImageDir:    env.options.TrustImageDir,
		TrustRotationDir: env.options.TrustRotationDir,
		SecretsPath:      env.options.SecretsPath,
		System:           env.options.System,
		Data:             env.options.Data,
		Image:            env.options.Image,
		Platform:         env.options.Platform,
		Base:             env.options.Base,
		Now:              env.options.Now,
		ConfigApplier:    env.options.ConfigApplier,
		BaselineBinary:   env.options.BaselineBinary,
	}
}

func digestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestRecordHelperOutcomeKeepsTheClassifiedReason(t *testing.T) {
	env := newTestEnv(t)
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	err := errorf("config.schema-newer", "appliance.yaml schema version 4 is newer than this helper (3)")
	if recordErr := RecordHelperOutcome(env.stateRoot, Result{OperationID: "20261128T120000Z-g000007-abcdef01"}, err, now); recordErr != nil {
		t.Fatal(recordErr)
	}
	failure, readErr := ReadHelperFailure(env.stateRoot)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if failure == nil || failure.Rule != "config.schema-newer" {
		t.Fatalf("failure = %+v, want the config rule", failure)
	}
	if !strings.Contains(failure.Message, "schema version 4") {
		t.Fatalf("message = %q, want the redacted reason", failure.Message)
	}
	if info, statErr := os.Stat(HelperFailurePath(env.stateRoot)); statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("record mode = %v, %v, want 0600", info, statErr)
	}
	if recordErr := RecordHelperOutcome(env.stateRoot, Result{Status: ResultCommitted}, nil, now); recordErr != nil {
		t.Fatal(recordErr)
	}
	failure, readErr = ReadHelperFailure(env.stateRoot)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if failure != nil {
		t.Fatalf("failure = %+v, want removed after a commit", failure)
	}
}
