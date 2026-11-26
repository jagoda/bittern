# ui

`ui/` contains `bittern-ui`, the browser interface for appliance management. It is server-rendered
Go `html/template` with HTMX for partial updates, and every page works without JavaScript
([ADR 0019](../docs/adr/0019-ui-stack.md)). It covers the first-run wizard, camera onboarding,
status and health, update controls, logs, support bundle download, and reset. It talks to `bitternd`
over a local API and never writes rendered system files itself; mutations go through the agent apply
transaction. Live view follows [ADR 0034](../docs/adr/0034-live-view-owned-by-bittern.md); the
single-camera view, the grid and the browser full-screen mode are implemented. The Frigate UI is
not a product surface.

The product serves the UI over TLS on `0.0.0.0:443` with a self-signed certificate generated at
first boot. The wizard and every page show its SHA-256 fingerprint; the browser warning is explained
rather than suppressed. `--ui-insecure-http` is a loopback-only development mode and is not part of
the product service. The status API stays on its separate loopback listener.

## Implemented flow

The UI is served behind `bitternd --ui`. Without `--ui-mock` its applier persists the wizard's
`appliance.yaml` changes and calls the BIT-011 apply transaction. The apply POST starts one
persisted operation under the state root and redirects to `/ui/wizard/apply`; the operation page
polls a live fragment with HTMX, shows the current step, elapsed time, completed and remaining steps,
and survives a reload or a closed browser. The Technical details disclosure sits outside the live
region, so an open disclosure and its focus survive every poll. A lost poll shows a reconnecting
notice and leaves the panel in place. A second start is refused while one runs. On completion the
operation leads to `/ui/wizard/result`, which names every camera and its own picture check, or names
the failing step, the restored generation, the state of every camera, and the next action after a
failure or rollback. A camera the appliance refuses at apply time is named there and again on its
own card on the review, so one camera's settings are never summed up as a whole-configuration
failure. `--ui-mock` keeps the stub and writes nothing. The camera step discovers cameras on `cam0` with layered,
best-effort evidence:
the dnsmasq lease and ARP table, ONVIF WS-Discovery, an unauthenticated ONVIF device-information
attempt, and an RTSP `OPTIONS` probe. ONVIF is an optimization, not a requirement. Discovery is
read-only and bounded; every layer result is shown, and a camera that does not appear can always be
entered by address with its credentials.

First boot writes the authoritative one-time setup token to `/etc/bittern/secrets.yaml` and a mode
0600 convenience copy to `/var/lib/bittern/setup-token`. The local console shows a QR for
`https://<lan0-address>/ui/setup#token=<token>`, the token in readable groups, and a separate short
one-time code. Scan the QR with the device that opens the wizard; the setup page reads the fragment
locally, clears it with `history.replaceState` before submitting, and posts the token once in the
form body. The fragment never reaches the server, an access log, or a `Referer`.

The first visit to `/ui/` is resolved from device state. Before setup completes it redirects to
`/ui/setup` and then `/ui/wizard`; after setup it sends a device with a camera to `/ui/live` and a
device with no camera to `/ui/status`. The setup-complete marker is
`/var/lib/bittern/setup-complete.json`, mode 0600. It is written when the first-run apply succeeds,
not when the administrator password is created, so a half-finished setup keeps landing on setup. A
legacy successful `operation.json` is migrated once so devices that completed setup before the
marker existed do not return to the wizard. A requested local `next` target wins over the landing
default and is validated by `SafeNextOr`; absolute URLs, protocol-relative paths, dot segments and
paths outside `/ui` are refused.

The setup page accepts the token or short code and asks the owner to set the administrator
password. The short code is a separate value, stored only as a
hash and expiry in `/etc/bittern/secrets.yaml`, and expires after 10 minutes. Both credentials use
the same per-address and global rate limit. The password is stored as a salted
PBKDF2-HMAC-SHA256 hash with 600,000 iterations in `/var/lib/bittern/auth.json`; both token copies
and the short-code material are removed and neither credential can be reused. Later visits use
`/ui/login`. If no local console is available, `sudo bittern setup-token --qr` prints the same block
on the appliance terminal or over SSH.

