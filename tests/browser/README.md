# browser

`tests/browser/` is the opt-in Playwright check for the responsive Bittern UI. It loads every
owner-facing route in a real Chromium layout engine at two phone widths and two desktop widths, fails
on page-level horizontal overflow, an unreachable control, an overlapped control, a visible image
outside the viewport, or a control below its role's touch target, and writes one full-page screenshot
per route and width. The screenshots
exist for the agent implementing a change to read at every width before human review, not only for a
human reviewer; the assertions still measure the viewport. The decision and its boundaries are
in [ADR 0025](../../docs/adr/0025-browser-check-for-the-responsive-ui.md).

This check is not part of `make check`, `make test`, `make schema`, `make style-check`,
`make link-check`, or `make ui-test`. It needs Node, a downloaded browser, and network access once
to install them, so it stays a deliberate developer step.

## Provision

The check is pinned to one pair: Playwright selects the Chromium revision, so there is no second
version to keep in step. The current pair is:

| Component | Version |
| --- | --- |
| Node.js | 24.22.3 |
| npm | 11.19.1 |
| `@playwright/test` | 1.65.1 (exact, no range) |
| Chromium revision | 1255 |
| Chromium build | 159.0.8154.6 (Chrome for Testing) |

From the repository root:

```sh
cd tests/browser
npm ci
npx playwright install --with-deps chromium
```

`--with-deps` installs the system libraries and needs root or passwordless sudo. The browser lives
in the container or user cache, so a recreated development container provisions it again. To change
the pair, update `package.json`, run `npm install` to refresh `package-lock.json`, install the new
Chromium, update this table, and run the check in the same change.

If Node, the package, or the browser is missing, `make ui-browser-check` fails with the exact
missing fact and the command above instead of a stack trace.

## Run

```sh
make ui-browser-check
```

The target builds `bitternd` into the gitignored `build/browser/`, then the harness starts it with a
throwaway config, secrets file, auth state, and state root. The daemon runs on two loopback ports
with `--ui --ui-mock --ui-insecure-http`; `--ui-mock-apply` lets the preview wizard finish its apply
page without writing any system file. The harness completes the real setup and login flow, walks
`routes.json`, and removes the temporary root on exit.

Screenshots go to `build/ui-screenshots/` and nowhere else, named
`<screen>-<width>x<height>.png`. A screen whose loaded page contains a closed `<details>`
disclosure also gets `<screen>-<width>x<height>-expanded.png`, written after the default state has
been measured, with every closed disclosure opened. The `-expanded` image is written only when at
least one disclosure opens; a page with no disclosures, or with every disclosure already open, gets
none. Each image is a full-page capture (`fullPage: true`): the whole document at that viewport
width, however tall it is, with no scaling, cropping, or tiling. The default image for a screen is
written before that screen's assertions run, so a failing screen still leaves its screenshot. The
expanded image is a review artifact only: the assertions still measure the default state at the 360
by 800, 390 by 844, 1280 by 800, and 1920 by 1080 viewport, never the expanded state or the
full-page image. A disclosure that throws or disappears while opening produces a warning for that
screen and never fails the check. That directory is already covered by the repository's `/build/`
ignore rule. No screenshot is committed while [OQ-28](../../docs/open-questions.md) is open.

## Actions

An entry may also name `actions`, an ordered list of interaction steps to run after the page loads
and before its screenshot. Each step has an `action` name and a CSS `selector` for the first matching
element:

| Action | Fields | Effect |
| --- | --- | --- |
| `click` | `selector` | Clicks the first element matching the selector. |
| `wait` | `selector`; optional `state`; optional `decoded` | Waits for the first element to reach `state` (`attached`, `detached`, `visible`, or `hidden`; default `visible`). With `decoded: true` it then waits for the element to be an `img` that is `complete` with `naturalWidth > 0`. |

The screenshot is written after every action succeeds, so a failed action leaves no image for that
screen and fails the check. An unknown action, unknown wait state, missing selector, or timeout is a
failure that names the entry and step; nothing is skipped. This list exists to reach a state that
only appears after interaction, not to become a general test framework.

The camera list with a test picture taken is the first screen to use it. The `picture-taken` entry
clicks `Test picture` on `/ui/cameras`, waits for `img[data-picture-image]` to become visible and
decoded, then captures and measures that state as `cameras-picture-taken-<width>x<height>.png`. The
`-expanded` capture still applies.


## Live player checks

`tests/browser/live.mjs` runs after the route walk on its own daemon with a fixed four-camera
inventory. It stubs `WebSocket`, `MediaSource` and the media element so every player state can be
walked without a camera or go2rtc: connecting, playing, stalled, reconnecting, offline, unsupported,
session ended and switching. It also checks the 1/4/9 viewport ceiling at 360, 390, 1280 and 1920,
and in the playing state it swaps the sibling camera-state fragment and asserts the player, video
element and state survive. Every case asserts the player and video are outside an `hx-*` ancestor.

Six grid checks follow the state walk: switching all three presets, assigning a camera to an empty
tile while the other tile keeps playing, a status poll with an open picker and four running players,
one camera failing while three keep playing, full width to main and back to sub with the other
players alive, and a phone clamping a four-tile layout to one live tile with the ceiling sentence.
The full-width check writes `build/ui-screenshots/live-full-width-1280x800.png`.

`tests/browser/qemu-live.mjs` is the host-side QEMU assertion. It signs in to the forwarded
appliance, opens the authenticated live grid, waits for the four stored tiles and for both applied
virtual cameras to report playing with a non-zero video size and time, and writes
`build/qemu/logs/<run>/live-view.png`.

