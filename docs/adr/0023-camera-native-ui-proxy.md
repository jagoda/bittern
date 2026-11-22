# 0023. Camera native UI through an authenticated proxy

*The managed-camera listing gains an "Open native UI" action, and each camera's own web UI is served
through a per-camera hostname on the Bittern TLS listener behind the administrator session and a
scoped authenticated proxy.*

- Status: Accepted
- Date: 2026-11-15
- Deciders: maintainer
- Related: [ADR-0005](./0005-dual-nic-isolated-camera-network.md),
  [ADR-0009](./0009-remote-access-via-tailscale.md),
  [ADR-0010](./0010-security-and-privacy-posture.md),
  [ADR-0016](./0016-bittern-owns-frigate-config.md),
  [ADR-0019](./0019-ui-stack.md),
  [ADR-0022](./0022-community-functionality-ux-wins.md),
  [architecture](../architecture.md), [open questions](../open-questions.md),
  [UI README](../../ui/README.md), [implementation plan](../plan/implementation-plan.md),
  [BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0),
  [clean-install runbook](../runbooks/gk41-clean-install.md),
  [media surfaces note](../notes/2026-11-14-media-surfaces.md),
  [kiosk note](../notes/2026-11-15-kiosk-mode.md)

## Context

Bittern owns the appliance control surface and the camera inventory. Frigate is the NVR engine and
its UI remains a loopback support path, not a product surface
([ADR-0016](./0016-bittern-owns-frigate-config.md)).
[BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0) owns camera
discovery, adoption, and the managed-camera listing with its edit, test, and snapshot actions. That
listing covers the Bittern-managed camera settings. It does not cover settings that exist only in
the camera firmware: image and exposure, motion zones, ONVIF and network options, PTZ presets, local
storage, and firmware update.

Some of those settings are needed during installation and support. Today the only paths are the
opt-in Tailscale subnet route or a temporary cable
([ADR-0005](./0005-dual-nic-isolated-camera-network.md),
[ADR-0009](./0009-remote-access-via-tailscale.md)). Both are support procedures. Neither is an
everyday action for an owner on a phone or a laptop on the house LAN.

The camera segment is deliberately isolated. Cameras live on `10.77.0.0/24` behind `cam0`, with no
route to `lan0`, no forwarding, and no camera port open on the LAN. Publishing camera web ports on
`lan0` or adding a forward rule would undo that isolation and expose old camera firmware to every
device on the house LAN. The proxy must terminate on the appliance and connect to `cam0` as an
appliance-originated client.

A path-prefix proxy is not enough. Camera web UIs commonly use root-relative and absolute-path URLs
for static assets, XHR endpoints, redirects, cookies, and WebSocket upgrades. Under
`https://appliance/camera/<id>/`, a request for `/js/app.js` or a redirect to `/login` leaves the
prefix and breaks. Rewriting response bodies to repair every URL is fragile and camera-specific.

The appliance already has one browser listener on TCP 443 with TLS, a self-signed certificate, a
server-side administrator session, CSRF protection, and rate limiting
([ADR-0019](./0019-ui-stack.md)). A second listener, port, or proxy process would add a second
authentication boundary and another firewall exception. The new surface must reuse the existing
listener and session.

The proxy keeps Bittern as the only browser-facing surface under
[ADR-0016](./0016-bittern-owns-frigate-config.md): the camera UI is served through the Bittern
listener under the administrator session, not as a second listener or a camera port on the LAN. It
does not supersede [ADR-0005](./0005-dual-nic-isolated-camera-network.md); the camera isolation
rules are unchanged.

Camera firmware is untrusted. Its UI may use its own login, cache aggressively, frame itself, load
inline script, open WebSockets, return absolute redirects, or break without notice. The proxy has to
treat every response as untrusted, keep the camera origin separate from the administrator origin,
and fail closed when the session or the upstream is not valid.

The camera's web login is separate from the RTSP credential in `/etc/bittern/secrets.yaml`. The
proxy must not read, store, or replay either secret. The camera login remains a second,
independent authentication step.

The Tailscale subnet route remains the support path. A future kiosk may open a camera UI on the
appliance's own display over `cam0`, but that is deferred ([OQ-25](../open-questions.md),
[kiosk note](../notes/2026-11-15-kiosk-mode.md)) and is not part of this decision.

## Decision

**1. BIT-008's managed-camera listing gains one action, "Open native UI".**

