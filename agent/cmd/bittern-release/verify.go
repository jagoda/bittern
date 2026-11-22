package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"bittern.invalid/agent/internal/release"
)

const verifyUsage = `usage: bittern-release verify MANIFEST.json MANIFEST.json.sig --trust-dir DIR
                         [--rotation-dir DIR] [--kind KIND] [--committed-generation N]

Verifies the canonical manifest and its detached signature against the pinned
trust store, read by the same internal/release loader the appliance uses. The
trust directory must be mode 0755 and its <key_id>.pub files mode 0644, owned by
this user, so the tool needs no test branch: a temporary directory with a test
key is the production code path with different material.

  --trust-dir DIR            image trust store of 32-byte <key_id>.pub files
  --rotation-dir DIR         optional rotation store, unioned with the image store
  --kind KIND                expected kind, app-bundle (default) or os-image
  --committed-generation N   require generation > N; default -1, no floor

Exit codes:
  0  the manifest and signature are valid
  3  the signature is missing, truncated or invalid
  4  the signer key is untrusted or the trust store is unreadable
  5  the manifest is not canonical or fails a field rule
  6  the manifest fails the kind or generation rule
`

func runVerify(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("verify", verifyUsage, stderr)
	trustDir := flags.String("trust-dir", "", "image trust store directory")
	rotationDir := flags.String("rotation-dir", "", "optional rotation trust store directory")
	kind := flags.String("kind", release.KindAppBundle, "expected manifest kind")
	committedGeneration := flags.Int64("committed-generation", -1, "require generation greater than this")
	flagArgs, positionals := splitArgs(flags, args)
	if proceed, code := parseFlags(flags, flagArgs); !proceed {
		return code
	}
	if !wantArgs(flags, positionals, 2) {
		return exitUsage
	}
	if *trustDir == "" {
		flags.Usage()
		fmt.Fprintln(stderr, "verify: --trust-dir is required")
		return exitUsage
	}
	manifestPath, err := filepath.Abs(positionals[0])
	if err != nil {
		return fail(stderr, "verify", "resolve the manifest path: %v", err)
	}
	signaturePath, err := filepath.Abs(positionals[1])
	if err != nil {
		return fail(stderr, "verify", "resolve the signature path: %v", err)
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return fail(stderr, "verify", "read the manifest %s: %v", manifestPath, err)
	}
	signature, err := os.ReadFile(signaturePath)
	if err != nil {
		if os.IsNotExist(err) {
			return reportRejection(stderr, "verify", &release.Error{
				Path:    signaturePath,
				Rule:    "signature.missing",
				Message: fmt.Sprintf("the signature file %s is missing", signaturePath),
			})
		}
		return fail(stderr, "verify", "read the signature %s: %v", signaturePath, err)
	}
	store, err := release.LoadTrustStore(*trustDir, *rotationDir)
	if err != nil {
		return reportRejection(stderr, "verify", err)
	}
	manifest, err := release.VerifyManifest(manifestBytes, signature, store, *kind, *committedGeneration)
	if err != nil {
		return reportRejection(stderr, "verify", err)
	}

	manifestSum := sha256.Sum256(manifestBytes)
	signatureSum := sha256.Sum256(signature)
	fmt.Fprintf(stdout, "verify: valid manifest for %s %s (generation %d, channel %s)\n",
		manifest.Product, manifest.Version, manifest.Generation, manifest.Channel)
	fmt.Fprintf(stdout, "verify: signer %s is in the pinned trust store\n", manifest.Signing.KeyID)
	if *committedGeneration >= 0 {
		fmt.Fprintf(stdout, "verify: generation %d is greater than the committed generation %d\n",
			manifest.Generation, *committedGeneration)
	}
	fmt.Fprintf(stdout, "verify: manifest sha256 %s\n", hex.EncodeToString(manifestSum[:]))
	fmt.Fprintf(stdout, "verify: signature sha256 %s (%d bytes)\n", hex.EncodeToString(signatureSum[:]), len(signature))
	return exitOK
}
