# 360px browser-check width

- Date: 2026-11-17
- Status: Measured and fixed; OQ-27 remains open
- Related: [ADR 0025](../adr/0025-browser-check-for-the-responsive-ui.md),
  [BIT-020](../plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui),
  [OQ-27](../open-questions.md), [tests/browser](../../tests/browser/README.md)

## What changed

`tests/browser/check.mjs` now includes 360 by 800 beside the existing 390 by 844 and 1280 by 800
viewports. The route manifest is unchanged at 17 screens from 13 routes, so a run checks 51
route-width screens and writes 51 screenshots.

## Result

Before the new width, the check passed 34 screens with 0 failures and 114 warnings, 57 per width.
The first 360 run checked 51 screens and found 5 failures at 360, all on the status routes. The
three action links wrapped inside one inline link box, so the measured box for "Back to setup"
spanned two lines and its centre fell in the gap between them, where the parent paragraph covered
it. The other widths were unchanged.

`ui/static/bittern.css` now lays the status action row out as a wrapping flex row, with
`ui/templates/status.html` supplying the class. The fix keeps the 24 pixel floor and the 44 pixel
warnings unchanged, does not clip or hide content, and is not a width-specific rule. The re-run
passed 51 screens with 0 failures and 171 warnings, 57 per width.

## Next step

Use this as evidence for [OQ-27](../open-questions.md), which still has to decide whether 360 is
only the design floor or also the acceptance width. Keep the warning list as the evidence for
[OQ-26](../open-questions.md), and do not commit screenshots while [OQ-28](../open-questions.md) is
open.
