# Signing and A/B proposals for BIT-009

- Date: 2026-11-21
- Status: proposal note. No code, keys, scripts, or accepted decisions.
- Related: [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [ADR 0030](../adr/0030-ab-from-the-first-image.md),
  [OQ-04](../open-questions.md), [OQ-05](../open-questions.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-032](../plan/implementation-plan.md#bit-032-mkosi-image-build-and-boot),
  [BIT-009 recon](2026-11-21-bit009-update-recon.md)

## What was proposed

[ADR 0029](../adr/0029-release-signing-and-key-custody.md) proposes Ed25519 detached signatures over
a canonical manifest, verified against a public key pinned in the image or a root-owned trust store.
It covers the manifest fields, the canonical form, the verification order, the rejection rules, a
dual-signed key rotation, and an ephemeral test key. The release key stays offline or hardware-backed
and is used manually per release.

[ADR 0030](../adr/0030-ab-from-the-first-image.md) proposes A/B from the first shipped image: two OS
slots and a shared data partition, with the app bundle on the shared partition and its own rollback.
It fixes only the slot boundary and leaves the mechanism, slot sizes, and bootloader to the image ADR.
It says plainly that BIT-009 still owns app rollback and must not write the bootloader or claim an OS
slot.

Both ADRs are Proposed. Neither is binding. ADR 0030 proposes the answer to OQ-05; OQ-04 stays open
for the image ADR.

## What remains for the maintainer

- Accept or reject ADR 0029. If accepted, confirm Ed25519, the in-tree verifier, the canonical form,
  the pinned trust store, and the dual-signed rotation rule. The custody choice (offline machine or
  hardware token) and the key-loss procedure are follow-ups, but they must exist before the first
  release.
- Accept or reject ADR 0030. If accepted, OQ-05 moves to resolved and OQ-04 remains open for the
  image ADR. Confirm the storage cost, the shared partition, and the OS generation compatibility
  rule.
- The two decisions are independent. Either can be accepted without the other. BIT-009's release path
  needs ADR 0029; its slot boundary follows ADR 0030, and the test-key slice needs neither.
- If ADR 0029 is rejected, the replacement must still satisfy the [ADR 0012](../adr/0012-testing-and-release-gates.md)
  release gate and the [ADR 0008](../adr/0008-updates-rollback-and-reset.md) signed-manifest
  requirement. If ADR 0030 is rejected, OQ-05 must be answered the other way before BIT-032 freezes
  the image layout.

## What can start now

BIT-009's test-key slice can start before either ADR is accepted: the manifest type, the
canonicalizer, the verifier, a signed local fixture with an ephemeral test key, the negative tests,
and the transaction shape that never writes a slot or the bootloader. It must not generate or commit
a release key, publish a channel, sign a release, or write a bootloader. The test-key slice is a test
artifact, not a release.

The plan prompt "Implement BIT-009. Decide no OQ" is safe only for that slice. The release path, the
channel publication, and the acceptance "unsigned or wrong-generation manifests are rejected" as a
shipped guarantee wait for ADR 0029, and the slot assumption waits for ADR 0030.

## Verification

`make link-check` and `make style-check` pass on 2026-11-21. No code, key, script, or release artifact
was produced.

## Next step

Review ADR 0029 and ADR 0030. If accepted, move OQ-05 to resolved, keep OQ-04 open for the image
ADR, and update BIT-032's slot scope. If rejected, record the reason in the ADRs and keep the
questions open.
