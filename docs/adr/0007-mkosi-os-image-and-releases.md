# 0007. mkosi OS image and releases

- Status: Accepted
- Date: 2026-11-14
- Deciders: project owner
- Related: [ADR-0004](./0004-docker-on-bare-metal-debian.md),
  [ADR-0008](./0008-updates-rollback-and-reset.md),
  [ADR-0012](./0012-testing-and-release-gates.md), [architecture](../architecture.md),
  [implementation plan](../plan/implementation-plan.md)

## Context

The appliance must boot into a known, minimal, reproducible system with no interactive installer
and no configuration drift. The image is the product boundary: if it is hand-built or assembled by
a long shell script, releases cannot be reproduced, audited, or rolled back reliably.

Debian 13 minimal is the chosen base: stable, familiar, well supported by the hardware in scope,
and carrying the packages the appliance needs. The image must be buildable from git, versioned,
signed, and suitable for an A/B update scheme. There is no CI yet, so the first builds will be
local or release-engineer driven.

mkosi builds images from declarative configuration and systemd tooling, produces a bootable image
without a separate installer framework, and fits a git-driven build. debootstrap plus scripts or
Packer tend to accumulate imperative post-processing that is hard to reproduce and audit.

## Decision

Build the `bittern-os` image with mkosi from the git repository.

- The mkosi configuration and all image inputs live under `image/`. The build is driven by the
  repository, not by a machine-local script.
- The base is Debian 13 minimal, with no desktop and no tooling beyond what the appliance needs.
- The root filesystem is immutable at runtime. Writable state lives in dedicated areas for `/var`,
  device configuration, secrets, recordings, and the Frigate database. The exact partition and
  overlay layout is settled with the update design.
- The image includes systemd-networkd, nftables, chrony, dnsmasq, Docker, Tailscale, `bitternd`,
  `bittern`, and the UI.
- Build inputs are pinned: mkosi version, Debian snapshot or package versions, and container
  digests. The build records a manifest of what went into the image.
- Releases are signed. The signing mechanism and key custody are recorded before the first public
  release; no unsigned artifact is published.
- Reproducibility is a goal: identical inputs should produce an identical image where feasible, and
  otherwise a diffable manifest that explains the difference.
- Image builds move into CI later. Until then the build is documented, run by a release engineer,
  and uses the same commands locally.
- Image version and build metadata are visible on the device and in the support bundle.

## Consequences

### Positive

- The appliance is buildable from a clean checkout, making releases reviewable and testable.
- A minimal Debian base keeps the attack surface and update volume small.
- Immutable system files make drift visible and recovery simpler.
- The image is a natural unit for signing, A/B slots, and factory reset.
- Declarative image configuration is easier to audit than imperative provisioning.

### Negative / costs

- mkosi behavior must be pinned and tracked; upgrades can change image layout.
- Build times and image size are significant, especially without a warm package cache.
- Signing key custody and release engineering are unsolved operational burdens.
- Without CI, early builds depend on a person and a documented environment.
- Reproducibility is aspirational until package and timestamp sources are controlled.
- An immutable root complicates software that expects to write to `/etc` at runtime.

### Follow-ups

- Settle partition layout, writable mounts, and overlay strategy with
  [ADR-0008](./0008-updates-rollback-and-reset.md).
- Choose and document the signing tool and key custody before any public artifact.
- Add a CI image build and a reproducible-build check when CI exists.
- Define the image manifest, SBOM, and build provenance format.
- Decide secure boot, TPM-backed encryption, and dm-verity scope; currently open in
  [open questions](../open-questions.md).
- Document offline and mirrored package builds.

## Alternatives considered

- debootstrap plus provisioning scripts. Rejected: unverifiable steps, poor reproducibility.
- Packer with a preseeded Debian installer. Rejected: an installer in the loop and awkward A/B
  image production.
- A prebuilt Debian image customized at first boot. Rejected: hidden state and network dependence.
- NixOS. Rejected for v1: strong reproducibility, but a steep contributor learning curve.
- Ubuntu Core. Rejected: snap-centric and less control over layout.
- Buildroot. Rejected: embedded firmware tooling, not a Debian Docker host.
- HAOS as the base. Rejected: owns the platform and update model.

## References

- mkosi: <https://mkosi.systemd.io/>
- Debian releases: <https://www.debian.org/releases/>
- [Architecture](../architecture.md)
- [Implementation plan](../plan/implementation-plan.md)
