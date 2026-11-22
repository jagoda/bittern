# First-run and setup copy rewrite

- Date: 2026-11-21
- Status: setup, sign-in and wizard slice landed. The camera list, status, operation and shared
  error pages are separate slices and are unchanged here.
- Related: [copy](../design/copy.md), [copy inventory](../design/copy-inventory.md),
  [audience](../design/audience.md),
  [BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy),
  [QEMU copy assertions](2026-11-21-qemu-copy-assertions.md).

## What changed

The setup, welcome, network, recording drive, camera, review and done screens now use the glossary
terms from [copy.md](../design/copy.md). The setup code replaces the token and short code in primary
copy. The screen names recording drive, stream port, stream path, camera search, hardware address,
saved configuration and sign-in. Protocol, path, algorithm and component detail moved into named
disclosures or was deleted from the wizard where the status page already carries it.

The done title is conditional: all camera checks passed reads "Setup complete", and any failed
picture check reads "Setup finished with a problem". A failed camera check keeps the
configuration-applied sentence visible and puts the raw check result in "Technical details".

The setup and camera error messages in `auth.go` and `wizard.go`, the camera field validation, the
discovery failure sentence, the apply failure sentence and the session failure sentence were
rewritten with the same terms. No route, form field, validation rule or apply-path behaviour
changed.

## Deferred rows

- C-73 and C-74 in the [copy inventory](../design/copy-inventory.md) wait for
  [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer).
  The done page still offers "Back to setup" and the return-to-setup bullet. Removing them
  changes the flow, not only the copy.
- C-75 to C-81 belong to `snapshot.go`, outside this slice. Its visible strings already say
  "recording service".
- The discovery failure sentence no longer repeats the raw search error in the wizard. The error
  stays in the journal, the status document and the support bundle.

## Test coupling

No QEMU assertion changed. The wizard and adoption assertions already read the `data-*` hooks added
in `5066a34`; the remaining rows in
[the QEMU note](2026-11-21-qemu-copy-assertions.md#remaining-copy-coupled-assertions) are
camera-list, journal and installer prose outside this slice.

`make check`, `make ui-test`, `make design-check` and `make ui-browser-check` pass. The browser
check walked 92 of 92 route-width screens with 0 failures and 0 warnings.

## Residual finding

[Minor] Q4 at `/ui/wizard/network`, 360 by 800: the address cell wraps `192.168.1.50/24` and
`10.77.0.1/24` across two lines. The value is also the `data-network-*` hook the QEMU harness reads,
and the table has no no-wrap utility. This slice leaves the value and records the wrapping for the
table and status owner. No action in this change.

## Before and after

The camera step is the largest visual change: candidate hardware addresses and discovery sources
moved from the row into "Technical details", the row keeps the address, state and vendor or model,
and the search action is now "Search again". At 360 by 800 the page grew from 3322 to 3490 pixels
because each candidate gained a disclosure summary, while the primary row lost two internal values.
The setup page is the largest copy change and shrank from 1707 to 1655 pixels at 360 because the
technical paragraph moved behind one disclosure.
