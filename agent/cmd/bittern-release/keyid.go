package main

import (
	"fmt"
	"io"
	"path/filepath"

	"bittern.invalid/agent/internal/release"
)

const keyIDUsage = `usage: bittern-release key-id KEYFILE [--pub-dir DIR]

Reads an existing signing key file with the same custody checks as sign and
prints its key id and public key, so a maintainer can re-derive the anchor after
moving the key file. With --pub-dir it also writes <key_id>.pub there with mode
0644 and reports the path the image build expects.

  --pub-dir DIR   also write <key_id>.pub into DIR
`

func runKeyID(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("key-id", keyIDUsage, stderr)
	publicDir := flags.String("pub-dir", "", "also write <key_id>.pub into this directory")
	flagArgs, positionals := splitArgs(flags, args)
	if proceed, code := parseFlags(flags, flagArgs); !proceed {
		return code
	}
	if !wantArgs(flags, positionals, 1) {
		return exitUsage
	}
	key, err := readSigningKey(positionals[0])
	if err != nil {
		return fail(stderr, "key-id", "%v", err)
	}
	fmt.Fprintf(stdout, "key-id: key file %s (32-byte Ed25519 seed, mode 0600)\n", key.path)
	fmt.Fprintf(stdout, "key-id: key id %s\n", key.id)
	fmt.Fprintf(stdout, "key-id: public key %s\n", publicKeyHex(key.public))
	if *publicDir != "" {
		absoluteDir, err := filepath.Abs(*publicDir)
		if err != nil {
			return fail(stderr, "key-id", "resolve the public key directory: %v", err)
		}
		if _, err := ensurePublicKeyDir(absoluteDir); err != nil {
			return fail(stderr, "key-id", "%v", err)
		}
		publicPath, created, err := writePublicKey(absoluteDir, key.public)
		if err != nil {
			return fail(stderr, "key-id", "%v", err)
		}
		if err := verifyPublicKeyFile(publicPath, key.public); err != nil {
			return fail(stderr, "key-id", "%v", err)
		}
		state := "created"
		if !created {
			state = "already present with the same content"
		}
		fmt.Fprintf(stdout, "key-id: public key file %s (32 raw bytes, mode 0644, %s)\n", publicPath, state)
	}
	fmt.Fprintf(stdout, "key-id: image anchor %s\n", filepath.Join(release.ImageStorePath, key.id+".pub"))
	fmt.Fprintf(stdout, "key-id: rotation anchor %s\n", filepath.Join(release.RotationStorePath, key.id+".pub"))
	return exitOK
}