## What is checked

The viewport list is provisional under [OQ-26](../../docs/open-questions.md) and
[OQ-27](../../docs/open-questions.md):

| Name | CSS pixels |
| --- | --- |
| phone-narrow | 360 by 800 |
| phone | 390 by 844 |
| desktop | 1280 by 800 |
| desktop-wide | 1920 by 1080 |

The manifest has 38 screens from 20 routes, so each run checks 152 route-width screens and writes
152 default full-page screenshots, plus an `-expanded` copy for each screen with a closed
disclosure. Fifteen status variants cover the five overall states and the update card: factory
baseline with an offered update, up to date, update available, installing, failed, rolled back,
offline, rate limited, and a failed check. The `live-nine-tiles` variant sets the saved layout
cookie and reloads, so the nine-tile grid and its empty-tile pickers appear at every width. The
`live-refresh` variant opens Technical details, focuses
its summary, marks a table scroll region, and waits for two real update-card polls. It then aborts
the fragment request, asserts the disclosure, focus, scroll, and card content are unchanged, and
writes `status-live-reconnect-<width>x<height>.png` with the reconnecting notice visible. The
rollback confirmation has a target and a no-target variant.
Each form factor has two sizes so a layout that only breaks at one end of
its range is caught: two phone widths for the narrow end and two desktop widths for the wide end.
The 360 width is the BIT-020 scope floor and the 390 width is the current acceptance width.
Measuring both is evidence for [OQ-27](../../docs/open-questions.md), not a decision between them.

For every route and width the harness asserts, against that width's viewport:

- `document.documentElement.scrollWidth <= window.innerWidth + 1`.
- Every visible link, button, input, select, textarea, and non-negative tab target can be scrolled
  fully into the viewport and can be focused.
- The centre of every such control is the control itself, or a descendant, rather than another
  element covering it.
- A control below its role's touch target fails. A control in the 24 floor role that reaches 24
  but not 44 warns instead of failing. The rule is [touch targets by role](#touch-targets-by-role).
- Every visible image is decoded and its rendered box has non-zero width and height and fits inside
  the viewport after scrolling to it. An image is a replaced element, so it is measured separately
  from the controls.

The live-refresh screen additionally asserts that two real polls keep the open disclosure, the
focused summary, the table scroll position, and the table wrapper in place. It aborts the fragment
request to prove that a lost poll shows the reconnecting notice without replacing the card. These
are behaviour checks, not markup checks.

A skip link that is deliberately off-screen until focused is measured after focus brings it into
view. An off-screen control that does not appear on focus still fails.

## Touch targets by role

The accepted rule is 44 by 44 CSS pixels for an interactive control in a navigation, an action row,
or a whole-row target, and 24 by 24 as the hard floor everywhere else, including a link inside a
sentence. Hit area counts and visual size does not: a control in its own wrapping `<label>` is
measured by the label, because clicking anywhere on the label activates the control.

The roles live in one rule list in `assertions.mjs` (`TARGET_RULES`, with `FLOOR` as the fallback).
An element that matches more than one role takes the first match, and every 44 role is listed before
the floor, so a control that matches both a 44 role and the floor gets 44. The same role block is in
`ui/static/bittern.css`.

| Role | Target | Selector hooks |
| --- | --- | --- |
| navigation | 44 by 44 | `nav`, `[role='navigation']`, `.site-header`, `.action-links`, `.page-links` |
| action row | 44 by 44 | `.form-actions`, `.camera-actions`, `.candidate-actions`, `.discovery-actions` |
| button | 44 by 44 | `button`, `.button`, `[role='button']`, `input[type='submit']`, `input[type='button']`, `input[type='reset']` |
| whole-row target | 44 by 44 | `label.candidate-row`; the label is the hit area |
| inline (floor) | 24 by 24 | everything else, including links inside prose |

A failure names the role and the measured size, for example `a "Refresh now" is 99x32, below the
44x44 navigation target`. A control in the floor role between 24 and 43 warns instead, so an inline
prose link stays visible as advisory without failing the run. The current templates have no inline
prose links, so a clean run reports 0 failures and 0 warnings across the 152 route-width screens; the
state links and every back link are in a 44 role.

`routes.json` covers every `Responsive` row in `agent/internal/ui/routes.go`, plus all five status
states. The `cameras` route has a `picture` variant that previews the test picture in `--ui-mock`, a
`picture-taken` variant that clicks `Test picture` and captures the loaded image, and the
`camera-picture` route loads the image endpoint directly. Each entry names the route, the concrete
path, the auth flow (`setup`, `guest`, or `auth`), and any wizard preparation (`cameras`,
`review`, `started`, or `applied`).

The Go test in `agent/internal/ui/ui_browser_manifest_test.go` fails when a responsive route is
missing from the manifest or the manifest names a route that is not a responsive entry. Regenerate
it with:

```sh
cd agent && go test ./internal/ui -run TestBrowserRouteManifest -update
```

The `-update` flag is shared with the generated route table in `ui/README.md`.

## Limitations

This is Chromium only, at fixed viewports. It does not test Firefox or WebKit, real touch input,
device pixel ratio, dynamic viewport units, browser zoom, the self-signed TLS interstitial, or
whether a physical camera can scan the setup QR. It is not a pixel-diff tool; a green run is
geometry evidence, not a substitute for looking at the full-page screenshots. The browser check does
not run in CI and is not a release gate.
