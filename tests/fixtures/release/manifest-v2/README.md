# Release manifest canonical vector v2

This directory is the checked-in canonical vector for the ADR 0036 manifest version 2. It carries one
bundle-declared unit and one typed migration, so the canonical form, the subsystem field set, and the
signing input are fixed together.

| File | Content |
| --- | --- |
| `manifest.json` | The 1157 canonical manifest bytes, with no trailing newline |
| `manifest.sha256` | SHA-256 of `manifest.json` |
| `signing-input.bin` | The 28-byte domain prefix `bittern-release-manifest-v1\n` and the manifest |
| `signing-input.sha256` | SHA-256 of `signing-input.bin` |

Expected values:

| Check | Value |
| --- | --- |
| `wc -c < manifest.json` | `1157` |
| `sha256sum manifest.json` | `0e25336c650488efed2def1d53d4c39efe1fecbfa9f78f375e4ef45aab3c94da` |
| `wc -c < signing-input.bin` | `1185` |
| `sha256sum signing-input.bin` | `c4d970b90c3bb22d209b6bac2b8195f45e2f2afe9b26b1880e60c8f22ca1641c` |
| Unit source digest | `e082fe494bda1a6cd93285077b7e125ee7c27e90ba3bd2e23d28e8f9aed63950` |

The unit source is `usr/lib/bittern/units/bittern-bundle-example.service` and its canonical file entry
has the same digest and mode `0644`. The unit entry points at `/etc/systemd/system/` and declares
`enabled: true` and `reload: true`. The migration entry names `appliance-schema-2-to-3`.

Like the v1 vector, this is a canonical-form test, not a signable release. The key id is the
placeholder `2222...` and the payload digest is a placeholder. The tests substitute a per-run key id
before signing, as ADR 0029 section 9 requires.

The v1 vector remains unchanged at [manifest-v1](../manifest-v1/README.md). The verifier accepts both;
the release tooling emits version 1 unless a subsystem change is present. The signature domain prefix
stays `bittern-release-manifest-v1\n` because it names the signature envelope, not the field set.

To regenerate `signing-input.bin`:

```sh
printf 'bittern-release-manifest-v1\n' > signing-input.bin
cat manifest.json >> signing-input.bin
sha256sum -c manifest.sha256 signing-input.sha256
```

Related: [ADR 0036](../../../../docs/adr/0036-update-bundles-deliver-subsystem-changes.md),
[the v1 vector](../manifest-v1/README.md), [the release verifier](../../../../agent/internal/release/verify.go).
