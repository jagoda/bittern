# Engineering

- Date: 2026-11-18
- Status: active
- Process: [findings and principles](../notes/2026-11-15-findings-and-principles-process.md)
- Rules: [principles](../principles.md)
- Registers: [defect classes](defect-classes.md), [field parity](field-parity.md)

Engineering quality in Bittern means a defect is closed when its class is named and a check would
catch the next instance, not when the reported instance is fixed. The network path produced seven
notes and fixes in one day on 2026-11-15, and two release-gate failures then escaped a green QEMU
suite because the suite started from an environment the field did not have. Both are the same
failure of the process: the instance was recorded and fixed, but nothing asked what made the next
instance possible.

This directory holds the engineering side of the
[findings and principles loop](../notes/2026-11-15-findings-and-principles-process.md). It extends
that loop with a class register, a field-parity table, and the checks those two produce. It is not a
second tracker and it does not replace a dated note, a ticket, an ADR, or an open question.

Two rules do the work:

- **A defect class seen twice triggers a class audit, not another instance fix.** The second
  occurrence is evidence about the mechanism, not only about the second unit, template, or script.
  Stop, audit the class, and record it in [defect classes](defect-classes.md).
- **A rule violated twice becomes an automated check.** If a principle is broken again after it was
  recorded, the missing artifact is the check. Add the smallest check that fails on the real defect
  and runs in a gate, or record the manual step and the ticket that will automate it.

## How a defect becomes a rule

1. **Capture the instance.** Record it in a dated note under [docs/notes](../notes/) with the
   evidence, the field environment, and the destination that owns the fix. This is the existing
   loop, unchanged.
2. **Count the class.** Ask whether the same mechanism appears in another note, ticket, run, or
   surface. A second instance, or one severe instance whose review shows the mechanism generalizes,
   opens a class audit.
3. **Audit the class.** Name the mechanism without product nouns, list every instance, and check
   whether each fix changed the mechanism or only the symptom. Record the result in
   [defect classes](defect-classes.md) with the detection and enforcement that should catch the next
   one.
4. **Check the baseline.** Compare the test that validated the fix with the environment that produced
   the failure in [field parity](field-parity.md). A green test on a more favourable baseline is a
   finding, not evidence.
5. **Record the rule.** If the class generalizes, add or update a principle in
   [principles](../principles.md) with the evidence, a do and don't, and the check. Do not add a near
   duplicate; revise the existing principle instead.
6. **Add the check.** Put the smallest reliable check in the gate that owns the boundary. Use
   `make check` when the check is static and offline, the QEMU harness when the defect only appears
   after a boot, and HIL when it only appears on hardware.
7. **Review at the next event.** Review the register and the principles after a run, feature
   delivery, or incident, not on a calendar. An `open` or `mitigated` row is a visible debt, not a
   closed subject.

## How a rule becomes an automated check

- **Assert the property, not the prose.** Check the unit state, the rendered value, the file mode, or
  the status field. Do not search a page for a sentence that a template can rewrite. This is
  [EN-2](../principles.md#en-2-tests-assert-structure-and-values-not-user-facing-copy).
- **Fail closed.** A parse that finds no source list, a missing fixture, or an absent constant is an
  error. A check that passes because it could not read its input is worse than no check.
- **Name the missing thing.** The failure says which unit, file, route, or value is missing and
  which source expected it. A check that only prints `failed` sends the next person back to the
  transcript.
- **Keep the check narrow.** One boundary, one property, one failure message. The unit-enablement
  check compares the units the payload starts, enables, or renders with the harness reboot
  assertion; it does not try to prove systemd works.
- **Prefer offline.** A static or fixture check in `make check` runs on every change. A QEMU or
  hardware check is a release gate and runs when the release is tested. Both are valid; the offline
  one is cheaper and catches drift earlier.
- **No brittle grep.** If a static check cannot distinguish the real defect from a legitimate
  variant, do not add it. Record the manual step in the register with why, the owner, and the
  ticket that will automate it. A manual step is debt, not an exemption.

## The artifact set

| Artifact | Question it answers | Home |
| --- | --- | --- |
| Dated note | What happened, with what evidence, in which environment | [docs/notes](../notes/) |
| Ticket | Who fixes the instance, and what proves the fix | [implementation plan](../plan/implementation-plan.md) |
| Principle | What is the rule, and what is the do and don't | [principles](../principles.md) |
| Class register | What class is this, how did it reach the field, what catches the next one | [defect classes](defect-classes.md) |
| Field-parity table | What does each environment provide before a test starts | [field parity](field-parity.md) |
| Automated check | What fails the next instance without a human remembering | `make check` or the release gate |

The note is the evidence. The ticket owns the fix. The principle states the rule. The register owns
the class. The parity table owns the baseline. The check owns the future.

## Boundaries

- This directory is not a tracker. A class row links the note and ticket that own the work; it does
  not open a second queue.
- A class that needs a decision is an [ADR](../adr/README.md). A class that is undecided is an
  [open question](../open-questions.md). Neither is settled by a row here.
- UX, operations, security, and research keep their principles in
  [principles](../principles.md). Their defect classes are still registered here when the mechanism
  is shared.
- A check that needs a new CI workflow, a new dependency, or a network service needs the decision
  first. The repository's `make check` stays offline.
- A manual step in a register is named with the ticket that removes it, in line with the automation
  rule in [AGENTS.md](../../AGENTS.md).
