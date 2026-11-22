# Touch-target floor and long-string wrapping

- Date: 2026-11-17
- Status: Browser check passes; the sub-44 warning list stays open for OQ-26
- Related: [ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md),
  [BIT-020](../plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui),
  [OQ-26](../open-questions.md), [OQ-28](../open-questions.md),
  [first browser-check run](2026-11-17-browser-check-first-run.md),
  [tests/browser](../../tests/browser/README.md)

## What changed

`ui/static/bittern.css` now defines a 24px hard floor and a 44px target. Inline links carry a
minimum 24px hit area through vertical padding, which leaves prose links in normal inline flow so
they still wrap. Header navigation links, the brand, the sign-out control and primary buttons reach
44px; the camera discovery label is at least 44px and its radio input is 24px. Long footer strings
such as the TLS certificate fingerprint wrap with `overflow-wrap: anywhere` instead of widening the
page at phone width. `ui/README.md` records the provisional floor and its source.

A Go test in `agent/internal/ui` guards the few stylesheet invariants the browser check depends on:
the fluid `.wrap` width, the footer long-string rule, the breakpoint below 44rem, the two
touch-target values, and that the base link rule stays wrappable. It is an assertion set, not a CSS
parser.

## Second browser-check run

`make ui-browser-check` at 390 by 844 and 1280 by 800 moved from 194 failures and 86 warnings to
0 failures and 114 warnings. The warning count rose because 84 former hard failures in body links
and 8 discovery radio inputs became warnings, while 64 warnings for the brand and sign-out cleared.
No page overflow, off-viewport control, overlap or focus failure remains.

The 114 warnings are the OQ-26 evidence and group as follows:

| Control | Size | Why it stays below 44 |
| --- | --- | --- |
| Inline links in paragraphs and state lists | 25 high | Enlarging a link inside a sentence would distort the text layout. |
| `Back to setup`, `Back to welcome`, `Back to status`, `Back to network`, `Back to storage`, `Back to cameras`, `Refresh now`, `Manage cameras`, `Open status`, `Edit camera`, `Remove`, `Cancel`, and the five status-state links | 25 to 31 high | Same inline and compact action-row reason. |
| Camera discovery radio inputs | 24 by 24 | The native input is 24px; the wrapping label is the larger tappable target, but the check measures the input box. |

The brand, sign-out, all primary buttons and every header or navigation link now meet 44 by 44.

## Next step

Use this warning list to answer [OQ-26](../open-questions.md). Do not raise the browser-check
thresholds to clear warnings, and do not commit screenshots while [OQ-28](../open-questions.md) is
open.
