# Session brief: P0 release channel and engine

- Status: in progress; decisions recorded 2026-11-22, CI phase one not started
- Tickets: BIT-042, BIT-009
- Branch: `p0/release-channel-engine`
- Plan: [implementation plan](../implementation-plan.md)

## Goal

Turn the 2026-11-22 channel and CI decisions into a working release path: CI phase one, the BIT-009
transaction core, the bundle packer with draft publication on a tag, and the QEMU update scenario.

## Read first

- [ADR 0031](../../adr/0031-update-channel-on-github-releases.md) for the GitHub Releases channel,
  the tag scheme, the asset names, the payload digest answer, and the deferred items.
- [ADR 0032](../../adr/0032-continuous-integration-and-build-pipeline.md) for the CI phases, the
  SHA-pinning rule, the CI-only test key, and the QEMU limit.
- [ADR 0008](../../adr/0008-updates-rollback-and-reset.md) for the two update loops and the
  transaction discipline; [ADR 0012](../../adr/0012-testing-and-release-gates.md) for the release
  gates; [ADR 0029](../../adr/0029-release-signing-and-key-custody.md) for the offline signature and
  trust anchor; [ADR 0030](../../adr/0030-ab-from-the-first-image.md) for the slot boundary.
- [BIT-009](../../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback)
  and [BIT-042](../../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline), then the
  [release work order](../implementation-plan.md#release-work-order-2026-11-22).
- [BIT-009 update recon](../../notes/2026-11-21-bit009-update-recon.md) for the existing apply
  transaction, the gaps, and the failure cases.
- [Release signing tool](../../notes/2026-11-21-release-signing-tool.md) for the existing
  `manifest.json` and `manifest.json.sig` flow.
- [Channel, CI, and publication note](../../notes/2026-11-22-channel-ci-and-publication.md) for the
  engine invariants and the repository publication record.
- [Architecture](../../architecture.md#update-flow), [status contract](../../status.md), and
  [defaults](../../../defaults/README.md).

## Scope

1. **CI phase one.** One GitHub Actions workflow on push and pull request. It runs `make check` and
   builds the app bundle as an unsigned workflow artifact. No secrets, no publishing, no write
   permission. Pin every action by full commit SHA and record the pins in the review.
2. **BIT-009 transaction core.** The signed update transaction with preflight, fetch, verify,
   backup, stop, activate, migrate, start, health, commit or roll back. The platform generation is
   supplied by a provider. The transaction writes only under `/var/lib/bittern` and never assumes a
   writable root or slot knowledge. The status contract records the base fingerprint. The test-key
   path is enough for this step.
3. **Bundle packer with publish on tag.** Build the files-only payload and the canonical manifest,
   create the draft release, upload `manifest.json`, `manifest.json.sig`, and `bittern-app.tar.gz`,
   and publish the draft as the visible flip. CI test bundles use the CI-only key. A production
   release still uses the offline signature from ADR 0029.
4. **QEMU update scenario.** Exercise a valid update, an invalid signature, a digest mismatch, a
   downgrade, and a rollback on the local or bench QEMU path.
5. **Documentation.** Keep the plan, ADRs, runbook, status contract, and defaults aligned with the
   behavior in the same change.

## Non-goals

- No release private key in CI, no release trust anchor in CI, and no automated release signing.
- No QEMU scenario in CI. The hosted-runner KVM question stays open until it is measured.
- No OS image build, no image asset hosting, and no A/B mechanism decision. ADR 0030 and OQ-04 remain
  open.
- No schema change and no migration. The channel set stays `stable` and `beta`, and beta is the only
  live channel until the first field release.
- No security channel unless ADR 0021 is accepted.
- No change to the Frigate image or its configuration.

## Acceptance criteria

Each bullet is a test, not a claim.

- Phase one runs on push and pull request, and a failing check names the same file and check a local
  `make check` names.
- The phase one bundle artifact is unsigned, labelled as a test artifact, and not published.
- Every workflow action is pinned by a 40-character commit SHA, and phase one has no secret and no
  write permission.
- A tag in the ADR 0031 scheme creates a draft release with exactly the three named assets, and
  publishing the draft makes them visible together.
- A manifest signed only by the CI test key is rejected by a release trust store. A production
  release still verifies with the offline key.
- The transaction writes only under `/var/lib/bittern`, records the base fingerprint, and passes the
  QEMU update, invalid-signature, digest, downgrade, and rollback cases.
- `make check` passes, and the QEMU update scenario passes on the local or bench path.
- No secret, key, camera credential, or unredacted config appears in an artifact, log, or release.

## Constraints

- GitHub Actions is the CI host. Do not add a second CI account or a release server.
- Release signing stays offline and airgapped. The CI key is a development key for bench devices only.
- The tag is a discovery input, not a trust input. The signed manifest remains authoritative.
- The payload is a files-only archive; `payload.sha256` is its digest exactly as distributed, and
  the manifest lists per-file digests.
- Do not publish a release to the stable namespace until the first field release is declared.
- QEMU and hardware tests stay on the local or bench path.
- No new dependency without an ADR. No workflow change that needs a secret without a recorded scope.

## Deliverable

Branch `p0/release-channel-engine`. Commits in the form `ci: ...`, `update: ...`, `release: ...`, or
`docs: ...`. One review report at the end listing the commands run and their output. No push.

## Rules

- Do not edit Accepted ADRs. Write a new ADR for a changed decision.
- Stop and ask if a ticket needs an open question answered. OQ-12 and OQ-35 stay open.
- Keep the work order: CI phase one, then the BIT-009 transaction core, then the bundle packer with
  publish on tag, then the QEMU update scenario.
- Leave the repository in a state where `make check` and the existing local checks pass.
