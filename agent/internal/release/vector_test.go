package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestSigningInputVector(t *testing.T) {
	manifest := readVectorFile(t, "manifest.json")
	expected := readVectorFile(t, "signing-input.bin")
	actual := SigningInput(manifest)
	if !bytes.Equal(actual, expected) {
		t.Fatal("the checked-in signing input is not the domain prefix plus the manifest")
	}
	if len(actual) != 734 {
		t.Fatalf("signing input length = %d, want 734", len(actual))
	}
	sum := sha256.Sum256(actual)
	if got, want := hex.EncodeToString(sum[:]), "c8bf57b86c9e033f6ef08d39a70ffdd1562599b9c8e1b2b3d4d4f0f616356559"; got != want {
		t.Fatalf("signing input sha256 = %s, want %s", got, want)
	}
	if !bytes.HasPrefix(actual, []byte(DomainPrefix)) {
		t.Fatal("the signing input does not start with the domain prefix")
	}
}

func TestVectorHashFiles(t *testing.T) {
	cases := []struct {
		file string
		want string
	}{
		{file: "manifest.sha256", want: "4fa961778a947699d48064140323d3ed378aaa21d4dd5c8e162b5b53a2f8e1c1"},
		{file: "signing-input.sha256", want: "c8bf57b86c9e033f6ef08d39a70ffdd1562599b9c8e1b2b3d4d4f0f616356559"},
	}
	for _, test := range cases {
		fields := strings.Fields(string(readVectorFile(t, test.file)))
		if len(fields) != 2 || fields[0] != test.want {
			t.Fatalf("%s does not record %s", test.file, test.want)
		}
	}
}
