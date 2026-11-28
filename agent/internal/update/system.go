package update

import (
	"time"

	"bittern.invalid/agent/internal/apply"
)

// UnitRunner is the subset of apply.ExecSystem the update transaction uses.
// The production value is *apply.ExecSystem; tests supply a fake.
type UnitRunner interface {
	StopUnit(unit string) error
	RestartUnit(unit string) error
	ProbeHealth(probe apply.Probe) apply.ProbeResult
	UnitActive(unit string) (bool, error)
}

// ExecSystem implements UnitSystem and UnitWaiter with the existing apply
// systemd commands and probe kinds. Stop, start, and probes are delegated, so
// there is no second systemctl path and no second probe implementation.
type ExecSystem struct {
	Runner   UnitRunner
	Interval time.Duration
}

func (s *ExecSystem) runner() UnitRunner {
	if s.Runner == nil {
		return apply.NewExecSystem()
	}
	return s.Runner
}

// StopUnit stops a unit through apply's systemd path.
func (s *ExecSystem) StopUnit(unit string) error { return s.runner().StopUnit(unit) }

// RestartUnit restarts a unit through apply's systemd path. The update engine
// restarts the agent after activation so the fixed launcher execs the new
// bundle, and restarts the previous bundle after a rollback.
func (s *ExecSystem) RestartUnit(unit string) error { return s.runner().RestartUnit(unit) }

// ProbeHealth delegates to apply's probe implementation. The update engine
// passes apply.ProbeUnit for units and apply.FrigateHealthProbe() for Frigate.
func (s *ExecSystem) ProbeHealth(probe apply.Probe) apply.ProbeResult {
	return s.runner().ProbeHealth(probe)
}

// DaemonReload asks systemd to read the unit files the subsystems step wrote.
func (s *ExecSystem) DaemonReload() error {
	if manager, ok := s.runner().(interface{ DaemonReload() error }); ok {
		return manager.DaemonReload()
	}
	return errorf("system.unavailable", "the unit runner cannot run systemctl daemon-reload")
}

// UnitEnabled reports whether systemd will start the unit at boot. The
// subsystems step compares it with the signed declaration after enabling or
// disabling the unit.
func (s *ExecSystem) UnitEnabled(unit string) (bool, error) {
	if manager, ok := s.runner().(interface{ UnitEnabled(string) (bool, error) }); ok {
		return manager.UnitEnabled(unit)
	}
	return false, errorf("system.unavailable", "the unit runner cannot run systemctl is-enabled")
}

// WaitUnitActive waits for a started unit to report active. It is the
// UnitWaiter the engine uses after start-agent and start-frigate.
func (s *ExecSystem) WaitUnitActive(unit string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = time.Minute
	}
	interval := s.Interval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	lastReason := "the unit is not active"
	for {
		active, err := s.runner().UnitActive(unit)
		if err == nil && active {
			return nil
		}
		if err != nil {
			lastReason = err.Error()
		}
		if !time.Now().Before(deadline) {
			return errorf("unit.start", "%s did not report active within %s: %s", unit, timeout, lastReason)
		}
		time.Sleep(interval)
	}
}
