package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestManifestV2VectorSigningInput(t *testing.T) {
	manifest := readVectorFileAt(t, vectorDirV2, "manifest.json")
	expected := readVectorFileAt(t, vectorDirV2, "signing-input.bin")
	actual := SigningInput(manifest)
	if !bytes.Equal(actual, expected) {
		t.Fatal("the v2 signing input is not the domain prefix plus the manifest")
	}
	if len(manifest) != 1157 || len(actual) != 1185 {
		t.Fatalf("v2 manifest length = %d, signing input length = %d; want 1157 and 1185", len(manifest), len(actual))
	}
	sum := sha256.Sum256(actual)
	if got, want := hex.EncodeToString(sum[:]), "c4d970b90c3bb22d209b6bac2b8195f45e2f2afe9b26b1880e60c8f22ca1641c"; got != want {
		t.Fatalf("v2 signing input sha256 = %s, want %s", got, want)
	}
	fields := strings.Fields(string(readVectorFileAt(t, vectorDirV2, "manifest.sha256")))
	if len(fields) != 2 || fields[0] != "0e25336c650488efed2def1d53d4c39efe1fecbfa9f78f375e4ef45aab3c94da" {
		t.Fatalf("the v2 manifest hash file is %q", fields)
	}
}

func TestManifestV2VerifierAcceptsV1AndV2(t *testing.T) {
	v1 := readVectorFile(t, "manifest.json")
	v1Manifest, err := ParseManifest(v1)
	if err != nil {
		t.Fatalf("parse the v1 vector with the v2 verifier: %v", err)
	}
	if v1Manifest.ManifestVersion != ManifestVersionV1 || v1Manifest.Subsystems != nil {
		t.Fatalf("the v1 vector changed field set: %+v", v1Manifest)
	}
	canonicalV1, err := v1Manifest.Canonical()
	if err != nil {
		t.Fatalf("canonicalize the v1 vector: %v", err)
	}
	if !bytes.Equal(canonicalV1, v1) {
		t.Fatal("the v1 vector no longer canonicalizes to its checked-in bytes")
	}

	v2Bytes := readVectorFileAt(t, vectorDirV2, "manifest.json")
	v2Manifest, err := ParseManifest(v2Bytes)
	if err != nil {
		t.Fatalf("parse the v2 vector: %v", err)
	}
	if v2Manifest.ManifestVersion != ManifestVersionV2 || v2Manifest.Subsystems == nil || len(v2Manifest.Subsystems.Units) != 1 {
		t.Fatalf("the v2 vector did not decode the subsystem section: %+v", v2Manifest)
	}
	_, privateKey := newTestKey(t)
	v2Manifest.Signing.KeyID = KeyID(privateKey.Public().(ed25519.PublicKey))
	canonicalV2, err := v2Manifest.Canonical()
	if err != nil {
		t.Fatalf("canonicalize the v2 vector: %v", err)
	}
	signature := ed25519.Sign(privateKey, SigningInput(canonicalV2))
	keyID := v2Manifest.Signing.KeyID
	store := &TrustStore{keys: map[string]ed25519.PublicKey{keyID: privateKey.Public().(ed25519.PublicKey)}}
	if _, err := VerifyManifest(canonicalV2, signature, store, KindAppBundle, v2Manifest.Generation-1); err != nil {
		t.Fatalf("verify the v2 vector round trip: %v", err)
	}
}

func TestManifestV1VerifierRejectsV2(t *testing.T) {
	// This is the old verifier rule, kept as a test so a future loosening of
	// Validate has to notice that a v1-only reader must fail closed.
	manifest := v2VectorManifest(t)
	if manifest.ManifestVersion == ManifestVersionV1 {
		t.Fatal("the v2 fixture declares the v1 version")
	}
	if err := validateManifestVersion(manifest.ManifestVersion, manifest.Kind); err != nil {
		t.Fatalf("the dual-version verifier rejected the v2 fixture: %v", err)
	}
	legacy := func(version int64) error {
		if version != ManifestVersionV1 {
			return errorAt("$.manifest_version", "manifest.version", "manifest_version %d is not %d", version, ManifestVersionV1)
		}
		return nil
	}
	requireRule(t, legacy(manifest.ManifestVersion), "manifest.version")
}

