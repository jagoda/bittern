# Release manifest canonical vector v1

This directory is the checked-in canonical vector for the release manifest defined by
[ADR 0029](../../../../docs/adr/0029-release-signing-and-key-custody.md). It is small, fixed, and
safe to use from the signing tool and from a second implementation.

| File | Content |
| --- | --- |
| `manifest.json` | The 706 canonical manifest bytes, with no trailing newline |
| `manifest.sha256` | SHA-256 of `manifest.json` |
| `signing-input.bin` | The 28-byte domain prefix `bittern-release-manifest-v1\n` and the manifest |
| `signing-input.sha256` | SHA-256 of `signing-input.bin` |

Expected values:

| Check | Value |
| --- | --- |
| `wc -c < manifest.json` | `706` |
| `sha256sum manifest.json` | `4fa961778a947699d48064140323d3ed378aaa21d4dd5c8e162b5b53a2f8e1c1` |
| `wc -c < signing-input.bin` | `734` |
| `sha256sum signing-input.bin` | `c8bf57b86c9e033f6ef08d39a70ffdd1562599b9c8e1b2b3d4d4f0f616356559` |

This is a **canonical-form test vector**, not a signable release. The `signing.key_id` is the
placeholder `2222...`, not a real key, and the vector has no signature. Signing this manifest with
any real signing key must fail with exit 1, because the key file derives its own id and the manifest
declares the placeholder instead. Do not give it to the verifier as a trusted release: the verifier
rejects the placeholder key as unknown. The vector fixes the canonical form, not a signer.

The tests substitute a per-run key id before signing the vector. `TestGoldenVectorSigningInput` in
[`vector_test.go`](../../../../agent/cmd/bittern-release/vector_test.go) performs that substitution
and signs the result with the generated key, as required by ADR 0029 section 9. No private key is
committed.

The manifest records a placeholder source commit, a placeholder payload digest, and one file digest
of zeros. Those values are fixtures, not claims about a real bundle.

To regenerate the canonical bytes, write the manifest with the in-tree encoder and compare it with
`manifest.json`. Do not pretty-print, reorder keys, or add a newline. To regenerate
`signing-input.bin`, concatenate the prefix and the manifest bytes:

```sh
printf 'bittern-release-manifest-v1\n' > signing-input.bin
cat manifest.json >> signing-input.bin
sha256sum -c manifest.sha256 signing-input.sha256
```

Related: [ADR 0029](../../../../docs/adr/0029-release-signing-and-key-custody.md),
[the token capability note](../../../../docs/notes/2026-11-21-hardware-token-capability.md),
[the release verifier](../../../../agent/internal/release/verify.go).
