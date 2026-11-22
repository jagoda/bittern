package update

import (
	"os"
	"path/filepath"
	"strings"
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

func TestPlatformProviderReadsThePreparationKitMetadata(t *testing.T) {
	path := filepath.Join("..", "..", "..", "packaging", "platform.json")
	provider := &FilePlatformProvider{
		Path: path,
		UID:  func() int { return os.Geteuid() },
	}
	platform, err := provider.Platform()
	if err != nil {
		t.Fatal(err)
	}
	if platform.Generation != 1 || platform.Source != "image-metadata" {
		t.Fatalf("platform = %+v, want generation 1 from image-metadata", platform)
	}
}

func TestKitPreparedPlatformFixturePassesPreflightAndMissingMetadataFails(t *testing.T) {
	env := newTestEnv(t)
	fixture := filepath.Join(env.root, "usr", "lib", "bittern", "platform.json")
	if err := os.MkdirAll(filepath.Dir(fixture), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "packaging", "platform.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	env.options.Platform = &FilePlatformProvider{
		Path: fixture,
		UID:  func() int { return os.Geteuid() },
	}
	env.mustInstall(t, env.writeValidBundle(t, 1))

	if err := os.Remove(fixture); err != nil {
		t.Fatal(err)
	}
	result, err := env.install(t, env.writeValidBundle(t, 2))
	if err == nil {
		t.Fatalf("a kit fixture without platform metadata was accepted: %+v", result)
	}
	if result.Status != ResultRejected {
		t.Fatalf("status = %s, want rejected", result.Status)
	}
	if !strings.Contains(result.Failure, "platform.missing") {
		t.Fatalf("failure = %q, want platform.missing", result.Failure)
	}
	assertPointerVerified(t, env, 1)
}
