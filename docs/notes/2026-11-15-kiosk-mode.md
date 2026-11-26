# Kiosk mode on an attached display

- Date: 2026-11-15
- Updated: 2026-11-26. [OQ-23](../open-questions.md) is resolved by
  [ADR 0034](../adr/0034-live-view-owned-by-bittern.md). The kiosk can reuse the Bittern live
  surface, but it still needs its own display, session, authentication and recovery decision. The
  dependency below is historical and no longer blocks on OQ-23.
- Status: Deferred, not decided
- Related: [OQ-25](../open-questions.md), [OQ-23](../open-questions.md),
  [ADR 0009](../adr/0009-remote-access-via-tailscale.md),
  [ADR 0019](../adr/0019-ui-stack.md), [architecture](../architecture.md),
  [UI](../../ui/README.md), [BOM research](../research/bom-2026-11-14.md),
  [implementation plan](../plan/implementation-plan.md)

This note records a possible local kiosk mode for later. It is not an ADR, a ticket or a plan
change, and no implementation is scheduled.

## The idea

The appliance could boot to a local full-screen UI on an attached display, in addition to the
network UI. A small screen beside the appliance would show status, live view and recent events
without a phone or laptop. The display stays optional; a headless appliance remains the default.

## One interface, not a fork

Reuse `bittern-ui`: the same Go `html/template` pages, the same HTMX assets, the same handlers and
the same TLS service. The local browser points at a loopback TLS endpoint instead of the LAN
address. The kiosk does not get its own templates, API or build.

A kiosk principal is separate from the network administrator. It gets a local device credential or
PIN, a limited start page, and an explicit admin unlock before configuration, reset or other
privileged actions. The network admin password is not stored in the display session, and physical
access alone must not grant administration.

The kiosk shell is a minimal full-screen browser with a start page for status, live and review, and
no address bar or desktop. Touch targets, an on-screen keyboard where needed, and overscan margins
belong in the shell, not in a second UI. This is a current idea, not a decision.

## Boot behavior

A systemd unit starts a full-screen session under cage or greetd with no desktop environment. It can
be the default local session when a display is present, but the appliance must boot headless if the
session or display fails. The kiosk is a view, not a boot dependency.

It must not make the appliance unserviceable. Keep a documented escape path:

- VT switch to a text console, and a GRUB or boot-menu entry that does not start the kiosk.
- The Tailscale support path from ADR 0009, with local network UI access as the fallback.
- Physical access to keyboard, USB and monitor for recovery.
- Updates, reset, support bundles and logs reachable even when the kiosk browser is wedged.

The exact escape sequence and fallback unit belong in the ADR and runbook, not here.

## Lift

| Area | Work | Main dependency |
| --- | --- | --- |
| Local session and auth | Session unit, kiosk principal, local credential or PIN, admin unlock, idle timeout and audit | A principal model beyond the current single admin login |
| Image and display packages | cage or greetd, browser engine, fonts, input and touch drivers, DRM and GPU support, screen blanking, overscan | Image size, boot target and package maintenance |
| Live and review | Live view, event list, clips, snapshots and a touch-friendly review flow | OQ-23; the largest part |
| Recovery and support | Escape path, VT and boot menu, support access, and update or reset entry while the local UI is broken | A new recovery design |
| Testing | Virtual display and screenshots in QEMU; HIL with monitor, touch, video, power and update | Test harness and reference hardware |
| ADR, OQ and BOM | Write the ADR, keep OQ-25, and add display, touch and mounting choices to the BOM if promoted | The display choice is not in the current BOM |

## Why it is not v1

Kiosk mode depends on OQ-23 for live and review, on a display and touch choice that the BOM does not
make, and on a recovery design for a local session that can fail. None of that is needed for the
current clean-install and hardware gate, which boots to a reachable network wizard and does not
require a monitor. An ADR is required before implementation. This note only keeps the idea and its
costs visible.

## Testing sketch

QEMU with a virtual display and screenshot capture: boot to the kiosk session, render the start page,
sign in with the local credential, reach live and review, open admin unlock, and survive a browser or
session restart. The existing headless QEMU path must stay green.

HIL on the reference unit with a real monitor and touch: overscan and scaling, touch targets and the
on-screen keyboard, video decode while recording, power cut and update rollback, and the documented
kiosk escape. A wedged kiosk must leave the network UI, Tailscale support, or the physical console
and the recovery path working.

## Next step

Deferred. Revisit after the v1 hardware gate, answer OQ-23, choose the display, and write the ADR.
No implementation ticket now.