The header marks the current destination with `aria-current="page"`, omits `Setup` after the setup
marker exists, and omits `Live` when the device has no configured camera. `Cameras` and `Status`
remain available because their screens have useful empty and recovery states.

The setup page keeps working with JavaScript disabled: the grouped token or short code is entered by
hand. `ui/static/setup-fragment.js` only fills the field and clears the fragment when JavaScript is
available. `sudo bittern setup-token` without `--qr` keeps the plain token output for scripts.

Sessions are server-side, expire after 12 hours, and use a `Secure`, `HttpOnly`,
`SameSite=Strict` cookie scoped to `/ui`. Every mutating form carries a per-session CSRF token. Login
and setup attempts are limited per remote address and globally, with a short lockout. The login
page returns the same message for an unknown user and a wrong password. Every flow works without
JavaScript.

## Routes

`agent/internal/ui/routes.go` is the route inventory. The mux, the route tests, and the later
BIT-020 browser check read it. A handler that is not in the inventory fails the guard test. The
table below is generated from the inventory, and `TestUIRouteTableREADME` fails when the two drift.
The BIT-020 column is the route's responsive flag. Regenerate the table from `agent/` with
`go test ./internal/ui -run TestUIRouteTableREADME -update`; do not edit it by hand.

<!-- BEGIN uiRoutes table: generated by agent/internal/ui/routes_readme_test.go; do not edit -->

| Method | Path | Template | Auth | BIT-020 |
| --- | --- | --- | --- | --- |
| GET | `/ui` | - | Yes | No |
| GET | `/ui/{$}` | - | Yes | No |
| GET | `/ui/login` | `login` | No | Yes |
| POST | `/ui/login` | `login` | No | No |
| GET | `/ui/setup` | `setup` | No | Yes |
| POST | `/ui/setup` | `setup` | No | No |
| POST | `/ui/logout` | - | Yes | No |
| GET | `/ui/wizard` | `welcome` | Yes | Yes |
| GET | `/ui/wizard/network` | `network` | Yes | Yes |
| POST | `/ui/wizard/network` | - | Yes | No |
| GET | `/ui/wizard/storage` | `storage` | Yes | Yes |
| POST | `/ui/wizard/storage` | - | Yes | No |
| GET | `/ui/wizard/camera` | `camera` | Yes | Yes |
| POST | `/ui/wizard/camera/continue` | - | Yes | No |
| POST | `/ui/wizard/camera/discover` | - | Yes | No |
| POST | `/ui/wizard/camera` | `camera` | Yes | No |
| GET | `/ui/wizard/camera/{ref}/settings` | `camera` | Yes | Yes |
| POST | `/ui/wizard/camera/{ref}/settings` | `camera` | Yes | No |
| POST | `/ui/wizard/camera/{ref}/remove` | - | Yes | No |
| GET | `/ui/wizard/review` | `review` | Yes | Yes |
| POST | `/ui/wizard/apply` | - | Yes | No |
| GET | `/ui/wizard/apply` | `operation` | Yes | Yes |
| GET | `/ui/wizard/apply/fragment` | `operation-panel` | Yes | No |
| GET | `/ui/wizard/result` | `operation-result` | Yes | Yes |
| GET | `/ui/wizard/done` | `done` | Yes | Yes |
| POST | `/ui/wizard/cancel` | - | Yes | No |
| GET | `/ui/cameras` | `cameras` | Yes | Yes |
| GET | `/ui/cameras/{id}/edit` | `camera` | Yes | Yes |
| POST | `/ui/cameras/{id}/edit` | `camera` | Yes | No |
| POST | `/ui/cameras/{id}/discover` | - | Yes | No |
| POST | `/ui/cameras/{id}/snapshot` | - | Yes | No |
| GET | `/ui/cameras/{id}/picture` | - | Yes | Yes |
| GET | `/ui/cameras/{id}/remove` | `camera-remove` | Yes | Yes |
| POST | `/ui/cameras/{id}/remove` | `camera-remove` | Yes | No |
| GET | `/ui/live` | `live` | Yes | Yes |
| GET | `/ui/live/{camera}` | `live` | Yes | Yes |
| POST | `/ui/live/layout` | - | Yes | No |
| POST | `/ui/live/tile` | - | Yes | No |
| GET | `/ui/live/{camera}/status` | `live-status` | Yes | No |
| GET | `/ui/live/media/{camera}/{profile}/{kind...}` | - | Yes | No |
| GET | `/ui/status` | `status` | Yes | Yes |
| POST | `/ui/status/updates/check` | - | Yes | No |
| POST | `/ui/status/updates/install` | - | Yes | No |
| GET | `/ui/status/updates/rollback` | `update-rollback` | Yes | Yes |
| POST | `/ui/status/updates/rollback` | - | Yes | No |
| GET | `/ui/status/fragment` | `status-panel` | Yes | No |
| GET | `/ui/status/updates/fragment` | `update-card-fragment` | Yes | No |
| GET | `/ui/static/` | - | No | No |
| * | `/ui/` | `error` | Yes | Yes |

