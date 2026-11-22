package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"bittern.invalid/agent/internal/release"
)

const signUsage = `usage: bittern-release sign MANIFEST.json --key PATH [--yes] [--force] [--dry-run]

Prints product, version, generation, channel, source_commit, payload.sha256,
key_id and the manifest SHA-256 in that order, requires the typed word sign or
the version unless --yes is given, signs the domain-prefixed canonical manifest,
writes MANIFEST.json.sig as exactly 64 bytes, and re-verifies the signature with
the internal/release verifier before reporting success. A failed self-check
deletes the signature and exits non-zero.

  --key PATH   32-byte Ed25519 seed file, mode 0600, owned by this user
  --yes        skip the typed confirmation (for a checked, scripted ritual)
  --force      replace an existing signature file
  --dry-run    print the fields and stop before signing
`

func runSign(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := newFlagSet("sign", signUsage, stderr)
	keyPath := flags.String("key", "", "32-byte Ed25519 seed file")
	yes := flags.Bool("yes", false, "skip the typed confirmation")
	force := flags.Bool("force", false, "replace an existing signature file")
	dryRun := flags.Bool("dry-run", false, "print the fields without signing")
	flagArgs, positionals := splitArgs(flags, args)
	if proceed, code := parseFlags(flags, flagArgs); !proceed {
		return code
	}
	if !wantArgs(flags, positionals, 1) {
		return exitUsage
	}
	if *keyPath == "" {
		flags.Usage()
		fmt.Fprintln(stderr, "sign: --key is required")
		return exitUsage
	}
	manifestPath, err := filepath.Abs(positionals[0])
	if err != nil {
		return fail(stderr, "sign", "resolve the manifest path: %v", err)
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return fail(stderr, "sign", "read the manifest %s: %v", manifestPath, err)
	}
	manifest, err := release.ParseManifest(manifestBytes)
	if err != nil {
		return reportRejection(stderr, "sign", err)
	}
	canonical, err := manifest.Canonical()
	if err != nil {
		return reportRejection(stderr, "sign", err)
	}
	if !bytes.Equal(canonical, manifestBytes) {
		return fail(stderr, "sign", "the manifest %s is not its own canonical form", manifestPath)
	}
	key, err := readSigningKey(*keyPath)
	if err != nil {
		return fail(stderr, "sign", "%v", err)
	}
	if manifest.Signing.KeyID != key.id {
		return fail(stderr, "sign",
			"the manifest declares signing.key_id %s but the signing key derives %s; set signing.key_id on the build host to the key id, then sign again",
			manifest.Signing.KeyID, key.id)
	}
	signaturePath := manifestPath + ".sig"
	if err := preflightSignaturePath(signaturePath, *force); err != nil {
		return fail(stderr, "sign", "%v", err)
	}

	digest := manifestDigest(manifestBytes)
	fmt.Fprintf(stdout, "product: %s\n", manifest.Product)
	fmt.Fprintf(stdout, "version: %s\n", manifest.Version)
	fmt.Fprintf(stdout, "generation: %d\n", manifest.Generation)
	fmt.Fprintf(stdout, "channel: %s\n", manifest.Channel)
	fmt.Fprintf(stdout, "source_commit: %s\n", manifest.SourceCommit)
	fmt.Fprintf(stdout, "payload.sha256: %s\n", manifest.Payload.SHA256)
	fmt.Fprintf(stdout, "key_id: %s\n", manifest.Signing.KeyID)
	fmt.Fprintf(stdout, "manifest sha256: %s\n", digest)
	fmt.Fprintf(stdout, "signature file: %s\n", signaturePath)

	if *dryRun {
		fmt.Fprintf(stdout, "sign: dry run, no signature written\n")
		return exitOK
	}
	if !*yes {
		fmt.Fprintf(stderr, "sign: type \"sign\" or %s to confirm: ", manifest.Version)
		if !confirmed(stdin, manifest.Version) {
			fmt.Fprintf(stderr, "sign: confirmation declined, the manifest was not signed\n")
			return exitFailure
		}
	}

	signature := signMessage(key.private, release.SigningInput(canonical))
	if len(signature) != ed25519.SignatureSize {
		return fail(stderr, "sign", "the signer returned %d bytes, not %d", len(signature), ed25519.SignatureSize)
	}
	if err := writeSignature(signaturePath, signature, *force); err != nil {
		return fail(stderr, "sign", "%v", err)
	}
	if err := selfCheck(canonical, signature, manifest, key.public); err != nil {
		os.Remove(signaturePath)
		fmt.Fprintf(stderr, "sign: self-check failed, the signature was deleted\n")
		return reportRejection(stderr, "sign", err)
	}
	if *force {
		fmt.Fprintf(stdout, "sign: replaced %s (64 bytes, mode 0644)\n", signaturePath)
	} else {
		fmt.Fprintf(stdout, "sign: wrote %s (64 bytes, mode 0644)\n", signaturePath)
	}
	fmt.Fprintf(stdout, "sign: self-check passed, the signature verifies with key %s\n", key.id)
	return exitOK
}

