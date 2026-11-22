package assets

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// repoRoot walks up from the package directory until it finds the agent module,
// then returns the repository root above it.
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

func root(t *testing.T) string {
	t.Helper()
	return repoRoot(t)
}

// TestEmbeddedSchemaMatchesRepository fails when the generated schema copy has
// drifted from schema/appliance.schema.json.
func TestEmbeddedSchemaMatchesRepository(t *testing.T) {
	want, err := os.ReadFile(filepath.Join(root(t), "schema", "appliance.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(Schema, want) {
		t.Fatal("embedded schema differs from schema/appliance.schema.json; run agent/scripts/sync-assets.sh")
	}
}

// TestEmbeddedTemplatesMatchRepository fails when a template was added, removed
// or edited without regenerating the embedded asset tree.
func TestEmbeddedTemplatesMatchRepository(t *testing.T) {
	repository := make(map[string][]byte)
	templateRoot := filepath.Join(root(t), "templates")
	err := filepath.WalkDir(templateRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() == "README.md" {
			return nil
		}
		relative, err := filepath.Rel(templateRoot, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		repository[filepath.ToSlash(relative)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	embedded := make(map[string][]byte)
	err = fs.WalkDir(Templates, "templates", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := Templates.ReadFile(path)
		if err != nil {
			return err
		}
		embedded[strings.TrimPrefix(path, "templates/")] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for name := range repository {
		names = append(names, name)
	}
	for name := range embedded {
		if _, ok := repository[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		want, wantOK := repository[name]
		got, gotOK := embedded[name]
		if !wantOK {
			t.Errorf("embedded template %q has no repository source", name)
			continue
		}
		if !gotOK {
			t.Errorf("repository template %q is missing from the embedded tree; run agent/scripts/sync-assets.sh", name)
			continue
		}
		if !bytes.Equal(want, got) {
			t.Errorf("embedded template %q differs from templates/%s; run agent/scripts/sync-assets.sh", name, name)
		}
	}
}

// TestEmbeddedDefaultsMatchRepository fails when a product default was added,
// removed or edited without regenerating the embedded asset tree.
func TestEmbeddedDefaultsMatchRepository(t *testing.T) {
	repository := make(map[string][]byte)
	defaultsRoot := filepath.Join(root(t), "defaults")
	err := filepath.WalkDir(defaultsRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() == "README.md" {
			return nil
		}
		relative, err := filepath.Rel(defaultsRoot, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		repository[filepath.ToSlash(relative)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	embedded := make(map[string][]byte)
	err = fs.WalkDir(Defaults, "defaults", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := Defaults.ReadFile(path)
		if err != nil {
			return err
		}
		embedded[strings.TrimPrefix(path, "defaults/")] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for name := range repository {
		names = append(names, name)
	}
	for name := range embedded {
		if _, ok := repository[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		want, wantOK := repository[name]
		got, gotOK := embedded[name]
		if !wantOK {
			t.Errorf("embedded default %q has no repository source", name)
			continue
		}
		if !gotOK {
			t.Errorf("repository default %q is missing from the embedded tree; run agent/scripts/sync-assets.sh", name)
			continue
		}
		if !bytes.Equal(want, got) {
			t.Errorf("embedded default %q differs from defaults/%s; run agent/scripts/sync-assets.sh", name, name)
		}
	}
}

// TestEmbeddedUIAssetsMatchRepository fails when a UI template or static file
// was added, removed or edited without regenerating the embedded asset tree.
func TestEmbeddedUIAssetsMatchRepository(t *testing.T) {
	repository := make(map[string][]byte)
	uiRoot := filepath.Join(root(t), "ui")
	for _, directory := range []string{"templates", "static"} {
		directoryRoot := filepath.Join(uiRoot, directory)
		err := filepath.WalkDir(directoryRoot, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || entry.Name() == "README.md" {
				return nil
			}
			relative, err := filepath.Rel(uiRoot, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			repository[filepath.ToSlash(relative)] = data
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	embedded := make(map[string][]byte)
	err := fs.WalkDir(UI, "ui", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := UI.ReadFile(path)
		if err != nil {
			return err
		}
		embedded[strings.TrimPrefix(path, "ui/")] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for name := range repository {
		names = append(names, name)
	}
	for name := range embedded {
		if _, ok := repository[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		want, wantOK := repository[name]
		got, gotOK := embedded[name]
		if !wantOK {
			t.Errorf("embedded UI asset %q has no repository source", name)
			continue
		}
		if !gotOK {
			t.Errorf("repository UI asset %q is missing from the embedded tree; run agent/scripts/sync-assets.sh", name)
			continue
		}
		if !bytes.Equal(want, got) {
			t.Errorf("embedded UI asset %q differs from ui/%s; run agent/scripts/sync-assets.sh", name, name)
		}
	}
}