<!-- END uiRoutes table -->

`--ui-mock` selects the mock status provider, which makes every documented overall state
(`booting`, `ready`, `degraded`, `updating`, and `failed`) selectable from the status page. Without
that flag the status page uses the real BIT-006 builder, and the setup network and storage checks
read the same builder through `ui.AgentChecks`. `--ui-mock` keeps the sample `ui.MockChecks` data.
The status page is one column at phone width and a two-column card grid from 60rem. The overall
state, configuration, render checks, network interfaces and hardware cards span both columns; the
remaining cards pair up. Only the Updates card is live. Its facts and actions refresh every 15
seconds, or every 2 seconds while an update or rollback is running, so the current step updates
without a second progress surface. The rest of the status page, including the Technical details
disclosure and the table scroll regions, stays in place. A lost poll shows a reconnecting notice
beside the live card and leaves the last content visible until the next successful poll.

The Updates card is the browser update surface. It names the running version, the factory version
on a kit baseline, the last check time and result, the verified offered version and update number,
the current install or rollback step, a failed step and its plain reason, or the rolled-back
running and last installed versions with the next-update rule. A failed, offline, or rate-limited
check is never shown as up to date. Check, install, and rollback are POST forms with the
administrator session and CSRF token. Install is absent without a verified newer version, rollback
is absent without a saved previous version, and every action is absent while an operation runs.
Manual rollback has its own confirmation page and states that configuration and recordings are not
reverted. `AgentUpdateService` calls the same `internal/update` functions as the CLI; the verified
download runs in the background, then the fixed `bittern-update.service` owns the transaction and
the status document exposes its journal as `current_operation`. `--ui-mock` previews the card with
`?update=baseline`, `available`, `up-to-date`, `installing`, `failed`, `rolled-back`, `offline`,
`rate-limited`, or `check-failed`.

A preview build names the review configuration as a preview instead of showing the temporary
preview path; a real appliance shows the configured path. The apply boundary is an `Applier`
interface: `AgentApplier` writes the validated config and calls the apply transaction, `StubApplier`
returns `apply is not implemented`, and tests use a fake. `AgentApplier` also implements
`OperationApplier`, which observes the transaction steps without changing them. The operation
record is `<state-root>/operation.json` at mode 0600 and contains the step key and label, counts,
start and finish times, the camera names and addresses, and the redacted result; it never contains a
camera URL, a username or a password.

