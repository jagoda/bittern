# 0034. Live view owned by Bittern, composed from go2rtc and the Frigate API

*Bittern serves the live view on its authenticated 443 surface, using go2rtc for MSE and HLS media
and the Frigate API for snapshots and camera state, and never embeds the Frigate UI or its web
player.*

- Status: Accepted; grid, stream-quality and full-screen clauses superseded in part by
  [ADR 0037](./0037-nvr-grid-per-pane-quality-and-full-screen.md)
- Date: 2026-11-26
- Deciders: maintainer
- Related: [ADR-0003](./0003-frigate-as-nvr-engine.md),
  [ADR-0010](./0010-security-and-privacy-posture.md),
  [ADR-0016](./0016-bittern-owns-frigate-config.md),
  [ADR-0019](./0019-ui-stack.md),
  [ADR-0022](./0022-community-functionality-ux-wins.md),
  [ADR-0023](./0023-camera-native-ui-proxy.md),
  [ADR-0025](./0025-browser-check-for-the-responsive-ui.md),
  [ADR-0026](./0026-design-system-and-ui-review.md),
  [ADR-0027](./0027-flow-layer-above-patterns.md),
  [ADR-0037](./0037-nvr-grid-per-pane-quality-and-full-screen.md),
  [architecture](../architecture.md), [open questions](../open-questions.md),
  [implementation plan](../plan/implementation-plan.md), [UI README](../../ui/README.md),
  [upstream verification](../notes/2026-11-26-live-view-upstream-verification.md),
  [live refresh finding](../notes/2026-11-26-live-refresh-state-loss.md),
  [media surfaces](../notes/2026-11-14-media-surfaces.md)

## Context