func TestManifestV2SubsystemRules(t *testing.T) {
	zero := strings.Repeat("0", 64)
	validUnit := func(m *Manifest) Unit {
		return m.Subsystems.Units[0]
	}
	cases := []struct {
		name   string
		mutate func(*Manifest)
		rule   string
	}{
		{name: "unit name", mutate: func(m *Manifest) { m.Subsystems.Units[0].Name = "ssh.service" }, rule: "manifest.unit-name"},
		{name: "unit source missing", mutate: func(m *Manifest) { m.Subsystems.Units[0].Source = "usr/lib/bittern/units/other.service" }, rule: "manifest.unit-source"},
		{name: "unit digest", mutate: func(m *Manifest) { m.Subsystems.Units[0].SHA256 = zero }, rule: "manifest.unit-digest"},
		{name: "unit mode", mutate: func(m *Manifest) { m.Subsystems.Units[0].Mode = "0755" }, rule: "manifest.unit-mode"},
		{name: "unit owner", mutate: func(m *Manifest) { m.Subsystems.Units[0].Owner = "bittern:bittern" }, rule: "manifest.unit-owner"},
		{name: "unit path", mutate: func(m *Manifest) { m.Subsystems.Units[0].Path = "/etc/systemd/system/other.service" }, rule: "manifest.unit-path"},
		{name: "empty subsystems", mutate: func(m *Manifest) { m.Subsystems = &Subsystems{} }, rule: "manifest.subsystems-empty"},
		{name: "migration target", mutate: func(m *Manifest) { m.Subsystems.Migrations[0].Target = "/etc/passwd" }, rule: "manifest.migration-target"},
		{name: "migration range", mutate: func(m *Manifest) { m.Subsystems.Migrations[0].To = m.Subsystems.Migrations[0].From }, rule: "manifest.migration-version"},
		{name: "unsorted units", mutate: func(m *Manifest) {
			unit := validUnit(m)
			second := unit
			second.Name = "bittern-bundle-a.service"
			second.Path = "/etc/systemd/system/" + second.Name
			m.Subsystems.Units = append(m.Subsystems.Units, second)
		}, rule: "manifest.unsorted-units"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			manifest := v2VectorManifest(t)
			test.mutate(manifest)
			_, err := manifest.Canonical()
			requireRule(t, err, test.rule)
		})
	}
}

func TestManifestV1RejectsSubsystems(t *testing.T) {
	manifest := vectorManifest(t)
	manifest.Subsystems = &Subsystems{Migrations: []Migration{{
		ID: "appliance-schema-2-to-3", From: 2, To: 3, Target: "appliance.yaml",
	}}}
	_, err := manifest.Canonical()
	requireRule(t, err, "manifest.version-field")
}

func TestManifestV2WithoutSubsystemsIsValid(t *testing.T) {
	manifest := vectorManifest(t)
	manifest.ManifestVersion = ManifestVersionV2
	data, err := manifest.Canonical()
	if err != nil {
		t.Fatalf("a v2 manifest without subsystems is invalid: %v", err)
	}
	parsed, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("parse a v2 manifest without subsystems: %v", err)
	}
	if parsed.Subsystems != nil {
		t.Fatalf("a v2 manifest without subsystems gained a section: %+v", parsed.Subsystems)
	}
}

func TestManifestV2RejectsOSImage(t *testing.T) {
	manifest := vectorManifest(t)
	manifest.ManifestVersion = ManifestVersionV2
	manifest.Kind = KindOSImage
	_, err := manifest.Canonical()
	requireRule(t, err, "manifest.version-kind")
}
