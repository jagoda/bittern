# 0006. Declarative config and renderer

- Status: Accepted; ownership-mode decision superseded in part by [ADR-0016](./0016-bittern-owns-frigate-config.md)
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0005](./0005-dual-nic-isolated-camera-network.md),
  [ADR-0008](./0008-updates-rollback-and-reset.md),
  [ADR-0011](./0011-monorepo-layout-and-naming.md), [architecture](../architecture.md),
  [implementation plan](../plan/implementation-plan.md)

## Context

A working appliance configures several daemons: systemd-networkd for interfaces, nftables for
filtering, dnsmasq for camera DHCP, chrony for time, Compose for Frigate, and Frigate's own
`config.yml`. Each has its own syntax, validation rules, and failure modes. Hand-editing them from
a UI or shell script produces drift, partial application, and configurations that cannot be
reproduced or tested.

Bittern also needs a path to fleet management later, which requires a machine-readable description
of intended state rather than a pile of mutated files. The device must still work offline and must
not depend on a configuration server.

Frigate's UI can write `config.yml`. If Bittern also writes it, the two writers will conflict unless
ownership is explicit.

## Decision

`appliance.yaml` is the single source of truth for device configuration.

- The schema is versioned. Every schema change ships with a migration, applied in order.
- A renderer in `bitternd` turns `appliance.yaml` plus bundle templates into systemd-networkd
  units, nftables rules, dnsmasq configuration, chrony configuration, the Compose project, and
  Frigate `config.yml`.
- Generated files carry a header marking them as generated. They are never hand-edited; a manual
  edit is overwritten or rejected.
- Apply is transactional: render to a staging area, validate, apply atomically, run health checks,
  and roll back on failure.
- Validation uses native checkers where they exist: `nft -c`, `dnsmasq --test`, and Frigate's own
  config check. The schema is validated before rendering.
- Secrets are separate. They live in `/etc/bittern/secrets.yaml` with mode `0600`, are generated or
  imported at first boot, and reach Frigate through environment substitution. Fleet secrets use
  SOPS with age and never enter git.
- Frigate config ownership has two explicit modes:
  - `ui-managed` is the default. Frigate's UI owns `config.yml`; the agent backs it up, observes it,
    and mirrors the camera inventory into `appliance.yaml` rather than overwriting it.
  - `git-managed` is for fleets and advanced users. The UI editor is locked and generated
    configuration is authoritative.
- The agent never blindly overwrites `config.yml`; an ownership mode and content hash gate every
  write.

## Consequences

### Positive

- One reviewable description of intended state, suitable for tests, backup, and fleets.
- Generated daemon configuration is reproducible and validated before it is applied.
- Rollback is possible because the previous rendered state is retained.
- Camera inventory can be mirrored from the UI without giving up a declarative core.
- The same primitives support a future signed per-device bundle without redesign.

### Negative / costs

- The renderer is critical; a bug can misconfigure networking or the firewall.
- Every new knob needs a schema field, template change, validation, and test. This is deliberate
  friction against configuration sprawl.
- Schema migrations accumulate and must remain backward compatible across updates.
- Two ownership modes for Frigate configuration add branching and test cases.
- In `ui-managed` mode `appliance.yaml` is not fully authoritative for camera settings.
- Template and schema drift is a persistent risk without golden tests.

### Follow-ups

- Design the `appliance.yaml` schema and versioning rules, with a migration test harness.
- Choose the renderer language, consistent with the agent language decision in
  [open questions](../open-questions.md).
- Define health checks and the exact atomic apply mechanism.
- Specify how `ui-managed` mode detects and reconciles external `config.yml` changes.
- Decide staging and backup paths for rendered configuration.
- Add golden render and negative tests (see [ADR-0012](./0012-testing-and-release-gates.md)).

## Alternatives considered

- Hand-edited configuration with documentation. Rejected: unreproducible and error-prone.
- Ansible, Puppet, or another configuration manager at runtime. Rejected: a large dependency, poor
  offline fit, and it still needs a source of truth and a firewall-safe apply path.
- GUI-only state in a database. Rejected: opaque, hard to back up, hostile to fleet workflows.
- Imperative shell scripts per feature. Rejected: no single model, no validation, partial
  application is the normal failure mode.
- Frigate's UI as the only source of truth. Rejected: it does not cover networking, firewall, DHCP,
  or updates.

## References

- [Architecture](../architecture.md)
- [Initial design session](../notes/2026-11-14-initial-design-session.md)
- Frigate configuration reference: <https://docs.frigate.video/configuration/advanced/reference>
- nftables: <https://www.nftables.org/>
- dnsmasq: <https://thekelleys.org.uk/dnsmasq/doc.html>
- SOPS: <https://github.com/getsops/sops>
