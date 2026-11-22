# Release manifest and verifier slice

- Date: 2026-11-21
- Status: implementation note for the library slice of
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback).
  No CLI, signing tool, transaction or update route is part of this slice.
- Related: [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [BIT-009 update recon](2026-11-21-bit009-update-recon.md),
  [token capability note](2026-11-21-hardware-token-capability.md),
  [the canonical vector](../../tests/fixtures/release/manifest-v1/README.md).

## What landed

`agent/internal/release/` now provides the canonical form, manifest type, trust-store reader and
Ed25519 verifier that the update transaction needs whichever custody and slot layout are chosen.

| File | Role |
| --- | --- |
| `canonical.go` | Strict parser, strict re-encoder and byte-equality check for ADR 0029 section 2 |
| `manifest.go` | Manifest, nested structures and every field rule in section 3 |
| `trust.go` | Image and rotation store reader, ownership, mode, naming and duplicate rules from section 4 |
| `verify.go` | Domain-prefixed Ed25519 verification, key-id matching, generation and payload checks |
| `doc.go` | Package boundary: verification only, no private key |

The checked-in vector is `tests/fixtures/release/manifest-v1/`: the 706-byte canonical manifest, its
SHA-256, the 734-byte domain-prefixed signing input and its SHA-256. Its key id is the placeholder
from the token note, so it fixes the canonical form, not a signer. The verifier tests generate a key
per run and sign a real manifest against a temporary trust store, so no private key is committed.

The verifier has no test branch, environment variable, build tag or configurable bypass. A test
trust store is a temporary directory read by the same `LoadTrustStore` production uses.

## Interpretations that need review

ADR 0029 leaves these details open. The implementation chose the smallest reversible reading and
records each one here rather than hiding it.

| Point | Reading in this slice | Why it matters |
| --- | --- | --- |
| Trust-store owner | The reader requires files and directories to be owned by the effective uid. The update helper runs as root, so this is the root rule in production; unit tests run as their own user. | If the helper ever runs as a non-root user, this check no longer enforces the literal root-owned rule. |
| Equal generation | A generation equal to the committed generation is rejected. The section 9 resume exception needs the active journal's candidate manifest digest, which belongs to the transaction. | The transaction must add the resume comparison rather than weakening this verifier. |
| Timestamp | `created` must parse as RFC 3339 and end in uppercase `Z`; fractional seconds remain valid RFC 3339. | The ADR says UTC without naming the accepted spellings. |
| Numeric minimums | Negative values are rejected, ranges must have min at most max, and fields that the ADR does not call positive may be zero. Missing fields are rejected separately. | `generation` zero is left to the committed-generation comparison instead of an invented field rule. |
| File order | `files` must be sorted by path and paths must be unique; unsorted or duplicate entries are rejected rather than reordered. | The ADR says the release tooling emits sorted files and the verifier does not reorder; rejecting is the fail-closed reading. |
| File modes | Only four-digit octal modes whose first digit is zero are accepted, which excludes setuid, setgid and sticky bits. | Section 5 rejects setuid and setgid at extraction; rejecting them in the manifest is earlier and stricter. |
| File types | Manifest paths are checked lexically. Symlink, hardlink, device, socket and FIFO checks stay at extraction, where the archive entry type exists. | The manifest has no file-type field, so this layer cannot make the type decision. |
| Source commit length | Any non-empty lowercase hexadecimal value is accepted. | The ADR does not say whether the full commit is 40 or 64 characters; the provenance value is not a trust input. |
| Trust-store paths | A file path under `usr/lib/bittern/trust`, `etc/bittern/trust` or `var/lib/bittern/trust` is rejected. | The transaction maps bundle paths to the app generation, but rejecting the known trust prefixes adds no bypass. |
| Payload check | `Verify` checks size and digest supplied by the caller. `VerifyManifest` stops before the payload so the transaction can verify the signature before fetching the archive. | ADR 0029 puts signature verification before the payload fetch and the digest check after it. |

## Verification

`make check` passes. The package tests cover the canonical round trip, every section 2 rejection
category, every section 3 field rule, valid and invalid signatures, an unknown key, a key id that
does not match its material, a wrong-mode key file, a symlinked key file, a duplicate id with
different material, a missing image store, a missing rotation store, generation rollback and payload
size and digest mismatch.

## Next

The next slice is the signing tool, key-file handling and CLI. Do not add them to the appliance
verifier path. The update transaction still owns channel matching, OS and schema compatibility,
resume, fetch, extraction and the A/B boundary.