The camera form collects a username and password for each camera. The wizard builds the complete RTSP
URL with percent-encoded credentials, keeps the password out of every rendered page, and passes one
URL per camera to the applier in memory. `AgentApplier` writes each URL atomically to `secrets.yaml`
at mode 0600 under that camera's own `cameras.<id>.rtsp_url` key, then writes `appliance.yaml` with
only the key names. The transaction renders the camera env file from those keys and refuses a
duplicate environment name. A run that does not commit restores `secrets.yaml` as it was, so a
sign-in never outlives the configuration that references it.
The credential key is not a browser field. Leaving both fields blank keeps the saved credential when
the camera address, port and path are unchanged, or records a no-auth camera. A camera that names a
key which is missing fails before the config is written. No password is ever returned to the
browser or written to a log.

The discovery list shows one compact row per candidate: the radio, address and seen state, with
the hardware address, vendor, model and sources on one line below. Each candidate's layer results
stay behind a no-script evidence disclosure. Choosing a radio sends an HTMX GET for the same page
with `selected=<address>` and swaps the camera identity fields; the handler fills the name, address
and hardware address from a candidate discovery actually reported, keeps the wizard session
untouched, and never invents a value. The name is the reported vendor and model, or `Camera N` in
list order when neither is useful. With HTMX, a name the owner has typed is sent with the selection
and is never replaced. The secondary Use this camera button submits the same GET without HTMX and
renders the full page, so a browser without JavaScript still completes the step. A status line names
the camera that filled the form. A failed probe is evidence, not a dead end, so manual entry always
remains available. The optional hardware address, RTSP port, path and sign-in fields sit in one
collapsed Advanced settings disclosure, grouped as camera identity, stream and camera sign-in,
already filled with port `554` and path `/`. When the address is inside the pool and discovery has
no hardware address, the owner can open that disclosure and finish the reservation by hand. The
chosen camera's vendor and model are stored as display metadata in its `appliance.yaml` entry.
Discovery never sends credentials, refuses an XAddr outside `cam0`, and refuses redirects.

If the selected address is inside the `cam0` dynamic pool, the wizard uses the MAC from its own
discovery result (never from the form) and writes a reservation. The renderer emits the Bittern-owned
`/etc/dnsmasq.d/bittern-reservation.conf`, and the cleanup script recognises its name and generated
header. A manual reservation in another file is left alone and fails the clean-state check.
After apply, the result page asks the recording service for each camera's latest JPEG through the
loopback API and shows the picture, or the failure, on that camera's own row. It never rolls back a
committed apply for a missing picture; it says the configuration stays applied and that the camera is
not recording.

`/ui/cameras` is the managed list. It reads the status document's `cameras` section, which combines
`appliance.yaml` with the non-secret `<state-root>/camera-health.json` written by adoption, an edit,
and the explicit Test picture action. The page never calls a camera to render. Each card shows the
name, enabled state, address, last seen time, and either one compact check summary or the checks
that are failing, with Edit, Test picture, and Remove. MAC, vendor/model, the per-check states, the
last test picture, and the check detail sit behind a no-script Details disclosure. Edit and remove
call the same `Applier`; remove also deletes the camera's secret. The native camera UI action is
deliberately absent until the proxy ticket. A failed test picture is shown as failed and degrades the
camera section.

`GET /ui/cameras/{id}/picture` serves the test picture after a successful test. It resolves the
camera from `appliance.yaml`, fetches the JPEG once from the loopback recording service with a 5
second timeout, and rejects a response that is not JPEG data or is larger than 8 MiB. A successful
response is `image/jpeg` with `Cache-Control: no-store`; failures are a short sentence with the same
cache header. The bytes stay in memory for that response only. They are never written to the health
cache or the status document, never logged, and never included in a support bundle. The card shows
the image only after the recorded test at the cache-busting time in the URL, and replaces it with a
sentence and the retry action when the fetch fails.

