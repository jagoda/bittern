package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateHelperUnitIsFixedAndArgumentFree(t *testing.T) {
	root := filepath.Join("..", "..", "packaging")
	data, err := os.ReadFile(filepath.Join(root, "bittern-update.service"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{
		"Type=oneshot",
		"ExecStart=/usr/bin/bittern update helper",
		"ConditionPathExists=/var/lib/bittern/update/request.json",
		"User=root",
		"Group=root",
		"ReadWritePaths=/etc /var/lib/bittern",
		"CapabilityBoundingSet=",
		"NoNewPrivileges=true",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("unit is missing %q:\n%s", required, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Environment="):
			t.Fatalf("unit parses environment: %s", trimmed)
		case strings.HasPrefix(trimmed, "EnvironmentFile="):
			t.Fatalf("unit reads an environment file: %s", trimmed)
		case strings.Contains(trimmed, "%i"), strings.Contains(trimmed, "%n"), strings.Contains(trimmed, "%j"):
			t.Fatalf("unit uses an instance or unit-name specifier: %s", trimmed)
		case strings.Contains(trimmed, "$"):
			t.Fatalf("unit uses variable expansion: %s", trimmed)
		}
	}
	install, err := os.ReadFile(filepath.Join("..", "..", "..", "packaging", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"bittern-update.service",
		"enable_unit bittern-update.service",
	} {
		if !strings.Contains(string(install), required) {
			t.Fatalf("installer is missing %q", required)
		}
	}
}
