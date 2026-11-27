# The NVR grid wall, per-pane quality and full screen land

- Date: 2026-11-27
- Scope: [ADR 0037](../adr/0037-nvr-grid-per-pane-quality-and-full-screen.md),
  [BIT-023](../plan/implementation-plan.md#bit-023-live-view), `ui/`, `tests/browser/`

## What changed

`/ui/live` is now the wall ADR 0037 describes: one, four, six, eight, nine or sixteen panes from
one area function, a hairline gap and no per-pane chrome, a per-pane main, sub and snapshot choice,
a class prior with a dropped-frame capacity signal, a 15 second snapshot cadence for panes beyond
capacity, full screen of the layout, and an overlay that hides after three seconds and is inert and
click-through while hidden. `/ui/live/{camera}` is the same wall with one pane and a link-based main
and sub choice.

The geometry lives in `liveAreasForCount`, the CSS projection is six `grid-template-areas` blocks
that `TestStylesheetMatchesTheAreaFunction` compares with that function, and the capacity state
machine is `ui/static/live-capacity.js`, driven from the wall every two seconds and exercised
directly by the browser suite.

## Findings from the run

Each finding names its evidence and where it was triaged. The browser run that produced them is
`make ui-browser-check`, and the images are in `build/ui-screenshots/`, which is gitignored.

1. **Empty panes read as a form grid.** At 360 and 1920, every empty pane carried a camera select
   and a `Choose camera` submit button, so a sixteen-pane wall with four cameras showed twelve
   selects. The ADR says an empty pane shows one control. Fix: with JavaScript the select is hidden
   and the button selects the pane and opens the camera picker in the overlay; without JavaScript
   the select and submit remain, because that is the working no-script path. Evidence:
   `live-wall-16-1920x1080.png` before and after.
2. **Empty panes were counted as live capacity.** The capacity view included panes with no player,
   so a six-pane wall on a phone counted six automatic panes and demoted two camera panes. An empty
   pane holds no media connection, so it is not capacity. Fix: only panes with a player enter the
   capacity view.
3. **Fractional pane widths defeated the reading-order tie-break.** In a nine-pane wall at 360 the
   columns measured 120.33 and 120.34 CSS pixels, so the "equal area" test never matched and the
   least-visible order demoted arbitrary camera panes instead of the last empty ones. Fix: areas
   within one percent are the same rendered size, and reading order decides.
4. **The overlay panel was a ragged flex row.** The camera picker wrapped under the quality control,
   an empty gap sat between the picker and the layout group, and the full screen control floated in
   the middle of the right edge. Fix: the panel is a three-column grid at 48rem, with the selected
   pane in the first column, the layout group in the second, the actions aligned bottom right, and
   the state and hint lines spanned across. Evidence: `live-wall-overlay-1280x800.png`.
5. **The focus ring was invisible on the pane.** `--color-focus` at `#0b5d4b` measured 2.20:1
   against the dark pane. ADR 0037 fixes the answer as a token change. Fix: `--color-focus` is
   `#1a7a5f`, 3.27:1 on the pane, 4.82:1 on the page and 5.26:1 on a surface, recorded in
   [tokens.md](../design/tokens.md) and [the changelog](../design/CHANGELOG.md). The name scrim adds
   `--color-scrim` and `--color-on-scrim` at 6.77:1 over the lightest video frame.
6. **Two harness artefacts, both fixed in the suite rather than the product.** The overlay
   screenshot was taken during the 120 ms fade and showed the panel as translucent, and the
   "hidden" image was captured while focus still sat in the overlay, so it showed a focused control
   rather than the hidden state. The suite now waits for the settled state before each capture, and
   the phone overlay image was added to the four-width read.

One finding is recorded and not fixed. A pointer move reveals the overlay before a click, so a click
aimed at a pane under the panel lands on the panel. A tap on touch is unaffected, the panel is the
control surface, and it hides after three seconds. No action: the cost is one gesture, and moving
the panel to another edge only moves which panes it covers.

## Where the implementation reads the ADR slightly differently

- The selected pane's quality control without JavaScript is a radio group with one `Set quality`
  submit button, not three submit-transport buttons. It is a real form, it applies with a page
  load, and it works for any pane through the pane picker; the ADR's three-button shape would need
  three more buttons in a panel that already carries the layout and camera controls.
- The full screen control is a button rather than a link, because the Fullscreen API needs a click
  handler and no-JavaScript full screen is not possible. It is hidden in the no-script markup's
  effect by doing nothing, which is honest: the API is script-only.

## What the screenshots changed

The first read of the six layouts at four widths in normal and full screen produced findings 1 to 4,
the overlay images produced finding 4, and the four-width focus read produced finding 5. Geometry
assertions cannot see a select in an empty pane, a camera pane demoted where an empty one should
have been, or a ring that is the same colour as the picture.

## Unfinished and next

- BIT-027 collects real ONVIF profiles. Until then every camera is one stream, the quality control
  is absent, and the page says so.
- The measured capacity of the bench boxes and the dropped-frame behaviour on real hardware remain
  [OQ-44](../open-questions.md#oq-44) and [OQ-46](../open-questions.md#oq-46). The priors and
  thresholds are the ADR's planning numbers until that evidence exists.
- The QEMU live phase still asserts the previous four-tile page and has not been moved to the wall
  and the second profile. It is the next concrete step before the wall is called field-ready.
