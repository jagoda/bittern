# 0032. Continuous integration and build pipeline

*CI runs the existing local checks on every push and pull request, builds an unsigned bundle
artifact first, and adds tag-driven publication later with a CI-only test key, never the release
key.*

- Status: Accepted
- Date: 2026-11-22
- Deciders: maintainer
- Related: [ADR 0007](./0007-mkosi-os-image-and-releases.md),
  [ADR 0012](./0012-testing-and-release-gates.md),
  [ADR 0015](./0015-agent-language.md),
  [ADR 0021](./0021-security-updates.md),
  [ADR 0029](./0029-release-signing-and-key-custody.md),
  [ADR 0031](./0031-update-channel-on-github-releases.md),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline),
  [implementation plan](../plan/implementation-plan.md), [architecture](../architecture.md)

## Context

The repository has had no CI. That was a plan constraint rather than an Accepted architecture
decision: [ADR 0007](./0007-mkosi-os-image-and-releases.md) says image builds move into CI later,
and [ADR 0021](./0021-security-updates.md) defers the CI choice to another ADR. The local checks are
`make check`, which runs the link, schema, style, design, bundle, cleanup, storage and unit
enablement checks and then the Go tests ([`Makefile`](../../Makefile), lines 75 to 87). They are run
by a developer or agent, not by the repository.

The repository is now public at `github.com/jagoda/bittern`, and [ADR 0031](./0031-update-channel-on-github-releases.md)
puts the update channel on GitHub Releases in the same repository. The release key remains offline
and airgapped under [ADR 0029](./0029-release-signing-and-key-custody.md), so CI cannot be the
release signer. The useful automation is therefore the repeatable check and build work around the
release, with the human and airgap kept at the signature.

The previous exclusion is reversed because the public repository now needs a check that does not
depend on one person remembering to run it, and because the channel needs a reproducible build and
upload path. The reversal does not put the release key, the appliance, or QEMU on a hosted runner.

## Decision

**1. CI is GitHub Actions in `jagoda/bittern`.** It is introduced in two phases. Phase one is the
existing local check target on every push and every pull request, plus a bundle build as a workflow
artifact. Phase one has no secrets, no write permission, and no publishing. The artifact is an
unsigned test build and is labelled as such.

**2. Phase two publishes on a tag.** A tag matching the application bundle scheme in
[ADR 0031](./0031-update-channel-on-github-releases.md) creates a draft release, uploads
`manifest.json`, `manifest.json.sig`, and `bittern-app.tar.gz`, and, after the approval in point 7,
publishes the draft as the visible flip. The job runs only after the phase one checks pass. The tag
suffix is parsed as fixed-width six-character lowercase hexadecimal per
[ADR 0031](./0031-update-channel-on-github-releases.md); a decimal parse is a defect, and the
manifest generation remains authoritative.

CI test bundles are signed with a CI-only Ed25519 development key held in GitHub Actions secrets.
That key signs test bundles only. Its public key is pinned only on bench devices, never in a release
image or a release trust store, and it is never the release key. The production release manifest is
signed on the airgapped signing machine under [ADR 0029](./0029-release-signing-and-key-custody.md)
and the signature is attached to the draft. CI never holds the release seed and cannot produce a
signature that a field device trusts. The two key ids make the test and release paths distinguishable
in the manifest and the update record.

The two signing paths are distinct. While the project is pre-release, the beta channel uses phase
two: CI test releases signed with the CI test key. A release that a field device trusts is assembled
differently: CI builds the bundle and uploads it to the draft, the manifest is signed on the
airgapped machine, the signature is attached to the same draft, and only then, after the environment
approval, is the draft published. The CI test key and the release key never sign the same manifest.

**3. Third-party actions are pinned by full commit SHA.** Every action, including first-party
GitHub actions, uses a 40-character commit SHA rather than a version tag or branch, and the repository
setting requires SHA pinning. A pin changes only in a reviewed commit. This is a supply-chain control,
not a convenience: an immutable release protects the assets after publication, and the offline
signature protects the manifest, but a mutable action could alter the bundle before either control
applies. A workflow that cannot be expressed with SHA-pinned actions is rejected rather than given a
tag exception. Workflow permissions are least privilege: phase one has read-only repository access,
and phase two gives `contents: write` only to the publish job.

**4. CI is responsible for checks, build artifacts, and the channel upload.** It is not responsible
for release signing, release approval, the OS image, the appliance runtime, or hardware tests.

**5. QEMU scenarios are outside CI for now.** The hosted-runner KVM question is unresolved and is
not assumed either way. QEMU and hardware tests stay on the local or bench path until a runner is
measured to provide usable acceleration and the test cost is understood. This is a named limit, not
a claim that CI covers the release gate.

**6. CI must never hold the release private key, a release trust anchor, a device secret, camera
credentials, or a publishing credential beyond the scoped GitHub token.** The only long-lived secret
in phase two is the CI-only test key. A change that needs another secret needs its own decision.

**7. Publication runs in a protected environment with maintainer approval.** The publish job runs in
a GitHub environment named for the channel, `beta` or `stable`. The environment requires maintainer
approval, and only the maintainer, as the environment's required reviewer, can approve. The CI test
key secret is stored in that environment rather than at repository scope. A tag push starts the
workflow but cannot publish on its own. An unapproved, rejected, or cancelled run leaves a draft and
no published release; the tag may exist, but the release stays invisible until approval.

