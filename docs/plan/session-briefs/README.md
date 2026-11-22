# Session briefs

A session brief is a one-page work order for a fresh agent: one branch, one deliverable, and enough
context to start without reading the whole repository. A brief points at the ADRs and the
[implementation plan](../implementation-plan.md); it does not restate or replace them.

## Briefs

| Brief | Tickets | Branch | Status |
| --- | --- | --- | --- |
| [P0 config compiler](p0-config-compiler.md) | BIT-001, BIT-002, BIT-003 | `p0/config-compiler` | done |
| [P0 runtime harness](p0-runtime-harness.md) | BIT-004, BIT-005 | `p0/runtime-harness` | done |
| [P0 hardware reconnaissance](p0-hardware-recon.md) | OQ-09, OQ-10 (no ticket) | `recon/hardware` | not started |

Status values: `done` means the plan records the work as complete. `in progress` means work has
started and the plan still lists it as unfinished. `not started` means no work has started. A brief
that is still `proposed` has no priority decision; it remains `not started` until one is recorded.

Evidence for the statuses:

- **P0 config compiler: done.** The [current state](../implementation-plan.md#current-state) records
  schema version 2 with its migration and the Go renderer with golden tests. The
  [plan truth pass](../../notes/2026-11-16-plan-truth-pass.md#what-retires-it) states that BIT-002
  and BIT-003 are done.
- **P0 runtime harness: done.** The [current state](../implementation-plan.md#current-state) records
  the BIT-004 QEMU harness and the BIT-005 Frigate fixture bundle, and neither appears in its
  Missing list. The fixture remains a fixture, not the product bundle.
- **P0 hardware reconnaissance: not started.** The brief is still `proposed` and has no priority
  decision, and OQ-09 remains open in the [open questions](../../open-questions.md). The
  [BOM note](../../research/bom-2026-11-14.md) is research input, not this brief's deliverable.

## Next work

The near-term queue now starts at item 4, BIT-009 app bundle update transaction with rollback.
Level 2 passed on 2026-11-21 with summary evidence
([result note](../../notes/2026-11-21-level2-result.md)); BIT-019 (setup token QR) and BIT-020
(responsive and mobile-friendly UI) landed on 2026-11-17. The 2026-11-17 Level 2 run failed its
reboot checks on `systemd-networkd` and `bittern-frigate`, both fixes are in the payload, and the
gate has since passed. No brief in this directory covers item 4; use the
[near-term queue](../implementation-plan.md#near-term-queue) and the
[suggested next-session prompts](../implementation-plan.md#suggested-next-session-prompts) for the
work orders. The hardware reconnaissance brief is not the Level 2 gate: it stays proposed and feeds
OQ-09 and OQ-10.

The completed P0 briefs stay listed as records. They are not open work orders.

## Rules

- A brief never overrides an Accepted ADR. If execution needs a different decision, write an ADR
  first and update [open questions](../../open-questions.md).
- One brief, one branch, one deliverable. Do not fold unrelated cleanup into a brief.
- Stop and ask when a ticket needs an open question answered; do not pick an answer silently.
- Commit on the brief's branch, do not push, and end with a short review report that lists the
  commands run and their result.
- Leave the repository in a state where the checks that exist still pass.

Read the [vision](../../vision.md), the [architecture](../../architecture.md), and the reading list
in the brief before starting. Decisions already taken are in the [ADR index](../../adr/README.md).
