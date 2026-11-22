# Setup token QR implementation

- Date: 2026-11-17
- Status: BIT-019 implemented on the `bit019-setup-qr` branch. The Go tests and the QEMU firstboot
  scenario pass, including the QR decode and journal/status scans; a real phone scan was not
  completed in this session.
- Related: [BIT-019](../plan/implementation-plan.md#bit-019-setup-token-qr-exchange),
  [setup token QR design](2026-11-15-setup-token-qr.md),
  [ADR 0019](../adr/0019-ui-stack.md), [ADR 0022](../adr/0022-community-functionality-ux-wins.md),
  [UI](../../ui/README.md), [status contract](../status.md),
  [clean-install runbook](../runbooks/gk41-clean-install.md)

## What changed

- `bittern setup-token --qr` renders a terminal QR for
  `https://<lan0-address>/ui/setup#token=<token>`, the grouped token and a short one-time code. The
  token goes to `qrencode` on standard input; it is never an argument, a temporary file or a
  command line. The plain `bittern setup-token` output remains for scripts.
- First boot writes the same block directly to the local console when
  `BITTERN_CONSOLE=/dev/tty1` is set by the first-boot unit. The block never goes to stdout, stderr
  or the journal. Without a console, the existing instruction and `sudo bittern setup-token --qr`
  remain.
- `ui/static/setup-fragment.js` reads `#token=`, fills the setup field, clears the fragment with
  `history.replaceState` before submission, and leaves the no-JavaScript path alone. The setup form
  accepts the grouped token or the short code in the same field.
- The short code is eight Crockford base32 characters, stored only as a SHA-256 hash and RFC 3339
  expiry in `/etc/bittern/secrets.yaml`, expires after 10 minutes, is valid once and uses the
  existing per-address and global setup rate limiter. A successful setup clears the token, the mode
  0600 convenience copy and the short-code material.
- `qrencode` is in `image/packages.txt`, `packaging/install.sh` and the POC payload README, and is in
  `THIRD_PARTY_NOTICES.md`. `zbar-tools` is a test-only host package for the QEMU QR decode.
- The clean-install and POC runbooks no longer ask the operator to copy a token between devices.

## Findings

- qrencode's `ANSI` terminal output gives dark modules on a white background and is 74 columns wide
  at margin 2 for the current 43-character token. `UTF8` and `ANSIUTF8` are visually inverted in a
  normal terminal, so the block uses `-t ANSI`. The QEMU decoder parses that ANSI output into a PGM
  image and passes it to `zbarimg` on stdin, so the QR image is never written to disk either.
- The setup token alphabet is base64url and includes a hyphen. The grouped token therefore separates
  groups with spaces, not hyphens; normalizing a pasted token removes whitespace only. The short code
  alphabet excludes hyphen, so `XXXX-XXXX` is unambiguous.
- A systemd service's stdout is the journal. The first-boot block writes to the device named by
  `BITTERN_CONSOLE` instead. That keeps the journal clean and still puts the QR on the HDMI console.
- The first-boot golden tests do not set `BITTERN_CONSOLE`, so the secrets golden is unchanged. The
  console path is covered by `TestFirstBootConsoleBlockGoesOnlyToTheConsole`.
- `go test ./internal/setupqr -run TestRenderQRRoundTrip` skipped only when `qrencode` or `zbarimg`
  is missing; on this host both are present and the encode/decode roundtrip passed.

## Unverified here

- The browser does not run in this container, so the fragment script was checked by source
  assertions and by the server-side one-shot tests, not by a real browser history and back-button
  test. BIT-020 owns the phone and desktop layout check.
- The QEMU firstboot scenario passed on 2026-11-17 with KVM and host `zbarimg`: the decoded QR was
  the exact `https://<lan0-address>/ui/setup#token=<token>` payload, the grouped token matched, the
  short code was accepted without consuming it, setup completed from the fragment, a second
  submission and a setup reload were rejected, and the journal, serial log, harness transcript and
  status contained neither credential. The adoption scenario with the same QR phase also passed.
  A real phone camera scan and browser history test remain.
- The support bundle is still BIT-010 and has no implementation to scan. When it lands, its
  allowlist and leak test must cover `setup_token`, the two short-code fields, and the fragment
  string.
- `THIRD_PARTY_NOTICES.md` lists qrencode as a Debian package; the image manifest and SBOM entry are
  still BIT-032 and release work.

## Plan delta

The implementation plan is being rewritten by a parallel worker and was not edited. The block below
is ready to paste when the near-term queue is updated.

```markdown
### BIT-019 Setup token QR exchange
**Status.** Implemented and verified on 2026-11-17: `setup-token --qr`, the first-boot console
block, the fragment reader, the short-code lifecycle and the package/runbook changes are in place.
The Go suite, including the qrencode-to-zbarimg roundtrip, and the QEMU firstboot scenario pass.
The QEMU run decoded the QR, completed setup from the fragment, rejected a second submission and
reload, and scanned the journal, status and logs for the token and short code. A real phone scan
and the BIT-020 phone/desktop layout check remain.
**Depends on.** BIT-007, BIT-011. **Size.** M.
```
