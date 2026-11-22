# Vision

Bittern is a local-first camera appliance that a technical person can build and give to a
non-technical person. It wraps Frigate NVR in a reproducible OS image, a management agent, and a
small UI. Nothing is implemented yet; this document defines the target, not working software.

## Problem

The starting point was a small commercial NVR. Support was poor, features were stale, firmware
updates were difficult or impossible, and camera compatibility caused recurring pain. Vendor
lock-in extended to cloud accounts and mobile apps required for basic remote viewing.

Frigate is a credible open-source alternative and the author has used it successfully. It is capable
and actively developed (verified 2026-11-14, see
[the Frigate assessment](research/frigate-assessment.md)), but a raw install assumes comfort with
YAML, Docker, Linux networking, and release notes. It has no guided first run, hardened dual-NIC
network, supported update path, or recovery story for someone who cannot use a shell. Handing a stock
Frigate install to a household member is not a reasonable support plan.

The gap is not the NVR engine but the appliance around it: installation, onboarding, network
isolation, updates, rollback, reset, and a UI that keeps the non-technical path non-technical.

## Who it is for

**The builder and installer.** A technical person who assembles a two-NIC mini-PC, flashes the
image, connects the house LAN and a PoE switch, onboards cameras, and remains first-line support.
They need reproducible builds, readable configuration, logs, a support bundle, and recovery without a
reinstall. They may use a shell but should not need one for routine work.

**The operator and household member.** Someone who watches live views and recordings, receives
alerts, and asks "is the camera working?" They need a small, legible UI, clear status, safe update
controls, and no exposure to Docker, YAML, or firewall rules. They must not be able to break the
appliance by clicking around, or need a cloud account.

**The future maintainer and coding agent.** A developer or agent who inherits the repository months
later. They need reasoning, not just code: accepted decisions, dated research, open questions, and a
plan that says what is intentionally missing.

## Product principles

1. **Local-first and private by default.** Streams, recordings, and metadata stay on the appliance
   and the house LAN. There is no Bittern cloud. Cloud and analytics features are off unless the
   operator explicitly enables them, and the cost of doing so is documented.
2. **Boring and updatable.** Use Debian, systemd, Docker, and upstream Frigate in their normal form.
   Novelty is a maintenance cost. The appliance must remain updatable years after it is built.
3. **One appliance, not a platform.** Bittern solves one job: a camera NVR for a home or small site.
   It is not a home automation hub, a general container host, or a multi-tenant service.
4. **The UI carries the non-technical path.** First run, camera onboarding, status, updates, logs,
   support bundle, and reset are reachable without a shell. Frigate UI remains the video interface.
5. **Reversible operations.** Configuration changes and both update loops have a commit point and a
   rollback path. Destructive actions require confirmation and preserve a recovery route.
6. **Documented decisions.** Significant choices are recorded as ADRs with context and consequences.
   Undecided questions are tracked rather than guessed, and future agents read the record first.
7. **Privacy by default.** No telemetry, no camera data uploads, no hidden version checks. The
   default configuration produces only the documented update and time traffic.
8. **Small trusted computing base.** Few components, narrow interfaces, pinned dependencies. The
   privileged agent is the only writer of system configuration, and Frigate gets only what it needs.

## v1 scope

In scope:

- A reproducible, signed Debian 13 image for a supported class of two-NIC x86 mini-PCs, built from
  git with mkosi.
- A browser UI for first run, camera onboarding, health, updates, logs, support bundle, and reset.
- Frigate as the NVR engine, pinned by digest, in Docker with host networking and hardware decode.
- Guided onboarding for at least four ONVIF/RTSP cameras, using ONVIF WS-Discovery where it works,
  plus unicast subnet scanning and RTSP probes, because ONVIF LAN discovery is not part of Frigate's
  wizard (upstream issue #21914, closed as not planned, verified 2026-11-14).
- A versioned `appliance.yaml` with a renderer, validation, atomic apply, and rollback for network,
  firewall, DHCP, NTP, compose, and managed Frigate fragments.
- Two update loops, A/B OS and application bundle, with health checks, rollback, and anti-rollback.
- Optional Tailscale remote support over outbound connections only, plus reset paths and a redacted
  support bundle.

Out of scope for v1:

- Forking, patching, or redistributing Frigate, or building a competing NVR engine.
- A Bittern cloud service, hosted fleet control plane, mobile apps, or subscription features.
- Our own video analytics, model training, or features beyond what Frigate provides.
- Multi-site management, customer device management, commercial support, or warranty.
- Home Assistant add-on packaging, camera hardware, or a certified hardware SKU and bill of materials.
- Disk encryption, secure boot, and TPM attestation. These are open questions, not v1.
- Localization and accessibility beyond a usable English UI. They are tracked, not ignored.
- Exposing RTSP port 8554 or Home Assistant integration by default.

## Definition of done for v1

v1 is done when, using only the published image and documentation:

- A builder can flash the image, join a normal house LAN, and onboard four cameras through the UI in
  under 30 minutes, with no shell commands and no manual YAML editing.
- The operator can see live view, recordings, and alerts, and check camera health from a LAN
  browser, using the linked Frigate UI for video and the Bittern UI for appliance status.
- Application and OS updates can be applied, and both loops recover from a failed update or power
  cut without a reinstall. Rollback returns to the previous known-good state.
- Wipe and reset returns to first-run state, erases recordings, snapshots, and secrets, and leaves a
  working OS slot behind.
- Under the default configuration, camera data stays on the LAN. Only documented update and time
  traffic leaves the appliance, and Frigate telemetry and version checks are handled explicitly.

## Success criteria

Modest targets, measured on reference hardware and recorded in the test plan. They keep the
definition of done concrete without promising commercial service levels.

| Criterion | Target |
| --- | --- |
| Time to first recording, four reference cameras | Median under 20 minutes, ceiling 30 minutes |
| Camera onboarding without a shell | 4 of 4 reference cameras for documented models |
| Configuration apply success | 99 percent of valid changes in the test matrix |
| Update success | 99 percent of updates reach the healthy commit point |
| Rollback correctness | Previous working generation restored in under 5 minutes |
| Power-cut safety | No unrecoverable state at any tested transaction step |
| Default outbound traffic | No telemetry or camera data; only update and NTP endpoints |
| Network isolation | No camera-to-LAN or camera-to-WAN traffic in isolation tests |
| Recovery | Dead appliance rebuildable from image plus backup by the documented procedure |

## Phases

**P0: documentation and skeleton.** Current phase. Write vision, architecture, ADRs, research, open
questions, plan, and component stubs. Exit when a new agent can navigate the docs.

**P1: single-camera vertical slice.** Boot a QEMU two-NIC image, run Frigate, onboard one virtual
camera through a minimal agent and UI, record and view a clip. Exit when the path works end to end.

**P2: onboarding and config renderer.** Schema, renderer, validation, atomic apply, four-camera
onboarding, camera inventory, and a usable first-run and status UI. Exit when onboarding needs no
shell.

**P3: update and reset.** Application bundle update with health and rollback, A/B OS update,
anti-rollback generation, support bundle, and reset paths. Exit when failure and power cut recover.

**P4: hardening and appliance release.** Isolation tests, security review, reproducibility, signed
artifacts, hardware-in-the-loop soak, documentation pass, and a release candidate. Exit when the
definition of done is met on reference hardware.

The detailed sequence is in [the implementation plan](plan/implementation-plan.md), decisions are in
[the ADR index](adr/README.md), and unresolved questions are in
[open questions](open-questions.md).
