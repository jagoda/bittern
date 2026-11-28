package launcher

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func fixture(t *testing.T, generation int64) (Config, string) {
	t.Helper()
	root := t.TempDir()
	stateRoot := filepath.Join(root, "var", "lib", "bittern")
	baseline := filepath.Join(root, "usr", "bin", "bitternd")
	helperBaseline := filepath.Join(root, "usr", "bin", "bittern")
	bundleBinary := filepath.Join(stateRoot, "app", "bundles", strconv.FormatInt(generation, 10), "files", "usr", "bin", "bitternd")
	helperBundleBinary := filepath.Join(stateRoot, "app", "bundles", strconv.FormatInt(generation, 10), "files", "usr", "bin", "bittern")
	for _, directory := range []string{filepath.Dir(baseline), filepath.Dir(helperBaseline), filepath.Dir(bundleBinary), filepath.Join(stateRoot, "app")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(baseline, []byte("#!/bin/sh\necho baseline\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(baseline, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helperBaseline, []byte("#!/bin/sh\necho helper baseline\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(helperBaseline, 0o755); err != nil {
		t.Fatal(err)
	}
	if generation > 0 {
		if err := os.WriteFile(bundleBinary, []byte("#!/bin/sh\necho bundle\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(bundleBinary, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(helperBundleBinary, []byte("#!/bin/sh\necho helper bundle\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(helperBundleBinary, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	config := Config{
		StateRoot: stateRoot,
		Pointer:   filepath.Join(stateRoot, "app", "current"),
		Baseline:  baseline,
		AgentPath: "usr/bin/bitternd",
		UID:       func() int { return os.Getuid() },
	}
	return config, bundleBinary
}

func writePointer(t *testing.T, config Config, content string) {
	t.Helper()
	if err := os.WriteFile(config.Pointer, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLauncherPicksTheActiveBundle(t *testing.T) {
	config, bundleBinary := fixture(t, 2)
	writePointer(t, config, "2\n")
	choice, err := Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	if choice.Baseline {
		t.Fatalf("choice = %+v, want the active bundle", choice)
	}
	if choice.Path != bundleBinary || choice.Generation != 2 {
		t.Fatalf("choice = %+v, want %s generation 2", choice, bundleBinary)
	}
}

func TestLauncherFallsBackToTheBaselineWithoutAPointer(t *testing.T) {
	config, _ := fixture(t, 2)
	choice, err := Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	if !choice.Baseline || choice.Path != config.Baseline {
		t.Fatalf("choice = %+v, want the baseline %s", choice, config.Baseline)
	}
}

func TestLauncherFallsBackToTheBaselineForGenerationZero(t *testing.T) {
	config, _ := fixture(t, 2)
	writePointer(t, config, "0\n")
	choice, err := Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	if !choice.Baseline || choice.Path != config.Baseline {
		t.Fatalf("choice = %+v, want the baseline %s", choice, config.Baseline)
	}
}

func TestLauncherFallsBackToTheBaselineWhenThePointerIsUnreadable(t *testing.T) {
	config, _ := fixture(t, 2)
	if err := os.Mkdir(config.Pointer, 0o755); err != nil {
		t.Fatal(err)
	}
	choice, err := Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	if !choice.Baseline || choice.Path != config.Baseline {
		t.Fatalf("choice = %+v, want the baseline %s", choice, config.Baseline)
	}
}

func TestLauncherRejectsARelativePath(t *testing.T) {
	config, _ := fixture(t, 2)
	config.StateRoot = "var/lib/bittern"
	config.Pointer = filepath.Join(config.StateRoot, "app", "current")
	if _, err := Resolve(config); err == nil || !strings.Contains(err.Error(), "not absolute") {
		t.Fatalf("error = %v, want a relative path refusal", err)
	}
}

func TestLauncherRejectsAnEscapingPointerPath(t *testing.T) {
	config, _ := fixture(t, 2)
	config.Pointer = config.StateRoot + "/app/../../current"
	if _, err := Resolve(config); err == nil || !strings.Contains(err.Error(), "is not a clean path") {
		t.Fatalf("error = %v, want a path refusal", err)
	}
}

func TestLauncherRejectsAnEscapingBundlePath(t *testing.T) {
	config, _ := fixture(t, 2)
	writePointer(t, config, "2\n")
	config.AgentPath = "../../../../usr/bin/evil"
	if _, err := Resolve(config); err == nil || !strings.Contains(err.Error(), "not a local path") {
		t.Fatalf("error = %v, want a path refusal", err)
	}
}

func TestLauncherRejectsAMalformedPointer(t *testing.T) {
	config, _ := fixture(t, 2)
	writePointer(t, config, "../../../../usr/bin/evil\n")
	if _, err := Resolve(config); err == nil || !strings.Contains(err.Error(), "not a generation") {
		t.Fatalf("error = %v, want a pointer refusal", err)
	}
}

func TestLauncherRejectsASymlinkedBundleBinary(t *testing.T) {
	config, bundleBinary := fixture(t, 2)
	writePointer(t, config, "2\n")
	realBinary := filepath.Join(t.TempDir(), "bitternd")
	if err := os.WriteFile(realBinary, []byte("#!/bin/sh\necho bundle\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(bundleBinary); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBinary, bundleBinary); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(config); err == nil || !strings.Contains(err.Error(), "is a symlink") {
		t.Fatalf("error = %v, want a symlink refusal", err)
	}
}

func TestLauncherRejectsABundleBinaryWithTheWrongOwner(t *testing.T) {
	config, _ := fixture(t, 2)
	writePointer(t, config, "2\n")
	config.UID = func() int { return os.Getuid() + 1 }
	if _, err := Resolve(config); err == nil || !strings.Contains(err.Error(), "owner uid") {
		t.Fatalf("error = %v, want an owner refusal", err)
	}
}

func TestLauncherRejectsAMissingBundleBinary(t *testing.T) {
	config, _ := fixture(t, 2)
	writePointer(t, config, "3\n")
	if _, err := Resolve(config); err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("error = %v, want a missing binary refusal", err)
	}
}

func TestAgentUnitStartsTheFixedLauncher(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "packaging", "bitternd.service"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "ExecStart=/usr/libexec/bittern/bitternd --ui") {
		t.Fatalf("bitternd.service does not start the fixed launcher:\n%s", text)
	}
	if strings.Contains(text, "ExecStart=/usr/bin/bitternd") {
		t.Fatalf("bitternd.service still starts the installed binary:\n%s", text)
	}
}

func TestPreparationKitInstallsTheLauncherAndPlatformMetadata(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "packaging", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{
		"bittern-launch",
		"/usr/libexec/bittern/bitternd",
		"/usr/libexec/bittern/bittern-update",
		"platform.json",
		"/usr/lib/bittern/platform.json",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("installer is missing %q", required)
		}
	}
}

// TestHelperLauncherPicksTheActiveBundleThenFallsBackToTheBaseline is the
// update helper's version of the agent launcher contract. The fixed unit starts
// the launcher under its second installed name; the launcher must resolve the
// active bundle's usr/bin/bittern and fall back to /usr/bin/bittern.
func TestHelperLauncherPicksTheActiveBundleThenFallsBackToTheBaseline(t *testing.T) {
	config, _ := fixture(t, 2)
	helperConfig := ConfigForName("/usr/libexec/bittern/bittern-update")
	helperConfig.StateRoot = config.StateRoot
	helperConfig.Pointer = config.Pointer
	helperConfig.Baseline = filepath.Join(config.StateRoot, "..", "..", "..", "usr", "bin", "bittern")
	helperConfig.UID = config.UID

	writePointer(t, config, "2\n")
	choice, err := Resolve(helperConfig)
	if err != nil {
		t.Fatal(err)
	}
	wantBundle := filepath.Join(config.StateRoot, "app", "bundles", "2", "files", "usr", "bin", "bittern")
	if choice.Baseline || choice.Path != wantBundle || choice.Generation != 2 {
		t.Fatalf("choice = %+v, want the active bundle %s", choice, wantBundle)
	}

	if err := os.WriteFile(helperConfig.Pointer, []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	choice, err = Resolve(helperConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !choice.Baseline || choice.Path != helperConfig.Baseline {
		t.Fatalf("choice = %+v, want the baseline %s", choice, helperConfig.Baseline)
	}
}

func TestLauncherProfilesAreSelectedByTheInstalledName(t *testing.T) {
	agent := ConfigForName("/usr/libexec/bittern/bitternd")
	if agent.Baseline != "/usr/bin/bitternd" || agent.AgentPath != "usr/bin/bitternd" {
		t.Fatalf("agent profile = %+v, want the agent binary", agent)
	}
	helper := ConfigForName("/usr/libexec/bittern/bittern-update")
	if helper.Baseline != "/usr/bin/bittern" || helper.AgentPath != "usr/bin/bittern" {
		t.Fatalf("helper profile = %+v, want the CLI binary", helper)
	}
	other := ConfigForName("/tmp/bittern-launch")
	if other.Baseline != "/usr/bin/bitternd" || other.AgentPath != "usr/bin/bitternd" {
		t.Fatalf("unknown name profile = %+v, want the agent profile", other)
	}
}
