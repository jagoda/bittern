# Trust anchors

Repository copies of public trust anchors. Private keys never live here.

## Beta CI test anchor

`beta-ci.pub` is the public half of the CI-only Ed25519 key generated on 2026-11-22 for the
`beta` GitHub Actions environment. It signs test bundles only. It is a bench test anchor: it is
pinned only on bench devices and never in a release image or a release trust store, and it is never
the release anchor. Release manifests are signed offline under
[ADR 0029](../../docs/adr/0029-release-signing-and-key-custody.md). ADR 0032 records the CI
decision.

The file is the raw 32-byte Ed25519 public key, so the format has no room for a comment. Its key id
is `40be72d21dfe5682f3bd715a1135c134d50bdf9a92ee22c328e1eb28141eaec8`, the lowercase SHA-256 of
the public key. The appliance trust store reads the key from `<key_id>.pub`, so a bench device needs
the file renamed to `40be72d21dfe5682f3bd715a1135c134d50bdf9a92ee22c328e1eb28141eaec8.pub`. The
key id is also the SHA-256 of this file. The private half is held only as the
`BITTERN_CI_SIGNING_KEY` environment secret in `beta`, base64-encoded. A workflow must decode it to
the 32 raw seed bytes before `bittern-release sign`.

Public key (hex): `1ff8f35f7c930c5d30101f1f15171818d03eec69d69d974998f6a31f0e3d6222`
