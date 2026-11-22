# 0031. Update channel on GitHub Releases

*Application bundles are published as immutable GitHub Releases in `jagoda/bittern`, discovered by a
generation tag, and beta is the only live channel until the first field release.*

- Status: Accepted
- Date: 2026-11-22
- Deciders: maintainer
- Related: [ADR 0007](./0007-mkosi-os-image-and-releases.md),
  [ADR 0008](./0008-updates-rollback-and-reset.md),
  [ADR 0012](./0012-testing-and-release-gates.md),
  [ADR 0021](./0021-security-updates.md),
  [ADR 0029](./0029-release-signing-and-key-custody.md),
  [ADR 0030](./0030-ab-from-the-first-image.md),
  [ADR 0032](./0032-continuous-integration-and-build-pipeline.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline),
  [OQ-35](../open-questions.md), [defaults/release.yaml](../../defaults/release.yaml),
  [architecture](../architecture.md)

## Context

[ADR 0008](./0008-updates-rollback-and-reset.md) fixes the app bundle transaction and requires a
signed manifest, an anti-rollback generation, and a channel. It leaves the hosting, the pointer
format, and the update-check policy open. [ADR 0029](./0029-release-signing-and-key-custody.md)
fixes the signature primitive and key custody and explicitly leaves hosting, the release server,
channel metadata, and the update-check policy to the release decision. [ADR 0021](./0021-security-updates.md)
proposes a security channel but is still Proposed. The channel set in the schema is `stable` and
`beta` ([schema/appliance.schema.json](../../schema/appliance.schema.json), lines 314 to 330).

The repository is public at `github.com/jagoda/bittern` as of 2026-11-22. `main` is a single import
commit. The publication path must work for one maintainer, must not require a server or a cloud
account beyond the repository, and must make a published release hard to replace. It must also fit
the verifier that already exists: `bittern-release sign` writes `<manifest>.sig` beside the manifest
([`agent/cmd/bittern-release/sign.go`](../../agent/cmd/bittern-release/sign.go), line 78), and the
documented names are `manifest.json` and `manifest.json.sig`
([`agent/README.md`](../../agent/README.md), lines 171 to 172; [`agent/cmd/bittern-release/verify.go`](../../agent/cmd/bittern-release/verify.go),
line 14).

GitHub Releases gives tagged, asset-bearing releases with an immutable publication setting. GitHub
documents that up to 1000 assets may belong to one release, that each file must be under 2 GiB, and
that there is no total size or bandwidth limit (GitHub Docs, "About releases", storage and bandwidth
quotas, <https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases>,
accessed 2026-11-22). The same page is the source for the 2 GiB constraint in this decision. The
2 GiB per-asset limit is why the OS image loop cannot use this asset path unchanged: an OS image is
expected to exceed it, and that loop remains with [ADR 0007](./0007-mkosi-os-image-and-releases.md)
and [ADR 0030](./0030-ab-from-the-first-image.md).

An immutable release locks its assets and tag after publication. A draft is editable and has no
visible published state, so a draft can collect every asset before one publish action makes the
release visible (GitHub Docs, "Immutable releases", <https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases>,
accessed 2026-11-22). That gives an atomic publish primitive without a separate pointer file.

GitHub limits unauthenticated REST requests to 60 per hour per originating IP (GitHub Docs, "Rate
limits for the REST API", <https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api>,
accessed 2026-11-22). A conditional request that returns `304 Not Modified` avoids the primary rate
limit only when it is correctly authorized with an `Authorization` header, so an unauthenticated
conditional request still consumes quota (GitHub Docs, "Best practices for using the REST API",
<https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api>,
accessed 2026-11-22). Every device behind one NAT shares the same hourly budget.

## Decision

**1. The update channel is GitHub Releases in `jagoda/bittern`.** CI publishes an application
bundle on a tag, and release immutability is enabled for the repository before the first published
release. A draft release is the staging container: the build uploads `manifest.json`,
`manifest.json.sig`, and the payload asset to the draft, and publishing the draft is the visible
flip. No asset is added after publication.

