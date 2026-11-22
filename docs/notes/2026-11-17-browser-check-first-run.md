# First browser-check run for the responsive UI

- Date: 2026-11-17
- Status: Harness landed, layout findings open
- Related: [ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md),
  [BIT-020](../plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui),
  [OQ-26](../open-questions.md), [OQ-27](../open-questions.md),
  [tests/browser](../../tests/browser/README.md)

## What changed

`tests/browser/` now holds the opt-in Playwright check. `make ui-browser-check` starts its own
`bitternd` on loopback with a temporary config, auth state, and state root, loads the responsive
routes at 390 by 844 and 1280 by 800, and writes screenshots to `build/ui-screenshots/`. The route
manifest is checked against `uiRoutes` by a Go test with the same `-update` convention as the other
generated UI artifacts.

The preview daemon gained `--ui-mock-apply`, which lets the mock wizard complete the apply page
without writing files. `--ui-mock` alone still keeps `StubApplier` and its "apply is not
implemented" page, so the done route has no other path in the preview build.

## First run

The first run covered 13 responsive routes and 17 screens per width, including all five status
states. It wrote 34 screenshots. It found no page-level horizontal overflow, no control outside the
viewport after scrolling, no overlapped control, and no unfocusable control.

It did fail the provisional 24 by 24 CSS pixel floor on the same controls across the routes:

| Finding | Approximate size | Where |
| --- | --- | --- |
| Header navigation links | 47 to 71 by 19 | Every page |
| Inline links in paragraphs and state links | 19 to 25 high | Status, wizard, camera, error pages |
| Camera discovery radio inputs | 13 high, label is wider | Wizard camera, camera edit |
| Brand link and sign-out button | 30 and 25 high | Warnings only, below 44 |

The size failures are the evidence [OQ-26](../open-questions.md) asked for. The layout work in
BIT-020 owns the fix; no new ticket is needed. The 19 pixel link height is a line-box measurement,
so padding or a larger line box on inline and navigation links is the likely change.

## Next step

Run the check again after the BIT-020 layout work lands. Keep the 390 width provisional until
[OQ-27](../open-questions.md) is answered, and do not commit screenshots while
[OQ-28](../open-questions.md) is open.
