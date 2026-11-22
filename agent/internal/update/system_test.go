package update

import (
	"errors"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/apply"
)

type fakeUnitRunner struct {
	calls      []string
	active     []bool
	activeErr  error
	probe      apply.Probe
	probeState apply.ProbeResult
	stopErr    error
	restartErr error
}

func (f *fakeUnitRunner) StopUnit(unit string) error {
	f.calls = append(f.calls, "stop:"+unit)
	return f.stopErr
}

func (f *fakeUnitRunner) RestartUnit(unit string) error {
	f.calls = append(f.calls, "restart:"+unit)
	return f.restartErr
}

func (f *fakeUnitRunner) ProbeHealth(probe apply.Probe) apply.ProbeResult {
	f.probe = probe
	f.calls = append(f.calls, "probe:"+probe.Name)
	return f.probeState
}

func (f *fakeUnitRunner) UnitActive(unit string) (bool, error) {
	f.calls = append(f.calls, "active:"+unit)
	if f.activeErr != nil {
		return false, f.activeErr
	}
	if len(f.active) == 0 {
		return false, nil
	}
	value := f.active[0]
	f.active = f.active[1:]
	return value, nil
}

func TestExecSystemDelegatesToTheApplyUnitAndProbeKinds(t *testing.T) {
	runner := &fakeUnitRunner{
		probeState: apply.ProbeResult{Name: "frigate-api", Kind: apply.ProbeHTTP, Status: apply.ProbePassed},
	}
	system := &ExecSystem{Runner: runner}
	if err := system.StopUnit("bittern-frigate.service"); err != nil {
		t.Fatal(err)
	}
	if err := system.RestartUnit("bitternd.service"); err != nil {
		t.Fatal(err)
	}
	result := system.ProbeHealth(apply.FrigateHealthProbe())
	if result.Status != apply.ProbePassed {
		t.Fatalf("probe result = %+v, want passed", result)
	}
	if runner.probe.Kind != apply.ProbeHTTP || runner.probe.Target != apply.FrigateHealthURL {
		t.Fatalf("probe = %+v, want the existing Frigate health probe", runner.probe)
	}
	want := []string{"stop:bittern-frigate.service", "restart:bitternd.service", "probe:frigate-api"}
	if strings.Join(runner.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", runner.calls, want)
	}
}

func TestExecSystemWaitsForAStartedUnitToBecomeActive(t *testing.T) {
	runner := &fakeUnitRunner{active: []bool{false, false, true}}
	system := &ExecSystem{Runner: runner, Interval: time.Millisecond}
	if err := system.WaitUnitActive("bitternd.service", time.Second); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("calls = %v, want three active polls", runner.calls)
	}
}

func TestExecSystemReportsAUnitThatNeverBecomesActive(t *testing.T) {
	runner := &fakeUnitRunner{}
	system := &ExecSystem{Runner: runner, Interval: time.Millisecond}
	err := system.WaitUnitActive("bitternd.service", 10*time.Millisecond)
	if err == nil {
		t.Fatal("an inactive unit was accepted")
	}
	if !strings.Contains(err.Error(), "did not report active") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecSystemReportsTheLastUnitQueryError(t *testing.T) {
	runner := &fakeUnitRunner{activeErr: errors.New("systemd is unavailable")}
	system := &ExecSystem{Runner: runner, Interval: time.Millisecond}
	err := system.WaitUnitActive("bitternd.service", 10*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "systemd is unavailable") {
		t.Fatalf("error = %v, want the last systemd error", err)
	}
}
