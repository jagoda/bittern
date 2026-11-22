# 0019. UI stack and TLS

*`bittern-ui` is server-rendered Go `html/template` with HTMX, embedded in `bitternd`, and served
over TLS by default.*

- Status: Accepted
- Date: 2026-11-14
- Deciders: maintainer
- Related: [ADR-0006](./0006-declarative-config-and-renderer.md),
  [ADR-0010](./0010-security-and-privacy-posture.md),
  [ADR-0015](./0015-agent-language.md),
  [ADR-0016](./0016-bittern-owns-frigate-config.md),
  [OQ-17](../open-questions.md), [OQ-23](../open-questions.md),
  [architecture](../architecture.md), [implementation plan](../plan/implementation-plan.md)

## Context

OQ-03 bundled two decisions: the browser stack for `bittern-ui` and how live video reaches the
browser. The first decides build tooling, accessibility, and the authenticated browser surface. The
second decides whether 8555 is exposed on `lan0`, whether go2rtc needs authentication, and whether
video is proxied through `bitternd`. Keeping them together blocked the first-run wizard on a video
question it does not need answered.

The appliance has no Node toolchain, no package manager on the device, and no cloud build step.
[ADR 0015](./0015-agent-language.md) chooses Go for `bitternd` and the CLI and vendors every module.
[ADR 0016](./0016-bittern-owns-frigate-config.md) makes Bittern the only browser-facing control
surface, so the UI is the owner's path for first run, onboarding, status, updates, logs, support,
and reset. Frigate's UI stays on loopback.

Most owners reach the appliance from a browser on the house LAN.
[ADR 0010](./0010-security-and-privacy-posture.md) treats that LAN as partially trusted and requires
authentication, TLS, CSRF protection, and rate limiting at the browser boundary. The appliance has
no public name, no public CA identity, and may be offline, so a publicly trusted certificate is not
available in the default path.

The POC needs to move before the product TLS path exists. That exception must not become a shipped
HTTP mode.

## Decision

**`bittern-ui` is server-rendered Go `html/template` with HTMX for partial updates.**

- No Node toolchain, no SPA build, no client-side router. `html/template` renders every page on the
  server, and HTMX replaces or appends fragments when JavaScript is available.
- Templates and static assets live under `ui/` and are copied into the Go module by the existing
  asset sync step, then embedded in `bitternd`. There is no runtime asset directory on the device
  and no asset pipeline outside `make`.
- HTMX is vendored and pinned with the app bundle. Its version is recorded in the bundle manifest
  and in [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md) when the dependency is added.
- Progressive enhancement is mandatory. Every page and every critical flow works with JavaScript
  disabled: forms submit as normal requests, links are real links, and a full-page response is the
  fallback. HTMX only improves latency and partial updates; it is not required to navigate, submit,
  or recover from an error.
- The baseline includes semantic HTML, labels tied to controls, keyboard navigation for every
  action, a visible focus state, and contrast that meets WCAG 2.2 AA for text and controls. These
  are acceptance criteria for each UI ticket, not a later audit. [OQ-17](../open-questions.md) still
  owns localization and the wider accessibility scope.

**TLS is the product default.**

- The UI listens on TCP 443. The firewall admits 443 from `lan0` and the tailnet only, once the UI
  listener and its firewall rule exist. There is no HTTP listener in a product build.
- Binding uses systemd socket activation or `CAP_NET_BIND_SERVICE`; the UI process does not gain
  root to bind 443.
- First boot generates a self-signed certificate. The private key is stored with the other
  generated secrets in `/etc/bittern/secrets.yaml` at mode `0600`. The certificate is public and is
  served from the same generated state.
- The first-run wizard and the local console show the certificate's SHA-256 fingerprint. The status
  page keeps the fingerprint available so an owner can compare it later.
- A factory reset regenerates the certificate and key with the rest of the identity and secrets.
  Certificate rotation outside reset is not part of v1.
- The browser warning is accepted and explained. It is not suppressed, and the fingerprint check is
  the owner's out-of-band verification. The wizard says what the warning means and why the
  certificate is not publicly trusted.
- HTTP on `lan0` is rejected under [ADR 0010](./0010-security-and-privacy-posture.md). The admin
  password, session cookies, camera credentials, snapshots, logs, and control actions would cross a
  partially trusted LAN in clear text, where any device could read or modify them. TLS is not a
  substitute for authentication or CSRF protection; it is the transport baseline those controls
  rely on.
- A POC may run over HTTP on loopback or an isolated lab network with a written warning in its
  README and on its pages. That is not the product path: no release build exposes HTTP and no
  configuration option downgrades the product listener.

Live video is out of scope for this ADR. [OQ-23](../open-questions.md) decides whether the browser
uses direct WebRTC on 8555, a proxy or HLS through `bitternd`, or snapshots only in v1. Until it is
answered, 8555 stays closed on `lan0`, which matches the current renderer.

