# Session brief: P0 hardware reconnaissance

- Status: proposed; not started
- Tickets: none; feeds OQ-09 and OQ-10
- Branch: `recon/hardware`
- Plan: [implementation plan](../implementation-plan.md)

## Goal

Turn the hardware unknowns into a testable lab plan and a candidate bill of materials, so a later
session can buy one reference unit and start hardware-in-the-loop work without guessing.

## Read first

- [ADR 0005](../../adr/0005-dual-nic-isolated-camera-network.md) for the dual-NIC requirement and
  [ADR 0007](../../adr/0007-mkosi-os-image-and-releases.md) for the image and hardware assumptions.
- [ADR 0014](../../adr/0014-dedicated-recording-drive.md) for the required dedicated recording
  drive, and [OQ-19](../../open-questions.md) for recording-drive redundancy.
- [Open questions](../../open-questions.md), entries OQ-09 and OQ-10.
- [Frigate assessment](../../research/frigate-assessment.md): hardware acceleration, Coral, storage
  guidance.

## Scope

- Candidate components: a mini-PC class N100 or N305 board, two NICs, a PoE switch with 802.3af/at
  and per-port power cycling, a required dedicated recording drive, a UPS, and an optional Coral
  accelerator.
- The recording drive is required, not optional, and is sized against a stated retention target:
  days of continuous recording at a stated camera count, resolution, and bitrate. State the target
  before comparing capacities.
- v1 has one recording drive with no mirror and no network storage
  ([ADR 0014](../../adr/0014-dedicated-recording-drive.md)); redundancy is
  [OQ-19](../../open-questions.md) and is not part of this recon. The physical attachment, a second
  internal bay or an external USB enclosure, is part of OQ-09 and stays open.
- A lab checklist: what must be on the bench before the first spike.
- A spike plan for Debian with two NICs plus Frigate, without claiming results in advance.
- Measurements to capture: idle and load power, thermals, detect latency, recording throughput and
  drive health visibility, camera compatibility rows, PoE budget.
- The evidence that would close OQ-09 and OQ-10, stated before measuring.

## Non-goals

- No purchases and no vendor commitments.
- No code and no configuration changes in the repository.
- No benchmark claims without a measurement behind them.

## Method

1. Record the requirement each candidate must meet, taken from ADR 0005, ADR 0014, and the Frigate
   assessment.
2. Build a comparison table with price, NIC count, drive capacity and attachment options, power,
   thermals and availability.
3. Write the lab checklist and the spike plan as ordered steps with an expected observation per
   step.
4. Define the measurement table, including units, method and acceptable range.
5. State what evidence closes OQ-09 and OQ-10, and what stays open.

## Deliverable

A research note at `docs/research/YYYY-MM-DD-hardware-recon.md` linked from
[open questions](../../open-questions.md), plus a short note in `docs/notes/`. No device is bought
and no code is written. Status stays `proposed` until a priority decision is recorded.

## Rules

- Do not edit Accepted ADRs.
- Do not resolve OQ-09 or OQ-10 in this brief; collect evidence for the ADR that will.
- Date every measurement and name the tool used.
