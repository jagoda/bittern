package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/release"
)

func stageBundleFromSource(t *testing.T, staging, sourceDir string, manifest *release.Manifest) {
	t.Helper()
	manifestBytes, err := os.ReadFile(filepath.Join(sourceDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "manifest.json"), manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	signature, err := os.ReadFile(filepath.Join(sourceDir, "manifest.json.sig"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "manifest.sig"), signature, 0o600); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(sourceDir, manifest.Payload.Filename)
	payloadBytes, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, manifest.Payload.Filename), payloadBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(staging, "tree")
	modes, err := extractArchive(payload, tree)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyStagedFiles(tree, manifest, modes); err != nil {
		t.Fatal(err)
	}
}

func replaceBundleFromSource(t *testing.T, env *testEnv, bundleGeneration int64, sourceDir string, sourceGeneration int64) {
	t.Helper()
	destination := env.options.paths().bundleDir(bundleGeneration)
	if err := os.RemoveAll(destination); err != nil {
		t.Fatal(err)
	}
	final := env.seedBundle(t, sourceDir, sourceGeneration)
	if final != destination {
		if err := os.Rename(final, destination); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUpdateResumeRejectsDifferentGenerationManifest(t *testing.T) {
	for _, swapped := range []bool{false, true} {
		name := "manifest-only"
		if swapped {
			name = "swapped-tree-and-payload"
		}
		t.Run(name, func(t *testing.T) {
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
			journal := readJournalForTest(t, env.stateRoot)
			staging := filepath.Join(env.stateRoot, "app", "staging", journal.OperationID)

			alternate := env.newSourceDir(t)
			alternateManifest, alternateBytes, alternateSignature := env.writeBundle(t, alternate, bundleSpec{
				generation: 3,
				files: map[string]fileSpec{
					"share/alternate.txt": {data: []byte("alternate generation\n"), mode: 0o644},
				},
			})
			if swapped {
				stageBundleFromSource(t, staging, alternate, alternateManifest)
			} else {
				if err := os.WriteFile(filepath.Join(staging, "manifest.json"), alternateBytes, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(staging, "manifest.sig"), alternateSignature, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			env.system.calls = nil
			env.options.BeforeStep = nil

			result, err := env.install(t, source)
			if err == nil {
				t.Fatalf("resume accepted a different generation manifest: %+v", result)
			}
			requireRule(t, err, "journal.candidate-mismatch")
			if result.Status != ResultRejected {
				t.Fatalf("status = %s, want rejected", result.Status)
			}
			assertPointerVerified(t, env, 1)
			if calls := env.system.snapshot(); len(calls) != 0 {
				t.Fatalf("candidate mismatch performed unit actions: %v", calls)
			}
			if data, readErr := os.ReadFile(filepath.Join(staging, "manifest.json")); readErr != nil || string(data) != string(alternateBytes) {
				t.Fatalf("staging manifest was not left intact: %v", readErr)
			}
			if swapped {
				if _, statErr := os.Stat(filepath.Join(staging, "tree", "share", "alternate.txt")); statErr != nil {
					t.Fatalf("swapped staging tree was not left intact: %v", statErr)
				}
			}
		})
	}
}

func TestUpdateActivateRejectsReplacedBundle(t *testing.T) {
	for _, alternateGeneration := range []int64{3, 2} {
		name := "different-generation"
		if alternateGeneration == 2 {
			name = "same-generation"
		}
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t)
			env.mustInstall(t, env.writeValidBundle(t, 1))
			source := env.writeValidBundle(t, 2)
			env.options.BeforeStep = func(step Step) error {
				if step.Key == StepActivate {
					return ErrPowerLoss
				}
				return nil
			}
			_, err := env.install(t, source)
			if !errors.Is(err, ErrPowerLoss) {
				t.Fatalf("cut before activate did not interrupt: %v", err)
			}
			alternate := env.newSourceDir(t)
			alternateManifest, _, _ := env.writeBundle(t, alternate, bundleSpec{
				generation: alternateGeneration,
				files: map[string]fileSpec{
					"share/alternate.txt": {data: []byte("replacement bundle\n"), mode: 0o644},
				},
			})
			replaceBundleFromSource(t, env, 2, alternate, alternateManifest.Generation)

			callsAtActivate := -1
			unitActionDuringActivate := false
			pointerMovedDuringActivate := false
			env.options.BeforeStep = func(step Step) error {
				switch step.Key {
				case StepActivate:
					callsAtActivate = len(env.system.calls)
				case StepRollbackVerify:
					if callsAtActivate >= 0 {
						if len(env.system.calls) != callsAtActivate {
							unitActionDuringActivate = true
						}
						if current := readCurrentForTest(t, env.stateRoot); current != 1 {
							pointerMovedDuringActivate = true
						}
					}
				}
				return nil
			}
			result, err := env.install(t, source)
			if err == nil {
				t.Fatalf("resume accepted a replaced candidate bundle: %+v", result)
			}
			if result.Status != ResultRolledBack {
				t.Fatalf("status = %s, want rolled-back", result.Status)
			}
			if result.FailingStep != StepActivate {
				t.Fatalf("failing step = %s, want %s", result.FailingStep, StepActivate)
			}
			if !strings.Contains(result.Failure, "journal.candidate-mismatch") {
				t.Fatalf("failure = %q, want journal.candidate-mismatch", result.Failure)
			}
			if unitActionDuringActivate {
				t.Fatal("activate performed a unit action before refusing the bundle")
			}
			if pointerMovedDuringActivate {
				t.Fatal("activate moved the pointer before refusing the bundle")
			}
			if current := readCurrentForTest(t, env.stateRoot); current != 1 {
				t.Fatalf("pointer = %d, want the previous generation 1", current)
			}
			assertPointerVerified(t, env, 1)
		})
	}
}

func TestUpdateRollbackPointerReVerifiesPreviousBundle(t *testing.T) {
	env := newTestEnv(t)
	env.mustInstall(t, env.writeValidBundle(t, 1))
	source := env.writeValidBundle(t, 2)
	env.system.failProbe["frigate-api"] = 1
	env.options.BeforeStep = func(step Step) error {
		if step.Key == StepRollbackPointer {
			return ErrPowerLoss
		}
		return nil
	}
	_, err := env.install(t, source)
	if !errors.Is(err, ErrPowerLoss) {
		t.Fatalf("rollback was not interrupted before the pointer: %v", err)
	}
	journal := readJournalForTest(t, env.stateRoot)
	if journal == nil || journal.NextStep != StepRollbackPointer {
		t.Fatalf("journal before rollback pointer = %+v", journal)
	}
	if err := os.WriteFile(filepath.Join(env.stateRoot, "app", "bundles", "1", "manifest.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.options.BeforeStep = nil
	result, err := env.install(t, source)
	if err == nil {
		t.Fatalf("resumed rollback moved the pointer without verifying the previous bundle: %+v", result)
	}
	if result.Status != StateRollbackFailed {
		t.Fatalf("status = %s, want rollback-failed", result.Status)
	}
	if result.RollbackFailedStep != StepRollbackPointer {
		t.Fatalf("rollback failed step = %s, want %s", result.RollbackFailedStep, StepRollbackPointer)
	}
	record, readErr := readHistory(filepath.Join(env.stateRoot, "update", "history"), result.OperationID)
	if readErr != nil {
		t.Fatalf("read state-unknown history: %v", readErr)
	}
	if record.Result != ResultStateUnknown {
		t.Fatalf("history result = %s, want state-unknown", record.Result)
	}
	assertPointerVerified(t, env, 2)
}
