package update

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
)

// testConfigApplier is the update tests' ConfigApplier. It runs the real apply
// pipeline in-process against a temporary live root, so the tests exercise the
// same renderer, target map, native-check order, atomic writer, and rollback
// plan a manual apply uses without executing a bundle binary.
type testConfigApplier struct {
	env           *testEnv
	system        *testApplySystem
	applyCalls    int
	rollbackCalls int
	requests      []ApplyConfigRequest
	beforeApply   func(ApplyConfigRequest)
}

func (a *testConfigApplier) ApplyConfig(request ApplyConfigRequest) (ApplyConfigResult, error) {
	a.applyCalls++
	a.requests = append(a.requests, request)
	if a.beforeApply != nil {
		a.beforeApply(request)
	}
	result := ApplyConfigResult{SchemaVersion: 1, OperationID: request.OperationID, Result: "applied"}
	data, err := os.ReadFile(a.env.configPath)
	if err != nil {
		result.Result = "failed"
		result.Failure = err.Error()
		return result, err
	}
	cfg, validationErrors := config.Parse(data)
	if len(validationErrors) > 0 {
		result.Result = "failed"
		result.Failure = validationErrors[0].Error()
		return result, fmt.Errorf("%s", result.Failure)
	}
	staged, applyErr := apply.ApplyStaged(cfg, apply.Options{
		StateRoot:   a.env.stateRoot,
		LiveRoot:    a.env.root,
		System:      a.system,
		Secrets:     a.env.options.Secrets,
		SecretsPath: a.env.options.SecretsPath,
		Now:         a.env.options.Now,
	})
	result.Generation = staged.Generation
	result.PreviousGeneration = staged.PreviousGeneration
	result.Wrote = staged.Wrote
	result.ChangedFiles = staged.Changed
	result.RestartedUnits = staged.RestartedUnits
	result.FailedStep = staged.FailedStep
	result.Failure = staged.Failure
	for _, check := range staged.Checks {
		result.Checks = append(result.Checks, ConfigApplyCheck{Name: check.Name, Status: check.Status, Reason: check.Reason})
	}
	if applyErr != nil {
		result.Result = "failed"
		if result.Failure == "" {
			result.Failure = applyErr.Error()
		}
	}
	return result, applyErr
}

func (a *testConfigApplier) RollbackConfig(request RollbackConfigRequest) error {
	a.rollbackCalls++
	_, err := apply.RollbackStaged(apply.Options{
		StateRoot: a.env.stateRoot,
		LiveRoot:  a.env.root,
		System:    a.system,
	}, request.PreviousGeneration)
	return err
}

// testApplySystem is the apply System the test applier uses. It writes into the
// test live root and records every call so a test can assert the check-before-
// write order and which units were restarted.
type testApplySystem struct {
	calls      []string
	failCheck  string
	failWrite  string
	failOnce   bool
	writeTried bool
	active     map[string]bool
	daemonLoad int
}

func newTestApplySystem() *testApplySystem {
	return &testApplySystem{active: map[string]bool{}}
}

func (s *testApplySystem) WriteFile(path string, data []byte, mode fs.FileMode) error {
	s.calls = append(s.calls, "write:"+path)
	if s.failWrite != "" && strings.Contains(path, s.failWrite) && (!s.failOnce || !s.writeTried) {
		s.writeTried = true
		return fmt.Errorf("injected write failure for %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func (s *testApplySystem) RemoveFile(path string) error {
	s.calls = append(s.calls, "remove:"+path)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *testApplySystem) UdevReload() error {
	s.calls = append(s.calls, "udev:reload")
	return nil
}

func (s *testApplySystem) UdevTriggerNetwork() error {
	s.calls = append(s.calls, "udev:trigger")
	return nil
}

func (s *testApplySystem) WaitForInterfaces(names []string, timeout time.Duration) error {
	s.calls = append(s.calls, "udev:wait")
	return nil
}

func (s *testApplySystem) EnableUnit(unit string) error {
	s.calls = append(s.calls, "enable:"+unit)
	return nil
}

func (s *testApplySystem) DisableUnit(unit string) error {
	s.calls = append(s.calls, "disable:"+unit)
	return nil
}

func (s *testApplySystem) StartUnit(unit string) error {
	s.calls = append(s.calls, "start:"+unit)
	s.active[unit] = true
	return nil
}

func (s *testApplySystem) RestartUnit(unit string) error {
	s.calls = append(s.calls, "restart:"+unit)
	s.active[unit] = true
	return nil
}

func (s *testApplySystem) UnitActive(unit string) (bool, error) {
	return s.active[unit], nil
}

func (s *testApplySystem) DaemonReload() error {
	s.calls = append(s.calls, "daemon-reload")
	s.daemonLoad++
	return nil
}

func (s *testApplySystem) RunCheck(check apply.Check) apply.CheckResult {
	s.calls = append(s.calls, "check:"+check.Kind)
	if s.failCheck != "" && (check.Kind == s.failCheck || strings.Contains(check.Name, s.failCheck)) {
		return apply.CheckResult{Name: check.Name, Kind: check.Kind, Status: apply.CheckFailed, Reason: "injected validation failure"}
	}
	return apply.CheckResult{Name: check.Name, Kind: check.Kind, Status: apply.CheckPassed}
}

func (s *testApplySystem) ProbeHealth(probe apply.Probe) apply.ProbeResult {
	s.calls = append(s.calls, "health:"+probe.Name)
	return apply.ProbeResult{Name: probe.Name, Kind: probe.Kind, Status: apply.ProbePassed}
}

func (s *testApplySystem) firstIndex(prefix string) int {
	for index, call := range s.calls {
		if strings.HasPrefix(call, prefix) {
			return index
		}
	}
	return -1
}

// seedApply runs one manual apply so the update has a real previous
// configuration generation and live files to preserve or restore.
func (e *testEnv) seedApply(t *testing.T) {
	t.Helper()
	cfg, validationErrors := config.Load(e.configPath)
	if len(validationErrors) > 0 {
		t.Fatalf("seed apply config is invalid: %v", validationErrors[0])
	}
	_, err := apply.Apply(cfg, apply.Options{
		StateRoot:   e.stateRoot,
		LiveRoot:    e.root,
		System:      newTestApplySystem(),
		Secrets:     e.options.Secrets,
		SecretsPath: e.options.SecretsPath,
		Now:         e.options.Now,
	})
	if err != nil {
		t.Fatalf("seed apply failed: %v", err)
	}
}