**8. The beta preparation kit is a CI build product.** `make beta-payload` is adapted from
`make poc-payload`; it produces `bittern-beta-kit.tar.gz` and `SHA256SUMS` and supersedes the POC
payload target rather than duplicating it. Phase two publishes both files on the same release as the
update assets. The kit is unsigned, digest only, runs as root, installs only the CI test anchor, and
is not an update payload. [ADR 0031](./0031-update-channel-on-github-releases.md) owns its contents,
trust class, and preparation steps.

## Consequences

### Positive

- Every push and pull request gets the same offline check target, so a broken link, schema, render,
  or Go test is caught without a person remembering to run it.
- The bundle build is reproducible from the workflow and available as an artifact for review and
  bench testing before any publication path exists.
- The tag-to-draft-to-publish sequence matches the immutable release model in ADR 0031 and leaves a
  clear failure point: a failed check leaves no published release.
- The CI test key lets bench devices exercise the signed update path without putting the release key
  on a runner.
- SHA pinning removes a mutable-action path into the build.
- The local `make check` remains the same command developers run, so CI does not create a second
  definition of green.

### Negative / costs

- CI adds a second environment to maintain: runner images, action pins, workflow permissions, and
  artifact retention.
- The workflow is a supply-chain surface. Pinning by SHA reduces but does not remove the risk of a
  compromised action or runner.
- GitHub Actions is another dependency on the same host as the release channel. A GitHub outage
  blocks checks and publication together.
- Phase one cannot sign or publish, so it does not prove the release path; phase two is a separate
  risk and a separate review.
- Publishing now needs one interactive step: the maintainer approves the protected environment after
  the draft is complete. This is the deliberate trade for keeping a stray or compromised tag push
  away from bench devices.
- QEMU and hardware coverage remain outside CI, so a green CI run is not the ADR 0012 release gate.
- A CI test release is still a published immutable release. Its tag and assets cannot be reused, so
  test tags need a clear namespace and a disposal rule.
- Pinned actions need periodic review and update, which is recurring maintenance for one maintainer.

### Follow-ups

- Implement phase one in [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline)
  and record the exact action pins in the workflow review.
- Implement phase two after the bundle packer exists and [ADR 0029](./0029-release-signing-and-key-custody.md)
  is accepted. Record the CI test key generation, storage, rotation, and bench trust-anchor
  procedure.
- Decide how CI test releases are named and cleaned up so they do not collide with the production
  beta namespace.
- Measure KVM availability and QEMU cost on a candidate runner before adding any QEMU scenario to
  CI. The question stays unresolved until then.
- Add the OS image build only after the image work and its asset hosting are decided. It is not part
  of the app bundle workflow.
- Add a CI status or badge to the README only when phase one is actually running.
- Record the phase two workflow permissions and the secret scope in the release runbook.
- Add tag parser tests from [ADR 0031](./0031-update-channel-on-github-releases.md): assert that
  `app-beta-0000ff` parses to 255 and `app-beta-000010` parses to 16, and reject uppercase hex,
  five-character, seven-character, decimal-looking overflow, and empty suffixes. A decimal parse
  must fail loudly.
- Implement `make beta-payload` from the existing `make poc-payload` recipe, then retire the POC
  payload target rather than keeping two builders.

## Alternatives considered

- **Keep no CI.** The plan previously excluded CI to keep the surface small. The public repository
  and the release channel now make a repeatable check and build path worth the maintenance cost.
  Rejected.
- **A different CI host.** GitLab CI, Buildkite, or a self-hosted runner would separate CI from the
  release host. They add an account, a token, and a second integration for a one-maintainer project
  that already uses GitHub. Rejected for v1; a mirror or a second host is a deferred item in ADR 0031.
- **Put the release key in Actions secrets and sign in CI.** It removes the offline step and makes
  the runner a high-value target. It contradicts ADR 0029 and the airgap decision. Rejected.
- **Use a long-lived PAT for publishing.** The scoped `GITHUB_TOKEN` is enough for release creation
  and asset upload in the repository. A PAT adds a credential to rotate and leak. Rejected.
- **Run QEMU in CI immediately.** The KVM question is unresolved and the test cost is high. Deferred
  until a runner is measured. Rejected for the initial phases.
- **Publish unsigned bundles and verify later.** The device verifies before apply, so an unsigned
  asset would be rejected and the release would be useless. Rejected.
- **Pin actions by major version.** Easier to read, but a mutable tag can change under the workflow.
  Rejected because the repository requires SHA pinning.

## References

- [ADR 0007: mkosi OS image and releases](./0007-mkosi-os-image-and-releases.md)
- [ADR 0012: testing and release gates](./0012-testing-and-release-gates.md)
- [ADR 0015: agent language](./0015-agent-language.md)
- [ADR 0021: managed security updates and release automation](./0021-security-updates.md)
- [ADR 0029: release signing and key custody](./0029-release-signing-and-key-custody.md)
- [ADR 0031: update channel on GitHub Releases](./0031-update-channel-on-github-releases.md)
- [BIT-042: release channel and CI pipeline](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline)
- [Implementation plan](../plan/implementation-plan.md)
- [Channel, CI, and publication note](../notes/2026-11-22-channel-ci-and-publication.md)
- [Release signing tool note](../notes/2026-11-21-release-signing-tool.md)
- GitHub Actions, security hardening: <https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions>
- GitHub Docs, "Immutable releases": <https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases>