There is no separate pointer file, no Bittern-operated update-check endpoint, and no mutable channel
document in the device path at this stage. The device discovers releases with
`GET https://api.github.com/repos/jagoda/bittern/releases?per_page=100`, filters on the channel tag
prefix, and reads the highest generation. It does not use `/releases/latest`, which is repository
wide and not channel aware. "Newest" is the highest generation in the channel namespace, not the
API's publication order, so an out-of-order publish or a draft cannot move a device backwards. The
manifest channel must still equal the device channel, and the signature is verified before any
manifest field is trusted. A rate-limited response is a retry-later state; the device must never
report or behave as if it is up to date.

**2. The channel set stays `stable` and `beta`, and beta is the only live channel until the first
field release.** The schema already has both values, so this is no schema change and no migration.
[ADR 0008](./0008-updates-rollback-and-reset.md) remains in force and is not superseded. `stable`
is reserved but does not publish until the maintainer declares the first field release; a beta
device that changes channel waits for a stable generation that is newer than its committed
generation, as ADR 0029 requires.

**3. The tag is the publish signal and maps one to one onto the bundle generation.** Application
bundle tags use one of these forms:

| Channel | Tag form | Examples |
| --- | --- | --- |
| beta | `app-beta-<6 hex>` | generation 31 is `app-beta-00001f`; generation 1 is `app-beta-000001` |
| stable | `app-stable-<6 hex>` | generation 255 is `app-stable-0000ff` |

The suffix is a fixed-width six-character lowercase hexadecimal encoding of the generation. Fixed
width keeps lexicographic tag order equal to numeric generation order. Six hexadecimal characters
cover 16,777,215 generations, or 16.7 million, which is enough for any plausible lifetime of the
project. The parsing rule is base 16, exactly six characters, lowercase only, with no prefix inside
the number and no separators. A parser that reads the suffix as decimal is a defect, not a variation.
Generation starts at 1, so `000000` never appears and reads as unset. A generation appears in exactly
one channel, and the tag generation must equal `generation` in the signed manifest. The tag remains a
selector and never a trust input; the manifest integer is authoritative. The device filters on the
channel prefix `app-beta-` or `app-stable-`, reads the manifest, and applies the ADR 0029 channel and
generation checks. An `os-` tag namespace is not defined here and cannot be published through this
workflow.

Each release carries these three update assets, with these exact names and roles unchanged:

| Asset | Role |
| --- | --- |
| `manifest.json` | The canonical ADR 0029 manifest. |
| `manifest.json.sig` | The 64-byte detached Ed25519 signature over the domain prefix and the manifest. |
| `bittern-app.tar.gz` | The files-only payload archive named by `payload.filename`. |

The release also carries the beta preparation kit as the fourth asset, with its checksum file as a
fifth:

| Asset | Role |
| --- | --- |
| `bittern-beta-kit.tar.gz` | Unsigned beta preparation kit. It is not an update payload. |
| `SHA256SUMS` | SHA-256 checksums for the kit tarball. |

The kit contains everything needed to turn a vanilla Debian box into a beta device: the packaging
install and storage scripts, the units and templates the installer uses, the CI beta public key for
`/etc/bittern/trust/release-keys`, and a version stamp naming the bundle generation and source commit
it was built from. The manifest and its signature are not in the kit.

The kit is unsigned. Integrity is the published checksum plus release immutability. No verifier
exists on a bare Debian box, so the practical check is `sha256sum -c` against the published
checksums; a signature that the target cannot verify was rejected because it would not be checked on
that host. The kit is a different trust class from an update bundle: it is digest only and runs as
root, while a bundle is signed and verified by the device before use. The release notes and the kit's
own README must each say in one line that the kit is not an update payload and must never be
installed as one.

Preparation is download, verify the checksum, then run. Do not document a pipe from `curl` into an
interpreter or a root shell, because nothing is checked before it runs. The kit installs the CI test
anchor only. It must never carry a release anchor.