- The managed-camera listing from
  [BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0) owns the
  camera list, edit, test, and snapshot actions. This ADR does not introduce, restyle, or change
  that listing. It adds the native-UI action to each camera row or detail view that has a known web
  endpoint.
- The action is a CSRF-protected POST on the administrator host. It mints a short-lived, single-use
  grant bound to the administrator session and the camera, then redirects to the camera host. It
  works without JavaScript, is keyboard reachable, and has a visible disabled state with a reason
  when the camera has no web endpoint.
- The action never links to a camera IP, a camera port on `lan0`, or a path prefix.

**2. The proxy is part of the Bittern UI on the existing 443 listener.**

- `bitternd` serves it on the same `http.Server`, TLS certificate, firewall rule, and session store
  as the administrator UI. The proxy itself adds no new listener, port, process, or reverse-proxy
  dependency. It uses Go's `net/http/httputil.ReverseProxy` and standard library session
  primitives.
- Host-based routing sends the administrator host to the UI and each camera host to only its proxy.
  A camera host does not serve `/ui`, the agent API, or another camera's upstream.
- Every request is authenticated before any upstream connection. Navigation without a grant gets
  the administrator login page with a bounded return path; XHR, static asset, and WebSocket
  requests get 401. No unauthenticated response contains camera content.
- The administrator session cookie is not shared with camera hosts. The action mints the grant; the
  proxy exchanges it for a host-only `HttpOnly`, `Secure`, `SameSite=Strict`, `Path=/` cookie on the
  camera host. Every request revalidates that cookie against the live administrator session.
- Camera hosts use a separate local name space from the administrator host, so camera JavaScript is
  a separate browser origin and site. The exact suffix is fixed in BIT-021. This keeps camera
  content from reading the administrator UI and keeps the administrator cookie host-only.

**3. Each camera gets a per-camera host, not a path prefix.**

- The camera host is stable for the camera ID and resolves on `lan0` through a local name service.
  The appliance certificate includes a wildcard SAN for the camera name space, generated at first
  boot, so adding a camera does not rotate the certificate. There is no public DNS dependency, no
  hosts-file step, and no path-prefix fallback. The exact host suffix, local name service, and SAN
  set are in BIT-021.
- The host maps the camera root to the proxy root. Root-relative and absolute-path URLs, cookies,
  redirects, and WebSocket endpoints stay on the camera origin.
- The proxy rewrites `Location` headers to the camera host and never returns a redirect to the
  camera IP. It absorbs camera `Set-Cookie` headers into a per-grant, in-memory cookie jar, never
  sends camera cookies to the browser, and reconstructs the upstream `Cookie` header from that jar.
  Closing the grant discards the jar.
- The upstream `Host` header is the camera's configured address, not the Bittern hostname, because
  some firmware expects its own host. The proxy does not rewrite arbitrary HTML or JavaScript
  bodies. A camera that hard-codes its own origin in a body is a per-model compatibility case,
  recorded in the camera matrix, not a reason to build a generic HTML rewriter.

**4. The proxy is scoped to one camera's address and web port.**

- Each camera host maps to exactly one upstream scheme, address, and port from `appliance.yaml`.
  That is the per-camera upstream allowlist. The request cannot choose or change the upstream. There
  is no `CONNECT`, no absolute-form request target, no redirect following to another host, and no
  DNS resolution from request input.
- The camera's web endpoint is the adopted camera address and a configured web port. If the
  inventory has no web port, BIT-021 adds the smallest schema field and a migration. The adoption
  flow in BIT-008 is unchanged.
- HTTPS with a pinned camera certificate fingerprint is preferred. HTTP is allowed only on the
  isolated `cam0` segment and only when the camera has no HTTPS endpoint; the UI states that the
  camera login crosses the camera segment in clear text. A changed HTTPS fingerprint fails closed
  and needs an explicit re-pin.
- The proxy never reads the RTSP `credentials_ref`, never stores a camera username or password, and
  never injects credentials. The camera's own login page is served, and the camera's own login
  remains a second, independent step.

**5. Response headers and caching.**

- Every proxied response carries `Cache-Control: no-store` and no cache validators. The proxy does
  not cache.
- Framing is blocked with `X-Frame-Options: DENY` and `frame-ancestors 'none'`. A tested same-origin
  frame exception is per camera and recorded.
- A restrictive CSP allows only the camera's own origin and its WebSocket, plus the image data types
  the tested UI needs. Inline script or style is a per-camera exception, recorded and tested, not a
  default.
- `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer` are set. HSTS is not set
  because the certificate is self-signed ([ADR-0019](./0019-ui-stack.md)).

