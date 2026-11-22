# Trust anchors

Repository copies of public trust anchors. Private keys never live here.

## Beta CI test anchor

`beta-ci.pub` is the public half of the CI-only Ed25519 key generated on 2026-11-22 for the
`beta` GitHub Actions environment. It signs test bundles only. It is a bench test anchor: it is
pinned only on bench devices and never in a release image or a release trust store, and it is never
the release anchor. Release manifests are signed offline under
[ADR 0029](../../docs/adr/0029-release-signing-and-key-custody.md). ADR 0032 records the CI
decision.

`beta-ci.pub` is the raw 32-byte Ed25519 public key. Its key id is
`40be72d21dfe5682f3bd715a1135c134d50bdf9a92ee22c328e1eb28141eaec8`, the lowercase SHA-256 of the
public key. The appliance trust store reads the key from `<key_id>.pub`, so a bench device needs the
file renamed to `40be72d21dfe5682f3bd715a1135c134d50bdf9a92ee22c328e1eb28141eaec8.pub`. The key id
is also the SHA-256 of this file.

The private half is held only as the `BITTERN_CI_SIGNING_KEY` environment secret in `beta`, never
at repository or organization scope. The environment stores it base64-encoded, and a workflow must
decode it to the 32 raw seed bytes before `bittern-release sign`.

Public key (hex): `1ff8f35f7c930c5d30101f1f15171818d03eec69d69d974998f6a31f0e3d6222`

## Environment tag policy

The `beta` environment allows the tag glob `app-beta-*`, which is the documented `*` wildcard form.
The
[deployments and environments reference](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
(accessed 2026-11-22) documents the `*` wildcard and refers further syntax to Ruby `File.fnmatch`;
it does not document character classes or ranges as supported. The exact tag format is therefore
validated by the publish workflow before it creates the draft, not by an undocumented glob feature.
The workflow must accept only `app-beta-` followed by exactly six lowercase hexadecimal characters,
with generation starting at `000001`, and reject everything else.

The environment policy and the ADR 0031 tag scheme must change together. A change to either one
without the other either blocks a valid tag at deploy time or permits a tag the ADR does not
define.
