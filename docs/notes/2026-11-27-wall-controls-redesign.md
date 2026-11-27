# Live wall controls act in place, per pane

- Date: 2026-11-27
- Scope: [ADR 0038](../adr/0038-live-wall-controls-act-in-place.md),
  [ADR 0037](../adr/0037-nvr-grid-per-pane-quality-and-full-screen.md), `ui/`, `tests/browser/`

## What changed

`/ui/live` keeps its six layouts, one area rule, hairline gap, capacity model and player isolation.
The controls now match how the wall is used on real hardware:

- A layout choice acts in place. The server renders sixteen slots, a choice shows and hides them,
  the visible players keep their islands, and the cookie is saved with an inline request.
- Each occupied pane owns its overlay with the current-stream line, the stream selector and the
  camera chooser. The wall overlay carries only the layout picker and full screen.
- The layout picker is a native radio group with one inline SVG per arrangement, an accessible name
  and a selected state that is not colour alone.
- An empty pane is a placeholder with one plus icon and `Choose camera`, not a form. Pressing it
  opens that pane's camera chooser and opens no media session.
- Full screen is requested directly from the click on the wall, refuses with a visible sentence,
  exits with Escape or a swipe down of at least 120 pixels, and has no exit button.
- The stream control is an explicit selector with the current stream printed in the pane overlay;
  a one-stream camera shows one option and the reason.

## The two field defects

### Full screen did nothing

Root cause: the click handler called `requestFullscreen()` and swallowed the rejected promise. On a
browser that refused the request, the page changed nothing and said nothing.

Fix: the request is made synchronously in the click handler on the wall element. A missing API or a
rejected request shows a plain-language notice that names the browser's own full-screen control. The
browser test asserts that full screen actually engages on the wall and that a refused request leaves
a visible notice.

### Layout and quality controls navigated

Root cause: the layout form called `requestSubmit()`, so the browser posted the layout and reloaded
`/ui/live`. Every player was recreated, full screen ended, and the wall was unavailable for a control
that should be instant. The quality control had an in-place path, but its form still invited a
submit.

Fix: the layout picker applies the layout to the rendered wall and persists it with an inline POST
that returns `204`. The quality and camera forms are captured the same way. The browser test asserts
that the URL is unchanged, no navigation occurred, the player node is the same and at least one
player is still playing after a layout, quality or camera change.

## What the screenshots changed

The first read of the six layouts at four widths in normal and full screen confirmed the geometry and
the one pixel gap. It also produced these changes:

- The 360 overlay showed `Empty pane 7` scrims on placeholders. The pane name is now occupied-pane
  content only, so a placeholder is one plus icon and `Choose camera`.
- The first pane overlay covered most of a small pane. The pane overlay is capped at half the pane and
  scrolls its controls, so the video's centre stays visible and the browser check can measure it.
- The wall overlay covered the top of the wall and, on the first read, the pane centre at phone
  width. It is now the only wall-level surface, it is not shown by keyboard focus alone, and a Tab
  from the last pane control reaches it.
- The 1280 overlay showed the layout icons, the selected radio, the full screen control and one pane's
  own selector and camera chooser. The selected layout has a native radio dot, a thicker border and a
  stronger weight; the icons are arrangement shapes, not counts.
- The 16-pane images with two cameras show two pictures and fourteen placeholders at 1920, 1280, 390
  and 360. The placeholders do not read as a form grid and none has a player.
- The hidden overlay image is pure video; no pane name, control or scrim is visible at idle.

## Unfinished and next

- BIT-027 collects real ONVIF profiles. Until then most cameras report one stream, and the selector
  shows that one option and its reason.
- The capacity priors and thresholds remain the ADR planning numbers; the bench measurements are
  [OQ-44](../open-questions.md#oq-44) and [OQ-46](../open-questions.md#oq-46).
- The QEMU live phase still asserts the previous page. Moving it to the wall and the second profile is
  the next concrete step before the wall is called field-ready.
