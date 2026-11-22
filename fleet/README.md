# fleet

`fleet/` holds optional GitOps material for devices the maintainer operates: per-device
`appliance.yaml` trees, SOPS and age encrypted secrets, and the patterns for publishing signed
application bundles that a device can pull. It is optional and explicitly not part of v1. A standalone
appliance works fully offline, with no fleet server, no account, and no dependency on this directory.

## Boundaries

In scope:

- Per-device configuration directories for maintainer-operated appliances
- SOPS and age workflows for fleet secrets, never plaintext
- Signed bundle publishing and per-device channel assignment
- Documentation of the pull-based GitOps model and its trust assumptions

Out of scope:

- v1 appliance functionality and any requirement to use a fleet
- A hosted control plane, multi-tenant service, or customer device management
- Telemetry collection, remote shell without consent, or hidden management channels
- Storing device secrets in git in plaintext

Status: not implemented yet, and not planned for v1.

Related: [architecture](../docs/architecture.md), [ADR 0006 and ADR 0008](../docs/adr/README.md).
