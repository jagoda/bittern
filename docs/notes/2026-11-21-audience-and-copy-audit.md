# Audience and copy audit

- Date: 2026-11-21
- Status: open. Recorded while defining the audience and the copy rules. No `ui/`, handler, or test
  change is included.
- Applies to: every owner-facing string in `ui/templates/` and `agent/internal/ui/`, the console
  text the first-run flow depends on, and the QEMU assertions that match that text.
- Related: [audience](../design/audience.md), [copy](../design/copy.md),
  [copy inventory](../design/copy-inventory.md), [patterns](../design/patterns.md),
  [BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy),
  [EN-2](../principles.md#en-2-tests-assert-structure-and-values-not-user-facing-copy).

The audience and the copy rules are now documents: the NVR installer and small-site administrator
in [audience.md](../design/audience.md), the glossary and sentence rules in
[copy.md](../design/copy.md), and the work list in
[copy-inventory.md](../design/copy-inventory.md). The rules are cross-pattern rules C1 to C8 in
[patterns.md](../design/patterns.md), and they are recorded as a pattern-class change in
[the design changelog](../design/CHANGELOG.md#v07-2026-11-21).

The inventory is 133 rows. It covers every template and the handler messages that reach the owner.
The five worst current strings, in order of owner consequence:

1. `ui/templates/setup.html:3` asks the owner to run `sudo bittern setup-token --qr` on the console
   to read the setup code.
2. `ui/templates/review.html:53` says "validate, render, and write this configuration" and names the
   appliance agent.
3. `agent/internal/ui/cameras.go:167` tells the owner to edit `/etc/bittern/secrets.yaml` by hand.
4. `ui/templates/done.html:11` reports "Frigate returned a picture from camera_1 (image/jpeg, 48213
   bytes)".
5. `ui/templates/status_panel.html:124` prints "dial tcp 127.0.0.1:5000: connect: connection
   refused" as the status reason.

## Findings

All findings are open. They need a product or ticket decision; a copy change cannot make the
sentence true on its own. The full evidence is in
[the inventory](../design/copy-inventory.md#findings).

| Finding | Evidence | Impact | Destination |
| --- | --- | --- | --- |
| [Major] No screen changes the house network or the camera network after setup. The review shows both as read-only, and the apply request always writes DHCP. | `agent/internal/ui/wizard.go:419`; `ui/templates/review.html:19-22` | The owner cannot correct a wrong network without a console, and the copy cannot offer a setting that does not exist. | Product scope; ask before building a settings surface. |
| [Major] An orphaned camera sign-in has no automatic cleanup. The failure page names a YAML file and asks for a hand edit. | `agent/internal/ui/cameras.go:165-167` | The owner leaves a live credential on the appliance, or edits a file they were never shown. | A retry or cleanup action. |
| [Major] Showing the setup code again needs a shell command. | `ui/templates/setup.html:3`; `agent/internal/ui/auth.go:193` | The primary path needs a terminal, against the first ten minutes in the audience doc. | The console keeps showing the code, or the UI can re-issue it. Until then the command stays in technical details. |
| [Minor] [patterns.md](../design/patterns.md) and [components.md](../design/components.md) say the required stream port and path sit inside "Advanced settings". The current form keeps them outside the disclosure. | `ui/templates/camera.html:98-140`; the edit-form section in patterns.md; the fieldset and retire-list entries in components.md | The rewrite could move required fields back into a disclosure to satisfy a stale claim. | Correct the three claims with the rewrite. |
| [Minor] The done screen sends the owner back to setup after apply. | `ui/templates/done.html:26,31` | The owner replays the first-run flow to edit a camera. | [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer). |
| [Minor] The firstboot QEMU storage assertion already matches prose that no longer exists. | `tests/qemu/run.py:2933` against `agent/internal/ui/checks.go:75`, changed in `2c5e484` | The firstboot scenario cannot pass on the current tree, and the failure looks like a product regression. | [BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy). |
| [Minor] Hiding the media path removes the only identifier of which drive is in use. | `ui/templates/status_panel.html:139`; `agent/internal/status/status.go:156-166` (`path` is the only drive identifier) | The owner cannot tell which disk is recording, and a replacement drive cannot be confirmed. | A drive label in the status contract, or keep the path visible. |

## Test coupling

The rewrite is blocked on
[BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy).
The adoption and firstboot scenarios assert 27 distinct prose strings across 23 assertion sites;
the list is in [the inventory](../design/copy-inventory.md#test-coupling). Do not edit those
assertions in the copy change. Land the stable ids and the value lookups first, then the rewrite,
so a copy edit cannot fail the suite.

The coupling is not hypothetical. Commit `2c5e484` changed the storage sentence from "The recording
drive is present" to "The recording drive is mounted", and the firstboot assertion still looks for
the old string. Both alternatives at `tests/qemu/run.py:2933` are absent from the current templates
and handlers.

## Next step

1. Land BIT-035 and confirm the adoption and firstboot scenarios fail on a missing selector or
   value, not on a sentence.
2. Rewrite the 133 rows as one change, in screen order, with the disclosures and the technical
   details blocks that the rows name.
3. Correct the three stale claims in patterns.md and components.md in the same change.
4. Add the drive label or keep the path visible, once the status contract owner decides.
5. Run `make link-check`, `make style-check`, and `make check`, and read the changed screens at 360,
   390, 1280, and 1920 before review.
