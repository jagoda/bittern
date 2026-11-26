package ui

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
)

const (
	browserRouteManifestFile = "../../../tests/browser/routes.json"
	browserRouteManifestCmd  = "cd agent && go test ./internal/ui -run TestBrowserRouteManifest -update"
)

// updateBrowserRouteManifest follows the -update convention used by the golden
// tests and the UI README table. It reuses an existing -update flag when another
// generated artifact test already registered it, so the tests can share one
// switch without a duplicate flag panic.
var updateBrowserRouteManifest = func() *bool {
	if flag.Lookup("update") != nil {
		return nil
	}
	return flag.Bool("update", false, "regenerate generated UI route artifacts")
}()

type browserRouteManifestEntry struct {
	Route   string `json:"route"`
	Path    string `json:"path"`
	Flow    string `json:"flow"`
	Variant string `json:"variant,omitempty"`
	Prepare string `json:"prepare,omitempty"`
	Landing string `json:"landing,omitempty"`
}

// TestBrowserRouteManifest keeps tests/browser/routes.json aligned with the
// Responsive rows in uiRoutes. The check itself cannot invent a route, and a
// new responsive route cannot land without appearing in the manifest.
func TestBrowserRouteManifest(t *testing.T) {
	data, err := os.ReadFile(browserRouteManifestFile)
	if err != nil {
		t.Fatalf("read %s: %v", browserRouteManifestFile, err)
	}
	var manifest []browserRouteManifestEntry
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse %s: %v", browserRouteManifestFile, err)
	}

	responsive := make(map[string]route)
	for _, entry := range uiRoutes {
		if entry.Responsive {
			responsive[entry.Name] = entry
		}
	}

	problems := validateBrowserRouteManifest(manifest, responsive)
	if len(problems) == 0 {
		return
	}
	if updateBrowserRouteManifestRequested() {
		updated := regenerateBrowserRouteManifest(manifest, responsive)
		encoded, err := json.MarshalIndent(updated, "", "  ")
		if err != nil {
			t.Fatalf("encode %s: %v", browserRouteManifestFile, err)
		}
		encoded = append(encoded, '\n')
		if err := os.WriteFile(browserRouteManifestFile, encoded, 0o644); err != nil {
			t.Fatalf("write %s: %v", browserRouteManifestFile, err)
		}
		manifest = updated
		problems = validateBrowserRouteManifest(manifest, responsive)
		if len(problems) == 0 {
			t.Logf("updated %s", browserRouteManifestFile)
			return
		}
	}
	t.Fatalf("tests/browser/routes.json has drifted from the Responsive uiRoutes\n"+
		"run `%s` to regenerate it\n\n%s",
		browserRouteManifestCmd, strings.Join(problems, "\n"))
}

func updateBrowserRouteManifestRequested() bool {
	if updateBrowserRouteManifest != nil {
		return *updateBrowserRouteManifest
	}
	current := flag.Lookup("update")
	if current == nil {
		return false
	}
	getter, ok := current.Value.(flag.Getter)
	if !ok {
		return false
	}
	value, _ := getter.Get().(bool)
	return value
}

func validateBrowserRouteManifest(manifest []browserRouteManifestEntry, responsive map[string]route) []string {
	var problems []string
	seen := make(map[string]bool)
	for index, entry := range manifest {
		line := fmt.Sprintf("entry %d (%s)", index+1, entry.Route)
		if entry.Route == "" {
			problems = append(problems, line+": route is empty")
			continue
		}
		if _, ok := responsive[entry.Route]; !ok {
			problems = append(problems, fmt.Sprintf(
				"%s: route %q is not a Responsive route in uiRoutes", line, entry.Route))
			continue
		}
		seen[entry.Route] = true
		if entry.Path == "" {
			problems = append(problems, line+": path is empty")
		} else if !strings.HasPrefix(entry.Path, BasePath) {
			problems = append(problems, fmt.Sprintf("%s: path %q is outside %s", line, entry.Path, BasePath))
		}
		switch entry.Flow {
		case "setup", "guest", "auth":
		default:
			problems = append(problems, fmt.Sprintf("%s: flow %q is not setup, guest, or auth", line, entry.Flow))
		}
		switch entry.Prepare {
		case "", "cameras", "review", "started", "applied":
		default:
			problems = append(problems, fmt.Sprintf("%s: prepare %q is not cameras, review, started, or applied", line, entry.Prepare))
		}
		switch entry.Landing {
		case "", "uninitialized", "initialized-with-cameras", "initialized-no-cameras":
		default:
			problems = append(problems, fmt.Sprintf("%s: landing %q is not a known device state", line, entry.Landing))
		}
	}
	for _, entry := range uiRoutes {
		if !entry.Responsive || seen[entry.Name] {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"missing Responsive route %q; add it to tests/browser/routes.json", entry.Name))
	}
	return problems
}

func regenerateBrowserRouteManifest(existing []browserRouteManifestEntry, responsive map[string]route) []browserRouteManifestEntry {
	byRoute := make(map[string][]browserRouteManifestEntry)
	for _, entry := range existing {
		if _, ok := responsive[entry.Route]; ok {
			byRoute[entry.Route] = append(byRoute[entry.Route], entry)
		}
	}

	updated := make([]browserRouteManifestEntry, 0, len(existing))
	for _, entry := range uiRoutes {
		if !entry.Responsive {
			continue
		}
		if entries := byRoute[entry.Name]; len(entries) > 0 {
			updated = append(updated, entries...)
			continue
		}
		updated = append(updated, browserRouteManifestEntry{
			Route: entry.Name,
			Path:  browserRouteManifestPath(entry),
			Flow:  browserRouteManifestFlow(entry),
		})
	}
	return updated
}

func browserRouteManifestPath(entry route) string {
	if entry.Name == "not-found" {
		return BasePath + "/missing"
	}
	path := strings.ReplaceAll(entry.Pattern, "{id}", "front_door")
	path = strings.ReplaceAll(path, "{ref}", "c1")
	path = strings.ReplaceAll(path, "{camera}", "front_door")
	return strings.ReplaceAll(path, "{$}", "")
}

func browserRouteManifestFlow(entry route) string {
	switch entry.Name {
	case "setup":
		return "setup"
	case "login":
		return "guest"
	default:
		return "auth"
	}
}
