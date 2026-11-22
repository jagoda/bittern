# UI skeleton handoff

- Date: 2026-11-14
- Ticket: BIT-007
- Related: [ADR 0019](../adr/0019-ui-stack.md), [status contract](../status.md),
  [implementation plan](../plan/implementation-plan.md)

## What changed

The owner-visible first-run wizard and status page skeleton now exist behind `bitternd --ui` on the
loopback listener. The wizard covers welcome, network check, storage check, camera details, review,
and done. It stores its progress in a server-side browser session and works with JavaScript
disabled. HTMX 2.0.11 is vendored in `ui/static/` and only refreshes the status fragment.

The status page renders the BIT-006 document, including the overall state, reason, and every
section. `--ui-mock` selects a mock provider that previews `booting`, `ready`, `degraded`, `updating`,
and `failed`; unknown fields stay unknown. Tests parse every rendered page and fail on an input
without a label, and a palette test enforces WCAG 2.2 AA contrast for the chosen colors.

The apply boundary is `ui.Applier`. `ui.StubApplier` returns `apply is not implemented` and writes
nothing. Tests complete the whole flow with a fake applier.

## What is unfinished

- `ui.StubApplier` is not connected to the apply transaction from BIT-011.
- `bitternd` still serves the UI over loopback HTTP. TLS on 443, the certificate fingerprint, and
  authentication, CSRF protection, and rate limiting at the browser boundary are BIT-011 work.
- First boot, identity and secrets generation, camera discovery, updates, reset, support bundles,
  and the signed image are not part of this skeleton.

## Next concrete step

In BIT-011, replace the stub applier with the apply transaction, generate and serve the certificate,
and mount the UI on the TLS listener. Keep the no-JavaScript path, the status contract, and the
accessibility tests intact.

## Update

The TLS listener, certificate fingerprint display, authentication, CSRF protection, and rate
limiting described as unfinished above are implemented in
[the TLS UI auth note](2026-11-14-ui-tls-auth.md). The no-JavaScript wizard and status paths are
unchanged. Live view, playback and detection review are out of scope for this skeleton and stay
deferred as described in [the media surfaces note](2026-11-14-media-surfaces.md).

Update 2026-11-15: the setup network and storage checks now use `ui.AgentChecks`, which maps the
live status document onto the wizard. `--ui-mock` still selects `ui.MockChecks` for previews and
tests. The network step shows the real lan0 and cam0 addresses and carriers, and the storage step
shows the real path, presence, free space, reserve, and pool member state.

Long-running apply progress is tracked as [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress).
