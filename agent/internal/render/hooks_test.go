package render

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedConfigurationHooks(t *testing.T) {
	for _, testCase := range goldenCases {
		if len(testCase.wantErrors) > 0 {
			continue
		}
		t.Run(testCase.name, func(t *testing.T) {
			outputDir := t.TempDir()
			configPath := filepath.Join(repoRoot(t), testCase.configPath)
			files, validationErrors, err := Generate(configPath, outputDir)
			if err != nil || len(validationErrors) != 0 {
				t.Fatalf("generate: errors=%v err=%v", validationErrors, err)
			}
			if len(files) == 0 {
				t.Fatal("no files rendered")
			}

			t.Run("dnsmasq", func(t *testing.T) {
				runDnsmasqCheck(t, filepath.Join(outputDir, "dnsmasq", "dnsmasq.conf"))
			})
			t.Run("nftables", func(t *testing.T) {
				runNftCheck(t, filepath.Join(outputDir, "nftables", "bittern.nft"))
			})
			t.Run("chrony", func(t *testing.T) {
				runChronyCheck(t, filepath.Join(outputDir, "chrony", "chrony.conf"))
			})
		})
	}
}

func findTool(names ...string) string {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			return name
		}
	}
	return ""
}

func runDnsmasqCheck(t *testing.T, configPath string) {
	t.Helper()
	dnsmasq := findTool("dnsmasq", "/usr/sbin/dnsmasq", "/sbin/dnsmasq")
	if dnsmasq == "" {
		t.Skip("dnsmasq not installed; rendered dnsmasq.conf was NOT validated")
	}
	output, err := exec.Command(dnsmasq, "--test", "--conf-file="+configPath).CombinedOutput()
	if err != nil {
		t.Fatalf("dnsmasq --test rejected the rendered config: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "syntax check OK") {
		t.Fatalf("dnsmasq --test did not report a syntax check: %s", output)
	}
}

func runNftCheck(t *testing.T, rulesetPath string) {
	t.Helper()
	nft := findTool("nft", "/usr/sbin/nft", "/sbin/nft", "/usr/local/sbin/nft")
	if nft == "" {
		t.Skip("nft not installed; rendered ruleset was NOT validated")
	}
	output, err := exec.Command(nft, "-c", "-f", rulesetPath).CombinedOutput()
	if err == nil {
		return
	}
	text := string(output)
	if nftEnvironmentFailure(text) {
		// nft -c needs CAP_NET_ADMIN to build its netlink cache even for a
		// syntax-only check. The container cannot grant it, so the ruleset was
		// parsed but NOT validated. Report the environment failure plainly.
		t.Skipf("nft -c could not initialise netlink in this environment; "+
			"rendered ruleset was NOT validated: %s", strings.TrimSpace(text))
	}
	t.Fatalf("nft -c rejected the rendered ruleset: %v\n%s", err, output)
}

// nftEnvironmentFailure reports whether every non-empty output line is the
// netlink permission failure. A syntax error in the ruleset still appears
// alongside that failure, so it must not be mistaken for the environment skip.
func nftEnvironmentFailure(output string) bool {
	sawEnvironmentLine := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "netlink:"),
			strings.Contains(trimmed, "cache initialization failed"),
			strings.Contains(trimmed, "Operation not permitted"):
			sawEnvironmentLine = true
		default:
			return false
		}
	}
	return sawEnvironmentLine
}

func TestNftEnvironmentFailureClassifier(t *testing.T) {
	environment := "netlink: Error: cache initialization failed: Operation not permitted\n"
	if !nftEnvironmentFailure(environment) {
		t.Fatal("netlink permission failure was not classified as an environment failure")
	}
	syntax := environment + "bad.nft:4:47-50: Error: syntax error, unexpected number\n"
	if nftEnvironmentFailure(syntax) {
		t.Fatal("syntax error was misclassified as an environment failure")
	}
	if nftEnvironmentFailure("") {
		t.Fatal("empty output was classified as an environment failure")
	}
}

func runChronyCheck(t *testing.T, configPath string) {
	t.Helper()
	chronyd := findTool("chronyd", "/usr/sbin/chronyd", "/sbin/chronyd")
	if chronyd == "" {
		t.Skip("chronyd not installed; rendered chrony.conf was NOT validated")
	}
	output, err := exec.Command(chronyd, "-p", "-f", configPath).CombinedOutput()
	if err != nil {
		t.Fatalf("chronyd -p rejected the rendered config: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "bindaddress") {
		t.Fatalf("chronyd -p did not print the rendered config: %s", output)
	}
}

// TestNftSyntaxErrorIsNotSkipped proves that a broken ruleset is reported as a
// test failure even though nft -c also emits the netlink permission failure in
// this container.
func TestNftSyntaxErrorIsNotSkipped(t *testing.T) {
	nft := findTool("nft", "/usr/sbin/nft", "/sbin/nft", "/usr/local/sbin/nft")
	if nft == "" {
		t.Skip("nft not installed")
	}
	path := filepath.Join(t.TempDir(), "broken.nft")
	broken := "table inet bittern {\n  chain input {\n    iifname \"lan0\" tcp dport { 1984, 5000 8554 } drop\n  }\n}\n"
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(nft, "-c", "-f", path).CombinedOutput()
	if err == nil {
		t.Fatal("nft -c accepted a deliberately broken ruleset")
	}
	if nftEnvironmentFailure(string(output)) {
		t.Fatalf("syntax error was misclassified as a netlink environment failure:\n%s", output)
	}
}