The TLS certificate is loaded from `/etc/bittern/tls/server.crt` and the private key from
`tls.private_key_pem` in `/etc/bittern/secrets.yaml`. A missing certificate or key stops the daemon
instead of falling back to HTTP. The `--ui-certificate`, `--ui-secrets`, and `--ui-auth-state` flags
override the paths for tests. Security headers are a self-only CSP with `media-src 'self' blob:` for
the same-origin MSE object URL, `nosniff`,
`X-Frame-Options: DENY`, `frame-ancestors 'none'`, and `Referrer-Policy: no-referrer`. HSTS is not
set because the certificate is self-signed. HTMX injects an indicator `<style>` element that
`style-src 'self'` blocks, which logs a console error on every HTMX page. The defect and the two
remedies are recorded in
[the HTMX CSP note](../docs/notes/2026-11-18-htmx-inline-style-csp.md) and
[BIT-034](../docs/plan/implementation-plan.md#bit-034-remove-the-htmx-inline-style-csp-console-error);
this change does not alter the policy.

The pages provide labels for every input, fieldsets and legends for grouped fields, a skip link,
keyboard order from the document order, visible `:focus-visible` styles, and a palette checked
against WCAG 2.2 AA contrast. The rendered HTML is parsed in tests, which fail on an input without a
label.

## Findings and principles

Every finding from a run, review, incident, report, or research task is captured in a dated note
under [docs/notes](../docs/notes/), triaged to a ticket, open question, ADR, research note, or
documented no action, and distilled into the relevant [principles](../docs/principles.md) when it
recurs. Every UI change applies the [UX review checklist](../docs/principles.md#ux-review-checklist).
The loop is in
[the findings and principles process](../docs/notes/2026-11-15-findings-and-principles-process.md).
A repeated finding without a principle or regression test is a process failure.

## Responsive and mobile requirement

Every web interface is responsive and mobile friendly. The same templates serve phones, desktops,
and the future kiosk view; there is no separate mobile UI. The base layout keeps the viewport
declaration, uses fluid widths, keeps tables readable at narrow widths, keeps forms usable without
zoom, and gives controls touch-friendly targets.

The page shell fills the device width with only `--layout-gutter` as inset, and
`--layout-gutter-wide` at 80rem and above. There is no page maximum. Prose stays at
`--measure-prose`, help text at `--measure-hint`, controls at `--measure-control`, and tables use
the extra room or scroll inside `.table-wrap`; no table may widen the document. The header uses the
same full-width shell because it is navigation chrome for a dashboard, not prose. The live grid owns
the viewport on the live page, so its footer is omitted and its grid rows size to the available
height.

The browser check enforces the touch-target rule by role as failures: a control in a navigation, an
action row, or a whole-row target must reach 44 by 44 CSS pixels, and a control everywhere else
must reach the 24 by 24 floor and only warns from 24 to 43, so an inline prose link stays advisory.
Hit area counts, so a camera discovery candidate is measured by its whole label. Navigation and
action-row links carry the 44 target without changing the type scale; the brand, sign-out and
primary buttons reach 44. Inline links inside a sentence can stay below 44 because enlarging them
would distort the surrounding text. The roles and the current counts are in
[tests/browser](../tests/browser/README.md#touch-targets-by-role). Long unbreakable strings such as
the TLS certificate fingerprint wrap inside the footer instead of widening the page.

Every UI change is checked at phone width, around 360 to 390 CSS pixels, and at desktop width, at
least 1280 CSS pixels, before merge. `make ui-browser-check` measures 360 by 800, 390 by 844, 1280
by 800 and 1920 by 1080, writes one screenshot per route and width to the gitignored
`build/ui-screenshots/`, and is documented in [tests/browser](../tests/browser/README.md). The setup
token QR and short
code, login, wizard, status, updates, logs, support, reset, camera, and error screens all count. A
setup or control flow that requires copying a secret between devices is a defect unless no
reasonable alternative exists; the setup token QR exchange in
[BIT-019](../docs/plan/implementation-plan.md#bit-019-setup-token-qr-exchange) is the model; the QR
and fragment design is in
[the setup token QR note](../docs/notes/2026-11-15-setup-token-qr.md). The screen-by-screen acceptance
and screenshot requirement are in
[BIT-020](../docs/plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui).

## Vendored assets

HTMX is version 2.0.11, vendored as `static/htmx-2.0.11.min.js` with its license in
`static/htmx-2.0.11.LICENSE` (version and checksum checked against the vendored file on
2026-11-21). The setup fragment reader is `static/setup-fragment.js`, the slow-action busy state is
`static/form-busy.js`, and `static/reconnect-panel.js` shows the reconnecting notice after a failed
poll. All are local scripts allowed by the self-only CSP, and there is no inline script. The SHA-256
of the HTMX script is
`d6fdc75f204e6bdefa99b69bf1e6d4ac69b8a364f77929f45c13476b4000f717`. Bittern never loads it from the
network. Record the version and checksum in the app bundle manifest when that manifest exists.

`agent/scripts/sync-assets.sh` copies `templates/` and `static/` into the Go module before a build.
`make assets` runs the same step, and `make ui-test` runs the UI tests.

## Still missing

Real network and storage checks are wired to the status builder. Camera discovery is BIT-008.
The BIT-008 POC slice implements the layered lease, ARP, WS-Discovery, unauthenticated ONVIF device
and RTSP evidence, credential entry, the reservation and the managed camera list. Authenticated ONVIF
Media profiles and stream URIs, a bounded unicast sweep, re-probing, and the packet-capture
acceptance check remain open in BIT-008. The native camera UI proxy is a separate ticket; the list
does not wait for it. Camera capability controls (PTZ and presets, profile selection, event source
status, and unsupported-capability messages) are scoped in
[BIT-027](../docs/plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability).
Certificate rotation outside an identity reset, password recovery beyond a console reset, and a
support bundle are not part of this change.

Related: [UI skeleton note](../docs/notes/2026-11-14-ui-skeleton.md),
[status contract](../docs/status.md), [implementation plan](../docs/plan/implementation-plan.md),
[architecture](../docs/architecture.md), [ADR 0019](../docs/adr/0019-ui-stack.md),
[ADR 0034](../docs/adr/0034-live-view-owned-by-bittern.md).

## Live view

Live view follows [ADR 0034](../docs/adr/0034-live-view-owned-by-bittern.md). Slice 1a implements
`/ui/live` and `/ui/live/{camera}` as a single-camera view: the page reads the enabled cameras from
`appliance.yaml`, shows a simple picker when more than one exists, and keeps the player in a stable
node outside every HTMX swap region. A sibling line polls the camera's configuration state every 15
seconds. The player is never inside that poll, because a swap would destroy playback, fullscreen and
focus ([UX-14](../docs/principles.md#ux-14-live-updates-must-not-disturb-state-the-user-owns)).

The browser never receives an upstream URL, host, port, go2rtc stream name or camera address. It
asks for one of Bittern's own media routes:

| Route | Upstream |
| --- | --- |
| `/ui/live/media/{camera}/{profile}/ws` | go2rtc `/api/ws?src=<server-resolved stream>` |
| `/ui/live/media/{camera}/{profile}/stream.m3u8` | go2rtc `/api/stream.m3u8?src=<server-resolved stream>` |
| `/ui/live/media/{camera}/{profile}/hls/playlist.m3u8` | go2rtc `/api/hls/playlist.m3u8?id=<opaque id>` |
| `/ui/live/media/{camera}/{profile}/hls/init.mp4` | go2rtc `/api/hls/init.mp4?id=<opaque id>` |
| `/ui/live/media/{camera}/{profile}/hls/segment.m4s` | go2rtc `/api/hls/segment.m4s?id=<opaque id>&n=<segment>` |
| `/ui/live/media/{camera}/{profile}/hls/segment.ts` | go2rtc `/api/hls/segment.ts?id=<opaque id>&n=<segment>` |
| `/ui/live/media/{camera}/{profile}/snapshot.jpg` | Frigate loopback `/api/<camera>/latest.jpg` |

`bitternd` resolves the camera and profile against the rendered inventory, then opens exactly one
allowed upstream request. The proxy refuses absolute-form targets, traversal and encoded
separators, caller-supplied `src` or host values, unknown endpoints, non-GET methods and redirect
following. It drops the browser cookie and authorization header before the upstream call, sets
`Cache-Control: no-store` on every media response, and never logs a media path or query string.
Every media request passes the administrator session. A navigation without a session gets the login
page with a bounded return path; a fragment gets `HX-Redirect` to its parent page and media gets
`401` with no login HTML. A WebSocket revalidates the session every 30 seconds and closes when it
ends. The proxy keeps the hard nine-connection ceiling per session.

`/ui/live` is the grid. A layout is a preset of one, two, four or nine tiles plus a camera for each
tile; the server resolves it at render time from `bittern_live_layout`, a first-party, `HttpOnly`,
`Secure`, `SameSite=Strict` cookie scoped to `/ui/live` with a one-year lifetime. The cookie holds
the preset and camera IDs only. Unknown or duplicate cameras are dropped, more than nine tiles is
clamped to nine, and a missing or broken cookie falls back to automatic without an error. Automatic
shows one tile per enabled camera. Changing the preset or assigning a camera is an ordinary form
POST; with JavaScript a tile assignment replaces only that tile's island.

The page measures the viewport and applies the ADR 0034 ceiling: one active camera below 44rem,
four from 44rem to below 80rem, nine at 80rem and above. A saved layout above the ceiling starts
only the allowed tiles, leaves the rest as compact rows with their own actions, and explains the
limit in one sentence. The proxy keeps its own hard maximum of nine concurrent media sessions per
session, counted from held WebSockets and recently active HLS session ids; the browser cannot raise
it.

Every tile is a separate player island outside every HTMX swap region. A tile's sibling
camera-state line polls every 15 seconds and touches nothing else. The grid controller owns the
ceiling, the full-width selection and the one-tile replacement. Full width switches that tile to
`main`; leaving it switches back to `sub`. A failing camera keeps its own state sentence while the
others play.

`ui/static/live-player.js` owns each island's video element, MSE `SourceBuffer` and WebSocket. Its
states are connecting, playing, switching, stalled, reconnecting, idle (not started on this
screen), offline, unsupported and session ended. Recovery is bounded to three retries with visible
1, 2 and 4 second waits, then HLS where the browser supports it, then the snapshot with a retry
action. The current camera model has one RTSP path, so `main` and `sub` resolve to the same path and
the single-camera page says so. The profile control is a normal link without JavaScript and a
runtime switch with it.

The media area is an aspect box, not the video element's intrinsic size. It reserves 16:9 before
the first frame, and the video is contained inside it with `object-fit: contain`. The desktop grid
sizes its rows to the viewport, so nine tiles fit without page scrolling even at 1280 by 800. On a
phone the active tile fills the available area and the remaining tiles scroll inside the grid, not
the document. Each player also has a browser full-screen control. Full screen requests the player
root, keeps the media contained, hides document overflow, and exits through the control, Escape or
Back; the grid and the other streams keep their state.

## Boundaries

In scope:

- First-run and network setup, including confirmation of LAN and camera links
- Guided camera onboarding with discovery results and manual RTSP fallback
- Status, health, update progress, and update history
- Log viewing, redacted support bundle download, and reset flows with confirmation

Out of scope:

- Being the video wall: live view is a Bittern surface specified by ADR 0034; event review and
  recorded playback follow in BIT-024 and BIT-025. Frigate settings stay in Frigate's own UI, which
  is a support path, not a product surface.
- Owning or editing Frigate `config.yml` in Git-managed mode
- Direct root operations; all mutations go through the agent API
