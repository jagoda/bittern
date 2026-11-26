# Live view sizing on real hardware: the player had no height budget

- Date: 2026-11-26
- Status: fixed. The live player sizes to both axes, the desktop grid fits the viewport, and a
  browser full-screen mode contains the video.
- Related: [ADR 0034](../adr/0034-live-view-owned-by-bittern.md),
  [OQ-40](../open-questions.md#oq-40), [the live view slice 1b note](2026-11-26-live-view-slice-1b.md),
  [the UI design skill](../../.pi/skills/ui-design/SKILL.md),
  [the QEMU harness](../../tests/qemu/README.md).

## Report

The maintainer ran generation 2 on a bench box with a real camera. The live picture rendered at its
native size and ran off the end of the screen. They wanted the layout to fit the screen, one tile to
fill a phone's available area, and a real full-screen view with no overflow and no scrolling.

## Root cause

The player was width-driven and had no height budget:

- `ui/static/bittern.css:1338` gave `.live-video` `inline-size: 100%` and
  `aspect-ratio: 16 / 9`, but no block-size cap and no box that shrank with the available height.
- `ui/static/bittern.css:1450` gave `.live-grid` columns and a gap, but its rows were content-sized.
  Every tile's 16:9 media box plus its heading, status and controls set the row height, so the grid
  grew the document instead of fitting it.
- `ui/static/bittern.css:1471` gave the full-width state its own column and order rules, but no
  height rule. The 68rem media box at 1280 was 612 pixels tall before the heading, controls and
  status, so the page was taller than an 800 pixel viewport.
- The video element is a replaced element. Its intrinsic size participates in the flex and grid
  minimum sizes, so a 1920x1080 or 3840x2160 stream made the visible picture drive the page rather
  than the page drive the picture. The old rule did not contain that size on the block axis.

The report's "native size" is the visible effect: the media box was sized from the stream's width
and ratio, not from the space the layout had left. It was not a missing `object-fit`; it was a
missing container box and height constraint.

## Why the suite passed

- The browser stub in `tests/browser/live.mjs` never reported `videoWidth` or `videoHeight`, so the
  fake video had a 0 by 0 intrinsic size. The old width-driven box could not overflow in that
  fixture.
- The route fixture and the QEMU camera both served 640x360. The QEMU live phase checked that both
  players reached `playing` and had a non-zero video width, not the guest document's overflow.
- The browser check asserted horizontal page overflow only. It did not assert that the live grid's
  `scrollHeight` stayed inside the viewport, and it had no full-screen state.

That is the test gap: a convenient 640x360 fixture and a geometry-only assertion proved the page
rendered, not that a realistic stream fit.

## Fix

- `.live-media` is the aspect box. It reserves 16:9 before the first frame, is never wider than its
  container and is never taller than the height it is given. The video and snapshot are contained
  inside it with `object-fit: contain`.
- The live grid page owns the viewport. `main` is a flex column, the grid takes the remaining
  height, and its rows are `minmax(0, 1fr)`. The footer is omitted on the live grid page so nine
  tiles fit a 1280 by 800 screen. Short tiles compress their heading, detail and picker; the player
  status, controls and touch targets remain.
- On phone widths the grid scrolls inside its own region while the first active tile fills the
  available area. The document itself does not scroll.
- Each player has a browser full-screen control. It requests full screen on the player root, keeps
  the media contained, locks document overflow, and leaves through the control, Escape or Back.
  The existing full-width control, tiles, states and device ceilings are unchanged.

## Tests

- `tests/browser/live.mjs` now stubs 1920x1080 and 3840x2160. The large-stream cases assert no
  horizontal or vertical document overflow on the grid and in full screen at 360, 390, 1280 and 1920.
- `walkReservedBox` compares the media and video rectangles before and after metadata arrives and
  requires them to stay the same size.
- The same cases enter full screen from the tile control, leave once with Escape and once with Back,
  and write the four-width screenshots.
- `tests/qemu/config.json` serves the camera at 1920x1080. `tests/browser/qemu-live.mjs` asserts both
  players reach at least 1920x1080 and the guest document has no overflow; `tests/qemu/run.py`
  fails the live phase on either condition.

Before the fix, the first `assertNoOverflow` on the desktop grid would have failed on
`scrollHeight` because the content-sized rows made the document taller than the viewport. The
full-screen assertion would also have failed because there was no full-screen control or mode.

## Evidence

- `make ui-browser-check` passes the 27 live checks, including the eight large-stream cases and the
  reserved-box case, and the 14 routing checks.
- Screenshots read at 360x800, 390x844, 1280x800 and 1920x1080:
  `live-<width>.png`, `live-nine-tiles-<width>.png`, `live-camera-<width>.png`,
  `live-large-1920x1080-<width>.png`, `live-large-3840x2160-<width>.png`,
  `live-full-screen-1920x1080-<width>.png` and `live-full-screen-3840x2160-<width>.png`.
- QEMU firstboot was attempted twice. The harness never reached the readiness marker
  `/run/bittern-harness-ready`: the guest booted with network and Docker, but the Bittern payload
  units were absent and `dnsmasq` reported port 53 already in use. The failure is in the harness
  guest provisioning on this host, before any live phase ran, so the live phase is not claimed. The
  fixture change is 1920x1080 and the phase asserts guest overflow on the next run.
