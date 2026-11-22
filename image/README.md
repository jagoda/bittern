# image

The `image/` component builds `bittern-os`, the reproducible Debian 13 appliance image. It owns the
mkosi build definition, the partition layout, the read-only root and writable state partitions, the
A/B OS slots, first-boot provisioning, and image signing. The image ships product software, not site
settings: device identity and configuration are created at first boot by the agent and the setup
flow.

## Boundaries

In scope:

- mkosi build definition, package manifests, kernel and firmware selection, image assembly
- Partition layout, bootloader, A/B slots, slot selection, and rollback markers
- First-boot provisioning, machine identity, and factory reset hooks
- Reproducible build metadata, signing, and release artifacts

Out of scope:

- Frigate itself and its configuration (agent and templates)
- Device configuration UI (ui)
- Application bundle updates and their transactions (agent)
- Fleet management and per-device config distribution (fleet)

## Package set

[packages.txt](packages.txt) is the provisional package set for the image and the POC payload. It
includes `qrencode`, which renders the first-boot setup QR from standard input. The token is never a
command argument or a file. Keep the list in step with `PACKAGES` in
[packaging/install.sh](../packaging/install.sh) until BIT-032 creates the mkosi manifest.

Status: not implemented yet.

Related: [architecture](../docs/architecture.md), [ADR 0007](../docs/adr/README.md).