The asset URL is `<release base>/download/<tag>/<asset>`, for example
`https://github.com/jagoda/bittern/releases/download/app-beta-00001f/manifest.json`.
The product default for the release base is in [defaults/release.yaml](../../defaults/release.yaml).
GitHub's automatically generated source ZIP and tarball are not channel assets and the device ignores
them. Release notes are not a trust input. The device locates update assets by name: it reads
`manifest.json`, `manifest.json.sig`, and `payload.filename` from the manifest. It ignores every other
asset by name, including the kit and `SHA256SUMS`, so the kit cannot be mistaken for a payload
candidate. Nothing in the device path enumerates assets by position.

**4. The payload is a files-only archive, and `payload.sha256` covers that archive exactly as
distributed.** This answers [OQ-34](../open-questions.md). `manifest.json` and `manifest.json.sig`
are release assets beside the payload, not members of it. `payload.sha256` is the SHA-256 of the
archive bytes that the device downloads, so there is no self-referential digest. The manifest's
`files` list carries the size, SHA-256, and mode of every file in the extracted tree, so the
transport is checked against `payload.sha256` and the installed content is checked against the file
list. The rule does not depend on GitHub Releases; another host would use the same payload and
manifest layout.

## Consequences

### Positive

- One maintainer can publish from the public repository with no server, no credentials beyond
  GitHub, and no separate update service.
- Draft upload plus immutable publication gives a single visible flip and removes the window where
  a release is visible with a partial asset set.
- The tag carries the generation, so the device can find a candidate without a mutable pointer or a
  custom API. A tag that is not the highest generation in its channel cannot move a device forward.
- The release assets match the existing verifier names and the signing tool's output, so the
  offline signing ritual does not need a second naming convention.
- GitHub's asset bandwidth is not a project cost at this scale.
- The files-only payload gives `payload.sha256` a well-defined input and keeps the manifest out of
  its own digest.
- Publishing the beta preparation kit makes onboarding a single download, which is the point.

### Negative / costs

- GitHub is now a release dependency. An outage, a policy change, or a repository compromise
  affects update availability, although the signature still protects authenticity.
- Immutable releases cannot be edited or withdrawn in place. A bad release needs a new generation
  and, later, a pointer or advisory mechanism to tell devices which release is withdrawn.
- A public release listing invites traffic and scraping. The unauthenticated request budget is 60
  requests per hour per originating IP, so every device behind one NAT shares it. A rate limit is a
  retry-later state, never "up to date". This ADR does not add a mirror or a proxy.
- There is no promotion path from beta to stable yet. The first stable release needs a new generation
  and a recorded promotion process.
- The 2 GiB per-asset limit excludes the OS image loop from this channel unchanged.
- Drafts and tag-triggered publication add CI workflow state to the release process. A failed upload
  leaves a draft that must be discarded or repaired before the tag is published.
- The tag and the manifest can disagree only if the release tooling is wrong; the device must reject
  the mismatch rather than repair it.
- The beta preparation kit invites support questions for arbitrary Debian installations, and it is
  unsigned, digest only, and runs as root.

### Follow-ups

- Add a signed pointer file when a trigger below fires. The pointer would name the active generation
  for a channel and allow promotion, withdrawal, or a second host without changing the device's
  manifest verification.
- Record the beta-to-stable promotion process before the first field release. A new generation and a
  new signed manifest are the safe assumption because an immutable release cannot be retagged.
- Record the withdrawal process for a bad release, including the advisory text, the replacement
  generation, and the device behavior when the withdrawn release is already installed.
- Define the security channel only if [ADR 0021](./0021-security-updates.md) is accepted. It is not
  part of the current channel set.
- Add the OS image hosting decision before the first image release. The 2 GiB asset limit rules out
  the app bundle path unchanged.
- Update the update-check interval and automatic-check policy with [OQ-12](../open-questions.md).
  The default interval in `defaults/release.yaml` does not decide whether a device checks
  automatically.