// releaseSign is ed25519.Sign in pure mode, the primitive from ADR 0029.
func releaseSign(private ed25519.PrivateKey, message []byte) []byte {
	return ed25519.Sign(private, message)
}

// confirmed reads one answer and accepts the word sign or the release version.
func confirmed(stdin io.Reader, version string) bool {
	reader := bufio.NewReader(stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false
	}
	answer = strings.TrimSpace(answer)
	return answer == "sign" || answer == version
}

// preflightSignaturePath refuses a symlink, a non-regular file and, without
// force, any existing signature.
func preflightSignaturePath(path string, force bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read the signature path %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("the signature path %s is a symlink; refusing to replace it", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("the signature path %s is not a regular file", path)
	}
	if !force {
		return fmt.Errorf("the signature file %s already exists; pass --force to replace it", path)
	}
	return nil
}

// writeSignature writes exactly 64 bytes at mode 0644. Without replace it fails
// when the path exists, so two concurrent runs cannot both claim the signature.
func writeSignature(path string, signature []byte, replace bool) error {
	if replace {
		temporary, err := os.CreateTemp(filepath.Dir(path), ".bittern-release-sig-*")
		if err != nil {
			return fmt.Errorf("create a temporary signature file: %w", err)
		}
		temporaryPath := temporary.Name()
		cleanup := func(cause error) error {
			temporary.Close()
			os.Remove(temporaryPath)
			return cause
		}
		if err := temporary.Chmod(0o644); err != nil {
			return cleanup(fmt.Errorf("set the signature file mode: %w", err))
		}
		if _, err := temporary.Write(signature); err != nil {
			return cleanup(fmt.Errorf("write the signature file: %w", err))
		}
		if err := temporary.Sync(); err != nil {
			return cleanup(fmt.Errorf("sync the signature file: %w", err))
		}
		if err := temporary.Close(); err != nil {
			os.Remove(temporaryPath)
			return fmt.Errorf("close the signature file: %w", err)
		}
		if err := os.Rename(temporaryPath, path); err != nil {
			os.Remove(temporaryPath)
			return fmt.Errorf("replace the signature file %s: %w", path, err)
		}
		return nil
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return fmt.Errorf("create the signature file %s: %w", path, err)
	}
	cleanup := func(cause error) error {
		file.Close()
		os.Remove(path)
		return cause
	}
	if err := file.Chmod(0o644); err != nil {
		return cleanup(fmt.Errorf("set the signature file mode: %w", err))
	}
	if _, err := file.Write(signature); err != nil {
		return cleanup(fmt.Errorf("write the signature file: %w", err))
	}
	if err := file.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync the signature file: %w", err))
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return fmt.Errorf("close the signature file %s: %w", path, err)
	}
	return nil
}

// selfCheck verifies the signature just written with the same trust-store
// loader and verifier the appliance uses, with the signer's public key as the
// only key in the store.
func selfCheck(canonical, signature []byte, manifest *release.Manifest, public ed25519.PublicKey) error {
	store, cleanup, err := trustStoreFor(public)
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := release.VerifyManifest(canonical, signature, store, manifest.Kind, -1); err != nil {
		return err
	}
	return nil
}
