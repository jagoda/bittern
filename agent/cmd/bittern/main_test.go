package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/auth"
	"bittern.invalid/agent/internal/frigateimage"
	secretstore "bittern.invalid/agent/internal/secrets"
	"bittern.invalid/agent/internal/setupqr"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod above the test directory")
		}
		dir = parent
	}
}

func firstBootTestSystem(t *testing.T) *apply.ExecSystem {
	t.Helper()
	system := apply.NewExecSystem()
	system.ChronyConfigDir = t.TempDir()
	return system
}

func TestRenderPOCExample(t *testing.T) {
	output := t.TempDir()
	configPath := filepath.Join(repoRoot(t), "schema", "examples", "poc-gk41.yaml")
	if err := runRender([]string{"--config", configPath, "--out", output}); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		"chrony/chrony.conf",
		"dnsmasq/dnsmasq.conf",
		"networkd/10-cam0.network",
		"networkd/10-lan0.network",
		"nftables/bittern.nft",
	} {
		if _, err := os.Stat(filepath.Join(output, filepath.FromSlash(relative))); err != nil {
			t.Errorf("missing rendered file %s: %v", relative, err)
		}
	}
}

func TestRenderRefusesInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "out")
	if err := runRender([]string{"--config", configPath, "--out", output}); err == nil {
		t.Fatal("invalid configuration was accepted")
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid configuration wrote %d files", len(entries))
	}
}

func TestRenderRequiresPaths(t *testing.T) {
	if err := runRender(nil); err == nil {
		t.Fatal("render without --config and --out was accepted")
	}
}

func TestFirstBootOffline(t *testing.T) {
	root := t.TempDir()
	stateRoot := filepath.Join(root, "state")
	configRoot := filepath.Join(root, "etc")
	liveRoot := filepath.Join(root, "live")
	if err := runFirstBootWithSystem([]string{
		"--offline",
		"--state-root", stateRoot,
		"--config-root", configRoot,
		"--live-root", liveRoot,
	}, firstBootTestSystem(t)); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(stateRoot, "first-boot.json"),
		filepath.Join(stateRoot, "setup-token"),
		filepath.Join(configRoot, "appliance.yaml"),
		filepath.Join(configRoot, "secrets.yaml"),
		filepath.Join(configRoot, "tls", "server.crt"),
		filepath.Join(liveRoot, "etc", "nftables.conf"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("first boot did not write %s: %v", path, err)
		}
	}
	tokenInfo, err := os.Stat(filepath.Join(stateRoot, "setup-token"))
	if err != nil {
		t.Fatal(err)
	}
	if tokenInfo.Mode().Perm() != 0o600 {
		t.Fatalf("setup token mode = %o, want 600", tokenInfo.Mode().Perm())
	}
}