- Add the release channel and asset names to the support bundle and the update history.

### Deferred items and triggers

| Deferred item | Trigger |
| --- | --- |
| A signed pointer file | Promotion from beta to stable, withdrawal of a bad release, a security channel, a second host, or a need to represent channel state without listing releases. |
| A signed pointer file for device discovery | The unauthenticated request budget becomes binding (a fleet behind one NAT, or more devices than the hourly budget allows). |
| Promotion from beta to stable | The first field release. Stable stays unpublished until then. |
| Withdrawal of a bad release | The first bad published release. Immutable assets make withdrawal a new generation plus an advisory or pointer, not an edit. |
| A security channel | [ADR 0021](./0021-security-updates.md) is accepted, or the first security-only release is needed. |
| Any additional host or mirror | A GitHub availability, policy, rate, or size constraint that blocks a release, or a second distribution point. |
| OS image assets on GitHub Releases | The first image release, if the image can be kept under 2 GiB; otherwise a different host or a chunked layout. |
| The beta preparation kit | Withdrawn or replaced when the OS image ships, because publishing the kit makes a public install path real and the delivery model is still open under [OQ-35](../open-questions.md). |

## Alternatives considered

- **GitHub Pages pointer file.** A static, mutable JSON pointer is easy to serve and can be signed.
  It adds a second publish step, a second mutable object to protect, and a cache problem, while the
  release list already gives the newest generation. Rejected for now and kept as the deferred
  pointer option.
- **`ghcr.io` OCI artifacts.** The container registry already hosts Frigate and supports digest
  addressing. It adds registry authentication, a second artifact format, and a client that is not
  the existing release verifier; it also does not give the same draft-and-publish visibility. The
  project already consumes an OCI registry for Frigate, so this remains credible, but it is more
  machinery than the app bundle needs.
- **Self-hosted object storage.** Gives control over withdrawal, promotion, and asset size. It adds
  a server, credentials, cost, monitoring, and an availability promise that one maintainer cannot
  carry. Rejected for v1.
- **A development-machine static server.** The cheapest short-term host, but the machine must be
  online for every check, its address is not a stable product endpoint, and a home connection is a
  poor release origin. Rejected.
- **A mutable release asset or pointer inside an existing release.** An asset cannot be replaced
  after an immutable release, and a mutable pointer would reintroduce the state this decision
  avoids. Rejected.
- **Do nothing and keep local bundles.** It preserves no public channel and does not satisfy
  [ADR 0008](./0008-updates-rollback-and-reset.md) or the release gate in
  [ADR 0012](./0012-testing-and-release-gates.md). Rejected.

## References

- [ADR 0007: mkosi OS image and releases](./0007-mkosi-os-image-and-releases.md)
- [ADR 0008: updates, rollback, and reset](./0008-updates-rollback-and-reset.md)
- [ADR 0012: testing and release gates](./0012-testing-and-release-gates.md)
- [ADR 0021: managed security updates and release automation](./0021-security-updates.md)
- [ADR 0029: release signing and key custody](./0029-release-signing-and-key-custody.md)
- [ADR 0030: A/B from the first image](./0030-ab-from-the-first-image.md)
- [ADR 0032: continuous integration and build pipeline](./0032-continuous-integration-and-build-pipeline.md)
- [BIT-009: app bundle update transaction](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback)
- [BIT-042: release channel and CI pipeline](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline)
- [Open questions](../open-questions.md), including OQ-12, OQ-34, and OQ-35
- [Release defaults](../../defaults/release.yaml)
- [Release signing tool note](../notes/2026-11-21-release-signing-tool.md)
- [Channel, CI, and publication note](../notes/2026-11-22-channel-ci-and-publication.md)
- GitHub Docs, "About releases": <https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases>
- GitHub Docs, "Immutable releases": <https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases>
- GitHub changelog, immutable releases public preview, 2025-08-26:
  <https://github.blog/changelog/2025-08-26-releases-now-support-immutability-in-public-preview>