## Consequences

### Positive

- The UI builds with the same Go toolchain and vendored module tree as the agent. There is no Node
  install, lockfile, bundler, or second dependency ecosystem to secure and update.
- Embedded assets make the UI part of the signed app bundle. A bundle rollback restores the UI and
  the agent together, with no separate asset generation to forget.
- Server rendering gives a working page on first byte and keeps the no-JavaScript path honest.
  `html/template` contextual escaping reduces the chance of an XSS mistake in a small codebase.
- HTMX is a small pinned file with no build step, so partial updates do not require a client-side
  framework or a second source of truth for page state.
- TLS by default keeps credentials, sessions, snapshots, logs, and control actions off the LAN in
  clear text. The fingerprint gives the owner a concrete way to verify the certificate without a
  public CA.

### Negative / costs

- Every first visit to a new appliance shows a browser interstitial. The warning looks like a fault,
  owners are tempted to click through it, and it trains the wrong habit. The certificate is not
  publicly trusted and cannot be in the default path, because the appliance has no stable public
  name and may be offline. Fingerprint comparison is manual and adds a step to first run.
- A self-signed certificate cannot rely on public CA validation, HSTS preload, or automatic
  rotation. A changed `lan0` address can make the certificate name stale, even though the
  fingerprint still identifies it.
- The UI is less interactive than an SPA. Full-page reloads remain the fallback, and partial updates
  are limited to what HTMX can express without custom JavaScript.
- Go templates are less expressive than a component framework. Complex pages need explicit layout
  templates and careful reuse rather than a client-side component model.
- Binding 443 needs a privileged mechanism or socket activation, and the firewall template needs an
  explicit allow rule before the UI is reachable.
- The accessibility baseline is real work in every UI ticket: keyboard tests, focus states,
  contrast checks, and semantic markup cannot be deferred to a final audit.

### Follow-ups

- BIT-007 implements the first-run wizard in this stack, with no-JavaScript, keyboard, focus, and
  contrast tests.
- BIT-011 implements first-boot certificate generation, private-key storage, and the fingerprint
  display in the wizard and console.
- The nftables template and golden allow 443 on `lan0` and, when Tailscale is enabled, on
  `tailscale0` (2026-11-14).
- Define the certificate SANs, the certificate file path, and the regeneration wording in BIT-011.
  Revisit rotation if DHCP address changes make the name mismatch confusing.
- Add HTMX to `THIRD_PARTY_NOTICES.md` and the SBOM when it is vendored, and pin its version in the
  bundle manifest.
- Answer [OQ-23](../open-questions.md) before exposing 8555 or building live view. Resolve
  [OQ-17](../open-questions.md) for localization and the wider accessibility scope.

## Alternatives considered

- **React plus Vite SPA served by the agent.** Rejected: it adds a Node toolchain, a second
  dependency ecosystem, a client-side router, and a build artifact that must be embedded and
  versioned separately. The no-JavaScript path becomes a fallback that is easy to neglect, and
  accessibility work moves into components.
- **Server-rendered Go templates without HTMX.** Rejected: it is workable, but status polling and
  apply progress would need full-page reloads or hand-written JavaScript. HTMX is the smaller,
  pinned option and still keeps the no-JavaScript path.
- **Flutter, WebAssembly, or another compiled client.** Rejected: a large runtime, poor fit with
  browser accessibility and form behavior, and a new build toolchain for a small management UI.
- **A separate Node or Python UI service.** Rejected: it duplicates the HTTP server and template
  stack, adds a runtime to the image, and gives the network-facing surface a second dependency tree
  to patch.
- **HTTP on the LAN as the default.** Rejected under
  [ADR 0010](./0010-security-and-privacy-posture.md): credentials, sessions, snapshots, logs, and
  control actions would be readable and modifiable by any device on the house LAN. A local-first
  appliance cannot treat the LAN as a trusted channel.
- **A public CA certificate or ACME.** Rejected for v1: the appliance has no stable public name, may
  be offline, and should not depend on a certificate authority for first boot. This can be revisited
  if a stable name and a supported enrollment path exist.

## References

- [Architecture](../architecture.md)
- [Implementation plan](../plan/implementation-plan.md), BIT-007 and BIT-011
- [Open questions](../open-questions.md): OQ-17 and OQ-23
- [ADR-0010: security and privacy posture](./0010-security-and-privacy-posture.md)
- [ADR-0015: agent language](./0015-agent-language.md)
- [ADR-0016: Bittern owns the control surface](./0016-bittern-owns-frigate-config.md)
- Go `html/template`: <https://pkg.go.dev/html/template>
- HTMX: <https://htmx.org/>
- WCAG 2.2: <https://www.w3.org/TR/WCAG22/>
