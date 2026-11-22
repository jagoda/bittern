# Hardware token capability check for optional custody

*Record what a hardware token would have to prove if the maintainer ever replaces the airgapped key
file.*

- Date: 2026-11-21
- Status: not run, not required. The chosen custody is the airgapped signing machine in ADR 0029.
  This note records the optional token alternative.
- Related: [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-032](../plan/implementation-plan.md#bit-032-mkosi-image-build-and-boot),
  [signing and A/B proposals](2026-11-21-signing-and-ab-proposals.md)

## Why this is not on the critical path

[ADR 0029](../adr/0029-release-signing-and-key-custody.md) chooses an offline signing machine: the
release key is a 32-byte Ed25519 seed in a file on an encrypted volume on a machine that never joins
a network. A hardware token that supports EdDSA in PIV may replace the key file without changing the
verifier. The maintainer chose the airgap for simplicity, so the token is an alternative, not the
default.

This check is only needed if a token is considered. It has not been run. Nothing in ADR 0029 depends
on it, and a failure here would not change the chosen custody; it would only rule out the optional
alternative.

## Why this runs on the host

The development container has no USB passthrough. It cannot see a token, open a PC/SC session, or
touch a key. Every command in this note runs on the maintainer's host with the token attached, in a
normal shell, not in the container.

This note does not claim the check has run. It records what a token would have to prove, the exact
commands, the expected outputs, and the pass criteria.

## What a token would have to prove

If a token is used, it must provide all of the following:

1. The token firmware exposes Ed25519 in the PIV applet. Yubico's technical manual lists PIV
   Ed25519 key generation from firmware 5.7.4, so require 5.7.4 or later and verify it with
   `ykman info`.
2. The key is generated in the token, in the chosen slot, and cannot be exported. The slot's PIN
   policy is once per session and its touch policy is always.
3. The public key can be exported as raw 32 bytes, and the SHA-256 of those bytes is the `key_id`
   used by the manifest and the trust store.
4. The token returns a raw 64-byte RFC 8032 signature over the exact bytes the host presents, with
   no pre-hash, no ASN.1 wrapper, and no truncation. The signature verifies over those bytes and
   not over a hash of them.
5. The token can sign a manifest at least as large as the largest release manifest. The input cap
   is a pass criterion: a manifest that does not fit cannot be signed without changing the signed
   bytes or the token.
6. The PIN is prompted and never appears in shell history, a process argument, or an environment
   variable. Each signature requires a touch. No agent caches the PIN.
7. The in-tree verifier accepts the signed vector and rejects a one-byte change. The verifier does
   not exist yet; see "In-tree verifier" below.

The check does not prove the release tool's confirmation UX. It uses a low-level token tool on a
fixed vector. The airgap signing tool must separately print the manifest fields and require
confirmation before the key is used; a tool that signs a path argument without showing the contents
is not acceptable.

## What is missing in the tree

The tree has no release manifest, canonicalizer, signature verifier, signing tool, or checked-in
release test vector. `tests/fixtures/release/` does not exist. There is no release-manifest
signature code; the signature code that exists is TLS and test SSH keys. The airgap release path
also needs the manifest type, canonicalizer, verifier, and signing tool; this note's token check
uses the same pieces.

- The canonical test vector below is defined by this note, not checked in. It must be added to the
  test fixtures before either path can sign a checked-in vector.
- The "verify with the in-tree verifier" step cannot run yet. The verifier arrives with BIT-009.
  Until then, use OpenSSL as an independent RFC 8032 verifier and record the result. The token check
  is not complete until the in-tree verifier passes the same vector.
- The release signing tool does not exist. The commands below use `ykman` and `yubico-piv-tool` to
  prove the token, not to sign a release.

## Test token and host preconditions

- A dedicated test YubiKey 5.7.4 or later, not a production token. The check generates a key and can
  leave the token reset.
- Set a non-default PIV PIN and management key on the test token before the check. Do not use the
  defaults.
- `ykman` with `ED25519` in `ykman piv keys generate --algorithm`, and `yubico-piv-tool` 2.6.1 or
  later with Ed25519 signing support. Record both versions.
- OpenSSL 3.x with Ed25519 `pkeyutl -rawin` support, `sha256sum`, `wc`, and `od`. On macOS, use
  `shasum -a 256` wherever this note says `sha256sum`.
- A trusted host with no agent that supplies the PIN. Close or stop any tool that might hold a PC/SC
  session; the check needs the token exclusively.

## The canonical test vector

This is the smallest vector the check needs: one app-bundle manifest with one payload file. It is
706 bytes and signs as 734 bytes with the domain prefix. The manifest is the canonical form from
ADR 0029 section 2: sorted keys, no whitespace, no trailing newline.

The `signing.key_id` is a placeholder (`2222...`) because the key id comes from the token. Do not
feed this placeholder vector to the manifest verifier: the verifier will reject it as an unknown
key. The check uses it for the token's raw signature capability and for canonicalizer development.
After key generation, it copies the vector, replaces `signing.key_id` with the token's key id,
recomputes the canonical bytes and hashes, and uses that token-specific copy for the full manifest
verification. The placeholder vector stays as the canonicalizer vector. The token-specific copy is
a second fixture to check in after the check, with the token's public key and signature.

The fixture paths to add under `tests/fixtures/release/` are:

| Path | Content |
| --- | --- |
| `manifest-v1/manifest.json` | The placeholder manifest bytes below, with no trailing newline. |
| `manifest-v1/manifest.sha256` | The SHA-256 below and the file name. |
| `manifest-v1/signing-input.bin` | The domain prefix followed by the manifest bytes. |
| `manifest-v1/signing-input.sha256` | The SHA-256 below and the file name. |
| `manifest-v1/README.md` | What the vector is, how to regenerate it, and that the key id is a placeholder; link it from `tests/README.md`. |

The manifest bytes, exactly one line and no trailing newline:

```json
{"channel":"stable","compatibility":{"arch":"amd64","os_generation_max":1,"os_generation_min":1,"schema_max":2,"schema_min":1,"updater_min":1},"created":"2026-11-21T00:00:00Z","files":[{"mode":"0644","path":"usr/lib/bittern/example","sha256":"0000000000000000000000000000000000000000000000000000000000000000","size":1}],"generation":1,"kind":"app-bundle","manifest_version":1,"payload":{"filename":"bittern-example.tar.gz","sha256":"1111111111111111111111111111111111111111111111111111111111111111","size":1},"product":"bittern","signing":{"key_id":"2222222222222222222222222222222222222222222222222222222222222222"},"source_commit":"3333333333333333333333333333333333333333","version":"0.0.0-token-check"}
```

The signing input is the 28 ASCII bytes `bittern-release-manifest-v1\n` followed by those 706
bytes. Expected:

| Check | Expected |
| --- | --- |
| `wc -c < manifest.json` | `706` |
| `sha256sum manifest.json` | `4fa961778a947699d48064140323d3ed378aaa21d4dd5c8e162b5b53a2f8e1c1` |
| `wc -c < signing-input.bin` | `734` |
| `sha256sum signing-input.bin` | `c8bf57b86c9e033f6ef08d39a70ffdd1562599b9c8e1b2b3d4d4f0f616356559` |

Create the two files and check them before any signing:

```sh
mkdir -p bittern-token-check
cd bittern-token-check
# Put the bytes from the JSON block above into manifest.json, with no trailing newline.
printf 'bittern-release-manifest-v1\n' > signing-input.bin
cat manifest.json >> signing-input.bin
wc -c < manifest.json
sha256sum manifest.json signing-input.bin
```

If the SHA-256 of `manifest.json` is not `4fa96177...`, the copy has a newline, a space, or a
different byte. Fix the copy; do not sign a vector that does not match.

## Commands

### 1. Check the token and firmware

```sh
ykman list
ykman info
ykman piv info
```

Expected: `ykman info` reports `Firmware version: 5.7.4` or later. `ykman piv info` reports the PIV
version, the PIN tries remaining, the management key algorithm, and no key in slot 82 before
generation. If the firmware is below 5.7.4, stop; the rest of the check does not apply.

### 2. Generate the Ed25519 key in slot 82

```sh
ykman piv keys generate \
  --algorithm ED25519 \
  --pin-policy ONCE \
  --touch-policy ALWAYS \
  82 pub.pem
```

Do not pass `-P` or `-m`. The tool prompts for the PIN and, if the management key is not the
default, for the management key. Enter them at the prompt. Do not set a PIN environment variable and
do not use an agent that caches the PIN.

Expected: a PIN prompt, a possible management-key prompt, and a success message. `pub.pem`
contains a PEM `PUBLIC KEY` block. The key must be generated on the token, not imported.

Slot 82 is deliberate. PIV slot 9c is the digital-signature slot, but its semantics require the PIN
immediately before every signature, which conflicts with the once-per-session policy. Slot 82 is a
retired key-management slot with no default contents and accepts the chosen policies.

### 3. Read back the policies and the key type

```sh
ykman piv keys info 82
```

Expected, with labels that vary by `ykman` version:

```text
Slot 82 (RETIRED KEY MANAGEMENT):
  Private key type: ED25519
  Generated on YubiKey: yes
  PIN policy: ONCE
  Touch policy: ALWAYS
```

The pass condition is that the output names ED25519, says the key was generated on the token, and
reports the PIN policy ONCE and the touch policy ALWAYS. A key reported as imported, or a policy
other than ONCE and ALWAYS, fails the check.

### 4. Export the public key as raw 32 bytes

```sh
ykman piv keys export --verify 82 pub-export.pem
```

`--verify` asks the slot to check that the exported public key matches the private key; it may
prompt for the PIN and a touch. If `--verify` fails for Ed25519, record the failure and use the
`pub.pem` written at generation for the remaining steps. Public-key export is the required
capability; the verification path is a check on top of it.

```sh
openssl pkey -pubin -in pub.pem -outform DER -out pub.der
openssl pkey -pubin -in pub-export.pem -outform DER -out pub-export.der
cmp pub.der pub-export.der
test "$(wc -c < pub.der)" -eq 44
tail -c 32 pub.der > pub.raw
test "$(wc -c < pub.raw)" -eq 32
od -An -tx1 -N 12 pub.der
od -An -tx1 pub.raw
sha256sum pub.raw
```

Expected: `cmp` prints nothing and exits 0; `pub.der` is 44 bytes; `pub.raw` is 32 bytes. The first
12 DER bytes are the Ed25519 SubjectPublicKeyInfo header:

```text
30 2a 30 05 06 03 2b 65 70 03 21 00
```

The last 32 bytes of `pub.der` are the raw public key. `sha256sum pub.raw` is the `key_id`: 64
lowercase hexadecimal characters. Run the export twice if the first result is ambiguous; two
exports must produce the same 32 bytes.

### 5. Sign the fixed vector

```sh
yubico-piv-tool -a verify-pin --sign -s 82 -A ED25519 \
  -i signing-input.bin -o manifest.sig
```

Do not pass `-P`. The tool prompts for the PIN. When the token blinks, touch it. Expected output:
`Enter PIN:`, then `Touch your YubiKey...`, then `Signature successful!`. The exact wording varies
by version; the pass condition is the 64-byte signature, not the message text.

The low-level tool signs a file path and does not print the manifest fields. It is not acceptable as
the release tool; it is used here only to prove the token capability on a fixed vector. The airgap
release tool is a separate deliverable.

Check the signature shape:

```sh
wc -c < manifest.sig
od -An -tx1 -N 4 manifest.sig
```

Expected: exactly 64 bytes. A DER-wrapped Ed25519 signature would be 70 bytes and start with
`30 44`; that is a failure. Do not assume the first two bytes cannot be `30 44` by chance; the
length check is the reliable one.

### 6. Verify the signature independently

```sh
openssl pkeyutl -verify -pubin -inkey pub.pem -rawin \
  -in signing-input.bin -sigfile manifest.sig
```

Expected: `Signature Verified Successfully`.

A one-byte change must fail:

```sh
cp signing-input.bin tampered.bin
printf 'X' | dd of=tampered.bin bs=1 seek=0 conv=notrunc 2>/dev/null
openssl pkeyutl -verify -pubin -inkey pub.pem -rawin \
  -in tampered.bin -sigfile manifest.sig
```

Expected: `Signature Verification Failure`.

A tool that pre-hashed the input produces a signature over the 32-byte SHA-256, not over the
presented bytes. Check that this fails:

```sh
openssl dgst -sha256 -binary signing-input.bin > signing-input.sha256.bin
openssl pkeyutl -verify -pubin -inkey pub.pem -rawin \
  -in signing-input.sha256.bin -sigfile manifest.sig
```

Expected: `Signature Verification Failure`. If this command succeeds, the token or tool signed a
hash. That is a failure of the required capability; do not change the verifier to accept a hash.

Ed25519 is deterministic. Sign the same input a second time and compare:

```sh
yubico-piv-tool -a verify-pin --sign -s 82 -A ED25519 \
  -i signing-input.bin -o manifest-2.sig
cmp manifest.sig manifest-2.sig
```

Expected: `cmp` prints nothing and exits 0. A difference means the tool or token is not signing the
same bytes, or is adding randomness.

### 7. Measure the input cap

The tool and token may accept less than a full release manifest. Probe increasing sizes:

```sh
for n in 700 2000 3000 3071 3072 4096; do
  head -c "$n" /dev/zero > "probe-$n.bin"
  if yubico-piv-tool -a verify-pin --sign -s 82 -A ED25519 \
       -i "probe-$n.bin" -o "probe-$n.sig" 2> "probe-$n.err"; then
    printf '%s ok %s\n' "$n" "$(wc -c < "probe-$n.sig")"
  else
    printf '%s failed: %s\n' "$n" "$(tail -n 1 "probe-$n.err")"
  fi
done
```

Each iteration prompts for the PIN and a touch; that is expected. The probe uses zero bytes, not a
manifest, because it measures the input cap only.

Expected: the smaller sizes succeed with 64-byte signatures, and the larger sizes fail with a
`File too big` or token error. The `yubico-piv-tool` source checked on 2026-11-21 reads Ed25519
input into a 3072-byte buffer and rejects a read that fills it, so 3071 bytes is the tool's practical
maximum.

The token's own limit may be lower. Record the largest accepted size.

The real question is whether the largest release manifest fits. A manifest with one file is 706
bytes. A real bundle adds one entry per installed file; each entry is roughly 130 bytes with a
64-character digest. A manifest with 20 files is about 3.2 KB, which is over the tool's cap. Measure
the largest expected manifest, not only the fixture. If it does not fit, the token is not a viable
alternative; do not truncate the manifest or pre-hash it to make it fit.

### 8. Check the PIN handling

After the signing runs, check that the PIN did not leak:

```sh
history | grep -E 'ykman|yubico-piv-tool'
ps -ef | grep -E 'ykman|yubico-piv-tool' | grep -v grep
```

Expected: no PIN, no `-P`, no `--pin`, and no management key in the output. The commands above omit
all of them, so the only acceptable result is that they do not appear. A tool that requires the PIN
on the command line is not acceptable for signing.

## In-tree verifier

The in-tree verifier does not exist yet. BIT-009 adds the manifest type, canonicalizer, verifier,
and test-key harness. The airgap release path needs it too. For the optional token check, when it
lands:

1. Add the token-specific manifest, its signature, and the token's public key under
   `tests/fixtures/release/`.
2. Write the public key to a test trust store as `<key_id>.pub` with the raw 32 bytes.
3. Run the verifier test that loads the trust store and verifies the manifest.
4. Confirm it accepts the vector and rejects a one-byte change.

The exact test command is defined when BIT-009 lands; this note does not invent a package or test
name. Do not add a test-only bypass to production verification to make this pass. The test trust
store is the existing test seam. If the verifier needs a new seam, that is a design change for
BIT-009, not a reason to weaken the production path.

## Pass criteria for the optional token

If a token is used, it passes only when all of these hold:

| Check | Pass condition |
| --- | --- |
| Firmware | `ykman info` reports 5.7.4 or later. |
| Slot policy | `ykman piv keys info 82` reports ED25519, generated on the token, PIN ONCE, touch ALWAYS. |
| Public key | The export is 44-byte SubjectPublicKeyInfo; the last 32 bytes are the raw key; `key_id` is their SHA-256; two exports match. |
| Signature shape | `manifest.sig` is exactly 64 bytes with no DER wrapper. |
| Signature semantics | OpenSSL verifies over the exact signing input and fails over a one-byte change and over a SHA-256 of the input. |
| Determinism | Two signatures over the same input are byte-identical. |
| Size | The largest expected manifest is at most the largest accepted probe size. |
| PIN handling | No PIN or management key appears in shell history, a process argument, an environment variable, or a log; each signature needs a touch. |
| In-tree verifier | The verifier accepts the token-specific vector and rejects a tampered one. Pending BIT-009. |

These are not ADR 0029 acceptance criteria. They decide only whether the optional token alternative
works.

## If the check fails

- Firmware below 5.7.4, or Ed25519 not offered: use a supported token. Do not import a key; the
  alternative requires generation in the token.
- A 64-byte signature that does not verify over the presented bytes: the tool or token pre-hashes
  or wraps. Try another host tool that presents raw bytes, and record which one works. Do not change
  the verifier to accept a hash.
- A size cap below the largest manifest: the token cannot sign the canonical manifest as ADR 0029
  section 1 defines it. The airgap remains the chosen path. Do not truncate or pre-hash to fit.
- A PIN prompt that requires a command-line argument, or an agent that caches the PIN: the tool does
  not meet the custody rules. Do not use it for releases.
- The in-tree verifier missing: the token capability can be proven, but the full manifest check is
  blocked until BIT-009 lands the verifier and the vector passes.

## Findings recorded

- The maintainer chose the airgap signing machine, not a token. This note is an optional
  alternative and has not been run.
- No release manifest, canonicalizer, verifier, signing tool, or release test vector exists in the
  tree. This note defines the missing vector; BIT-009 owns the code.
- Yubico's technical manual lists PIV Ed25519 for firmware 5.7.4 and 5.8.x. Require 5.7.4 or later
  and verify it on the token with `ykman info`. The firmware is a precondition, not an assumption.
- PIV slot 9c requires the PIN before every signature, so it cannot meet the once-per-session
  policy. Slot 82 is the candidate.
- `yubico-piv-tool` reads Ed25519 signing input into a 3072-byte buffer and treats a full buffer as
  too big, so its practical maximum is 3071 bytes. A real manifest may exceed this. This is one
  reason the airgap is simpler: it has no token input cap.
- The in-tree verifier is missing, so the full manifest check is blocked on BIT-009.

## What this note does not decide

Whether to use a hardware token. ADR 0029 chooses the airgapped signing machine, and a token is an
optional alternative.

The production token model, the final slot, the PIN and management-key ceremony, and the storage
locations, if a token is ever used. Those are outside the chosen path.

The airgap ceremony, the offline machine, the encrypted volume, the second key file, and the
key-loss procedure are ADR 0029 follow-ups, not part of this note.

## References

- [ADR 0029: release signing and key custody](../adr/0029-release-signing-and-key-custody.md)
- [BIT-009: app bundle update transaction](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback)
- [BIT-032: mkosi image build and boot](../plan/implementation-plan.md#bit-032-mkosi-image-build-and-boot)
- [Signing and A/B proposals](2026-11-21-signing-and-ab-proposals.md)
- RFC 8032, Ed25519: <https://www.rfc-editor.org/rfc/rfc8032>
- YubiKey PIV compatible specifics, checked 2026-11-21:
  <https://docs.yubico.com/hardware/yubikey/yk-tech-manual/yk5-apps-piv.html>
- YubiKey firmware 5.7 specifics, checked 2026-11-21:
  <https://docs.yubico.com/hardware/yubikey/yk-tech-manual/yk5-firmware-5.7.html>
- YubiKey Manager PIV commands, checked 2026-11-21:
  <https://docs.yubico.com/software/yubikey/tools/ykman/PIV_Commands.html>
- yubico-piv-tool signing, checked 2026-11-21:
  <https://developers.yubico.com/yubico-piv-tool/Actions/signing.html>
- OpenSSL `pkeyutl`, checked 2026-11-21:
  <https://docs.openssl.org/master/man1/openssl-pkeyutl/>