func setupTokenFixture(t *testing.T) (string, string, string) {
	t.Helper()
	base := t.TempDir()
	stateRoot := filepath.Join(base, "state")
	configRoot := filepath.Join(base, "etc")
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	const token = "setup-token-value-0123456789"
	if err := secretstore.Save(filepath.Join(configRoot, "secrets.yaml"), secretstore.Document{
		SchemaVersion: 1,
		Identity:      secretstore.Identity{ID: "test-identity"},
		SetupToken:    token,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretstore.SetupTokenPath(stateRoot), []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return stateRoot, configRoot, token
}

func TestSetupTokenCommandPrintsOnlyWhenRequested(t *testing.T) {
	stateRoot, configRoot, token := setupTokenFixture(t)
	if err := os.Remove(secretstore.SetupTokenPath(stateRoot)); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runSetupTokenTo([]string{
		"--state-root", stateRoot,
		"--config-root", configRoot,
	}, &output, func() int { return 0 }); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != token {
		t.Fatalf("command output = %q", output.String())
	}
	info, err := os.Stat(secretstore.SetupTokenPath(stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("repaired setup token mode = %o, want 600", info.Mode().Perm())
	}

	output.Reset()
	if err := runSetupTokenTo([]string{
		"--state-root", stateRoot,
		"--config-root", configRoot,
	}, &output, func() int { return 1000 }); err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("non-root command error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("non-root command printed %q", output.String())
	}
}

func TestSetupTokenCommandRefusesAfterSetup(t *testing.T) {
	stateRoot, configRoot, token := setupTokenFixture(t)
	secretsPath := filepath.Join(configRoot, "secrets.yaml")
	store, err := auth.Open(auth.Options{
		SecretsPath: secretsPath,
		StatePath:   filepath.Join(stateRoot, "auth.json"),
		Iterations:  1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteSetup(token, "correct horse battery staple", "192.0.2.1:1000"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runSetupTokenTo([]string{
		"--state-root", stateRoot,
		"--config-root", configRoot,
	}, &output, func() int { return 0 }); err == nil || !strings.Contains(err.Error(), "already complete") {
		t.Fatalf("post-setup command error = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("post-setup command printed a token: %q", output.String())
	}
}

func TestFirstBootOutputDoesNotContainTheSetupToken(t *testing.T) {
	t.Setenv("BITTERN_CONSOLE", "")
	root := t.TempDir()
	stateRoot := filepath.Join(root, "state")
	configRoot := filepath.Join(root, "etc")
	liveRoot := filepath.Join(root, "live")

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	system := firstBootTestSystem(t)
	runErr := make(chan error, 1)
	go func() {
		err := runFirstBootWithSystem([]string{
			"--offline",
			"--state-root", stateRoot,
			"--config-root", configRoot,
			"--live-root", liveRoot,
		}, system)
		os.Stdout = original
		_ = writer.Close()
		runErr <- err
	}()
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if err := <-runErr; err != nil {
		t.Fatal(err)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}

	document, err := secretstore.Load(filepath.Join(configRoot, "secrets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if document.SetupToken == "" {
		t.Fatal("first boot did not generate a setup token")
	}
	if strings.Contains(string(output), document.SetupToken) {
		t.Fatal("first-boot stdout, which becomes the journal, contains the setup token")
	}
	if !strings.Contains(string(output), "sudo bittern setup-token") {
		t.Fatal("first-boot output does not tell the operator how to read the token")
	}
}

func TestSetupTokenQRPayloadAndConsoleBlock(t *testing.T) {
	stateRoot, configRoot, token := setupTokenFixture(t)
	originalLAN, originalRender, originalIssue, originalNow := currentLANAddress, renderSetupQR, issueSetupShortCode, now
	t.Cleanup(func() {
		currentLANAddress, renderSetupQR, issueSetupShortCode, now = originalLAN, originalRender, originalIssue, originalNow
	})
	currentLANAddress = func() (string, error) { return "192.168.1.44", nil }
	at := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return at }
	var encoded string
	renderSetupQR = func(w io.Writer, payload string) error {
		encoded = payload
		_, err := io.WriteString(w, "<QR>\n")
		return err
	}
	issueSetupShortCode = func(string, time.Time, io.Reader) (string, time.Time, error) {
		return "ABCD2345", at.Add(10 * time.Minute), nil
	}

	var output bytes.Buffer
	if err := runSetupTokenTo([]string{
		"--qr",
		"--state-root", stateRoot,
		"--config-root", configRoot,
	}, &output, func() int { return 0 }); err != nil {
		t.Fatal(err)
	}
	want := "https://192.168.1.44/ui/setup#token=" + token
	if encoded != want {
		t.Fatalf("QR payload = %q, want %q", encoded, want)
	}
	block := output.String()
	if !strings.Contains(block, setupqr.GroupToken(token)) {
		t.Fatalf("block does not contain the grouped token:\n%s", block)
	}
	if !strings.Contains(block, "ABCD-2345") {
		t.Fatalf("block does not contain the short code:\n%s", block)
	}
	if strings.Contains(block, token) {
		t.Fatal("the console block contains the raw token as one contiguous string")
	}
	if strings.Contains(block, "#token=") {
		t.Fatal("the printed block repeats the token fragment")
	}
	if strings.Contains(block, "<QR>") == false {
		t.Fatal("the console block does not contain the rendered QR")
	}
}

func TestSetupTokenQRDoesNotRotateShortCodeWhenQRFails(t *testing.T) {
	stateRoot, configRoot, _ := setupTokenFixture(t)
	originalLAN, originalRender, originalIssue := currentLANAddress, renderSetupQR, issueSetupShortCode
	t.Cleanup(func() {
		currentLANAddress, renderSetupQR, issueSetupShortCode = originalLAN, originalRender, originalIssue
	})
	currentLANAddress = func() (string, error) { return "192.168.1.44", nil }
	renderSetupQR = func(io.Writer, string) error {
		return errors.New("qrencode is not installed")
	}
	issued := false
	issueSetupShortCode = func(string, time.Time, io.Reader) (string, time.Time, error) {
		issued = true
		return "ABCD2345", time.Now().Add(time.Minute), nil
	}
	var output bytes.Buffer
	if err := runSetupTokenTo([]string{
		"--qr",
		"--state-root", stateRoot,
		"--config-root", configRoot,
	}, &output, func() int { return 0 }); err == nil {
		t.Fatal("a failed QR render was accepted")
	}
	if issued {
		t.Fatal("the short code was rotated even though the QR failed")
	}
	if output.Len() != 0 {
		t.Fatalf("a failed QR render printed output: %q", output.String())
	}
}

func TestFirstBootConsoleBlockGoesOnlyToTheConsole(t *testing.T) {
	root := t.TempDir()
	stateRoot := filepath.Join(root, "state")
	configRoot := filepath.Join(root, "etc")
	liveRoot := filepath.Join(root, "live")
	consolePath := filepath.Join(root, "console")
	if err := os.WriteFile(consolePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BITTERN_CONSOLE", consolePath)
	originalLAN, originalRender, originalIssue := currentLANAddress, renderSetupQR, issueSetupShortCode
	t.Cleanup(func() {
		currentLANAddress, renderSetupQR, issueSetupShortCode = originalLAN, originalRender, originalIssue
	})
	currentLANAddress = func() (string, error) { return "192.168.1.44", nil }
	var encoded string
	renderSetupQR = func(w io.Writer, payload string) error {
		encoded = payload
		_, err := io.WriteString(w, "<QR>\n")
		return err
	}
	issueSetupShortCode = func(string, time.Time, io.Reader) (string, time.Time, error) {
		return "ABCD2345", time.Now().Add(10 * time.Minute), nil
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = writer
	system := firstBootTestSystem(t)
	runErr := make(chan error, 1)
	go func() {
		err := runFirstBootWithSystem([]string{
			"--offline",
			"--state-root", stateRoot,
			"--config-root", configRoot,
			"--live-root", liveRoot,
		}, system)
		os.Stdout = originalStdout
		_ = writer.Close()
		runErr <- err
	}()
	journalOutput, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if err := <-runErr; err != nil {
		t.Fatal(err)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}

	document, err := secretstore.Load(filepath.Join(configRoot, "secrets.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(journalOutput), document.SetupToken) {
		t.Fatal("first-boot stdout contains the setup token")
	}
	if strings.Contains(string(journalOutput), setupqr.GroupToken(document.SetupToken)) {
		t.Fatal("first-boot stdout contains the grouped setup token")
	}
	consoleOutput, err := os.ReadFile(consolePath)
	if err != nil {
		t.Fatal(err)
	}
	if encoded != "https://192.168.1.44/ui/setup#token="+document.SetupToken {
		t.Fatalf("console QR payload = %q", encoded)
	}
	if !strings.Contains(string(consoleOutput), setupqr.GroupToken(document.SetupToken)) {
		t.Fatal("the local console block does not contain the grouped token")
	}
	if !strings.Contains(string(consoleOutput), "ABCD-2345") {
		t.Fatal("the local console block does not contain the short code")
	}
}

const fakeDockerScript = `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_DOCKER_LOG"
case "$1" in
    image)
        if [ -f "$FAKE_DOCKER_PRESENT" ]; then
            printf '%s\n' sha256:local
            exit 0
        fi
        printf '%s\n' "Error response from daemon: No such image" >&2
        exit 1
        ;;
    pull)
        if [ "$FAKE_DOCKER_PULL_FAIL" = "1" ]; then
            printf '%s\n' "Error response from daemon: network is unreachable" >&2
            exit 1
        fi
        : > "$FAKE_DOCKER_PRESENT"
        printf 'pulled %s\n' "$2"
        exit 0
        ;;
esac
exit 2
`

func fakeDocker(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker")
	if err := os.WriteFile(path, []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImagePrewarmDownloadsAndRecordsState(t *testing.T) {
	root := t.TempDir()
	present := filepath.Join(root, "present")
	t.Setenv("FAKE_DOCKER_PRESENT", present)
	t.Setenv("FAKE_DOCKER_LOG", filepath.Join(root, "docker.log"))
	statePath := filepath.Join(root, "image-prewarm.json")

	if err := runImagePrewarm([]string{"--state-path", statePath, "--docker", fakeDocker(t)}); err != nil {
		t.Fatal(err)
	}
	status, err := frigateimage.ReadStatus(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != frigateimage.StateReady {
		t.Fatalf("image state = %q, want %q (%s)", status.State, frigateimage.StateReady, status.LastError)
	}
	if status.Digest == "" {
		t.Fatal("image state does not record the pinned digest")
	}
	if _, err := os.Stat(present); err != nil {
		t.Fatalf("prewarm did not pull the image: %v", err)
	}
}

func TestImagePrewarmRecordsFailureWithoutBlame(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FAKE_DOCKER_PRESENT", filepath.Join(root, "present"))
	t.Setenv("FAKE_DOCKER_LOG", filepath.Join(root, "docker.log"))
	t.Setenv("FAKE_DOCKER_PULL_FAIL", "1")
	statePath := filepath.Join(root, "image-prewarm.json")

	if err := runImagePrewarm([]string{"--state-path", statePath, "--docker", fakeDocker(t)}); err == nil {
		t.Fatal("a failed download was reported as success")
	}
	status, err := frigateimage.ReadStatus(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != frigateimage.StateFailed {
		t.Fatalf("image state = %q, want %q", status.State, frigateimage.StateFailed)
	}
	if status.LastError == "" || !strings.Contains(status.LastError, "network is unreachable") {
		t.Fatalf("image state does not record the download error: %q", status.LastError)
	}
}