**6. Session close and audit.**

- A grant closes after 30 minutes without a proxied request or active WebSocket traffic, on logout,
  on administrator session expiry, or on explicit revocation. It never outlives the administrator
  session.
- Closing a grant revokes the proxy cookie, closes active upstream connections including hijacked
  WebSockets, and discards the camera cookie jar. The next request gets 401 and must start from the
  camera listing.
- Audit records one open event and one close event per grant, plus denied attempts. Fields are
  timestamp, camera ID, hostname, administrator session reference, client address, and close
  reason. The audit never contains cookies, `Authorization`, camera credentials, request paths,
  query strings, or bodies. The camera host has no per-request access log.

**7. Camera network isolation and support path.**

- The proxy adds no input rule, no forward rule, no DNAT, no port forward, and no route from `lan0`
  to `cam0`. The only browser listener remains 443. The upstream connection is appliance-originated
  output to `cam0`.
- The Tailscale subnet route remains the opt-in support path. Direct LAN port forwarding or
  publishing camera ports is rejected.
- A future kiosk may open a camera UI locally over `cam0` without this proxy. That is
  [OQ-25](../open-questions.md) and requires its own authentication decision; it does not create a
  LAN path.

## Consequences

### Positive

- The owner reaches camera-only settings from the existing camera listing without Tailscale or a
  cable. The everyday path is a button, not a support procedure.
- Camera isolation is unchanged. No camera port is published, no forward rule is added, and no
  route exists from `lan0` to `cam0`.
- One listener and one administrator session cover the new surface. There is no second Bittern
  login or credential store to keep in sync, and no second firewall exception.
- The camera origin is separate from the administrator origin, and the administrator cookie is
  host-only. Camera content cannot read the administrator UI.
- The proxy never stores the camera password and does not replace the camera's own login. A stolen
  administrator session reaches the camera UI only if the camera login is also satisfied, unless
  the camera has no login.
- `no-store`, framing denial, the CSP, and `no-referrer` reduce caching and content-injection risks
  from untrusted firmware.
- Idle and logout close the grant and the upstream connections, and the audit records open and close
  without recording credentials.
- The standard library reverse proxy keeps the dependency and build surface small, in line with
  [ADR-0022](./0022-community-functionality-ux-wins.md).

### Negative / costs

- The proxy is new, security-sensitive code in `bitternd`: host routing, grant exchange, cookie
  handling, redirect rewriting, WebSocket tunneling, header policy, idle tracking, connection
  close, and audit. It needs focused tests and review.
- Camera compatibility is not uniform. Hard-coded origins, JavaScript-readable session cookies,
  internal frames, inline scripts, self-signed HTTPS, unusual redirects, and firmware uploads are
  per-model cases. The camera matrix has to carry them, and some cameras will not work.
- Per-camera hostnames add local name publication, a wildcard certificate SAN, and a separate local
  name space. Local name resolution is not universal; some clients may not resolve the chosen name.
  The fallback is the Tailscale support path, not a path prefix or a camera IP link.
- The proxy puts camera UI traffic, including long-lived WebSockets and any firmware upload, through
  `bitternd`. That adds load, memory, latency, and a new failure mode for a process that also serves
  the administrator UI.
- Absorbing camera cookies into the grant jar ends the camera session when the grant closes and
  breaks camera UIs that read their own session cookie from JavaScript. Camera state kept in
  localStorage can still persist.
- The 30-minute idle timeout is a compromise. It may close a UI while an owner is reading, and it
  is shorter than the 12-hour administrator session.
- HTTP to a camera without HTTPS sends the camera login across the isolated camera segment in clear
  text. The proxy prefers HTTPS with a pinned fingerprint, but the fallback is a real risk on a
  segment with untrusted devices.
- The list of camera-only settings is open ended. The proxy makes the camera UI reachable but does
  not make every camera UI usable from a phone, and it does not remove the need for the camera
  matrix.
- Audit and redaction add work: no request paths or query strings may be logged, because camera
  URLs can carry session tokens.

### Follow-ups

- BIT-021 implements the action and the proxy.
- Extend the BIT-011 certificate generation with the camera wildcard SAN, document the fingerprint
  and hostnames, and keep the administrator certificate names unchanged.
- Add the camera web endpoint fields to the schema with a migration and migration test, and update
  the four-camera example. Keep the BIT-008 adoption flow unchanged.
- Add the local name service package chosen in BIT-021 to the image and POC payload, record it in
  [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md), and include it in the SBOM.
