# 0029. Release signing and key custody

*Application bundles and OS image artifacts are signed with Ed25519 over a canonical manifest, and the
appliance verifies them against a public key pinned in the image, never against data the bundle, the
owner, or the environment can change. The release private key is a key file on an airgapped signing
machine; a hardware token is an optional alternative.*

- Status: Proposed
- Date: 2026-11-21
- Deciders: maintainer
- Related: [ADR 0007](./0007-mkosi-os-image-and-releases.md),
  [ADR 0008](./0008-updates-rollback-and-reset.md),
  [ADR 0010](./0010-security-and-privacy-posture.md),
  [ADR 0012](./0012-testing-and-release-gates.md),
  [ADR 0021](./0021-security-updates.md),
  [ADR 0030](./0030-ab-from-the-first-image.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-032](../plan/implementation-plan.md#bit-032-mkosi-image-build-and-boot),
  [BIT-009 recon](../notes/2026-11-21-bit009-update-recon.md),
  [hardware token capability note](../notes/2026-11-21-hardware-token-capability.md),
  [managed update path](../notes/2026-11-15-managed-update-path.md),
  [architecture](../architecture.md)

## Context

[ADR 0008](./0008-updates-rollback-and-reset.md) requires the app bundle transaction to verify the
signed manifest and its compatibility with the running OS generation, and to reject an older
generation. Its follow-up list includes "Define the bundle manifest, signature scheme, and
compatibility metadata" and "Decide where update metadata is hosted and how it is signed." ADR 0012
makes "image and bundle artifacts are signed and signature verification is tested" a release gate.
ADR 0007 requires the signing mechanism and key custody to be recorded before the first public
release. ADR 0021 proposes automated build and test with a manual maintainer signing gate, and
defers the tool and custody to another ADR.

None of that exists in the tree. There is no manifest type, no signature code, no trust anchor, and
no release key. The current POC payload verifies `SHA256SUMS` from the same archive it installs.
That is an integrity check against accidental corruption, not authenticity: an attacker who can
replace the archive can replace the checksum. The update server is untrusted, the appliance may be
offline, and the owner is not expected to inspect a signature.

The choice is narrow. This ADR decides the signature primitive, the bytes that are signed, the
manifest fields, the trust anchor, the custody mechanism, the rotation rule, the recovery key, and
the test-key path. Hosting, the release server, the OS image signature envelope and verification
point, the update transaction, and the A/B layout are decided elsewhere. The decision must work
without CI, without a cloud service, and with one maintainer.

Facts dated 2026-11-21: Go's standard library provides Ed25519 in `crypto/ed25519`, so an in-tree
verifier needs no external dependency. Ed25519 is specified by RFC 8032. Some hardware tokens
support Ed25519 and some do not. The chosen custody is an offline signing machine, so the release path has
no token firmware dependency. A token remains an optional alternative, and its capability check in
[the token note](../notes/2026-11-21-hardware-token-capability.md) has not been run and is not
required. Cosign and TUF are external tools with their own release and verification models.
Re-check these facts before the first release if the toolchain changes.

## Decision

### 1. Ed25519 over a canonical manifest

Application bundles and OS image artifacts are signed with Ed25519 as specified by RFC 8032, in
pure mode, with a 32-byte public key and a 64-byte signature. For an application bundle, the signed
object is the canonical manifest, not the bundle archive and not a digest obtained from the same
channel as the archive. The detached signature is `manifest.sig`, exactly 64 bytes. During a key
rotation, additional signatures are stored as `manifest.sig.<key_id>`, one file per signer.

The signed message is the ASCII bytes `bittern-release-manifest-v1\n` followed by the canonical
manifest bytes. The domain prefix prevents a signature made for another document type from being
replayed as a release manifest.

The manifest names its signer in `signing.key_id`: the lowercase hexadecimal SHA-256 of the raw
32-byte public key. The verifier looks the key up in the pinned trust store and requires the key it
used to verify to have that id. A key id is a selector, not a trust decision.

OS image artifacts use the same primitive and trust-anchor rule. The image ADR owns the image's
signature envelope, the signing tool invocation, and the point where the image signature is verified.
It must use this scheme or supersede this ADR for images.

### 2. The canonical form

The manifest is canonical JSON. The canonical form is:

- UTF-8 without a byte-order mark. All strings are printable ASCII from U+0020 to U+007E, so the
  form is valid UTF-8 without a Unicode normalization question.
- One top-level object. Object keys are lowercase ASCII letters, digits, and underscore, matching
  `[a-z0-9_]+`. Keys are unique and sorted by byte order.
- No whitespace outside strings and no trailing data.
- Strings are double-quoted. The only escapes are `\"` for a quote and `\\` for a backslash. Control
  characters, non-ASCII characters, and `\u` escapes are rejected.
- Numbers are integers in the signed 64-bit range, written in decimal with no leading zero, no plus
  sign, no fraction, and no exponent. Floating-point numbers and `null` are rejected.
- `true` and `false` are the only other literals. Arrays preserve order.

The verifier parses the manifest with a strict parser, re-encodes it to this form, and requires the
result to equal the received bytes. It never normalizes, pretty-prints, or repairs a manifest. A
semantically valid but non-canonical manifest is rejected. The in-tree canonicalizer is the only
writer; the release tooling must use it.

This is a restricted JSON canonical form in the style of RFC 8785 (JCS), not full JCS. The
restriction is deliberate: the manifest needs no floating-point numbers, no `null`, no Unicode
strings, and no general escape syntax. A smaller form is easier to audit and has fewer
implementation differences between the signer and the verifier.

### 3. What the manifest contains

The table uses dotted paths for nested objects. The JSON keys themselves are the path segments, so
`compatibility.arch` is a member named `arch` inside the object named `compatibility`.

| Field | Meaning |
| --- | --- |
| `manifest_version` | Integer format version. This ADR defines version `1`; an unknown version is rejected. |
| `kind` | `app-bundle` or `os-image`. The app transaction accepts only `app-bundle`; the image path accepts only `os-image`. |
| `product` | `bittern`. A manifest for another product is rejected. |
| `version` | Human-readable release version, printable ASCII, shown to the owner and recorded in the update history. |
| `generation` | Monotonic integer anti-rollback counter. It is not the configuration generation. |
| `channel` | The release channel, `stable` or `beta` in the current schema. The manifest channel must match the device channel. |
| `created` | RFC 3339 UTC timestamp. It is recorded but is not a trust input. |
| `source_commit` | The git commit the bundle was built from, lowercase hexadecimal. It is provenance, not a trust input. |
| `signing.key_id` | Lowercase hexadecimal SHA-256 of the raw Ed25519 public key that signed this manifest. |
| `compatibility.arch` | The architecture, `amd64` for v1. |
| `compatibility.os_generation_min` | The oldest OS image generation this bundle runs on. |
| `compatibility.os_generation_max` | The newest OS image generation this bundle runs on. |
| `compatibility.schema_min` | The oldest appliance schema version this bundle can read and migrate. |
| `compatibility.schema_max` | The newest appliance schema version this bundle can read. A newer schema is rejected. |
| `compatibility.updater_min` | The oldest update helper version that can apply this manifest. A bundle that needs a newer helper requires an image update. |
| `payload.filename` | The bundle archive file name, not a path. |
| `payload.size` | The archive size in bytes. |
| `payload.sha256` | Lowercase hexadecimal SHA-256 of the archive bytes. |
| `files` | Every file the bundle installs: `path`, `size`, `sha256`, and `mode`. |

The `files` list is the complete app bundle, not a patch. Paths are relative to the bundle root, use
ASCII, and must not be absolute, contain `..`, or point at a symlink, hardlink, device node, socket,
or FIFO. The array is sorted by `path` in byte order; the verifier does not reorder it, but the
release tooling must emit it sorted so two builds produce the same manifest. `mode` is a four-digit
octal string such as `0644` or `0755`; owner and group are root and are not carried in the manifest.
A file present in the previous bundle but absent from the new manifest is removed at activation, so a
stale unit or script cannot survive an update. A bundle that would install a file into the trust
store is rejected: the transaction maps bundle paths to the app generation and never to the trust
directories.

The archive contains `manifest.json`, its signature files, and the payload files. The payload digest
covers the archive including the manifest and signatures. The `files` list covers the payload files
the transaction installs, not the manifest or signature. The manifest does not list itself.

The `generation` field is the app bundle generation. It is separate from the configuration
generation in `/var/lib/bittern/generations/<n>/` and `/var/lib/bittern/current`, which advances on
every apply. It is also separate from the OS generation in an `os-image` manifest. The app bundle's
`compatibility.os_generation_min` and `compatibility.os_generation_max` refer to the OS image
generation; the app bundle's own `generation` is the app anti-rollback counter.

### 4. Trust anchor and custody

Production public keys are files under `/usr/lib/bittern/trust/release-keys/` in the image. The
directory and files are owned by root, the directory is mode 0755, and each key file is mode 0644,
contains exactly the 32 raw bytes of the public key, and is named `<key_id>.pub`, where `<key_id>` is
the lowercase hexadecimal SHA-256 of those bytes. A file whose name does not match its content is
rejected.

A root-owned rotation store under `/etc/bittern/trust/release-keys/` may add keys before a new image
retires an old one. It follows the same ownership, mode, content, and naming rules. The verifier reads
both directories and trusts the union. Either directory may be absent: a missing directory is not an
error when the other store yields at least one valid key. The union must contain at least one key, so
two absent or empty stores are `trust.missing`. The image store is read first. A duplicate key id
with identical bytes is one key; a duplicate id with different key material is `trust.duplicate` and
is rejected rather than overriding the image key. A key in the image store cannot be removed by the
rotation store; retiring it requires a new image that does not carry it. The verifier rejects
symlinks, non-root ownership, group- or other-writable files, duplicate key ids with different key
material, and a key file whose id does not match its content. The verifier fails closed if it cannot
read a valid trust store or if neither store yields a key.

The rotation store must survive an OS slot switch and an OS rollback. If it lives inside an OS slot,
a rollback can remove a key that the current app bundle depends on. The image ADR must place it on the
shared partition or otherwise keep the image key set compatible with the running app bundle.

The verifier never reads a key from `appliance.yaml`, the bundle, the manifest, an environment
variable, a command-line flag, the channel, the UI, or the release server. The image is the root of
trust; the image signature covers the key files. The image signature and its verification point are
the image ADR's decision.

The root-owned store is part of the trusted computing base. An attacker who can write it as root has
already compromised the appliance; that case is outside this threat model and is covered by
[ADR 0010](./0010-security-and-privacy-posture.md).

On a release build there is no test key and no bypass. No build tag, environment variable,
configuration field, CLI flag, or UI action can disable verification or replace the trust store. The
existing `BITTERN_*` variables are install and test seams, not a precedent for trust.

#### Custody: an offline signing machine

The release private key is a 32-byte Ed25519 seed in a file, mode 0600, owned by the signing user,
on an encrypted volume on a machine that never joins a network. The key is generated on that
machine, never on the build host, the release server, the appliance, or a CI runner. The signing
machine holds the signing tool, the key, and nothing else. The tool is a static binary built
reproducibly from this repository, and its hash is recorded before the machine is used.

The airgap is the control. The only things that cross the boundary are the canonical manifest to the
signer and the 64-byte signature back. The payload never crosses: the manifest already carries the
payload digest and the file list, and the verifier checks the payload against them. The machine
never joins a network, and there is no writable path from the build host to it.

Before writing a signature, the tool prints product, version, generation, channel, `source_commit`,
`payload.sha256`, `key_id`, and the SHA-256 of the manifest bytes, and requires a typed
confirmation. The human compares `source_commit` and `version` against the build host's output and
confirms the rest of the fields. A tool that signs a path argument without showing the contents is
not acceptable.

The tool refuses to run unless all of these hold for the key file:

- It is exactly 32 bytes.
- It is owned by the signing user.
- It is mode 0600: not group-readable and not other-readable.
- It is a regular file, not a symlink.

It also refuses to run when the key path is on a filesystem other than the expected encrypted
volume, when the tool can check that.

The key is never printed, logged, copied into a support bundle, or committed to git. It is not
backed up by copying it to the build host or a network share. The recovery copy is a second key file
on a second encrypted volume, held separately (section 8).

#### Optional hardware token

A hardware token that supports EdDSA in PIV may replace the key file without changing the verifier.
It must export the raw 32-byte public key, return a raw 64-byte signature over the bytes presented
with no pre-hash or ASN.1 wrapper, be drivable from the host command line, and accept a manifest of
the largest release size. The maintainer chose the airgap for simplicity, so the token is an
alternative, not the default. [The token capability
note](../notes/2026-11-21-hardware-token-capability.md) records the check; it has not been run and is
not required for acceptance.

#### Threat model: what the signature attests

A compromised build host can obtain a valid signature. The signature attests that the maintainer
approved the manifest's claims (version, generation, source commit, digests), not that those claims
are true. Build integrity is out of scope for v1. The mitigations are reproducible builds or an
independent rebuild before signing.

The custody mechanism does not protect provenance. A signature proves that the signing key approved
the bytes of the manifest and the claims in it. It does not prove that the bundle was built from the
named `source_commit`, that the digests describe what the build produced, or that no code was added
after the build. A compromised build host can produce a manifest whose fields are internally
consistent with a malicious payload and hand it to the signing machine.

The airgap protects the key and the human gate. It keeps the key off the build host and the network,
and it makes the signing step a deliberate act with a printed manifest. It does not prove
provenance: a host that built something else can describe it accurately, and the human gate checks
the manifest, not the build. The confirmation step catches a manifest the maintainer did not intend;
it does not catch a build that produced something else.

Reproducible builds are a follow-up decision in their own right. They are the only control that
verifies provenance rather than asserting it: an independent party rebuilds from `source_commit` and
compares the artifacts, or the maintainer rebuilds independently before signing. Until that exists,
the release signature is an approval of claims, not a proof of build integrity. v1 accepts this
residual risk explicitly; it is not a gap that a later implementation can close by signing more.

### 5. Verification order in the transaction

The transaction verifies in this order. A failure before promotion leaves the live bundle,
configuration, and database unchanged.

1. **Preflight.** Under the update transaction's exclusive lock, read the configured channel, check
   clock validity and free space, and read the committed bundle generation and the active update
   journal. No live write.
2. **Fetch.** Fetch `manifest.json` and its signature files over TLS with bounded size and time.
   Reject a redirect to plain HTTP or to a different host. The fetch client and the channel metadata
   format belong to BIT-009 and the release ADR; this ADR fixes only that the manifest is fetched
   and verified before the payload is trusted.
3. **Canonical check.** Parse strictly, re-encode to the canonical form, and require byte equality.
   Reject duplicate keys, non-ASCII strings, floats, `null`, trailing data, and any byte sequence
   that does not re-encode identically.
4. **Key selection.** Read `signing.key_id` and select the pinned key. Reject an unknown key, a key
   outside the trust store, a duplicate id with different material, and a key file that fails its
   ownership or mode check.
5. **Signature.** Verify the detached signature over the domain prefix and the canonical bytes.
   `manifest.sig` is required and must verify with `signing.key_id`. Each present
   `manifest.sig.<key_id>` must verify with that key. The manifest is accepted when at least one
   signature verifies with a trusted key; a rotation statement that requires two signatures is
   accepted only when both verify. A present but truncated, wrong-length, or invalid signature is a
   rejection, and a manifest with no valid signature is rejected. Until this step passes, every other
   manifest field is untrusted input.
6. **Identity.** Validate `manifest_version`, `kind`, and `product`. The app transaction accepts only
   `manifest_version` 1, `kind` `app-bundle`, and `product` `bittern`.
7. **Channel.** Require the manifest channel to equal the device channel. A channel change does not
   lower the generation: a beta device that returns to stable waits for a stable release with a
   higher generation, and a stable device cannot install a beta manifest without changing its
   channel.
8. **Compatibility.** Require `arch` to match. Require the running OS generation, read from the
   image's root-owned metadata and never from `appliance.yaml`, to be within
   `os_generation_min` to `os_generation_max`. Require the running appliance schema version to be
   within `schema_min` to `schema_max`; a newer schema is rejected because the bundle cannot read it,
   and an older schema is rejected because the bundle does not support migrating it. Require the
   running helper version to be at least `updater_min`. A bundle that needs a newer helper is an
   image update, not an app update.
9. **Generation.** Require `generation` to be greater than the committed bundle generation. Equal is
   accepted only when the active journal's candidate manifest digest matches this manifest, which is
   a resume of the same update. Lower is always rejected, even with a valid signature. The committed
   generation advances only at commit and is stored with the update state, not in
   `/var/lib/bittern/current`.
10. **Payload digest.** Fetch the archive. Verify `payload.size` and `payload.sha256` before reading
    or extracting the whole archive.
11. **Extract and verify files.** Extract into a staging directory that is not executable and is
    removed on failure. For every file, verify the path rules, size, SHA-256, mode, and the absence
    of setuid or setgid bits. Reject any archive file not in the `files` list and any manifest file
    missing from the archive. Create parent directories as root with mode 0755.
12. **Promote and activate.** Re-verify the staged tree, then move it to an immutable bundle
    generation on the shared partition. The activation pointer moves only after this step. A failure
    after activation is a rollback, not a verification failure, and the update record names the step.

The record stores the SHA-256 of the canonical manifest bytes, the signer key id, the bundle
generation, the version, the channel, the result, and the failing step. The manifest digest is a
record for support and comparison, not a trust input.

### 6. Rejection rules

The verifier rejects, and the transaction records, at least these categories:

- a missing, truncated, or non-canonical manifest;
- a missing, truncated, or invalid signature;
- an unknown signer, a signer outside the trust store, or a key file that fails its ownership check;
- a wrong `manifest_version`, `kind`, or `product`;
- a channel mismatch;
- an incompatible architecture, OS generation, schema version, or updater version;
- a generation that is older than, or equal to, the committed generation without a matching resume;
- a payload size or digest mismatch;
- a file path, size, digest, mode, or file-set mismatch;
- an extra file in the archive or a missing file from the archive.

There is no "ignore signature", "allow unsigned", "accept older generation", or "use this key instead"
option in a release build. A failure names the check and the next action in the update record and the
UI; it does not say only "signature invalid".

### 7. Key rotation

Outside a rotation overlap, a release is signed by one active key. The trust store may contain more
than one key.

A planned rotation adds the new key to the rotation store with a rotation statement signed by both
the outgoing active key and the incoming key. The statement names the outgoing and incoming public
keys and their key ids. The verifier accepts the statement only when the outgoing signature verifies
with the named outgoing key, that key is currently trusted, the incoming signature verifies with the
named incoming key, and the outgoing key is in the current set. The statement adds the incoming key;
it does not remove the outgoing key from the image store. A normal app bundle cannot change the trust
store. An OS image that changes the key set embedded in `/usr/lib/bittern/trust/release-keys/` must
be signed by the current key, and for a planned rotation by both the current and the incoming key.
The carrier of the rotation statement (an OS image, a dedicated rotation artifact, or another signed
object) is the image and release ADRs' decision; this ADR fixes the dual-signature rule.

After the rotation statement is installed, releases may be signed by the new key. During the overlap,
releases are signed by both the old and the new key, so a device that has not installed the rotation
can still verify them with the old key and a device that has installed it can verify them with the
new key. The old key is retired by a new image that no longer carries it; that image must be signed
by the new key and, during the overlap, by the old key as well, so a device that has not installed
the rotation can still verify the image. The old key is dropped from release signing only when the
maintainer accepts that devices which missed the rotation will not update remotely. Without telemetry
there is no way to know when that is true, so the overlap is indefinite or the risk is accepted
explicitly.

A recovery rotation is a variant of the same statement. The first image pins the primary release key
and a separately generated recovery key (section 8). If the primary key is lost, the recovery key
takes the outgoing role and the replacement key takes the incoming role. The statement names the
lost primary key id, the recovery key id, and the replacement key id. The verifier accepts it only
when the recovery signature verifies with the pinned recovery key, the replacement signature
verifies with the named replacement key, and both keys are in the current set. It is the only
statement that does not carry a signature by the lost primary key. It adds the replacement key; it
does not remove the lost primary key, which only a new image can do. The new image that carries the
replacement trust store is signed by the recovery key and the replacement key, so a device that
trusts the first image can verify it.

If the pinned key must change because the image is rebuilt with a new anchor, the new image must be
signed by the current key, or by both the current and the new key. A device on the old image cannot
verify an image signed only by a key it does not trust. The new image carries the new trust store;
after it boots, the device trusts the new key. If the current key is unavailable, the image cannot be
verified remotely and the device needs the recovery path below.

### 8. Key loss and compromise

A lost release key has no remote replacement from the lost key itself. A device that trusts only the
lost key cannot verify a new manifest. The physical recovery path is the USB or physical reinstall
from ADR 0008, using an image that carries a new anchor. The recovery key below is the path that
avoids a physical reinstall; the physical path remains for when the recovery key is also lost or
unavailable. The key-loss procedure must be written and tested before the first release, including
how the owner is told, which image is used, and how the device is re-anchored.

The first image pins a second recovery key, generated separately and held as a second key file on
a second encrypted volume. Both keys are in the image store as `<key_id>.pub` files under the rules
in section 4. The recovery key file is stored separately from the primary key file and from the
signing machine, and it is not used for routine releases. If the primary key file is lost or
destroyed, the recovery key signs the recovery rotation statement in section 7 with the replacement
key, and signs the new image that carries the replacement trust store. That is what keeps a lost key
from automatically forcing a physical reinstall for every device.

The recovery key is not a backup of the primary key. It is a separate key with its own key id,
generated separately and held on its own encrypted volume, and it cannot produce a signature that
verifies under the primary key id. Pinning it adds a second trusted signer, so a compromise of the
recovery key is as serious as a compromise of the primary key. The recovery key file follows the
same mode, ownership, and encrypted-volume rules as the primary.

The lost key is removed only by the new image. Until a device installs that image, it still trusts
the lost key, so a lost primary key is treated as a possible compromise until the new image is
installed. The recovery path restores the ability to ship a trusted image; it does not invalidate
signatures made with the lost key. If both key files are lost, the physical reinstall remains the
only path.

A compromised release key is worse. An attacker with the private key can sign a bundle or image that
devices accept until they receive a new trust store. A dual-signed rotation can stop a compromised
incoming key from installing itself without the old key, but it does not stop an attacker who holds
the old key: they can generate a new key and sign both halves of the statement. It also does not
invalidate bundles the attacker already signed, and the attacker can still sign old-key releases
that devices on the old image accept. Once the old key is compromised, there is no secure remote
recovery: any image the device can verify with the old key could have been signed by the attacker.
The response is an advisory, a new image installed through the physical recovery path, and physical
recovery for devices that may have installed a malicious bundle. There is no online revocation
service, transparency log, or timestamp authority in v1, so revocation is offline and eventual.
This is why the private key stays on the airgapped signing machine, never on the build host or a
network share, and is used manually per release.

### 9. Test-key path

The test key is generated per test run with `crypto/ed25519`; no private key is committed to git. The
test signs a manifest and fixture bundle with the generated key and gives the verifier the matching
public key through a test-only trust store constructor. The production verifier has no test branch,
no environment variable, and no build tag that accepts a test key.

The QEMU harness generates a test key, writes its public key into the guest's root-owned rotation
store at `/etc/bittern/trust/release-keys/` as root before the update, and signs the fixture manifest
and bundle on the host. The guest's verifier uses the same trust-store path as production; only the
key material differs. The test key is removed with the guest. A release image contains no test key,
and a release trust store rejects a manifest signed only by the test key.

The unit tests cover the canonical form, the signature, the key selection, every rejection category,
the generation resume rule, and the rotation statement. The canonicalizer has checked-in test
vectors so a second implementation can verify the form. The QEMU scenario covers a valid update, an
invalid signature, a digest mismatch, a downgrade, and a rollback.

The test-key path is enough to build and test the verifier and the transaction before this ADR is
accepted. It is not a release path and does not satisfy the ADR 0012 gate. The release gate is
satisfied only when a release artifact is signed by the release key and the verifier is tested
against the production trust store.

### 10. Out of scope

- Hosting, the release server, channel metadata, and the update-check policy (OQ-12). The release ADR
  owns them.
- The OS image signature envelope, the image signing tool invocation, and the point where the image
  signature is verified. The image ADR owns them; this ADR fixes the primitive and trust-anchor rule
  they must use or supersede.
- The bundle archive format, the fetch client, the transaction steps, backup, database migration,
  health checks, and rollback. BIT-009 and ADR 0008 own them.
- The A/B slot layout, the bootloader, and slot selection. ADR 0030 and the image ADR own them.
- CI, the build pipeline, and the release cadence. ADR 0021 and the release ADR own them.
- The exact signing machine, encrypted volume, and key file layout; the key ceremony; and the
  key-loss procedure. The airgap is decided here; those details are follow-ups below.
- The optional hardware token and its capability check. The airgap is the chosen mechanism; the
  token is an alternative, not a requirement.
- Reproducible builds and independent rebuilds. They are the follow-up decision that would verify
  provenance rather than assert it; v1 signs the maintainer's claims about the build.
- Secure boot, TPM, and disk encryption (OQ-06).

## Relationship to ADR 0008 and ADR 0021

[ADR 0008](./0008-updates-rollback-and-reset.md) is Accepted and requires a signed manifest and an
anti-rollback generation. This ADR fills its follow-up for the manifest and signature; it does not
change the transaction order, the backup order, or the rollback semantics. ADR 0021 is Proposed and
proposes automated build and test with a manual maintainer signing gate. This ADR supplies the
mechanism that ADR 0021 defers. Accepting one does not accept the other; if both are accepted, they
are consistent.

## Consequences

### Positive

- The signature is verified offline, on the appliance, with no cloud service, no OCI registry, and no
  network beyond the bundle fetch. The same artifacts work for a USB update.
- Ed25519 is in Go's standard library, so the verifier is small, vendorable, and testable. There is
  no external verifier to trust or pin.
- One manifest format and one signature primitive cover app bundles and OS images. The app bundle
  path has one signing step and one verification rule; the image path reuses the primitive and may
  wrap the signature differently.
- The trust anchor is not owner-editable. A compromised update server, a modified `appliance.yaml`,
  or a tampered bundle cannot change the key or disable verification. A compromised server can
  withhold updates or serve a validly signed bundle, but it cannot change the code, the key, or the
  generation.
- The canonical form and the domain prefix make the signed bytes deterministic and prevent
  cross-document replay.
- The private key never exists on the build host, the release server, the appliance, or a CI runner.
  It lives on one encrypted volume on a machine that never joins a network.
- The airgap keeps the key off the network, and the tool's key-file checks reject a key that is
  readable by another user or reachable through a symlink.
- The second key file on a separate encrypted volume gives a lost primary key a recovery path
  without a physical reinstall for every device.
- The test-key path lets the transaction be built and tested before the release key exists.

### Negative / costs

- Signing is manual per release. The maintainer is the bottleneck, and a release cannot ship while
  the signing machine or its key holder is unavailable.
- Signing needs the offline machine, the encrypted volume, the key file, and a human at the console.
  It cannot be unattended, and the maintainer must be physically present.
- Key custody is an operational burden: an offline machine, an encrypted volume, a second key file,
  a ceremony, and a key-loss procedure. A lost primary key has no recovery without the second key
  file, and losing both has no online recovery.
- The offline machine is a second system to maintain, patch offline, and keep physically secure.
  The encrypted volume and the key file are a single point of failure.
- The airgap ritual is manual and error-prone: the wrong manifest or signature can be carried, and
  the tool's printed fields are the main check.
- The recovery key is a second trusted signer, so a compromise of the recovery key is as serious as
  a compromise of the primary key.
- The signature still does not attest build integrity. A compromised build host can obtain a valid
  signature over a manifest that accurately describes malicious output. Reproducible builds or an
  independent rebuild before signing are the only mitigations, and both are follow-ups.
- Rotation needs an overlap where releases are signed by both keys. The old key is retired by a new
  image, and without telemetry the maintainer cannot know when every device has installed it. The
  overlap is indefinite or the risk of stranding devices is accepted explicitly.
- The canonicalizer and verifier are security-critical in-tree code. A parsing or canonicalization
  bug is a verification bypass; they need their own tests, test vectors, and review.
- Revocation is offline and eventual. A compromised key can sign releases that devices accept until
  they receive a new trust store. There is no transparency log or timestamp authority in v1.
- The ASCII-only canonical form is deliberately narrow. It rejects manifests from generic JSON tools
  that add whitespace, floats, Unicode, or a different key order. The release tooling must use the
  in-tree canonicalizer, which is one more thing to maintain.
- If the optional hardware token is used, its input cap may be below the size of a real manifest and
  its capability check has not been run. The airgap path does not have that limit.

### Follow-ups

- Exercise the airgap ritual end to end: generate a test key on the offline machine, sign a
  canonical manifest, carry it back, verify it with the in-tree verifier, and record the tool hash
  and the key-file checks. This is the release-path rehearsal, not the optional token check.
- Write the key ceremony and custody procedure for the primary and recovery key files: who holds
  each, where each encrypted volume is stored, how the public keys are exported and pinned in the
  first image, how access is logged, and how a release is signed and verified.
- Write and test the key-loss and compromise procedure, including the recovery rotation statement,
  the replacement image, and the physical recovery path when both key files are lost.
- Choose the offline machine, encrypted volume, and key file layout, and confirm the key-file
  checks, before the ceremony.
- Add the release manifest vector from the [token capability
  note](../notes/2026-11-21-hardware-token-capability.md) to the test fixtures, with its canonical
  bytes and SHA-256, and use it in the verifier tests.
- The optional hardware token is not on the critical path. Run the [token capability
  note](../notes/2026-11-21-hardware-token-capability.md) only if the maintainer considers a token;
  do not block acceptance on it.
- Decide reproducible builds in their own ADR. They are the only control that verifies provenance
  rather than asserting it, and v1 does not have them. Include the independent rebuild before
  signing as an interim option.
- Implement the manifest type, canonicalizer, verifier, and test-key harness in
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback).
- Add canonicalization test vectors and the negative tests listed in this ADR.
- Add the signing step to the release process and verify every release on a clean machine before
  publication.