[OQ-23](../open-questions.md) asks how live video reaches the browser. It has been open since
[ADR 0019](./0019-ui-stack.md) split the question out of the UI stack decision, and
[BIT-023](../plan/implementation-plan.md#bit-023-live-view) cannot implement a live page until it is
answered. The first-run milestone uses a snapshot, so nothing in the shipped wizard depends on the
answer today.

The appliance has no live view. The camera list can fetch one JPEG from the Frigate loopback API and
re-serve it under the administrator session, and the QEMU fixture serves one RTSP test pattern, but
there is no player, no media proxy and no go2rtc configuration. The route inventory, templates and
static assets contain no live route and no `<video>` element.

The constraints are already decided:

- [ADR 0016](./0016-bittern-owns-frigate-config.md) makes Bittern the only browser-facing control
  surface and the only writer of Frigate's `config.yml`. Frigate's UI is a loopback support path,
  not a product surface. `bitternd` uses Frigate's unauthenticated API on `127.0.0.1:5000`.
- [ADR 0022](./0022-community-functionality-ux-wins.md) says to use the community component as
  shipped, fix a UX gap with the smallest Bittern layer, and never fork, vendor a modified copy, or
  restyle the upstream UI. Bittern owns the product UX; Frigate owns the video work.
- [ADR 0019](./0019-ui-stack.md) fixes the browser surface at TLS on 443, server-rendered Go
  templates with HTMX, and a working no-JavaScript path. There is one browser listener. This ADR
  resolves the live-video follow-up that ADR 0019 left open; it does not change the UI stack or the
  TLS decision.
- [ADR 0003](./0003-frigate-as-nvr-engine.md) listed Frigate's HTTP API and UI as an available
  integration surface. [ADR 0016](./0016-bittern-owns-frigate-config.md) is later and more specific:
  it makes Frigate's UI a loopback support path and Bittern the only browser control surface, and
  [ADR 0022](./0022-community-functionality-ux-wins.md) says not to embed, restyle or fork it. This
  ADR applies that boundary. It does not reopen ADR 0003, ADR 0016 or ADR 0022.
- The firewall and renderer goldens allow TCP 443 on `lan0` only, plus DHCP and NTP on their
  segments. They drop 1984, 5000, 8554, 8555 and 8971 on `lan0` and `cam0`, and UDP 8555 as well.
  The Frigate container uses host networking and publishes no port. Frigate's config binds 5000 and
  8971 to `127.0.0.1`; go2rtc's 1984, 8554 and 8555 use upstream defaults and depend on the host
  firewall.
- Cameras live on the isolated `10.77.0.0/24` segment behind `cam0`. There is no route or forward
  from `lan0`, and camera ports are not published.
- The owner uses a phone and a desktop. Every browser surface must work at 360 and 390 CSS pixels
  and at 1280 and 1920 pixels, pass the [UX review
  checklist](../principles.md#ux-review-checklist), and avoid secrets in the browser.

The upstream facts were re-verified on 2026-11-26 against the pinned release and its bundled
go2rtc. The full table, quotes, URLs and access dates are in
[the upstream verification note](../notes/2026-11-26-live-view-upstream-verification.md). The facts
that decide this ADR are:

- The pinned release is Frigate 0.18.2, and its digest matches the GHCR image index. Frigate bundles
  go2rtc 1.9.14. go2rtc's HTTP API and web player default to port 1984, and localhost requests to
  that API bypass authentication.
- Frigate's live view uses jsmpeg without go2rtc, defaults to MSE when go2rtc is configured, and
  reserves WebRTC for cases that need it. go2rtc also offers HLS, MP4 and MJPEG.
- WebRTC has the lowest latency, but it needs port 8555 and a direct peer connection. go2rtc states
  that WebRTC "can't pass media data through your Nginx or Cloudflare or Nabu Casa HTTP TCP
  connection". MSE runs over TCP and arrives in order. HLS is the worst of the three for real-time
  streaming and exists for devices that do not support MSE, including iPhone Safari before 17.1.
- There are no default `main` and `sub` stream names. Streams are named keys under
  `go2rtc.streams`; `cameras.<name>.live.streams` maps a friendly name to a stream key, and the
  first entry is the default for Frigate's All Cameras dashboard. Frigate's own stream choice is
  per device and stored in browser local storage.
- Frigate's PTZ protocol is ONVIF. The documented movement commands are MQTT payloads on
  `frigate/<camera_name>/ptz` (`MOVE_<dir>`, `ZOOM_<dir>`, `preset_<name>`, `STOP`). The HTTP API
  exposes `GET /api/<camera_name>/ptz/info` for capability and presets, not movement. Product
  config disables MQTT.
- Frigate authentication defaults to on and a generated admin password is printed in the logs. The
  internal port 5000 is unauthenticated admin by design, and 8971 authenticates users unless auth is
  explicitly disabled. Snapshots and the camera list need no login on 5000.

One finding is outside the live view decision but must not be lost: 0.18.3, published 2026-11-19,
is a security release for CVE-2026-123007, an unauthenticated nginx HTTP/2 RCE as root. The pin is
one patch behind it. The advisory gate in [AGENTS.md](../../AGENTS.md) and
[ADR 0012](./0012-testing-and-release-gates.md) blocks a release until the pin moves in a separate,
tested bundle change. This ADR does not change the pin.

The product job is narrower than Frigate's dashboard. The owner wants to glance at the cameras,
open one, see it clearly, switch to a substream on a phone, and recover without reloading the
appliance. Detection review, event review and recorded playback are separate tickets. PTZ is a
later capability. Audio is undecided under OQ-21. The live view must fit the existing authenticated
surface rather than add a second one.

## Decision

### 1. Bittern owns the live view; Frigate and go2rtc supply the media

Bittern owns the live view page, the player controller, the layout data, stream selection, the
connection and failure states, the device limits, the authentication boundary, the snapshot
fallback and the media proxy. The product routes are `/ui/live` for the grid and
`/ui/live/{camera}` for the single-camera view, both behind the administrator session.

Bittern reuses these upstream components and endpoints:

| Reused component or endpoint | What Bittern uses it for |
| --- | --- |
| go2rtc inside the pinned Frigate container | RTSP ingest, restream and the media packet path. Bittern does not run a second go2rtc. The pinned 0.18.2 image bundles 1.9.14. |
| go2rtc WebSocket API `/api/ws?src=<stream>` | MSE fMP4 over WebSocket, including the client's WebRTC or MSE signalling. Bittern uses the MSE path only. |
| go2rtc HLS endpoint `/api/stream.m3u8?src=<stream>` | HLS fallback for browsers that cannot play MSE, especially iPhone Safari before 17.1. |
| go2rtc stream info `/api/streams` | Server-side readiness and codec checks. The browser never calls it. |
| Frigate `GET /api/<camera_name>/latest.jpg` on `127.0.0.1:5000` | The snapshot fallback and the tile poster. This is the endpoint the camera list already uses. |
| Frigate `GET /api/<camera_name>/ptz/info` on `127.0.0.1:5000` | PTZ capability and preset discovery for the later PTZ control. No movement command uses it. |
| Frigate's rendered `go2rtc.streams` and `cameras.<name>.live.streams` | The stream definitions and the friendly names for the support UI. Bittern's renderer owns the block. |
| Frigate's `config.yml`, Compose project and container | The engine, the media pipeline and the existing lifecycle. No fork, patch or sidecar. |

Bittern does not reuse Frigate's UI, go2rtc's web player (`/`, `/stream.html`, `/webrtc.html`),
Frigate's `/live/mse/api/ws` or `/live/webrtc/api/ws` nginx paths, jsmpeg, or the Frigate dashboard.
The `/live/...` paths are UI implementation detail, not a documented API, and the web players are
diagnostic surfaces with their own look, navigation and assumptions. The live page is a Bittern page;
the player is a Bittern-owned controller speaking go2rtc's documented protocol.

The camera list comes from `appliance.yaml` and the status document, not from Frigate's
`GET /api/config`. Bittern is the single writer of the camera inventory
([ADR 0016](./0016-bittern-owns-frigate-config.md)); `/api/config` is a diagnostic endpoint and
contains a large amount of configuration the live page does not need.

Every media request is authenticated before any upstream connection. The browser asks for a camera
and a profile, never for a URL, host, port or go2rtc stream name. `bitternd` resolves the camera and
profile against the rendered config and its own allowlist, then opens exactly one upstream
connection. There is no arbitrary path, no request-controlled `src`, no redirect following, no
proxy `CONNECT`, and no DNS resolution from request input. Media responses carry
`Cache-Control: no-store`; no media bytes are written to disk, logs, the status document or the
support bundle.

### 2. Transport is MSE first and HLS fallback; WebRTC is not exposed in v1

The player tries MSE over the authenticated WebSocket proxy. If the browser cannot play MSE, it
falls back to HLS over the authenticated HTTP proxy. If neither starts, it shows the snapshot with
a named failure and a retry action.

MSE is the default because it is Frigate's default with go2rtc, it runs over TCP, every frame
arrives and is decoded in order, it needs no STUN or public relay, and it passes through an HTTP
reverse proxy on the existing 443 listener. HLS is the fallback because it is the only option on
older iPhone Safari, even though it has the worst latency and startup behavior of the three. WebRTC
is rejected for v1: it needs 8555, candidate configuration, and a direct peer connection that
go2rtc documents as unable to pass through an HTTP proxy. Proxying WebRTC would require a TURN or a
custom relay, which is a second media server and a new security surface.

The consequences are explicit:

- Port 8555 stays closed on `lan0` and the tailnet. No STUN, TURN, public relay or cloud dependency
  is added. The default data flow stays local.
- 1984, 5000, 8554 and 8971 stay loopback-only and firewall-dropped. No new listener or forward
  rule is added.
- The MSE and HLS proxies both terminate on the existing 443 listener and the existing
  administrator session. There is no separate media listener, hostname or certificate.
- The player is muted and has no audio control in v1. Audio remains undecided under
  [OQ-21](../open-questions.md); a camera stream that carries audio is played with the audio track
  ignored.
- H.264 is the supported video codec. H.265-only cameras get a named unsupported state and the
  snapshot, not a silent transcode. Transcoding is a CPU cost and a separate product decision.
- The startup budget is provisional: first frame at p95 within 5 seconds for MSE and 10 seconds for
  HLS on the house LAN, measured by BIT-023 on the QEMU fixture and the first hardware run. If the
  budget is missed, BIT-023 tunes the buffer, the substream and the proxy, and records the measured
  result. It does not raise the device ceiling or reopen WebRTC without evidence.

The player has named states: connecting, live, switching stream, reconnecting, snapshot fallback,
camera offline, unsupported codec, and session ended. Reconnection uses bounded backoff and never a
page reload loop. A missing camera, a removed camera, an expired session or a failed upstream closes
the media connection and leaves a sentence with the next action.

### 3. Player isolation: no player inside an HTMX swap region

**Rule.** No `<video>`, `<audio>`, player-owned DOM node or MSE `SourceBuffer` may be mounted inside
an element that HTMX can swap. The player controller must not open a media WebSocket whose lifetime
is tied to a swappable region. The player container is a stable node rendered once by the server.
It is never the target of `hx-get`, `hx-post`, `hx-target` or `hx-swap`, and it is never a descendant
of an element that carries those attributes. Only a sibling status or metadata region may poll.

**Mechanism.** The live template puts the player in a dedicated container outside every HTMX
region. A template guard test fails if a player marker appears inside an element with an HTMX swap
attribute or inside a known polled region such as `#status-panel` or `#operation-panel`. At runtime,
the player controller refuses to mount if `closest('[hx-get],[hx-post],[hx-target],[hx-swap],
[hx-trigger]')` finds an ancestor. The controller owns the video element, the media source and the
connection; it updates the tile in place when the camera or profile changes. `hx-preserve` is not
the mechanism. It preserves only the nodes it marks, still runs the swap lifecycle, and cannot save
a player when the swap target is an ancestor.

**Why.** [The live refresh finding](../notes/2026-11-26-live-refresh-state-loss.md) records that the
status and operation polls replace every child of their panel with `innerHTML`. Open disclosures,
focus, selection and table scroll are lost on every poll. A video element in that region would be
destroyed and recreated on the poll, stopping playback, dropping fullscreen and Picture-in-Picture,
and reconnecting on a timer. The finding's candidate principle is
[UX-14](../principles.md#ux-14-live-updates-must-not-disturb-state-the-user-owns): a live update may
change server-owned data, but it must not close a disclosure, move focus, change scroll or
selection, or discard state the owner controls. The player is owner state in the strongest sense.
The live page may poll the camera list or a status line, but the player region is outside the poll.

### 4. Layout is data, resolved at render time and remembered per device

A layout is a preset plus a per-tile camera assignment:

| Field | Meaning |
| --- | --- |
| `preset` | `auto`, `1`, `2`, `4` or `9`. `auto` means the viewport default. |
| `tiles` | An ordered list of camera IDs, one per tile, up to the preset's count. An empty tile is allowed and renders a "Choose camera" action. |

The server parses the layout from a first-party, non-secret, per-device cookie, validates it against
the current camera inventory, drops unknown or duplicate camera IDs, caps the list at 9, and renders
the concrete tile list with camera names and stream labels. An invalid or missing cookie falls back
to `auto` without an error. The cookie contains only the preset and camera IDs. It contains no
camera address, credential, stream URL or owner name. It is `Secure`, `HttpOnly`,
`SameSite=Strict`, `Path=/ui/live`, and has a one-year lifetime. It is per device and is never
synced to another browser or to the administrator account.

The default is chosen by viewport, not by a stored preference alone. With no cookie, `auto` is a
render-time layout: the server emits the camera assignment and the CSS grid chooses one column at
phone width, two at tablet width and three at desktop width, so no stored preset decides it before
the viewport does. The controller then starts only the tiles the viewport can show and the device
ceiling allows. A stored preset is a request, not a guarantee: on a smaller viewport it is clamped
to that device's ceiling, and the page says so in one sentence rather than silently starting too
many streams. A stored `9` on a phone becomes one active tile; the remaining tiles are not started,
and the page explains the clamp rather than showing nine posters.

Layout changes are ordinary form submissions. The owner chooses a preset, assigns a camera to a
tile, or clears a tile, and the POST sets the cookie and redirects to the same page. That path works
without JavaScript and never swaps the player region. A JavaScript drag-and-drop enhancement may be
added later, but it is not required for v1 and it must not become the only way to change the layout.
The server is the only writer of the cookie and the only resolver of the tile list.

### 5. Stream selection per tile: main for full screen, sub for grid, switchable at runtime

Each camera has two named profiles in the Bittern model: `main` and `sub`. Grid tiles use `sub`.
The single-camera route and the fullscreen view use `main`. Entering fullscreen from a grid tile
switches that tile to `main`; leaving fullscreen returns it to `sub` unless the owner chose
otherwise. A per-tile menu may override the choice to `main` in the grid or `sub` in full screen,
and the tile says which profile is playing.

The switch happens at runtime through the player controller. The controller closes the current
media connection and opens the same authenticated proxy route with the other profile. The page does
not reload and the player container is not swapped. The tile keeps its geometry, shows a short
"Switching to main stream" or "Switching to sub stream" state, and resumes. The server resolves the
profile to a go2rtc stream name from the rendered config, so the browser never sees the stream name
or a camera URL.

The data model has to support this. The current schema carries one `rtsp_port` and one `rtsp_path`
per camera, and the renderer emits one direct RTSP input. BIT-023 adds the `go2rtc.streams` block and
the `cameras.<name>.live.streams` mapping, plus the smallest schema change for a named second
stream. Until BIT-027 collects real ONVIF profiles, `main` and `sub` both resolve to the single
configured path and the UI says "one stream". A camera with only one stream can still be viewed; the
profile menu is absent rather than offering a false choice.

If the requested profile is unavailable, the player falls back to the other profile and labels the
fallback. It never silently plays `main` in a grid tile, because that would spend the owner's
bandwidth and decode budget without saying so.

### 6. PTZ boundary: design the controls now, build them only when a camera proves them

The PTZ control surface is specified in this ADR and in the BIT-023 design work, but it is not built
in BIT-023. It is built in BIT-027 only after a real PTZ camera verifies continuous move, stop,
zoom, focus where supported, presets, and the stop behavior on every failure path below. A camera
without a verified PTZ capability has no PTZ control at all; the tile does not show a disabled pad
or a teaser.

The control surface is a directional pad, zoom in and out, a preset list, and a visible Stop
control. Movement is hold-to-move: pointer down starts, pointer up stops. There is no persistent
"moving" toggle and no mode that keeps the camera moving after the owner's hand leaves the control.
Presets are discrete moves, not continuous movement. Click-to-move and drag-to-zoom are not in v1.

The safety rules are part of the control contract, not a later polish pass:

- A continuous move starts only on a pointer press or an equivalent keyboard press, and a
  `pointerup`, `pointercancel` or `lostpointercapture` always sends Stop.
- Stop is also sent on window `blur`, `visibilitychange` to hidden, `pagehide`, `beforeunload`, a
  route change, a player disconnect, a stream loss, a camera removal, and session expiry.
- The server holds a short move lease, one second by default. The client renews it while the control
  is held. If the lease expires, the server sends Stop without waiting for the browser.
- The server serializes commands per camera, rejects commands for a camera that is not the active
  single-camera view, and rejects commands while the stream is not live. PTZ is never available in a
  grid tile.
- The Stop control is always reachable while a move is active and is the first control in the
  keyboard order. Escape also stops.
- If the command path or the stream is uncertain, the result is Stop, not "try again later". A
  failed Stop is shown as a named failure with the camera and the next action.

The transport is not decided. Frigate's documented movement commands are MQTT, and product config
sets `mqtt.enabled: false`. The HTTP `ptz/info` endpoint returns capability and presets, not
movement. go2rtc has no PTZ API; its ONVIF source returns RTSP and snapshot URLs. BIT-027 must choose
between a local MQTT broker behind Frigate, an ONVIF client in `bitternd`, a future documented
Frigate HTTP command, or no PTZ at all. That choice is [OQ-39](../open-questions.md). The docs do not
promise that a camera behind go2rtc accepts ONVIF commands, so BIT-027 must test the exact camera,
firmware and stream topology. Until then, PTZ is not shipped.

### 7. Device limits are a product decision, not a user preference

| Device class | Viewport | Maximum active tiles | Reasoning |
| --- | --- | --- | --- |
| Phone | below 44rem (704 CSS pixels) | 1 | A grid tile at 360 to 390 pixels is too small to identify a person or a plate. One active decoder keeps heat and battery within a phone's budget. Full screen uses `main`; the grid is a camera picker, not a wall. |
| Tablet and small laptop | 44rem to below 80rem (704 to 1280 CSS pixels) | 4 | A 2 by 2 grid leaves each tile about 330 pixels wide at 704 and about 620 at 1280. Four sub-stream decoders fit mid-range hardware. Nine would make the tiles smaller than the face they are meant to show and add five decoders for no usable detail. |
| Desktop | 80rem (1280 CSS pixels) and above | 9 | A 3 by 3 grid leaves each tile about 400 pixels wide at 1280 and more at 1920. We choose nine concurrent H.264 sub-stream decoders as the practical ceiling for the mid-range desktop this appliance targets. Sixteen would add seven decoders and roughly double the LAN bitrate while shrinking the tiles. |

The hard maximum is 9 active media connections per session. The proxy enforces that maximum itself;
the device table is the UX ceiling and the player enforces it before opening a connection. A client
cannot raise the ceiling by sending a different value. The owner can choose fewer tiles, and the
preset clamp explains why a larger saved layout is not fully active on this device. There is no
`appliance.yaml` setting for the ceiling, no query parameter and no hidden preference. Changing a
ceiling is a product decision with new evidence, tests and a design review, not a setting the owner
is asked to understand.

The viewport is measured when the page opens and when it crosses a class boundary. A resize that
lowers the class stops the excess players in a bounded order and leaves their tiles as posters with
a sentence. A resize that raises the class does not auto-start new players; the owner can start them.
This avoids a window drag causing a burst of decoders.

### 8. Security, privacy and failure boundaries

The live view keeps the existing security posture and adds no new listener:

- Every route, including every HLS segment, init segment, key request and WebSocket upgrade,
  requires the administrator session and a valid CSRF token where the request mutates state.
  Unauthenticated navigation gets the login page with a bounded return path; unauthenticated media
  gets 401 and no bytes.
- The proxy revalidates the session for long-lived WebSocket connections at a bounded interval and
  closes the connection on logout, session expiry, password change, camera removal, or a disabled
  camera. The HLS handler revalidates on every playlist and segment request.
- The proxy has a per-camera and per-session connection limit, a bounded read buffer, and
  backpressure. It does not buffer a whole segment or a whole stream in memory, and it does not log
  request paths or query strings, because a camera URL can carry a token.
- The browser DOM contains camera names, tile positions, profile labels and state sentences. It
  does not contain RTSP URLs, camera credentials, Frigate credentials, go2rtc stream names, session
  identifiers in a URL, or a direct 5000, 8971, 1984, 8554 or 8555 URL.
- The CSP keeps `media-src 'self'`, `connect-src 'self'`, and `frame-ancestors 'none'`. No
  third-party player, CDN, font or analytics script is added. There is no service worker and no
  media cache.
- The default data flow is local. No STUN, TURN, public relay, cloud transcoder or vendor account is
  added. A future remote path that needs STUN is a separate decision with a written data flow and an
  explicit opt-in, as [ADR 0010](./0010-security-and-privacy-posture.md) requires.
- The snapshot fallback uses the existing Frigate `latest.jpg` fetch on loopback. It is one JPEG,
  bounded in size, authenticated, `no-store`, and never written to disk. It is a fallback, not a
  second live path.
- Frigate's unauthenticated 5000 API and go2rtc's unauthenticated localhost API remain loopback
  surfaces. The live view proxy must not become a generic SSRF bridge to them. It only reaches the
  exact camera and profile in the rendered inventory.

The failure states are owner-facing and named: the camera is offline, the stream is unsupported,
the proxy is unavailable, the session ended, the device limit was reached, and the player could not
start. Each state says what happened, what is still working, and the next action. A failed live view
does not degrade the whole appliance status; it reports the camera section, like the existing
snapshot check.

### 9. Scope boundary and the open advanced view

This ADR decides the live view only. It does not decide event review, recorded playback, the
timeline, camera configuration, PTZ commands, audio, the kiosk or a Frigate UI proxy. Those remain
in BIT-024, BIT-025, BIT-026, BIT-027, OQ-21, OQ-25, OQ-38 and OQ-39.

The Frigate UI remains a deliberate support and diagnostic path on loopback, reachable through the
console or the tailnet procedure in [ADR 0016](./0016-bittern-owns-frigate-config.md) and
[BIT-022](../plan/implementation-plan.md#bit-022-tailscale-remote-access-and-frigate-ui-support-path).
It is not linked from the live page and it is not a fallback for a live view failure.

[OQ-38](../open-questions.md) records the unresolved question of a gated advanced view that proxies
Frigate's own UI for the long tail. No default is chosen. The trade-off is recorded there: a proxy
would reach settings and diagnostics that Bittern does not plan to build, but it would add a second
UI, couple Bittern to Frigate's frontend and authentication, and risk reopening the single-writer
rule if it can change `config.yml`. Any such proxy needs a new ADR that supersedes the exposure
clause of [ADR 0016](./0016-bittern-owns-frigate-config.md) and keeps the one-writer rule.

## Consequences

### Positive

- The owner gets live video on the existing 443 surface, with the existing login, certificate and
  firewall rule. There is no second listener, no 8555 exception and no camera port on `lan0`.
- MSE gives near-real-time playback over TCP without STUN, and HLS covers older iPhone Safari. The
  fallback is a transport decision the player can make, not a page the owner has to understand.
- Bittern keeps one control surface and one writer, as [ADR 0016](./0016-bittern-owns-frigate-config.md)
  and [ADR 0022](./0022-community-functionality-ux-wins.md) require. Frigate and go2rtc do the media
  work; Bittern does the product layer.
- The player cannot be destroyed by a status poll. The rule is structural and testable, so a later
  live update cannot regress the video by adding a poll around it.
- Layout is data, so the server can render the correct tiles on first byte and the owner can move
  between phone and desktop without copying a layout. The viewport default prevents a phone from
  inheriting a desktop wall.
- Device ceilings are explicit product decisions with reasoning. They bound the proxy load, the
  browser decode load and the LAN bitrate before a field failure sets them by accident.
- PTZ safety is designed before any PTZ code exists. The stop contract covers pointer release,
  blur, navigation, stream loss and session loss, and the server lease covers a browser that
  disappears.
- The facts, the 0.18.3 security release and the unconfirmed PTZ behavior are recorded rather than
  assumed, so the next ticket starts from evidence.

### Negative / costs

- The media proxy is new, security-sensitive code in `bitternd`: WebSocket upgrades, HLS segment
  authorization, session revalidation, per-connection limits, backpressure and close-on-logout. It
  needs focused tests, review and load evidence.
- The Bittern player controller is new browser code. It must speak go2rtc's protocol, manage an MSE
  `SourceBuffer`, handle HLS fallback, recover from a dropped socket and work with the no-JavaScript
  snapshot fallback. That is more code than a native `<video src="...m3u8">`, and it can drift if
  go2rtc changes its protocol.
- MSE is not universal. iPhone Safari before 17.1 gets HLS and its worse latency. Firefox is H.264
  only. H.265-only cameras are unsupported and show a snapshot, which may be a real field gap.
- WebRTC is not available in v1. If MSE fails the startup or latency budget on real hardware, the
  product has no lower-latency option without a new ADR and a new proxied or direct path.
- The device ceilings are conservative. A powerful desktop is limited to 9 tiles, and a phone is
  limited to 1 active tile even when the owner wants a quick 4-up glance. The ceiling is a product
  guarantee, not a benchmark of the owner's hardware.
- The per-device cookie is lost in private browsing, after a cookie clear, or on a new device. The
  layout resets to `auto`; there is no account-level sync.
- Layout changes are form submissions and a page load. The player reconnects on a layout change.
  That is acceptable for a rare action but less fluid than a drag-and-drop wall.
- PTZ is unavailable until BIT-027, which may need a local MQTT broker or an ONVIF client. The
  documented command path does not exist in the current product config.
- The renderer change is larger than a page. It adds `go2rtc.streams`, the live mappings and a
  schema migration, and it touches the Frigate goldens and the first-boot path.
- The pinned 0.18.2 is behind the 0.18.3 security release. No release can ship on the current pin;
  the bump is a separate bundle change with its own verification.
- The live view depends on `bitternd` for both the page and the media. A daemon restart stops
  playback, and [OQ-37](../open-questions.md) still owns session continuity across that restart.
- The proxy is a single point of failure for all tiles. There is no direct go2rtc fallback in the
  browser by design, because a direct fallback would bypass authentication and the firewall.

### Follow-ups

- [BIT-023](../plan/implementation-plan.md#bit-023-live-view) implements the grid and single-camera
  routes, the MSE and HLS proxy, the snapshot fallback, the layout cookie, the device ceilings, the
  player isolation guard and the authentication tests. It adds the routes to the browser check
  manifest under [OQ-29](../open-questions.md).
- BIT-023 adds the `go2rtc.streams` block and the `cameras.<name>.live.streams` mapping to the
  renderer, with golden tests. It adds the smallest schema change for a named second stream and a
  migration test. Until BIT-027 supplies real profiles, `main` and `sub` resolve to the same path.
- BIT-023 updates the stale `OQ-03` comment in the nftables template and the goldens, and keeps 8555
  closed. It adds the no-8555 listener assertion to the QEMU firewall test.
- BIT-023 adds a media fixture or a QEMU path that proves the proxy and player, not only RTSP
  reachability. The current virtual camera is RTSP only and is not a camera compatibility test.
- BIT-023 adds the live view pattern and player component to the design system, and walks the phone,
  desktop, keyboard and no-JavaScript states with the [ui-design
  skill](../../.pi/skills/ui-design/SKILL.md).
- [BIT-024](../plan/implementation-plan.md#bit-024-event-list-and-review) and
  [BIT-025](../plan/implementation-plan.md#bit-025-playback-and-timeline) reuse the authenticated
  media proxy, the player isolation rule and the no-secret browser boundary. They do not reuse the
  live player as a recorded-playback player and they do not call Frigate's UI.
- [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability)
  implements PTZ after a real camera verifies movement, stop, zoom, presets and stop-on-loss. It
  chooses the command path in [OQ-39](../open-questions.md) and collects the main and sub profiles
  that BIT-023 provisionalized.
- Move the Frigate pin to 0.18.3 or later in a separate bundle change, with the digest, renderer,
  goldens, notices and release tests. The advisory gate blocks a release until this is done.
- Revisit [OQ-38](../open-questions.md) after BIT-023 to BIT-026, with evidence from the support
  path. Do not proxy the Frigate UI before that decision.
- Measure the MSE and HLS startup, steady-state latency, CPU, memory and LAN bitrate on the QEMU
  fixture and the first hardware run. Record the result in a dated note and tighten or relax the
  provisional budget with evidence.
- Revisit the device ceilings when measured decode data exists. A change is a product decision with
  tests, not a user setting.

## Alternatives considered

- **Embed the Frigate UI in an iframe or link to 8971.** Rejected: it contradicts
  [ADR 0016](./0016-bittern-owns-frigate-config.md) and
  [ADR 0022](./0022-community-functionality-ux-wins.md), adds a second login and a second visual
  language, breaks framing and mobile behavior, and cannot compose a per-tile Bittern layout. It
  also puts a large configuration UI behind the administrator session.
- **Proxy the Frigate UI under the Bittern session as the live view.** Rejected for v1: it couples
  Bittern to Frigate's frontend, authentication and release cadence, adds the whole config UI to the
  browser surface, and risks a second writer to `config.yml`. The long-tail version is
  [OQ-38](../open-questions.md), not a default.
- **Direct WebRTC on 8555 with go2rtc authentication.** Rejected: it adds a second listener and a
  firewall exception, requires candidates and possibly STUN, cannot pass through the authenticated
  443 proxy, and makes mobile and NAT behavior part of the product. It also conflicts with the
  current firewall goldens.
- **WebRTC proxied through `bitternd` with a TURN or custom relay.** Rejected: it adds a second media
  server, a relay protocol and a new security surface, and duplicates go2rtc's job. It is more code
  than MSE for a latency gain that has not been shown to be necessary.
- **HLS only.** Rejected as the primary transport: it is the worst of the three for real-time
  streaming, has the highest startup and latency, and would make the live view feel delayed. It
  remains the fallback for older iPhone Safari.
- **Snapshots only in v1.** Rejected: it does not meet the live view job. It remains the first-boot
  fallback and the last-resort state when no transport starts.
- **jsmpeg.** Rejected: it is capped at the detect frame rate and 720p, has no audio and is a
  lower-quality fallback for installations without go2rtc. Bittern will have go2rtc.
- **Use Frigate's `/live/mse/api/ws` or `/live/webrtc/api/ws` nginx paths.** Rejected: they are
  UI-internal, not documented as an API, and tied to Frigate's own auth and frontend. Use go2rtc's
  documented `/api/ws` and proxy it with Bittern's own session and allowlist.
- **Expose go2rtc 1984 or Frigate 5000 to the LAN.** Rejected: both are unauthenticated local admin
  surfaces, and 1984 bypasses even configured auth on localhost. The firewall and
  [ADR 0016](./0016-bittern-owns-frigate-config.md) already reject this.
- **Let the browser choose the go2rtc `src` directly.** Rejected: it would let the browser select
  any stream, including a camera it should not see, and it would expose internal stream names. The
  server resolves camera and profile to a fixed upstream.
- **Build a custom WebRTC stack or a second go2rtc.** Rejected: it duplicates a maintained community
  component, adds a second media server to patch, and violates
  [ADR 0022](./0022-community-functionality-ux-wins.md).
- **Store the layout on the administrator account.** Rejected: one account serves several devices,
  so a phone layout would follow the owner to a desktop and a desktop wall would follow them to a
  phone. The layout is per device, and the viewport still clamps it.
- **Let the owner set the tile ceiling.** Rejected: it asks a non-technical owner to reason about
  decoders and bandwidth, and it lets a phone start nine streams. The ceiling is a product decision
  with reasoning, not a preference.
- **Ship PTZ in BIT-023.** Rejected: no PTZ camera has verified the behavior, the documented command
  path is MQTT and product MQTT is disabled, and an unverified continuous move can leave a camera
  moving. The control surface is designed now and built in BIT-027.
- **Use the camera's native UI for live view through the ADR 0023 proxy.** Rejected: the camera UI is
  for camera-only settings, not a multi-camera product surface, and it would give up the Bittern
  layout, stream selection and device limits. ADR 0023 remains for camera native settings.
- **Do nothing and keep the tailnet Frigate UI as the live path.** Rejected: it is a support
  procedure, needs a tailnet client, and does not give the owner a live view on the house LAN. It
  also contradicts the product goal in [ADR 0002](./0002-product-scope-and-vision.md).

## References

- [ADR-0002: Product scope and vision](./0002-product-scope-and-vision.md)
- [ADR-0003: Frigate as NVR engine](./0003-frigate-as-nvr-engine.md)
- [ADR-0010: Security and privacy posture](./0010-security-and-privacy-posture.md)
- [ADR-0016: Bittern owns the control surface and the Frigate config](./0016-bittern-owns-frigate-config.md)
- [ADR-0019: UI stack and TLS](./0019-ui-stack.md)
- [ADR-0022: Community functionality, UX wins](./0022-community-functionality-ux-wins.md)
- [ADR-0023: Camera native UI through an authenticated proxy](./0023-camera-native-ui-proxy.md)
- [ADR-0025: Browser check for the responsive UI](./0025-browser-check-for-the-responsive-ui.md)
- [ADR-0026: Design system and UI review](./0026-design-system-and-ui-review.md)
- [ADR-0027: Flow layer above patterns](./0027-flow-layer-above-patterns.md)
- [Architecture](../architecture.md)
- [Open questions](../open-questions.md): OQ-21, OQ-23, OQ-25, OQ-29, OQ-37, OQ-38, OQ-39
- [Implementation plan](../plan/implementation-plan.md): BIT-023, BIT-024, BIT-025, BIT-027
- [UI README](../../ui/README.md)
- [Upstream verification, 2026-11-26](../notes/2026-11-26-live-view-upstream-verification.md)
- [Live refresh state loss, 2026-11-26](../notes/2026-11-26-live-refresh-state-loss.md)
- [Media surfaces, 2026-11-14](../notes/2026-11-14-media-surfaces.md)
- [Principles, UX-14](../principles.md#ux-14-live-updates-must-not-disturb-state-the-user-owns)
- [QEMU harness](../../tests/qemu/README.md)
- Frigate 0.18.2 live view documentation:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/live.md>
- Frigate 0.18.2 restream documentation:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/restream.md>
- Frigate 0.18.2 authentication documentation:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/authentication.md>
- Frigate 0.18.2 PTZ and camera documentation:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/configuration/cameras.md>
- Frigate 0.18.2 MQTT documentation:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/docs/integrations/mqtt.md>
- Frigate 0.18.2 API schema:
  <https://github.com/blakeblackshear/frigate/blob/v0.18.2/docs/static/frigate-api.yaml>
- go2rtc 1.9.14 README: <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/README.md>
- go2rtc 1.9.14 API documentation: <https://github.com/AlexxIT/go2rtc/blob/v1.9.14/api/README.md>
- Frigate 0.18.2 release: <https://github.com/blakeblackshear/frigate/releases/tag/v0.18.2>
- Frigate 0.18.3 security release: <https://github.com/blakeblackshear/frigate/releases/tag/v0.18.3>
