# Release signing tool

- Date: 2026-11-21
- Status: implementation note for the signing half of BIT-009, following
  [the manifest and verifier slice](2026-11-21-release-manifest-verifier.md). The update transaction,
  the bundle layout, the release server and the image signature envelope are not part of this slice.
- Related: [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [OQ-34](../open-questions.md) (what `payload.sha256` covers),
  [the canonical vector](../../tests/fixtures/release/manifest-v1/README.md),
  [the agent README](../../agent/README.md#release-signing-tool).

## What landed

`agent/cmd/bittern-release/` is a plain static binary for the airgapped signing machine. It builds
with `cd agent && go build ./cmd/bittern-release`, or `make bittern-release` into `agent/bin/`. It
has no new dependency, no network access, no configuration file and no environment input beyond its
flags. It is not in the appliance payload and no payload or image target builds it.

| File | Role |
| --- | --- |
| `main.go` | Subcommand dispatch, interspersed-flag parsing, rejection classification and exit codes |
| `keys.go` | Key-file custody checks, seed generation, public-key anchor write, temporary trust store |
| `init.go` | `init`: generate and write the seed and `<key_id>.pub` |
| `sign.go` | `sign`: field report, typed confirmation, detached signature, self-check and delete on failure |
| `verify.go` | `verify`: the production trust-store reader and verifier, with distinct exit codes |
| `manifest.go` | `manifest`: stream a tar or tar.gz payload once into digest, size and the sorted file list |
| `keyid.go` | `key-id`: re-derive the key id, public key and anchor paths from a key file |
| `*_test.go` | Custody, signature, rejection, exit-code, archive, environment and golden-vector tests |

The tool reuses `agent/internal/release/` for the canonical form, the manifest rules, the
domain-prefixed signing input, the trust-store reader and verification. It does not implement the
scheme a second time. `sign` verifies its own output through `release.LoadTrustStore` and
`release.VerifyManifest`, with the signer's public key in a temporary 0755 store, so the self-check
is the production lookup path with different material. The QEMU test-key path is the same path: a
temporary key file and a temporary trust directory passed by flag. No environment variable, build
tag or test branch can replace the store or skip verification.

## Command surface and exit codes

| Command | Output |
| --- | --- |
| `init --key PATH [--pub-dir DIR] [--force] [--dry-run]` | Seed mode 0600, `<key_id>.pub` mode 0644, key id, public key, image and rotation anchors |
| `sign MANIFEST.json --key PATH [--yes] [--force] [--dry-run]` | `<manifest>.sig`, exactly 64 bytes, after the fixed field block and the typed confirmation |
| `verify MANIFEST.json SIGNATURE --trust-dir DIR [--rotation-dir DIR] [--kind KIND] [--committed-generation N]` | A verification record and an exit code |
| `manifest --payload ARCHIVE ... --out PATH [--dry-run]` | Canonical manifest bytes; `--out -` writes them to stdout and the record to stderr |
| `key-id KEYFILE [--pub-dir DIR]` | Key id, public key, anchor paths, and optionally the anchor file |

Exit codes are shared by `sign` and `verify`: 0 valid, 1 signer-side or I/O failure, 2 usage, 3 bad
signature, 4 untrusted key or unreadable trust store, 5 non-canonical or invalid manifest, 6 kind or
generation rejection. `sign` classifies a rejected manifest the same way, so a script sees one
vocabulary.

## Key custody checks

`init` and the readers behind `sign` and `key-id` enforce the section 4 rules on every use:

| Check | Reading in this slice |
| --- | --- |
| Size | Exactly 32 bytes; a 64-byte expanded private key is refused |
| Type | A regular file; a symlink or directory is refused, and `O_NOFOLLOW` closes the check-to-open race |
| Owner | The effective uid, the same rule the trust-store reader uses |
| Mode | No group or other permission bits, and the owner must be able to read it; 0600 passes, 0644 fails, and 0400 is also accepted because it is narrower |
| Name | A path ending in `.pub` is refused before it is read, because a public key file and a seed are both 32 bytes |

`init` refuses a group- or other-accessible existing key even with `--force` and tells the operator
to `chmod` it first, rather than silently tightening a file it did not create. An existing anchor
with the same content is reported as `already present`, so a second `init` or `key-id --pub-dir` is
a no-op; an anchor whose content does not match its name is refused, not overwritten.

## Findings

### The payload digest is self-referential

ADR 0029 section 3 says the archive contains `manifest.json`, its signature files and the payload
files, and that `payload.sha256` covers "the archive including the manifest and signatures". The
manifest contains `payload.sha256`, so the digest would have to cover bytes that contain the digest.
There is no fixed point: changing the digest changes the archive, which changes the digest.

The task for this slice defines `manifest --payload` as hashing the archive it is given, so the tool
does exactly that and records the archive's own digest and size. The interim reading is a files-only
payload archive, with the manifest and signature shipped beside it rather than inside it. The
contradiction, the options and the owner are recorded as [OQ-34](../open-questions.md); the release
path cannot assemble its bundle until it is answered. The signing tool is not blocked: it signs
whatever canonical manifest the build host produces, and the transaction already verifies
`payload.size` and `payload.sha256` against the archive it fetches.

### The signature file name

The task specifies `<manifest>.sig`, so signing `build/manifest.json` writes
`build/manifest.json.sig`. ADR 0029 describes the detached signature as `manifest.sig` inside the
bundle. `verify` takes the signature path explicitly, so both layouts work; the bundle assembly
step must decide the published name. This is not recorded as a new question because it is a naming
choice in the bundle layout, which the release ADR owns.

### The canonical vector is not a signable release

`tests/fixtures/release/manifest-v1/manifest.json` exists to fix the canonical form. Its
`signing.key_id` is the placeholder from the token note, so `sign` correctly exits 1 with a key id
mismatch when given any real key. The fixture README now says this, and `vector_test.go` is the test
that substitutes a per-run key id before signing it. The documented recipe was run end to end in a
temporary directory; the tamper cases recorded a bad signature as exit 3, an untrusted key as exit 4
and a non-canonical manifest as exit 5.

### Format handling

`manifest` accepts a plain tar and a gzip-compressed tar, detected by the gzip magic rather than by
the file name. It lists regular files only: directories are skipped because the transaction creates
parent directories, and symlinks, hardlinks, device nodes, sockets and FIFOs are refused because
ADR 0029 forbids them and the manifest has no file-type field. Setuid, setgid and sticky bits are
refused before the library sees the mode, because the four-digit mode rule cannot express them.
Timestamps, owners and group IDs are not manifest fields and are ignored. The archive is read once
and streamed, so a release-sized payload is not held in memory.

## Interpretations that need review

| Point | Reading in this slice | Why it matters |
| --- | --- | --- |
| Key-file mode | Refuse any group or other bit and require owner-read, not literally `0600`; `init` writes `0600` | The ADR's clarification is "not group-readable and not other-readable"; rejecting an owner-write-only or owner-read-only file would be stricter than the threat requires. |
| Key id mismatch | `sign` refuses a manifest whose `signing.key_id` differs from the key file instead of rewriting the manifest | Only the signature crosses the airgap back, so the manifest must arrive already naming its signer; a rewrite would create a second document to carry back. |
| Generation floor | `verify` defaults `--committed-generation` to -1 and uses `VerifyManifest`, not `Verify` | The release tool has no committed generation and no payload archive; the transaction still calls the full `Verify` with both. |
| Payload digest | Digest and size are the archive bytes as supplied | The alternative is the self-referential reading above; [OQ-34](../open-questions.md) owns the decision. |
| Required flags | `--source-commit`, `--key-id`, the compatibility range and `--updater-min` are required, not inferred | The manifest is the claim the maintainer signs; reading a commit from the current directory or a schema range from a default would sign a guessed provenance. `--created` defaults to now UTC and is a flag when determinism matters. |
| Manifest replacement | `manifest` rewrites its output only when the bytes change | A second run with the same inputs is a no-op and reports `unchanged`, which is the check a build script needs. |
| Trust store on the signing machine | The self-check writes a temporary 0755 image store and a 0644 key file, then removes them | It exercises the production reader without adding a constructor or a bypass to `internal/release`. |
| Signature checks | A missing, truncated or invalid signature is exit 3; an unknown key, a name/content mismatch or a missing store is exit 4 | The categories match ADR 0029 section 6 and let the transaction's future CI script separate a bad artifact from a bad anchor. |

## Not implemented

- The encrypted-volume check from section 4. The tool cannot prove that a path is on a dm-crypt
  volume from a plain file path, and no flag names the expected volume without becoming manual
  configuration. The key ceremony and the signing-machine runbook must check the mount instead.
- Dual signatures and the rotation statement. ADR 0029 section 7 defines `manifest.sig.<key_id>` and
  a dual-signed statement but leaves the statement's fields and carrier to the release and image
  ADRs. The tool signs one key per invocation and writes `<manifest>.sig` only.
- The key ceremony, the key-loss procedure and the airgap rehearsal. These are ADR 0029 follow-ups,
  not tool behavior.
- The QEMU update scenario. BIT-009 has no transaction yet; the test-key path is covered by the
  command tests and the rotation-store test here.

## Verification

`make check` passes, including `go test ./...` for the module. The command tests cover `init` on a
fresh and an existing key, a symlink, a group-readable file, a non-regular path and a symlinked
anchor directory; `sign` on the word and the version, a declined prompt, an existing signature, a
symlink signature, a mismatched key id and a failed self-check; `verify` on a tampered manifest, a
tampered and a truncated signature, a missing signature, a wrong and a missing trust store, a
world-readable store, a non-canonical document, an unsorted document, an unknown field, a wrong kind
and a generation floor; and `manifest` on a tar.gz, a plain tar, sorted output, directory skipping,
symlink and hardlink entries, special mode bits, absolute and parent paths, duplicate paths, trust
store paths, missing flags and invalid fields. The golden test signs the checked-in vector with the
placeholder key id replaced, asserts that the signing input is the fixture signing input with the
same single substitution, and verifies the 64-byte signature with both `crypto/ed25519` and
`release.VerifyManifest`.

## Next

1. Answer [OQ-34](../open-questions.md) with the release ADR so the bundle layout has one digest
   input.
2. Wire `bittern-release` into the QEMU update scenario when the transaction lands: generate a key
   per run, write the anchor into the guest rotation store as root, sign the fixture manifest, and
   run the valid, invalid-signature, digest and downgrade cases.
3. Write the key ceremony and custody runbook, including the encrypted-volume check, the anchor
   export, the airgap rehearsal and the recorded tool hash from the ADR 0029 follow-ups.
