package update

import "testing"

// The fake system models the fixed launcher: a restart reads the activation
// pointer and the active bundle's manifest version. These cases prove the start
// step restarts the agent, so an update runs the new bundle and a rollback runs
// the previous one.

func TestUpdateRestartRunsTheNewBundleVersion(t *testing.T) {
	env := newTestEnv(t)
	env.system.restartVersion = func(unit string) string {
		if unit != "bitternd.service" {
			return ""
		}
		return env.runningAgentVersion(t)
	}
	env.mustInstall(t, env.writeValidBundle(t, 1))
	if got := env.system.runningVersion["bitternd.service"]; got != "0.1.1" {
		t.Fatalf("running version after generation 1 = %q, want 0.1.1", got)
	}
	env.mustInstall(t, env.writeValidBundle(t, 2))
	if got := env.system.runningVersion["bitternd.service"]; got != "0.1.2" {
		t.Fatalf("running version after generation 2 = %q, want 0.1.2", got)
	}
}

func TestUpdateRollbackRestartsThePreviousBundleVersion(t *testing.T) {
	env := newTestEnv(t)
	env.system.restartVersion = func(unit string) string {
		if unit != "bitternd.service" {
			return ""
		}
		return env.runningAgentVersion(t)
	}
	env.mustInstall(t, env.writeValidBundle(t, 1))
	env.mustInstall(t, env.writeValidBundle(t, 2))
	env.system.failProbe["agent-unit"] = 1
	result, err := env.install(t, env.writeValidBundle(t, 3))
	if err == nil {
		t.Fatalf("health failure reported success: %+v", result)
	}
	if result.Status != ResultRolledBack {
		t.Fatalf("status = %s, want rolled-back", result.Status)
	}
	if got := env.system.runningVersion["bitternd.service"]; got != "0.1.2" {
		t.Fatalf("running version after rollback = %q, want 0.1.2", got)
	}
	assertPointerVerified(t, env, 2)
}
