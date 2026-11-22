# Setup token QR exchange

- Date: 2026-11-15
- Status: design note for [BIT-019](../plan/implementation-plan.md#bit-019-setup-token-qr-exchange)
  and [BIT-020](../plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui);
  BIT-019 implementation is recorded in
  [the setup token QR implementation note](2026-11-17-setup-token-qr-implementation.md)
- Related: [ADR 0010](../adr/0010-security-and-privacy-posture.md),
  [ADR 0019](../adr/0019-ui-stack.md), [UI](../../ui/README.md),
  [clean-install runbook](../runbooks/gk41-clean-install.md),
  [status contract](../status.md), [implementation plan](../plan/implementation-plan.md)

This note records the intended QR design and why the token travels in the URL fragment. BIT-019 now
implements the QR, grouped token and short-code fallback; the implementation findings are in the
linked note.

## Why change the current path

First boot stores a one-time setup token in `/etc/bittern/secrets.yaml` and a mode 0600 convenience
copy at `/var/lib/bittern/setup-token`. The operator reads it with `sudo bittern setup-token` and
then carries the value to the phone or laptop that opens the wizard. That is a setup flow that
requires copying a secret between devices, and the USB copy workaround makes it worse.

A QR on the appliance console removes the copy. The owner scans it with the device that will run the
wizard, and the token arrives in the browser without being typed or written down.

## QR design

`bittern setup-token --qr` and first boot use the same formatter. It prints three things to the
local console:

- a QR for `https://<lan0-address>/ui/setup#token=<token>`;
- the full token in readable groups for manual entry;
- a short one-time code for a device that cannot scan the QR or use the fragment.

The QR is generated with the Debian `qrencode` package from standard input, not from an argument or
a temporary file. The token must not appear in the process list, a shell history, a temporary file,
or an application log.

First boot writes this block directly to the local console only, and only when a terminal is
available. The existing first-boot instruction remains the fallback when no console is attached.
The token never goes to the service journal, the status document, the support bundle, or a remote
server.

## Fragment decision

The token is in the fragment, not the path or query string, because a browser does not send the
fragment in the HTTP request. It cannot appear in the daemon access log, a proxy, or a `Referer`
header. The setup page reads `location.hash` locally, removes it with `history.replaceState` before
the form is submitted, and posts the token once in the form body to the local appliance.

The fragment is a convenience, not the only path. With JavaScript disabled the page cannot read it,
so the grouped token and short code remain available. The setup page still works without JavaScript.

The short code is a separate random value, never a prefix or truncation of the setup token, because
a short prefix would weaken the full token. It is single use, expires after a short interval, and
uses the existing per-address and global setup rate limit. The exact lifetime is an implementation
constant rather than an architectural choice. A successful setup clears the token, the convenience
copy, and the short code.

## Testing sketch

- Encode and decode the QR, then compare the URL, fragment, and token with the root-only value.
- Load the setup URL with the fragment, complete setup, and confirm the fragment is gone and a
  reload, back navigation, or second submission cannot reuse the token.
- Expire the short code, exhaust the rate limit, and confirm both fail closed.
- Scan the first-boot journal, daemon logs, HTTP access log, status document, and support bundle for
  the token, fragment, and short code.
- Run the setup page at phone and desktop widths as part of BIT-020, including the QR and short-code
  fallback.
