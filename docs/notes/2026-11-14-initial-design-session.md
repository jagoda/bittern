# 2026-11-14 initial design session

## Why we started

The immediate trigger was a failing commercial NVR: poor support, stale features, no practical
update path, and recurring camera compatibility problems. The author had used Frigate successfully
on home hardware and trusted it as an engine, but a raw Frigate install is not something you hand to
a household member. The question for the session was whether a reproducible appliance around Frigate
could close that gap without forking Frigate or building a cloud service.

## Path taken

We started from the desired outcome, a device a technical builder can install and a non-technical
operator can use, and worked backwards. The session evaluated Frigate itself: version cadence,
release history, licensing, security advisories, privacy defaults, and the state of its onboarding
UI. We then compared alternatives including Scrypted NVR, ZoneMinder, Shinobi, Viseron, Moonfire, and
OpenNVR. Frigate won on capability, maturity, and the fact that it can be used unmodified.

Platform choice followed. Home Assistant OS is a possible substrate but not the product target. A
native install avoids Docker but gives up the maintained Frigate packaging and device passthrough
conveniences. A VM adds a layer without solving anything. The session settled on Debian 13 minimal
with mkosi, Frigate in Docker on bare metal, host networking, and device passthrough.

The network design took the most time. The appliance is dual-NIC: `lan0` on the house LAN and `cam0`
on a private camera segment with a PoE switch. The camera segment is a leaf with DHCP and NTP only,
no forwarding, and no general DNS. Getting camera onboarding right means unicast probing rather than
multicast ONVIF discovery across two interfaces, plus handling cameras that ship with static fallback
addresses.

Finally we sketched the configuration and update model: declarative `appliance.yaml`, a renderer with
validation and atomic apply, two update loops with health checks and rollback, and a first-boot and
reset story. It became clear that these decisions were interlocking and easy to forget, so we wrote
the documentation set before writing code.

## What was decided

Thirteen decision records were accepted in one pass: ADRs 0001 through 0013. They cover documentation
process, vision and scope, Frigate as the engine without a fork, Docker on bare-metal Debian, the
dual-NIC appliance, declarative config with render, validate, apply, the mkosi image, two update
loops, Tailscale-only remote access, security and privacy posture, monorepo layout and naming, the
testing and release-gate strategy, and branding constraints. The full list and statuses are in the
[ADR index](../adr/README.md). The product name Bittern and the tagline "A quiet appliance for
Frigate" were chosen and checked against the Frigate trademark policy.

## What was deferred

Licensing was deliberately left unresolved at the start of the session. ADR 0013 originally recorded
the tension between Apache-2.0 and MIT; the decision was made later the same day, and the ADR now
records MIT for Bittern's own work (see ADR 0013 and `LICENSE`). Also deferred: the agent language,
the UI stack, the OS update mechanism (systemd-sysupdate, RAUC, or Mender), whether A/B updates ship
from day one, disk encryption and secure boot, the hardware SKU and bill of materials, Frigate+
support under its commercial terms, Home Assistant add-on support, the camera compatibility list,
multi-site fleet management, telemetry and update-check defaults, the support and warranty model,
localization, and target price. These are tracked in [open questions](../open-questions.md) rather
than guessed.

## What surprised us

- Frigate port 5000 is unauthenticated administration by design, and port 1984 for go2rtc is also
  unauthenticated. The appliance firewall must block both from the LAN (verified 2026-11-14).
- The Frigate container runs as root, and the admin password is printed to logs. Those are upstream
  behaviors to contain, not settings to toggle casually (verified 2026-11-14).
- ONVIF LAN discovery is not part of Frigate's Add Camera Wizard, tracked upstream as issue #21914.
  Bittern cannot rely on the wizard alone for onboarding (verified 2026-11-14).
- Frigate+ model licensing restricts deployment at customer sites and bundling with hardware without
  a commercial license, which rules out some appliance business models (verified 2026-11-14).
- Frigate had 30 published advisories, including a 2026 cluster of authentication and RBAC issues.
  Only the latest minor is supported, so pinning and advisory tracking are release gates, not
  housekeeping (verified 2026-11-14).
- Frigate's version check is on by default even though analytics is off by default, so a genuinely
  local-first default requires an explicit config change (verified 2026-11-14).

## Next step

Turn the design into a phased plan and a repository skeleton, then build the P1 vertical slice: a
QEMU two-NIC image running Frigate with one virtual camera, driven end to end through a minimal agent
and UI. The plan is in [the implementation plan](../plan/implementation-plan.md), the target
behavior is in [the vision](../vision.md), the intended design is in
[the architecture](../architecture.md), and the dated evidence is in
[the Frigate assessment](../research/frigate-assessment.md).
