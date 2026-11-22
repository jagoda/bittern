# Findings and principles process

- Date: 2026-11-15
- Status: active process for every finding from a run, review, incident, report, or research task
- Applies to: UX, bug, feature, research, hardware, security, and ops findings
- Related: [principles](../principles.md), [engineering quality](../engineering/README.md),
  [defect classes](../engineering/defect-classes.md), [field parity](../engineering/field-parity.md),
  [hardware ledger](2026-11-15-gk41-hardware-run.md),
  [camera wizard UX findings](2026-11-15-camera-wizard-ux-findings.md),
  [open questions](../open-questions.md), [research](../research/frigate-assessment.md),
  [ADR index](../adr/README.md), [dated notes](.), [AGENTS.md](../../AGENTS.md),
  [UI README](../../ui/README.md)

The project already records evidence in dated notes, tickets, open questions, ADRs, research docs,
and test results. The missing step is to review that evidence together, send each finding to the
right artifact, and distil repeated patterns into rules with a regression check. This process closes
that loop for every kind of finding, not only UX. The camera protocol path is proven while the owner
path still fails in predictable ways; the same pattern appears in hardware, security, research, and
operations work.

## The loop

1. **Capture the finding.** Every real hardware or owner run, UI or security review, incident,
   report, or research task produces a dated findings note in `docs/notes/` using
   `YYYY-MM-DD-topic.md`. Each finding has a type, evidence, impact, owner, destination, and status.
   Do not paste a transcript, a full log, or a secret. Link the smallest piece of evidence, such as a
   run step, a screen, a test, a source file, a measurement, or a dated upstream source.
2. **Type and triage it.** Use one of the types below. Send the finding to the artifact that owns it:
   a ticket for a fix, an open question for an undecided question, an ADR for a significant decision,
   a research note for dated external facts, or a documented no-action reason. Do not create a second
   tracker. The note, ticket, open question, ADR, or research note remains the source of truth.
3. **Carry the test or evidence with the fix or decision.** A bug fix carries the regression test that
   reproduces the failure. A UX fix carries the UI review checklist and the phone and desktop checks.
   A hardware fix carries the camera matrix row or HIL evidence. A research claim carries its dated
   source. A security fix carries the redaction or leak test. An ops fix carries the idempotent script
   and its runbook verification.
4. **Review for repeated patterns.** After each UX fix or run, feature delivery, hardware run, or
   incident, review the open and recently closed findings together. A pattern is recurring when the
   same defect appears in two or more notes, tickets, surfaces, runs, or incidents. A single severe
   finding can become a principle when the review shows the rule applies beyond that instance. The
   pattern is distilled into [principles](../principles.md), not left in a ticket comment or a run
   note, and its mechanism is recorded in the [defect-class
   register](../engineering/defect-classes.md) with the enforcement that should catch the next
   instance. A repeated class triggers an audit, not another instance fix
   ([EN-4](../principles.md#en-4-the-second-occurrence-of-a-defect-class-triggers-an-audit-of-the-class-not-another-instance-fix)).
5. **Record the principle.** Each principle states the rule, the evidence that produced it, a do and
   don't example, and the check or test that prevents regression. It is dated and linked to the
   findings note, ticket, open question, ADR, research note, or incident. Update an existing principle
   instead of adding a near duplicate. The sections are UX, engineering, operations, security, and
   research. A regression test names the environment it starts from in the [field-parity
   table](../engineering/field-parity.md), so a green test on a favourable baseline is not mistaken
   for evidence ([EN-3](../principles.md#en-3-a-regression-test-starts-from-the-state-that-failed-in-the-field-and-says-so)).
6. **Review with the maintainer.** Review the principles with the maintainer after a hardware run,
   feature delivery, or incident, not on a calendar. Confirm that each principle still has evidence
   and an enforcement check, merge duplicates, and mark a replaced principle as superseded with a
   link. A principle that changes an architectural decision becomes an ADR, not a silent edit.

## Finding shape

Use a short table or the same headings. One note can carry several findings, each with its own row.

| Field | Content |
| --- | --- |
| Type | `UX`, `bug`, `feature`, `research`, `hardware`, `security`, or `ops` |
| Finding | The observed defect, success, question, or fact, in one or two sentences |
| Evidence | The smallest link, screen, test, source reference, measurement, or dated source that proves it |
| Impact | Who is blocked, what they must know or do, and the severity |
| Owner | The person, role, component, or ticket owner responsible for the next action |
| Destination | A ticket, open question, ADR, research note, or `none` with the no-action reason |
| Status | `open`, `fixed <commit>`, `decided <ADR or OQ>`, `no action <reason>`, or `principle <ID>` |

The [hardware ledger](2026-11-15-gk41-hardware-run.md) and the
[camera wizard UX findings](2026-11-15-camera-wizard-ux-findings.md) are the first notes this process
draws on. They record real findings with their evidence and the artifacts that own the next action.

## Triage destinations

| Type | Destination | Evidence the destination carries |
| --- | --- | --- |
| UX | A ticket for the fix, or a documented no-action reason | UI checklist, phone and desktop checks, regression test |
| Bug | A ticket for the fix | The failing regression test, then the passing test with the fix |
| Feature | A ticket, or an open question or ADR when the need or decision is not settled | Acceptance test or review tied to the ticket |
| Research | A dated research note, or an open question or ADR when it decides something | Source, verification date, and the re-check before reuse |
| Hardware | A ticket, plus a camera matrix row or HIL test where the model or firmware matters | Model, firmware, probe evidence, test or HIL result |
| Security | A ticket or ADR, or a no-action reason with the threat rationale | Redaction or leak test, security review, incident evidence |
| Ops | A ticket, plus the runbook and idempotent script update | Script test, clean-state or health verification, incident evidence |

A finding that needs an architectural change stops and uses the ADR process in
[docs/adr/README.md](../adr/README.md). A finding that is genuinely undecided goes to
[open questions](../open-questions.md), not into a principle or a silent default. A finding that
rests on external facts goes to the relevant [research doc](../research/frigate-assessment.md) with a
date and source.

## Principles and the UX checklist

The distilled rules live in [docs/principles.md](../principles.md), with sections for UX, engineering,
operations, security, and research. The UX section carries the
[UX review checklist](../principles.md#ux-review-checklist), which applies to every UI change. The
other sections carry the test, matrix, script, or dated-source check that fits their domain.

## Current artifacts

| Artifact | Role |
| --- | --- |
| [Hardware ledger](2026-11-15-gk41-hardware-run.md) | Hardware run, manual bridges, bug ledger, and open tickets |
| [Camera wizard UX findings](2026-11-15-camera-wizard-ux-findings.md) | Owner-path findings and the camera UX tickets |
| [Open questions](../open-questions.md) | Deliberately undecided questions and their options |
| [Research docs](../research/frigate-assessment.md) | Dated external facts, with the options and language studies beside it |
| [ADRs](../adr/README.md) | Accepted and proposed decisions, including supersession |
| [Dated notes](.) | Run, review, research, and incident evidence |
| [Engineering quality](../engineering/README.md) | The class register, field-parity table, and how a rule becomes a check |
| [Defect classes](../engineering/defect-classes.md) | One row per defect class, with its enforcement and status |
| [Field parity](../engineering/field-parity.md) | What each environment provides before a test starts |

## Boundaries

This process records, triages, and reviews evidence. It does not replace a ticket, an open question,
an ADR, a research note, or a runbook, and it does not reorder the near-term queue. A new ticket
follows the normal implementation plan review. A finding that contradicts an Accepted ADR stops and
uses the ADR process.
