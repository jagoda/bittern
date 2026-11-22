package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformProviderReadsRootOwnedMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.json")
	if err := WritePlatformMetadata(path, 2); err != nil {
		t.Fatal(err)
	}
	provider := &FilePlatformProvider{Path: path}
	platform, err := provider.Platform()
	if err != nil {
		t.Fatal(err)
	}
	if platform.Generation != 2 || platform.Source != "image-metadata" {
		t.Fatalf("platform = %+v, want generation 2 from image-metadata", platform)
	}
}

func TestPlatformProviderFailsClosedWithoutMetadata(t *testing.T) {
	provider := &FilePlatformProvider{Path: filepath.Join(t.TempDir(), "missing.json")}
	_, err := provider.Platform()
	requireRule(t, err, "platform.missing")
}

func TestPlatformProviderRejectsWrongOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.json")
	if err := WritePlatformMetadata(path, 1); err != nil {
		t.Fatal(err)
	}
	provider := &FilePlatformProvider{
		Path: path,
		UID:  func() int { return os.Geteuid() + 1 },
	}
	_, err := provider.Platform()
	requireRule(t, err, "platform.owner")
}

func TestPlatformProviderRejectsGroupWritableMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.json")
	if err := WritePlatformMetadata(path, 1); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o664); err != nil {
		t.Fatal(err)
	}
	provider := &FilePlatformProvider{Path: path}
	_, err := provider.Platform()
	requireRule(t, err, "platform.mode")
}

func TestPlatformProviderRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := WritePlatformMetadata(target, 1); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "platform.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	provider := &FilePlatformProvider{Path: link}
	_, err := provider.Platform()
	requireRule(t, err, "platform.type")
}

func TestPlatformProviderRejectsMalformedMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform.json")
	if err := os.WriteFile(path, []byte("{\"schema_version\":1,\"generation\":1,\"extra\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := &FilePlatformProvider{Path: path}
	_, err := provider.Platform()
	requireRule(t, err, "platform.parse")
}

func TestPlatformProviderRejectsUnknownSchemaAndNegativeGeneration(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		data string
		rule string
	}{
		{name: "schema", data: "{\"schema_version\":2,\"generation\":1}\n", rule: "platform.schema"},
		{name: "generation", data: "{\"schema_version\":1,\"generation\":-1}\n", rule: "platform.generation"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(dir, testCase.name+".json")
			if err := os.WriteFile(path, []byte(testCase.data), 0o644); err != nil {
				t.Fatal(err)
			}
			provider := &FilePlatformProvider{Path: path}
			_, err := provider.Platform()
			requireRule(t, err, testCase.rule)
		})
	}
}
