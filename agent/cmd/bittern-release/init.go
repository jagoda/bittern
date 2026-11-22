package main

import (
	"fmt"
	"io"
	"path/filepath"

	"bittern.invalid/agent/internal/release"
)

const initUsage = `usage: bittern-release init --key PATH [--pub-dir DIR] [--force] [--dry-run]

Generates a 32-byte Ed25519 seed from crypto/rand and writes it to PATH with
mode 0600. It refuses an existing file without --force, a symlink, a non-regular
file, a file owned by another user, and a file that is group- or other-accessible.
It also writes the 32 raw public-key bytes to DIR/<key_id>.pub with mode 0644 and
prints the path the image build expects under the pinned trust store.

  --key PATH       key file to create or replace
  --pub-dir DIR    directory for <key_id>.pub (default: the current directory)
  --force          replace an existing key file (the new key gets a new anchor)
  --dry-run        check the path and report what would be created, write nothing
`

func runInit(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("init", initUsage, stderr)
	keyPath := flags.String("key", "", "key file to create or replace")
	publicDir := flags.String("pub-dir", ".", "directory for <key_id>.pub")
	force := flags.Bool("force", false, "replace an existing key file")
	dryRun := flags.Bool("dry-run", false, "check the path without writing anything")
	flagArgs, positionals := splitArgs(flags, args)
	if proceed, code := parseFlags(flags, flagArgs); !proceed {
		return code
	}
	if !wantArgs(flags, positionals, 0) {
		return exitUsage
	}
	if *keyPath == "" {
		flags.Usage()
		fmt.Fprintln(stderr, "init: --key is required")
		return exitUsage
	}
	absoluteKey, err := filepath.Abs(*keyPath)
	if err != nil {
		return fail(stderr, "init", "resolve the key path: %v", err)
	}
	absolutePublicDir, err := filepath.Abs(*publicDir)
	if err != nil {
		return fail(stderr, "init", "resolve the public key directory: %v", err)
	}
	exists, err := preflightKeyPath(absoluteKey, *force)
	if err != nil {
		return fail(stderr, "init", "%v", err)
	}
	if *dryRun {
		action := "create"
		if exists {
			action = "replace"
		}
		fmt.Fprintf(stdout, "init: dry run, no key written\n")
		fmt.Fprintf(stdout, "init: would %s the key file %s (32-byte Ed25519 seed, mode 0600)\n", action, absoluteKey)
		fmt.Fprintf(stdout, "init: would write the public key file <key_id>.pub in %s (32 raw bytes, mode 0644)\n", absolutePublicDir)
		return exitOK
	}

	if err := createKeyDir(absoluteKey); err != nil {
		return fail(stderr, "init", "%v", err)
	}
	key, err := newSigningKey()
	if err != nil {
		return fail(stderr, "init", "%v", err)
	}
	if err := writeSeed(absoluteKey, key.private.Seed(), exists); err != nil {
		return fail(stderr, "init", "%v", err)
	}
	// Read the file back with the same checks sign uses, so init cannot claim a
	// key that the signing path would refuse.
	written, err := readSigningKey(absoluteKey)
	if err != nil {
		return fail(stderr, "init", "read back the new key file: %v", err)
	}
	if written.id != key.id {
		return fail(stderr, "init", "the key file read back with id %s, not the generated %s", written.id, key.id)
	}

	directoryMode, err := ensurePublicKeyDir(absolutePublicDir)
	if err != nil {
		return fail(stderr, "init", "%v", err)
	}
	publicPath, createdPublic, err := writePublicKey(absolutePublicDir, written.public)
	if err != nil {
		return fail(stderr, "init", "%v", err)
	}
	if err := verifyPublicKeyFile(publicPath, written.public); err != nil {
		return fail(stderr, "init", "read back the new public key file: %v", err)
	}

	fmt.Fprintf(stdout, "init: key file %s (32-byte Ed25519 seed, mode 0600)\n", absoluteKey)
	if exists {
		fmt.Fprintf(stdout, "init: replaced the existing key file\n")
		fmt.Fprintf(stdout, "init: note: an earlier anchor, if any, is left in the public key directory for a rotation overlap\n")
	}
	fmt.Fprintf(stdout, "init: key id %s\n", written.id)
	fmt.Fprintf(stdout, "init: public key %s\n", publicKeyHex(written.public))
	state := "created"
	if !createdPublic {
		state = "already present with the same content"
	}
	fmt.Fprintf(stdout, "init: public key file %s (32 raw bytes, mode 0644, %s)\n", publicPath, state)
	fmt.Fprintf(stdout, "init: image anchor %s\n", filepath.Join(release.ImageStorePath, written.id+".pub"))
	fmt.Fprintf(stdout, "init: rotation anchor %s\n", filepath.Join(release.RotationStorePath, written.id+".pub"))
	if directoryMode != 0o755 {
		fmt.Fprintf(stderr, "init: note: the public key directory %s has mode %04o; the appliance trust-store reader requires 0755\n", absolutePublicDir, directoryMode)
	}
	return exitOK
}
