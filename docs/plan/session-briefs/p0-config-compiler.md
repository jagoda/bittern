# Session brief: P0 config compiler

- Status: done ([current state](../implementation-plan.md#current-state))
- Tickets: BIT-001, BIT-002, BIT-003
- Branch: `p0/config-compiler`
- Plan: [implementation plan](../implementation-plan.md)

## Goal

Produce a schema-valid `appliance.yaml` and a deterministic renderer that emits systemd-networkd,
nftables, dnsmasq and chrony configuration with offline validation, so the configuration pipeline
can be tested without hardware.

## Read first

- [Vision](../../vision.md) and [architecture](../../architecture.md), especially the three layers
  and the configuration flow.
- [ADR 0005](../../adr/0005-dual-nic-isolated-camera-network.md) for the network model,
  [ADR 0006](../../adr/0006-declarative-config-and-renderer.md) for the renderer contract,
  [ADR 0011](../../adr/0011-monorepo-layout-and-naming.md) for the repository layout,
  [ADR 0012](../../adr/0012-testing-and-release-gates.md) for the test layers, and
  [ADR 0014](../../adr/0014-dedicated-recording-drive.md) for the recording-drive separation and
  storage block.
- [Open questions](../../open-questions.md), entry OQ-03, which was open when this brief was written
  and is now resolved by [ADR 0019](../../adr/0019-ui-stack.md); it did not block this brief. OQ-02
  is resolved by [ADR 0015](../../adr/0015-agent-language.md); this brief does not reopen it.
- Component boundaries: [schema](../../../schema/README.md),
  [templates](../../../templates/README.md), [defaults](../../../defaults/README.md),
  [tests](../../../tests/README.md).
- The BIT-001, BIT-002 and BIT-003 entries in [the implementation plan](../implementation-plan.md).

## Scope

- Repository skeleton with a `Makefile` and local checks: link check, style check, `make check`.
- `appliance.yaml` schema v1, a four-camera example with a dedicated recording drive and a
  retention policy, and a validator.
- Renderer with golden fixtures for the four config families: networkd, nftables, dnsmasq, chrony.
- Validation hooks (`nft -c`, `dnsmasq --test`) and do-not-edit headers on every generated file.

## Non-goals

- No agent daemon, no HTTP API, no UI.
- No Docker image and no Frigate deployment.
- No live network changes on the development host.
- No update, rollback or reset machinery.

## Acceptance criteria

Each bullet is a test, not a claim.

- `make check` passes offline on a clean clone and names the failing check and file on failure.
- The four-camera example validates. Unknown keys and overlapping subnets are rejected with a path.
- A missing or unsupported `schema_version` fails with a path.
- Renderer output is byte-identical across runs and golden fixtures fail on drift.
- Bad pools or gateways are rejected before any file is written.
- No forwarding or WAN rules exist for `cam0`, and every generated file has a do-not-edit header.

## Constraints

- Checks run offline. Do not add a network dependency to `make check`.
- Keep the validator language-neutral where possible: JSON Schema is the contract, the CLI is the
  boundary. Do not reopen OQ-02; the agent language is Go per
  [ADR 0015](../../adr/0015-agent-language.md).
- Never apply generated configuration to the host. Render into a temporary directory.
- No secrets in fixtures or golden files.
- Schema v1 has no network-storage fields. The storage block names the recording drive by mount
  point or device role, plus retention and free-space reserve; it never contains a path on the OS
  drive ([ADR 0014](../../adr/0014-dedicated-recording-drive.md)).
- Every new schema field needs documentation and a failing-case test.

## Deliverable

Branch `p0/config-compiler`. Commits in the form `config: ...` or `schema: ...`. One review report
at the end listing the commands run and their output. No push.

## Rules

- Do not edit Accepted ADRs.
- Record a new decision as an ADR and update [open questions](../../open-questions.md).
- Stop and ask if a ticket needs an open question answered.