- Decide the rotation carrier and the storage location with the image and release ADRs, and prove
  that the store survives an OS slot switch. Section 4 now records the union rule, the missing-store
  rule, and the duplicate-id rule; this follow-up owns the carrier and the slot-surviving placement.
- Record the key id and manifest digest in the update record and the support bundle.
- Add the signing and verification check to the ADR 0012 release checklist and the release notes.
- Revisit TUF or cosign if the project grows beyond one maintainer or needs a hosted repository with
  expiry, transparency, or online revocation.

## Alternatives considered

- **Cosign key pair.** Cosign is a mature tool with a known verification story, and it can use an
  offline key, an HSM, or a KMS. The costs are real: it adds a large Go tool and a second supply
  chain to the release host and the appliance; the appliance would vendor it or shell out to it,
  which grows the trusted computing base; its signature envelope and OCI artifact handling are more
  complex than a 64-byte detached signature; using Ed25519 would be a non-default key configuration
  or a second format; keyless mode needs OIDC, Fulcio, and Rekor, which is a cloud dependency the
  default path cannot assume; and it does not solve the key-loss procedure or the anti-rollback
  generation. Rejected because it adds a tool and a format without removing the custody problem,
  and the appliance needs a small offline verifier.
- **TUF repository metadata.** TUF adds root, targets, snapshot, and timestamp roles, metadata
  expiry, and a repository. It gives the strongest rotation and anti-rollback story, including
  freeze and rollback protection, and it can sign a repository of manifests. The costs are large for
  one maintainer: four roles with different keys and custody, an online or frequently used
  timestamp/snapshot signer, a metadata repository, a client that implements the full update
  workflow, and a root key ceremony. The appliance still needs a pinned root, so the trust-anchor
  problem remains. Rejected for v1; it can be added later around the same Ed25519 artifacts if the
  project grows a hosted repository and a second operator. If TUF is adopted later, it supersedes
  the metadata layer, not the signature primitive.