- Add camera web UI rows to the camera matrix in [OQ-10](../open-questions.md), including login
  style, frames, inline script, WebSocket use, HTTPS support, and known absolute-origin quirks.
- Update the [UI README](../../ui/README.md), [architecture](../architecture.md), and support
  runbook when the action and proxy land. State that the Tailscale subnet route remains the support
  path.
- Test with real cameras. The fake camera fixture proves the proxy contract, not camera firmware
  behavior.
- Revisit body rewriting only if a tested camera needs it. Do not build a generic HTML or
  JavaScript rewriter now.
- Revisit the kiosk path under [OQ-25](../open-questions.md) separately. It does not change this
  decision.

## Alternatives considered

- **Path-prefix proxy on the appliance, such as `/camera/<id>/`.** Rejected: camera UIs use
  root-relative and absolute-path URLs for assets, XHR, redirects, cookies, and WebSockets. The
  prefix leaks on the first request, and repairing response bodies is fragile and camera-specific.
- **Route `cam0` onto `lan0`, add a forward rule, or publish camera ports.** Rejected: it breaks
  the isolation decision in [ADR-0005](./0005-dual-nic-isolated-camera-network.md), exposes old
  camera firmware to every LAN device, and replaces the administrator session with the camera's own
  login.
- **Use the Tailscale subnet route as the everyday path.** Rejected: every phone and laptop would
  need a tailnet client and a grant, and the route is support access, not a local-first everyday
  action. It remains the support path.
- **Embed the camera UI in an iframe inside the Bittern UI.** Rejected: many camera UIs block
  framing, the mixed origin weakens the administrator page, absolute URLs still break, and the
  action would hide which camera origin is loaded.
- **Link directly to the camera IP from the listing.** Rejected: `lan0` has no route to `cam0`, and
  making it work would require opening the camera port or routing the camera network.
- **Use the camera vendor's cloud app or portal.** Rejected: it adds an internet dependency, a
  vendor account, and an external data flow for a local appliance, and it does not work offline.
- **Run a separate nginx or Caddy reverse proxy with its own authentication.** Rejected: it adds a
  second listener, a second authentication stack, another package to update, and another firewall
  exception. The standard library reverse proxy inside `bitternd` is the smaller layer.
- **Serve the camera UI over HTTP to avoid certificate work.** Rejected by
  [ADR-0019](./0019-ui-stack.md). The camera login and session would cross the LAN in clear text.
- **Auto-login to the camera with a stored password.** Rejected: it stores the camera password,
  defeats the camera's own login as a second step, requires camera-specific login APIs, and widens
  the secret surface.
- **Put all cameras under one host with a path or query selector.** Rejected: it has the same
  path-prefix problem, mixes camera cookies and origins, and makes one camera's content able to
  reach another camera's route.
- **Do nothing and keep the cable or Tailscale procedure.** Rejected: camera-only settings remain a
  support task, and the owner cannot complete routine camera changes from the product UI.

## References

- [ADR-0005: Dual-NIC isolated camera network](./0005-dual-nic-isolated-camera-network.md)
- [ADR-0009: Remote access via Tailscale](./0009-remote-access-via-tailscale.md)
- [ADR-0010: Security and privacy posture](./0010-security-and-privacy-posture.md)
- [ADR-0016: Bittern owns the control surface and the Frigate
  config](./0016-bittern-owns-frigate-config.md)
- [ADR-0019: UI stack and TLS](./0019-ui-stack.md)
- [ADR-0022: Community functionality, UX wins](./0022-community-functionality-ux-wins.md)
- [Architecture](../architecture.md)
- [Open questions](../open-questions.md), including OQ-10 and OQ-25
- [UI README](../../ui/README.md)
- [Implementation plan](../plan/implementation-plan.md):
  [BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0),
  [BIT-020](../plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui), and
  [BIT-021](../plan/implementation-plan.md#bit-021-camera-native-ui-proxy)
- [Clean-install runbook](../runbooks/gk41-clean-install.md)
- [Media surfaces note](../notes/2026-11-14-media-surfaces.md)
- [Kiosk mode note](../notes/2026-11-15-kiosk-mode.md)
- Go `httputil.ReverseProxy`: <https://pkg.go.dev/net/http/httputil#ReverseProxy>
- Go `net/http/cookiejar`: <https://pkg.go.dev/net/http/cookiejar>
- Content Security Policy: <https://www.w3.org/TR/CSP3/>
- WebSocket protocol: <https://www.rfc-editor.org/rfc/rfc6455>
