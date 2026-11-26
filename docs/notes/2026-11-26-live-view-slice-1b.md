# Live view slice 1b: the grid, layouts and the device ceiling

- Date: 2026-11-26
- Status: implemented. The browser check is green and the QEMU live phase is recorded below.
- Scope: the grid at `/ui/live`, the 1/2/4/9 layout presets, the per-device layout cookie, the
  server-resolved tile assignment, one player island per tile, the in-place full-width view, the
  saved-layout clamp, the per-session media limit and the two-camera QEMU live phase.
- Related: [ADR 0034](../adr/0034-live-view-owned-by-bittern.md),
  [ADR 0016](../adr/0016-bittern-owns-frigate-config.md),
  [UX-14](../principles.md#ux-14-live-updates-must-not-disturb-state-the-user-owns),
  [slice 1a](2026-11-26-live-view-slice-1a.md),
  [upstream verification](2026-11-26-live-view-upstream-verification.md),
  [UI README](../../ui/README.md), [QEMU harness](../../tests/qemu/README.md).

## What changed

| Area | Change |
| --- | --- |
| Layout model | `agent/internal/ui/live_layout.go` holds `liveLayout` (a preset and a tile list), the cookie codec, `resolveLiveGrid`, the preset picker, the tile camera options and the two layout POST handlers. A preset is a tile count, so a later preset is one entry in `livePresetCounts`, not a new template. |
| Routes | `POST /ui/live/layout` sets the preset and keeps the resolved assignment. `POST /ui/live/tile` assigns or clears one tile; it redirects without JavaScript and returns just that tile for the in-page replacement. `GET /ui/live` is the grid; `GET /ui/live/{camera}` stays the single-camera page. |
| Cookie | `bittern_live_layout`, `Path=/ui/live`, `HttpOnly`, `Secure`, `SameSite=Strict`, one year. The value is a URL-encoded query with the preset and the camera IDs only: no address, credential, stream name or owner. |
| Templates | `ui/templates/live.html` defines the grid, the tile, the tile camera form and the shared player island. The preset picker, the ceiling note and the tile status line are outside every player island. |
| Player | `ui/static/live-player.js` has a `LiveGrid` that owns the viewport ceiling, the wanted set, the full-width tile and the one-tile replacement, and one `LivePlayer` per island. States are connecting, playing, switching, stalled, reconnecting, idle, offline, unsupported and session. |
| Proxy | `agent/internal/ui/live_proxy.go` counts a held WebSocket or a recently active HLS session id against the per-session maximum of nine. An HLS id expires after 30 seconds of silence. Above the maximum the proxy answers 429. |
| QEMU | The camera guest serves a second stream on a macvlan address with its own hardware address; the wizard adds two cameras; the live phase asserts a four-tile layout with both players in `playing`. |

## Layout model

The server is the only writer and resolver of the layout. On a render it:

1. Parses the cookie; a missing or unreadable cookie is the automatic layout, never an error.
2. Clamps the stored preset to the largest allowed count at or below it. A stored `16` becomes
   nine; a stored `3` becomes two; a stored `0` or text becomes automatic.
3. Resolves the tile list against the enabled cameras, drops unknown IDs and duplicate IDs, and
   pads to the preset count with empty tiles.
4. Renders one tile per position with the camera name, the sub profile, Bittern's own media base,
   the camera-state URL and the full-width URL.
5. Offers each empty tile the cameras that are not already in another tile, and says when every
   camera is already in the grid.

Automatic shows one tile per enabled camera, up to nine. The preset POST resolves the current
layout first, so switching from automatic to nine and back to one keeps the assignment. The tile
POST resolves the current layout first, sets or clears one position, drops trailing empty
positions and writes the cookie. The browser only remembers; it never decides which camera is in
which tile.

## Ceilings

| Class | Viewport | Active tiles | Detected by |
| --- | --- | --- | --- |
| Phone | below 44rem (704 CSS pixels) | 1 | `window.innerWidth` in `LiveGrid.sync` |
| Tablet and small laptop | 44rem to below 80rem (704 to 1280) | 4 | `window.innerWidth` |
| Desktop | 80rem (1280) and above | 9 | `window.innerWidth` |
| Hard maximum | any | 9 media sessions per session | `liveLimiter`, keyed by the server session |

The page selects the UX ceiling from the viewport because only the browser can measure it. A saved
layout above the ceiling starts only the first allowed tiles in DOM order; the rest are compact
rows with their own Start and Open full width actions, and one sentence names the limit. A resize
that lowers the class stops the excess players in order and does not restart them; a resize that
raises the class does not start new players.

The proxy does not read a ceiling from the request. It counts held WebSocket media connections and
recently active HLS session ids against the server session, using the same nine as the hard
maximum, and refuses the tenth with 429 before any upstream connection. The browser cannot raise
it, and the device table is not a setting.

## Tile isolation

Every occupied tile is a separate `[data-live-player]` island with its own video element,
MediaSource, WebSocket, timers and state. The grid holds only the wanted set and the ceiling. A
tile's sibling camera-state line polls every 15 seconds into its own element. The tile camera form
is submitted with `fetch` and replaces only that tile's island; without JavaScript the same POST
redirects and the page reloads.

Full width is a grid state, not a route. The chosen tile switches to the main profile and spans the
grid; the other islands stay mounted and playing. Returning switches the tile back to the sub
profile. On a phone, opening an idle tile stops the current live tile in bounded order, and
returning restores the previously wanted set.

## Tests

Server-side, in `agent/internal/ui/live_layout_test.go`:

- `TestLiveLayoutResolvesTheAssignmentAtRenderTime`: presets one, two, four and nine, the
  automatic fill, stored order, padding, unknown and duplicate cameras, a stored preset of 16
  clamped to nine, a stored three rounded down to two, a stored negative count, and an explicit
  automatic assignment.
- `TestLiveLayoutPresetPickerReflectsTheStoredPreset`
- `TestLiveLayoutCameraPickerExcludesCamerasInOtherTiles`
- `TestLiveLayoutCookieIsPerDeviceAndCarriesOnlyThePresetAndCameras`: path, flags, lifetime, and
  no address, stream name or credential in the value.
- `TestLiveLayoutMissingOrBrokenCookieFallsBackToAutomatic`
- `TestLiveGridPageResolvesTheStoredLayout`: four tiles, two players, the sub media bases, the
  ceiling note hook, the empty-tile sentence, and no leak.
- `TestLiveGridWithoutCamerasOffersTheCameraStep`
- `TestLiveLayoutChangeKeepsTheAssignment`
- `TestLiveTileAssignmentKeepsTheOtherTiles`: the in-page fragment carries one player and the
  ordinary POST redirects.
- `TestLiveTileAssignmentRefusesATileOutsideThePage`
- `TestLiveGridKeepsEveryPlayerOutsideEveryHTMXRegion`

Server-side, in `agent/internal/ui/live_proxy_test.go`:

- `TestLiveLimiterSharesTheMaximumAcrossSocketsAndHLSSessions`
- `TestLiveLimiterExpiresAnIdleHLSSession`
- `TestLiveMediaProxyRefusesMoreThanTheHardMaximumOfConcurrentSessions`: nine WebSockets upgrade,
  the tenth gets 429.
- `TestLiveMediaProxyRefusesMoreThanNineConcurrentHLSSessions`: nine session ids, the tenth gets
  429, and the upstream is not called for it.

Browser-side, in `tests/browser/live.mjs`, after the existing eight player states and four
device-ceiling widths:

- `presets`: one, four and nine tiles switch through the form, keep the assignment, and are
  remembered across a reload.
- `assignment`: a camera picked for an empty tile replaces only that tile; the first tile's marker
  and `playing` state survive.
- `poll`: a real status poll with four players and an open picker leaves every player, the open
  disclosure, focus, the tile count and the chosen preset untouched.
- `failure`: one camera fails while the other three keep playing and the failing tile names itself.
- `full width`: one tile switches to main and back to sub while the other three keep playing with
  their markers intact.
- `phone ceiling`: a stored four-tile layout at 390 starts one tile, opens one socket, leaves three
  idle, shows the "one camera" sentence and shows no error state.

## Evidence

- `make check` passes, including the new layout, cookie, proxy-limit and template-guard tests.
- `go vet` and `gofmt` are clean.
- `make ui-browser-check` passes 152 route-width screens from 38 screens and 20 routes at 360, 390,
  1280 and 1920, plus 18 live player, ceiling and grid checks with 0 failures and 0 warnings.
- The four-width screenshots are under `build/ui-screenshots/`: `live-<width>.png` (automatic,
  four cameras), `live-nine-tiles-<width>.png` (the saved nine-tile layout with empty pickers) and
  `live-camera-<width>.png`, plus `live-full-width-1280x800.png`.
- `python3 tests/qemu/run.py firstboot` passes on the two-camera fixture. The live phase asserts four
  tiles, two players, the authenticated HLS masters and a 1128-byte segment; the host browser
  reaches `playing` at 640x360 on both tiles. Screenshot:
  `build/qemu/logs/firstboot-20261126-135843/live-view.png`.

## Professional test

Changed screens: `/ui/live` in the automatic, nine-tile, phone-clamp, full-width and expanded-picker
states, and `/ui/live/{camera}` with four cameras in the picker. Changed transitions: the preset
POST, the tile assignment, full width in and out, and the tile status poll.

| Question | Result | Evidence |
| --- | --- | --- |
| Q1 | pass | The grid is read-only and has zero filled buttons. The empty state's one filled action is Add camera. Full width and the pickers are secondary. |
| Q2 | pass | 360: Live, the ceiling note, the first camera. 1280 and 1920: Live, the first row of tiles. The layout control is a disclosure named with the current preset. |
| Q3 | pass | `make design-check` passes; the new rules use tokens only, and the media queries use rem. |
| Q4 | pass | The browser check reports no overflow at 360, 390, 1280 or 1920. The grid is 1, 2 and 3 columns; the lede keeps its 65ch measure. At 1920 the shell's 68rem cap is [OQ-40](../open-questions.md#register). |
| Q5 | pass | The browser walk covers connecting, playing, idle, stalled, reconnecting, offline, unsupported, session, full width and the ceiling clamp; each has a word, a sentence and a next action. |
| Q6 | pass | The keyboard pass: Enter opens the layout disclosure; Tab reaches the preset; a preset change reloads with the disclosure open and focus visible; Enter on Open full width moves focus to Back to grid; Enter there returns focus to Open full width; the tile picker opens with Enter; a tile replacement focuses the new camera heading. The browser check reports 0 target warnings. |
| Q7 | pass | The ceiling note names the limit and the next action. The failing camera keeps its own named state. A bad tile number gets a 400 page with the next action. |
| Q8 | pass | The select is labelled, every button has a verb, the video has a name, and the target rules pass at every width. |
| Q9 | pass | Sentence case, verbs on controls, one noun per object. The layout hint says "as many cameras as it can at once", not "decode". |
| Q10 | pass | No path, status code or service name in primary copy; the DOM carries no stream name, camera address or credential (template scan and proxy tests). |
| Q11 | pass | The nav opens Live; no cameras goes to Add camera; a stored layout resolves; a removed or disabled camera is dropped at render. |
| Q12 | pass | Open full width, Back to grid, Show in this tile, and the single-camera route all name where they land. |
| Q13 | pass | Back after a preset POST returns to `/ui/live` and re-renders the stored layout. Full width has an explicit Back to grid and adds no history entry. |
| Q14 | pass | Preset and tile POSTs use a 303 and a cookie; the in-page tile replacement is a 200 fragment; a reload keeps the layout (browser check). |
| Q15 | pass | One failing camera leaves the others playing and names itself. A clamp explains the limit. A failed in-page replacement falls back to the normal POST. An expired session gets `HX-Redirect` and no login page inside a tile. |
| Q16 | pass | The layout is per device, one year, and never synced to the account; the phone and desktop clamp the same stored layout differently. |

Findings, all fixed in this change:

- [Minor] Q2 at `/ui/live`, 360: the five preset buttons were the second fixation before the first
  stream. The owner had to parse a control they rarely change to reach the video. Fix: the presets
  sit in a disclosure whose summary names the current layout; `live-<width>.png`.
- [Minor] Q6 at `/ui/live`, full width: the Back control sat below a 612 pixel video, and focus
  dropped to the document when the pressed control was hidden. The keyboard owner lost their place.
  Fix: the full-width controls sit above the video, focus moves to the matching control, and a tile
  replacement focuses the new camera heading.
- [Minor] Q9 at `/ui/live`, layout hint: "decode" is implementation language. Fix: "as many cameras
  as it can at once".
- [Minor] Q4 at `/ui/live`, 1920: the grid keeps the 1280 tile width because the shell caps the page
  at 68rem. The wall does not use the wide screen. Destination: [OQ-40](../open-questions.md#register),
  provisional 68rem for this slice.

No blocker or major finding is open.

## QEMU

The firstboot live phase now signs in, sets the four-tile preset over HTTP, and asserts four tiles
and two players with no stream name, camera address or loopback address in the page. It fetches the
authenticated HLS masters for both cameras and an HLS segment. The host browser opens the grid,
waits for both tiles to report `playing` with a non-zero video size and time, and writes
`build/qemu/logs/<run>/live-view.png`.

Result: `python3 tests/qemu/run.py firstboot` passed on 2026-11-26
(`build/qemu/logs/firstboot-20261126-135843`). The wizard added both virtual cameras, the apply
rendered both into the Frigate config, and the live phase resolved them into a four-tile grid. The
host browser reached `playing` at 640x360 on both tiles and the screenshot shows both test patterns.
The phase also fetched the authenticated HLS master, session playlist and a 1128-byte segment
through the proxy.

The fixture serves two streams from one camera guest (one mediamtx, two ffmpeg publishers, a
macvlan for the second hardware address). It proves two concurrent authenticated media sessions and
two player islands. It is not a second camera vendor, so interoperability with two real cameras
remains unproven.

## Findings

| ID | Finding | Destination |
| --- | --- | --- |
| F1 | The shared page shell caps the page at 68rem, so the grid keeps the 1280 tile width at 1920. ADR 0034's reasoning uses a wider 1920 wall. | [OQ-40](../open-questions.md#register); slice 1b keeps 68rem rather than choosing a new measure. |
| F2 | The device ceiling is necessarily selected in the browser because only it can measure the viewport; the server cannot verify it. The proxy still enforces nine sessions from the server session, so a lying client cannot exceed the hard maximum. | This note; the proxy limit test. |
| F3 | The QEMU fixture's second camera is a macvlan address on one guest and one mediamtx, so the phase proves two streams, not two camera vendors or two ONVIF profiles. | This note; the camera matrix and BIT-027 own real-device coverage. |
| F4 | The grid polls one camera-state line per tile: nine requests every 15 seconds at the desktop maximum. The poll is small and bounded by the ceiling, but a single grid-level poll would be cheaper. | No action this slice; the tile line is the BIT-044 pattern and the count is bounded by nine. |
| F5 | A JavaScript tile replacement uses the `HX-Request` and `HX-Redirect` auth contract even though it is a `fetch`, not HTMX, so an expired session gets a 401 and a login redirect instead of a login page inside the tile. | This change; the fetch falls back to a normal POST and the contract is the existing BIT-046 one. |

## Cut from this slice

PTZ, audio, event review, playback, camera configuration, the second-stream schema and the Frigate
pin are untouched. The wide live page measure is recorded as OQ-40, not chosen. The kiosk and the
gated advanced Frigate view remain out of scope.
