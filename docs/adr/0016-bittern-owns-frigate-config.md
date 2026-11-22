# 0016. Bittern owns the control surface and the Frigate config

*Bittern is the single writer of Frigate's `config.yml` and the single control surface for the
appliance.*

- Status: Accepted
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0002](./0002-product-scope-and-vision.md),
  [ADR-0003](./0003-frigate-as-nvr-engine.md),
  [ADR-0006](./0006-declarative-config-and-renderer.md),
  [ADR-0010](./0010-security-and-privacy-posture.md),
  [ADR-0013](./0013-branding-and-licensing-constraints.md), [architecture](../architecture.md),
  [open questions](../open-questions.md),
  [extension points note](../notes/2026-11-14-extension-points.md)

## Context

ADR 0006 left two Frigate configuration ownership modes: `ui-managed` by default and `git-managed`
for fleets and advanced users. It deferred the harder question, which component owns the control
surface. In `ui-managed` mode Frigate's UI writes `config.yml` and the agent mirrors the camera
inventory back into `appliance.yaml`; the follow-up that defines that reconciliation was never
resolved.

No device has shipped, so there is no installed base in either mode and no migration burden from an
existing ownership model. The decision can be made on the target design rather than compatibility.

Two operational scenarios forced the decision. A field camera add or remove through the Frigate UI
would need to be mirrored into `appliance.yaml` before Bittern could render the rest of the device
configuration. An application update that changes Frigate configuration would then face a three-way
merge between the old generated file, the owner's UI edit, and the new template. Both scenarios are
error-prone with two writers, and the failure is silent drift rather than a visible conflict. ADR
0010 also made the authenticated Frigate UI reachable from `lan0`; this decision narrows that
exposure to a support path. This ADR supersedes the 8971 network bullet of ADR 0010.

## Decision

**Bittern owns Frigate's `config.yml` and is the only browser-facing control surface.**

- `appliance.yaml` is fully authoritative, including the camera inventory. The renderer generates
  `config.yml`; there is one writer and no mirroring.
- Frigate's UI is not a product surface. It is not exposed on `lan0`; 8971 binds to loopback only.
  `bitternd` uses Frigate's unauthenticated API on `127.0.0.1:5000`. The browser talks only to
  Bittern; the Bittern UI stack and port remain OQ-03.
- Frigate's UI remains installed and can be reached deliberately from the console or a tailnet for
  support. That is a documented diagnostic action, not a default path.
- Removing a camera stops recording and keeps existing recordings until retention expires. Deleting
  recordings early is a separate action with explicit confirmation.
- v1 exposes a curated subset of Frigate settings. Adding a capability means a schema field, a
  template change, validation, a migration if the schema version changes, and tests. There is no raw
  Frigate passthrough and no plugin system.
- No zone or mask editor in v1. The owner does not need one yet, and its absence is why full
  ownership is viable now. This is a dated follow-up, not a permanent exclusion.

## Consequences

### Positive

- One writer means generated `config.yml` always matches `appliance.yaml`. There is no mirroring, no
  merge on update, and no drift between the two.
- Application updates re-render `config.yml` and run schema migrations. Rendering is deterministic,
  and rollback returns the previous bundle and config together.
- Owner edits are impossible by construction, so "my change vanished" cannot happen. No UI path
  writes a file that the renderer later replaces.
- Camera removal has a defined recording lifecycle: stop recording, keep existing footage until
  retention expires, and require explicit confirmation for early deletion.
- The browser surface is one authenticated Bittern UI. Frigate's unauthenticated 5000 API stays on
  loopback and is never exposed to the LAN.
- A curated schema keeps the supported surface testable and keeps Frigate changes behind the
  renderer.

### Negative / costs

- Every new Frigate setting needs a schema field, a template change, validation, a migration if
  needed, and tests. That is slower than exposing Frigate's own settings UI.
- The nftables ruleset and the harness smoke test still rely on 8971 from `lan0`. Both must stop
  after the current QEMU run; until then they describe the old path.
- Frigate's UI remains installed but becomes a diagnostic back door. Support must use it
  deliberately and keep it off the default path.
- No zone or mask editor in v1 leaves detection tuning coarse; the owner cannot draw zones until
  that follow-up lands.
- A camera removal keeps recordings until retention expires, so storage is not reclaimed
  immediately. Early deletion is a separate confirmed action.
- Schema migrations accumulate and must remain compatible across application updates.

### Follow-ups

- After the first QEMU run, change the renderer and the nftables golden to drop 8971 from `lan0` and
  keep Frigate's listeners on loopback.
- Move the harness smoke test from 8971 on `lan0` to loopback or through the Bittern surface.
- Define how support deliberately reaches loopback 8971 (an SSH tunnel, `tailscale serve`, or another
  mechanism) before this ADR is used to document a support path.
- Revisit a zone and mask editor after v1 (recorded 2026-11-14).
- When reservations need a hardware identity, add it as a schema field with a migration, not a
  shortcut.
- Keep the extension points in [the 2026-11-14 note](../notes/2026-11-14-extension-points.md) true,
  including device I/O, audio, discovery, and camera identity.

## Alternatives considered

- **Keep `ui-managed` as the default.** Rejected: it leaves two writers, requires a three-way merge
  on update, and the mirroring follow-up in ADR 0006 is still unresolved.
- **Embed Frigate's UI as the control surface.** Rejected: it couples Bittern to Frigate's frontend,
  authentication behavior, and version, complicates branding under ADR 0013, and still leaves a
  second writer for `config.yml`.
- **Expose a raw Frigate config passthrough.** Rejected: it would move Frigate's schema into the
  Bittern config surface, defeat validation, and invite schema sprawl and hidden settings.
- **Do nothing and leave ownership ambiguous.** Rejected: two writers drift, update behavior stays
  undefined, and the owner cannot tell which surface is authoritative.

## References

- [Architecture](../architecture.md)
- [ADR-0006: declarative config and renderer](./0006-declarative-config-and-renderer.md)
- [ADR-0010: security and privacy posture](./0010-security-and-privacy-posture.md)
- [Extension points for device I/O and discovery](../notes/2026-11-14-extension-points.md)
- [Open questions](../open-questions.md): OQ-03, OQ-21, OQ-22
- Frigate configuration reference: <https://docs.frigate.video/configuration/>
