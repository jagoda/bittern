# TLS UI listener, setup token and browser sessions

- Date: 2026-11-14
- Related: [ADR 0010](../adr/0010-security-and-privacy-posture.md),
  [ADR 0019](../adr/0019-ui-stack.md), [status contract](../status.md),
  [implementation plan](../plan/implementation-plan.md)

## What changed

`bitternd --ui` now serves the browser UI on a separate TLS listener, `0.0.0.0:443` by default. The
certificate is `/etc/bittern/tls/server.crt` and the key is `tls.private_key_pem` in
`secrets.yaml`. A missing certificate, key, or mismatched pair stops the UI before it listens. The
status API remains on its own loopback listener and is not mounted on the UI listener.

First boot generates a one-time `setup_token` in the secrets file and a mode 0600 operator copy at
`/var/lib/bittern/setup-token`. It prints instructions only, never the token. Read the token on the
appliance terminal or over SSH with `sudo bittern setup-token`. The first visit requires that token,
then sets the administrator password and removes both copies. Later visits use the login page.
Passwords are stored as salted PBKDF2-HMAC-SHA256 hashes with 600,000 iterations in
`/var/lib/bittern/auth.json`. Sessions are server-side with a 12 hour expiry, a `Secure`, `HttpOnly`,
`SameSite=Strict` cookie, and a per-session CSRF token. Failed login and setup attempts are rate
limited per address and globally.

The `bitternd.service` unit binds 443 with `CAP_NET_BIND_SERVICE`, keeps the status API on loopback,
and grants write access to `/etc` and `/var/lib/bittern` because the wizard now calls the apply
transaction in the daemon. It keeps `CAP_NET_ADMIN` and `AF_NETLINK` for `nft -c`. The nftables
ruleset remains the network boundary for the LAN listener.

## What remains

- The QEMU first-boot harness exercised the TLS listener, setup token, sessions and wizard over
  HTTPS on 2026-11-14. It completed the apply at generation 2 with Frigate healthy and a JPEG
  snapshot; see [the first-boot note](2026-11-14-qemu-firstboot.md).
- The certificate fingerprint is shown in the UI footer and setup page, and the CLI prints it after
  first boot. The image and console runbook still need to point at that output.
- The GK41 runbook now uses `sudo bittern setup-token`; the image and support bundle still need the
  same operator instruction.
- Password recovery and certificate rotation outside an identity reset are not implemented.
- Setup network and storage checks read the same status builder as the status page. `--ui-mock`
  still selects the sample `ui.MockChecks` data.
