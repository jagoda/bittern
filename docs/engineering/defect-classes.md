# Defect classes

- Date: 2026-11-18
- Status: active register
- Process: [engineering](README.md), [principles](../principles.md),
  [findings and principles](../notes/2026-11-15-findings-and-principles-process.md)
- Related: [field parity](field-parity.md), [GK41 hardware run](../notes/2026-11-15-gk41-hardware-run.md),
  [networkd after reboot](../notes/2026-11-18-networkd-disabled-after-reboot.md),
  [Frigate after reboot](../notes/2026-11-18-frigate-not-after-reboot.md),
  [QEMU adoption fixture](../notes/2026-11-18-qemu-adoption-fixture.md)

A class is a mechanism that can produce more than one defect. An instance is one note, ticket, run,
or surface. This register records one row per class: the mechanism, how it reached the field, what
detects it now, what should catch the next one, and whether that enforcement exists. The note and the
ticket remain the source of truth for each instance; this file is the class view, not a second
tracker.

Two rules produce the register:

- A defect class seen twice triggers a class audit, not another instance fix
  ([EN-4](../principles.md#en-4-the-second-occurrence-of-a-defect-class-triggers-an-audit-of-the-class-not-another-instance-fix)).
- A rule violated twice becomes an automated check. If no reliable automatic check exists, the row
  names the manual step, why, and the ticket that will automate it.

## Register

| Class | Root cause | How it reached the field | How it is detected now | Enforcement that should catch it | Status |
| --- | --- | --- | --- | --- | --- |
| Start without enable | The payload started or restarted a unit for the current boot and no stage owned static enablement. Apply only started or restarted, so a clean shutdown removed the container and the next boot had nothing enabled to restore it | Level 2 on a fresh Debian install rebooted into networkd `disabled`, no `lan0` or `cam0`, and no DNS; after the camera apply, a reboot left `bittern-frigate.service` `disabled` and `inactive` with no container ([networkd note](../notes/2026-11-18-networkd-disabled-after-reboot.md), [Frigate note](../notes/2026-11-18-frigate-not-after-reboot.md)) | The installer enables every unit it installs and verifies `systemctl is-enabled`; apply enables every rendered unit and verifies it; the QEMU scenarios disable the baseline and assert every unit enabled after install and after a post-apply reboot with no harness start ([install.sh](../../packaging/install.sh), [apply.go](../../agent/internal/apply/apply.go), [run.py](../../tests/qemu/run.py)) | [`unit_enablement.py`](../../tests/checks/unit_enablement.py) in `make check` compares the installer and rendered unit lists with the QEMU reboot assertions and fails naming the unit; the QEMU post-apply reboot is the live gate | checked |
| Transaction dependency order | `apply` restarted a unit that reads an interface or a mount before the step that creates it. The plan wrote the files and waited for the udev names, but restarted `dnsmasq.service` and `chrony.service` before `systemd-networkd.service`, the only unit that loads the rendered `.network` files; Debian 13's dnsmasq unit also requires `network-online.target` | Level 2 on 2026-11-21 failed first boot at `restart:dnsmasq.service` and rolled back to generation 0 while `cam0` existed but was down and dnsmasq had logged `interface cam0 does not currently exist`. A manual retry after connecting the camera NIC committed generation 2 ([dnsmasq first-boot note](../notes/2026-11-21-dnsmasq-firstboot-failure.md)) | The plan orders networkd first and inserts a bounded `wait:cam0-address` gate before the units that bind `cam0`, for an apply and for a rollback, and a Go test asserts the order ([apply.go](../../agent/internal/apply/apply.go)); the QEMU late-camera scenario asserts the journal order with `cam0` absent at apply time ([run.py](../../tests/qemu/run.py)) | `TestFirstApplyRestartsNetworkdBeforeCam0UnitsAndWaits`, `TestRollbackRestartsNetworkdBeforeCam0UnitsAndWaits`, `TestFirstBootWaitsForCam0BeforeRestartingCam0Units` and `make qemu-firstboot-late-camera`. A unit that newly binds another interface or the media mount is not detected by the check until `cam0BoundUnit` or the wait target learns about it | mitigated |
| Test baseline mismatch | A scenario inherited or created a more favourable state than the field: an enabled unit, a package installed through recommendations, an image cached, or a harness start in place of a reboot. The assertion then proved the harness, not the payload | Both reboot failures above escaped a green QEMU suite. The networkd assertion was satisfied by a harness `systemctl enable`, and the Frigate scenario restarted the unit instead of rebooting. The QEMU baseline also had `docker-cli` from recommendations while the GK41 had no `/usr/bin/docker` ([networkd note](../notes/2026-11-18-networkd-disabled-after-reboot.md), [Frigate note](../notes/2026-11-18-frigate-not-after-reboot.md), [compose note](../notes/2026-11-15-compose-standalone.md)) | The harness disables every baseline unit, removes `docker-cli` and `systemd-resolved`, reboots after apply without enabling or starting anything, and has a clean-network scenario and a 64G fixture disk that meets the configured storage reserve ([run.py](../../tests/qemu/run.py), [harness README](../../tests/qemu/README.md#first-boot-scenario)) | [EN-3](../principles.md#en-3-a-regression-test-starts-from-the-state-that-failed-in-the-field-and-says-so) and the [field-parity table](field-parity.md): every new or changed scenario declares its baseline row and every difference from the field case; review rejects an undeclared favourable baseline | mitigated |
| Assertions coupled to rendered copy | A test searched for a label-plus-value sentence rendered by a template, so an editorial copy change failed the harness even though the fixture and the product were correct | Two QEMU adoption failures from the single editorial commit `cfe24d2`: `Model Virtual Camera` and `Stored in /etc/bittern/secrets.yaml` were template prose, not product contracts. Each failure stops the release gate and invites weakening the assertion ([QEMU adoption fixture](../notes/2026-11-18-qemu-adoption-fixture.md)) | The assertion failure itself, and the review rule [EN-2](../principles.md#en-2-tests-assert-structure-and-values-not-user-facing-copy) | [BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy) acceptance: stable ids or `data-` attributes and value assertions, proven by a copy-only template edit that leaves the adoption and firstboot scenarios green without a harness edit | open |
| Manual bridge that should be a script | A repeated operator step was written in a runbook instead of a checked-in idempotent script, so each run repeated a manual sequence and the result depended on the operator | The GK41 manual bridges table records repeated cleanup, conffile restore, network and DNS steps; the install network bootstrap and `cleanup.sh` replaced them, and the recording-drive preparation remains the named BIT-012 exception ([GK41 hardware run](../notes/2026-11-15-gk41-hardware-run.md#manual-bridges), [install network bootstrap](../notes/2026-11-15-install-network-bootstrap.md), [runbook](../runbooks/gk41-clean-install.md#the-two-levels)) | The runbook states the remaining exception, and the cleanup and install scripts carry the steps that were removed ([cleanup.sh](../../packaging/cleanup.sh), [install.sh](../../packaging/install.sh)) | [OP-1](../principles.md#op-1-a-repeated-manual-bridge-becomes-an-idempotent-script), [`test_cleanup.py`](../../tests/cleanup/test_cleanup.py), and the QEMU firstboot and clean-network scenarios. [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation) owns the remaining manual storage step | mitigated |
| Generated artifact drifting from its source | A committed artifact is generated from a source, then edited by hand or not regenerated after the source changes; the consumer tests or ships the stale copy | Repository instances: the route table in `ui/README.md` and `tests/browser/routes.json` are both derived from `uiRoutes`, so a stale copy makes the browser check cover the wrong routes and the README describe the wrong surface. The embedded templates, defaults, schema and UI assets are another copy that a binary can serve stale ([ui README](../../ui/README.md), [routes.json](../../tests/browser/routes.json), [Makefile](../../Makefile)) | The generated artifacts have drift tests: `TestUIRouteTableREADME`, `TestBrowserRouteManifest`, the golden render suite, and the asset tests ([routes_readme_test.go](../../agent/internal/ui/routes_readme_test.go), [ui_browser_manifest_test.go](../../agent/internal/ui/ui_browser_manifest_test.go)) | Each drift test regenerates or compares the artifact and fails with the command to refresh it, in `make check`. A new generated artifact needs the same test in the same change; a manual regeneration command is not enforcement | checked |

## Status definitions

| Status | Meaning |
| --- | --- |
| `open` | No check catches the class. The known instances may be fixed, but the next one would not be caught |
| `mitigated` | A check or manual step covers some instances or axes, and the row names the gap and its owner |
| `checked` | An automated check in `make check` or a release gate fails on the class, and the row names it |

A row does not become `checked` because its instances are fixed. It becomes `checked` when the
mechanism has a gate that fails without a human remembering it.

## How to add a class

### Evidence threshold

Add a row when the same mechanism appears in two independent instances from separate notes,
tickets, runs, or surfaces, or when one severe instance and its review show the mechanism generalizes
beyond that instance. The row states which case it is and links every instance it counts.

Two local fixes with no shared mechanism are not a class. Record them as bug fixes with their
regression tests ([EN-1](../principles.md#en-1-a-bug-fix-carries-the-regression-test-that-reproduces-it)).
A class that rests on a decision is an [ADR](../adr/README.md); a class whose answer is not settled is
an [open question](../open-questions.md), not a row that quietly chooses.

### Who decides

The maintainer decides at the next review after a run, feature delivery, or incident, on a proposal
from the engineer or agent that found the instance. The proposer drafts the row, the instance list,
and the enforcement; the maintainer accepts it, sends it to a ticket, or sends it to the ADR or open
question process.

Adding a row is a documentation change in the same commit as its check, or as the named manual step
and the ticket that will automate it. It does not open a separate queue and does not reorder the
implementation plan.

### What the audit looks like

1. Name the mechanism in one sentence without product nouns, so the next instance is recognisable
   even when it is in a different component.
2. List every instance with a link and its status: fixed, open, or documented no action. Check that
   the second instance is genuinely the same mechanism and not a coincidence.
3. For each fix, ask whether it changed the mechanism or only the symptom. A fix that adds a third
   `enable`, sleep, retry, or manual step without changing the owner is an instance fix.
4. Check the detection. Would the current suite fail if the instance recurred today? Name the exact
   assertion, not the scenario. The two reboot failures escaped a scenario that was green, so "the
   QEMU test covers it" is not enough.
5. Choose the enforcement. Take the smallest check that fails on the real defect: a static check in
   `make check`, a QEMU assertion after the boot, or a HIL check when only hardware shows it. If no
   reliable check exists, record the manual step, why a check would cry wolf, the owner, and the
   ticket that removes it.
6. Record the status. `open` if there is no enforcement, `mitigated` if it is partial or manual,
   `checked` if an automated gate fails on the class.
7. If the mechanism generalizes across the project, add or revise a principle with the same evidence
   and check. If the class changes a decision, write an ADR that supersedes the old one.
8. Re-run the audit at the next instance or review. Do not close a row because time passed or because
   the last incident is old.

## Current open and mitigated rows

The rows that are not `checked` are the next audit targets:

- **Test baseline mismatch** is mitigated on the known axes (units, packages, reboot, network). A new
  scenario can still start from a favourable state that the table does not cover, so the review
  question stays with the field-parity table.
- **Assertions coupled to rendered copy** stays open until
  [BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy) lands
  and a copy-only change proves the harness no longer reads template prose.
- **Manual bridge that should be a script** is mitigated; the recording-drive preparation is the
  remaining named step, owned by [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation).
