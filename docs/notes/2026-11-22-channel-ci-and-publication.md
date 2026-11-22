# Channel, CI, and publication

- Date: 2026-11-22
- Status: decision note. Records the channel, CI, and engine decisions taken in session. No
  implementation code, workflow, key, or release artifact was produced.
- Related: [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [ADR 0007](../adr/0007-mkosi-os-image-and-releases.md),
  [ADR 0008](../adr/0008-updates-rollback-and-reset.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [ADR 0030](../adr/0030-ab-from-the-first-image.md),
  [OQ-34](../open-questions.md), [OQ-35](../open-questions.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline),
  [release signing tool](2026-11-21-release-signing-tool.md),
  [BIT-009 update recon](2026-11-21-bit009-update-recon.md)

## Repository publication

The repository was published publicly on 2026-11-22 at
<https://github.com/jagoda/bittern>. At the time of this note, `gh repo view` reports visibility
`PUBLIC` and creation time `2026-11-22T14:22:42Z`. `main` is a single import commit,
`060bc76` (`bittern: an appliance layer around Frigate NVR`), and the remote is
`git@github.com:jagoda/bittern.git`. Release immutability is enabled for the repository. No release
has been published yet.

The import commit is the history baseline. The earlier local branches and worktrees are not part of
the public history. Release tags are created from `main` after this point.

## Channel decision

[ADR 0031](../adr/0031-update-channel-on-github-releases.md) decides:

- The app bundle channel is GitHub Releases in `jagoda/bittern`.
- CI publishes on a tag, and release immutability is enabled.
- A draft release is the atomic publish primitive: upload `manifest.json`, `manifest.json.sig`, and
  `bittern-app.tar.gz` to the draft, then publish the draft as the visible flip.
- There is no separate pointer file and no Bittern-operated update-check endpoint. The device reads
  the newest published release in its channel namespace.
- The channel set stays `stable` and `beta`; beta is the only live channel until the first field
  release. There is no schema change, no migration, and no supersession of
  [ADR 0008](../adr/0008-updates-rollback-and-reset.md).
- The tag scheme is `app-beta-g<generation>` or `app-stable-g<generation>`, with a zero-padded
  19-digit generation. The tag generation must equal the signed manifest generation.
- The assets are `manifest.json`, `manifest.json.sig`, and `bittern-app.tar.gz`.
- The payload is a files-only archive. `payload.sha256` is the digest of that archive exactly as
  distributed, and the manifest lists per-file digests of the extracted tree. This answers OQ-34.
- GitHub limits each release asset to under 2 GiB, verified against the GitHub Docs on 2026-11-22.
  The OS image loop therefore cannot use this asset path unchanged.

The deferred list and its triggers is in the ADR. The short version: a signed pointer, beta-to-stable
promotion, withdrawal of a bad release, a security channel, another host, and OS image assets all
wait for their named trigger.

## CI decision

[ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md) decides:

- CI is GitHub Actions in `jagoda/bittern`. The previous exclusion was a plan constraint, not an
  Accepted ADR, and it is reversed here.
- Phase one runs the existing `make check` target on push and pull request and builds a bundle as a
  workflow artifact. It has no secrets and does not publish.
- Phase two publishes on a bundle tag. It creates a draft, uploads the assets, and publishes the
  draft. CI test bundles are signed with a CI-only development key in Actions secrets. That key is
  for test bundles only; its trust anchor exists only on bench devices and it is never the release
  key.
- Release signing stays offline and airgapped under [ADR 0029](../adr/0029-release-signing-and-key-custody.md).
  CI never holds the release seed.
- Third-party actions are pinned by full commit SHA, and the repository requires SHA pinning.
- QEMU scenarios are outside CI for now. The KVM question for hosted runners is unresolved and is
  not assumed either way.

## Engine invariants

These are stated where the update transaction is specified, in
[the architecture update flow](../architecture.md#update-flow), the
[status contract](../status.md), and
[BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback).

1. The platform generation is abstract and supplied by a provider. Image metadata can supply it
   now, and an installer fingerprint can supply it later, without changing the transaction.
2. The transaction writes only under `/var/lib/bittern`. It never assumes a writable root and never
   assumes knowledge of OS slots.
3. The agent records a base fingerprint in the status contract. It covers the Debian version, the
   kernel, the Docker version, device availability, and firmware presence. It is recorded evidence,
   not a trust input.

## Delivery model remains open

[ADR 0007](../adr/0007-mkosi-os-image-and-releases.md) remains in force. The current packaging
install path (`packaging/install.sh`) is a bridge, not product policy. The image becomes required
before the first device that is not the maintainer's. The delivery model and supported base are
[OQ-35](../open-questions.md), and the trigger is that first non-maintainer device.

## Operational caveat

The Git commit signing key is the container key. It changes when the container is recreated, so
commits made after recreation carry a new key until that key is registered with GitHub. Treat the
commit signature as a local identity check, not as the durable release trust anchor. The release
signing key is separate and stays on the airgapped machine under ADR 0029.

## Next step

Implement CI phase one in BIT-042, then the BIT-009 transaction core, then the bundle packer with
publish on tag, then the QEMU update scenario. No release is published until the offline signing
ceremony exists and ADR 0029 is accepted.

`make link-check` and `make style-check` were run for this note as part of the change that added it.