- **No signature.** Rejected because ADR 0012 makes signed artifacts and tested verification a
  release gate, ADR 0008 requires a signed manifest, and the POC's `SHA256SUMS` is in the same
  archive and is integrity, not authenticity. A compromised update server could replace the bundle
  and the checksum. The owner cannot inspect a signature, so the device must. Without a signature the
  generation field is attacker-controlled too, so anti-rollback cannot be enforced against a
  malicious server.
- **Minisign or signify.** These are small Ed25519 detached-signature tools with mature formats. They
  would still need a canonical manifest, and they add a release-host tool and a signature format with
  a key id and a trusted comment. The appliance would parse that format or shell out to it. Go's
  standard library already provides Ed25519, so the in-tree verifier is smaller and has no external
  tool. Rejected because the signature format is a second dependency without a custody benefit.

## References

- [ADR 0007: mkosi OS image and releases](./0007-mkosi-os-image-and-releases.md)
- [ADR 0008: updates, rollback, and reset](./0008-updates-rollback-and-reset.md)
- [ADR 0012: testing and release gates](./0012-testing-and-release-gates.md)
- [ADR 0021: managed security updates and release automation](./0021-security-updates.md)
- [ADR 0030: A/B from the first image](./0030-ab-from-the-first-image.md)
- [BIT-009: app bundle update transaction](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback)
- [BIT-032: mkosi image build and boot](../plan/implementation-plan.md#bit-032-mkosi-image-build-and-boot)
- [BIT-009 update recon](../notes/2026-11-21-bit009-update-recon.md)
- [Hardware token capability note](../notes/2026-11-21-hardware-token-capability.md)
- [Managed update path for a field camera fix](../notes/2026-11-15-managed-update-path.md)
- [Architecture](../architecture.md)
- RFC 8032, Ed25519: <https://www.rfc-editor.org/rfc/rfc8032>
- RFC 8785, JSON Canonicalization Scheme: <https://www.rfc-editor.org/rfc/rfc8785>
- Go `crypto/ed25519`: <https://pkg.go.dev/crypto/ed25519>
- YubiKey PIV compatible specifics: <https://docs.yubico.com/hardware/yubikey/yk-tech-manual/yk5-apps-piv.html>
- Cosign: <https://docs.sigstore.dev/cosign/overview/>
- The Update Framework: <https://theupdateframework.io/>
- Minisign: <https://jedisct1.github.io/minisign/>
