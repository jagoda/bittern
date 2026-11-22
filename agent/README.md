# agent

`agent/` contains `bitternd`, the privileged management daemon, `bittern`, the operator CLI, and
`bittern-launch`, the fixed launcher that starts the agent from the active app bundle. The
agent is the only component that writes system configuration. It validates `appliance.yaml` against
the schema, renders templates, applies changes atomically with health checks and rollback, discovers
and onboards cameras, produces support bundles, runs the application update transaction, and performs
reset operations. The CLI is a thin client over the same local API the UI uses.

## Boundaries

In scope:

- Schema validation, migrations, and the persisted camera inventory
- Rendering, validation, atomic apply, health checks, and rollback
- Camera discovery on `cam0` with dnsmasq lease, ARP, ONVIF WS-Discovery, unauthenticated ONVIF device info and RTSP layers, credential adoption, and DHCP reservations
- Support bundle assembly with redaction
- Application bundle updates, backups, and reset operations

Out of scope:

- NVR video processing, recording, and analytics (Frigate)
- UI rendering and browser flows (ui)
- OS image construction and A/B slot management (image)
- Owning Frigate `config.yml` when the device is in Git-managed mode

## Status

The config loader, the BIT-003 renderer including Frigate config, compose and unit output, read-only
hardware detection, the resumable apply transaction from BIT-011, first-boot provisioning, the
read-only status daemon from BIT-006, and the BIT-007 first-run wizard and status page skeleton are
implemented. The `bittern` CLI
has `detect`, `render`, `first-boot`, `setup-token` and `setup-token --qr`; it is still a local
command, not a client of a daemon API. `bittern-release` signs and verifies the release manifest on
an offline signing machine, using the same `internal/release` verifier as the appliance; it is not
built into the payload or the image.
`bitternd` serves `GET /api/status` and `GET /api/version` on loopback, and serves the authenticated
UI under `/ui/` over TLS on `0.0.0.0:443` when started with `--ui`. The setup network and storage
checks read the live status builder; `--ui-mock` selects mock status states and sample checks, while
without `--ui-mock` the applier persists `appliance.yaml` and calls the BIT-011 apply transaction.
`--ui-mock` keeps the stub. First boot generates a one-time setup token, the owner sets the
administrator password on the first visit, and later visits use the login page. Camera discovery is
wired to the wizard; there is no full CLI, update, or reset yet. The package boundaries above still describe the intended
daemon.

| Path | Role |
| --- | --- |
| `internal/config/` | YAML loading, JSON Schema validation, semantic rules, typed model |
| `internal/render/` | Deterministic renderers and the validation-before-write entry point |
| `internal/hardware/` | Read-only NIC, disk, accelerator, CPU and memory detection |
| `internal/apply/` | Generation staging, native checks, atomic live writes, journal, rollback and `Inspect` |
| `internal/firstboot/` | Identity, secrets, setup token, TLS certificate, minimal config, first apply and marker |
| `internal/secrets/` | Mode 0600 `secrets.yaml` document, redaction values, token and short-code invalidation |
| `internal/setupqr/` | Setup URL fragment, token grouping, short-code generation and hash, qrencode terminal QR |
| `internal/tlsutil/` | UI certificate and private-key loading, SHA-256 fingerprint, TLS configuration |
| `internal/auth/` | Setup token and short code, PBKDF2 password hash, sessions, CSRF, rate limits |
| `internal/status/` | Status document, injected probes, state aggregation, in-memory render checks |
| `internal/discovery/` | Layered lease, ARP, ONVIF WS-Discovery, ONVIF device and RTSP candidates on `cam0` |
| `internal/ui/` | First-run wizard, login and setup pages, status pages, embedded templates and assets |
| `internal/redact/` | Secret redaction shared by status, logging, apply and first boot |
| `internal/logging/` | Structured JSON logging with redaction on every attribute |
| `internal/release/` | Release manifest canonical form, validation, Ed25519 verifier and pinned trust store |
| `internal/assets/` | Tracked embed declarations plus a generated, gitignored asset copy |
| `cmd/bittern/` | Operator CLI: `detect`, `render`, `first-boot` and `setup-token` |
| `cmd/bitternd/` | Status API and optional UI server, with its HTTP handlers |
| `cmd/bittern-launch/` | Fixed agent launcher; execs the active bundle or the `/usr/bin/bitternd` baseline |
| `cmd/bittern-release/` | Offline release signing and verification tool (not in the appliance) |
| [packaging/](packaging/README.md) | Sample `bitternd.service` and first-boot units; nothing installs them yet |
| `scripts/sync-assets.sh` | Copies root `templates/`, `defaults/`, the schema, and `ui/` assets into the module |

The status contract, states, fields, unknown values, and redaction rules are in
[docs/status.md](../docs/status.md).

## Toolchain

The toolchain is installed from the official linux/amd64 tarball into `/usr/local`. It is
container-local and is never committed.

| Item | Value |
| --- | --- |
| Version | Go 1.27.4 |
| Platform | linux/amd64 |
| Tarball | <https://go.dev/dl/go1.27.4.linux-amd64.tar.gz> |
| SHA-256 | `7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840` |
| Verified | 2026-11-14, with `sha256sum` after download and before extraction |

Install and verify:

```sh
curl -sSL -o go1.27.4.linux-amd64.tar.gz https://go.dev/dl/go1.27.4.linux-amd64.tar.gz
printf '%s  %s\n' \
  7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840 \
  go1.27.4.linux-amd64.tar.gz | sha256sum -c -
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.27.4.linux-amd64.tar.gz
```

Put `/usr/local/go/bin` on `PATH`, or link `go` and `gofmt` into `/usr/local/bin`. Do not use
Debian 13's Go 1.24 package as the build toolchain, per [ADR 0015](../docs/adr/0015-agent-language.md).

### Module path

The module path is `bittern.invalid/agent`. It is **provisional** because OQ-14 (name and domain) is
open. The `.invalid` top-level domain is reserved by RFC 2606, so this path cannot claim a domain the
project does not own. It is not a naming decision and must change with OQ-14 before any published
build.

### Build settings

- `CGO_ENABLED=0` for a static binary.
- `-trimpath` and `-ldflags='-s -w'` for release builds.
- Every module is vendored in `agent/vendor/` with `go mod vendor`. Builds and tests use
  `GOPROXY=off` and `GOFLAGS=-mod=vendor`; no network access is needed once the clone and toolchain
  are present.
- `cmd/bittern`, `cmd/bitternd`, `cmd/bittern-launch` and `cmd/bittern-release` are the main
  packages. `go build ./...` compiles all four. `make bitternd` builds the daemon into `agent/bin/`,
  and `make bittern-release` builds the signing tool there; `make beta-payload` builds the launcher
  into the beta kit. The release flags above apply to each binary.

## Building and testing

```sh
make test       # generate the embed assets, then go test ./... offline
make render     # generate the embed assets, then run the golden render suite
make status     # run the status contract and golden tests
make ui-test    # run the first-run wizard and status page tests
make detect     # print the read-only hardware inventory and proposal
make render-poc CONFIG=schema/examples/poc-gk41.yaml OUT=build/poc-gk41
make apply-test # run the apply and first-boot tests only
make first-boot-poc FIRST_BOOT_ROOT=build/first-boot
make bitternd   # build the status daemon into agent/bin/bitternd
make bittern-release  # build the signing tool into agent/bin/bittern-release
```

`make detect`, `make render-poc` and `make first-boot-poc` run `go run ./cmd/bittern` from `agent/`,
so they need no installed binary. `render-poc` validates the document first and writes only under
`OUT`. `first-boot-poc` is an explicit offline simulation: it writes files under
`FIRST_BOOT_ROOT`, skips systemd and probes no live network, and says so in its output.

`make check` runs the repository checks and then `make test`; a failing Go test fails
`make check`. `make test` runs this from the repository root:

```sh
cd agent && CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=vendor go test -count=1 ./...
```

The tests need `dnsmasq` for `dnsmasq --test`, `nft` for `nft -c`, and `chronyd` for `chronyd -p`.
A missing checker skips that hook with a message; a checker that rejects the output fails the test.

Regenerate the committed golden files with:

```sh
cd agent && go test ./internal/render -update
cd agent && go test ./internal/status -update
```

Review the resulting diff before committing it. The renderer `-update` flag is documented in
[tests/render/README.md](../tests/render/README.md). The status golden files are
`internal/status/testdata/golden/healthy.json` and `degraded.json`.

## Release signing tool

`bittern-release` is the signing half of
[ADR 0029](../docs/adr/0029-release-signing-and-key-custody.md). It is a static binary built from
this module; it needs no network, no configuration file and no environment beyond its flags. It is
not part of the appliance payload or the image. The appliance verifies with `internal/release`, and
the tool calls the same package for its signature self-check and for `verify`, so there is one
implementation of the scheme.

| Command | Runs on | What it does |
| --- | --- | --- |
| `init --key PATH [--pub-dir DIR]` | signing machine | Generate a 32-byte Ed25519 seed with `crypto/rand`, write it mode 0600, and write `<key_id>.pub` (32 raw bytes, mode 0644) into `DIR` |
| `pack --tree DIR --out DIR ...` | build host | Pack a built file tree into the deterministic files-only `bittern-app.tar.gz` and its canonical `manifest.json`; no private key is needed |
| `manifest --payload ARCHIVE ...` | build host | Build the canonical manifest from an existing payload archive: digest, size and the regular-file list with modes, sorted by path |
| `sign MANIFEST.json --key PATH` | signing machine | Print the fields, require `sign` or the version, sign the domain-prefixed canonical bytes, write `MANIFEST.json.sig` (64 bytes), then verify its own output |
| `verify MANIFEST.json MANIFEST.json.sig --trust-dir DIR` | build host or any machine | Verify with the production trust-store reader, the same one the appliance uses |
| `tag TAG` | build host or workflow | Validate an ADR 0031 beta tag with the Go rule and print its generation; reject anything that is not `app-beta-` plus six lowercase hex characters |
| `key-id KEYFILE [--pub-dir DIR]` | signing machine | Re-derive the key id, public key and anchor path after a move |

Every command refuses an existing output without `--force` where replacement is meaningful, and
`init`, `manifest`, `pack` and `sign` have `--dry-run`; `manifest --out -` writes the canonical
bytes to stdout and `--dry-run` checks without writing. `manifest` rewrites only when the bytes
changed, so a second run reports `unchanged`. `pack` writes `bittern-app.tar.gz` and
`manifest.json` in the output directory and leaves signing to `sign`.

`init` and `sign` refuse a key file that is not exactly 32 bytes, not a regular file, a symlink, owned
by another user, or readable by group or others. `sign` also refuses a manifest whose
`signing.key_id` is not the key file's id, so the manifest that crosses the airgap must already name
its signer. It verifies the signature it just wrote with the production verifier and deletes the
signature file if that check fails.

Exit codes for `sign` and `verify`:

| Code | Meaning |
| --- | --- |
| 0 | valid manifest and signature |
| 1 | signer-side or I/O failure, such as a key file error, a `signing.key_id` mismatch or a write error |
| 2 | usage error |
| 3 | bad signature: missing, truncated or invalid |
| 4 | untrusted key or unreadable trust store |
| 5 | non-canonical or invalid manifest |
| 6 | manifest rejected by the kind or generation rule |

The CI test anchor for the `beta` environment is in
[packaging/trust](../packaging/trust/README.md). It is a bench test anchor and never the release
anchor; ADR 0032 records the CI decision.

### Release signing recipe

`init` runs once on the signing machine. `manifest` runs on the build host. Only the canonical
manifest and its 64-byte signature cross the airgap; the payload stays on the build host. `KEY` is
the seed file on the encrypted volume, `ANCHORS` receives the new `<key_id>.pub`, `TRUST` is the
pinned trust directory on the build host, and `XFER` is the removable medium.

```sh
# Signing machine, once per key. Record the printed key id as KEY_ID and carry the new .pub file
# to the build host.
bittern-release init --key "$KEY" --pub-dir "$ANCHORS"

# Build host, per release, using KEY_ID from init.
bittern-release manifest --payload build/bittern-app.tar.gz \
  --version 1.2.3 --generation 7 --source-commit "$(git rev-parse HEAD)" \
  --key-id "$KEY_ID" --os-generation-min 1 --os-generation-max 1 \
  --schema-min 1 --schema-max 1 --updater-min 1 --out build/manifest.json

# Move build/manifest.json to the signing machine, then sign it there.
bittern-release sign "$XFER/manifest.json" --key "$KEY"

# Move manifest.json.sig back to the build host, then verify before publication.
bittern-release verify build/manifest.json "$XFER/manifest.json.sig" \
  --trust-dir "$TRUST"
```

The manifest must arrive at the signing machine with the key id `init` printed. A mismatch exits 1
before any signature is written and names both the manifest's `signing.key_id` and the key file's
id. The smoke test of this recipe recorded a tampered signature as exit 3, an unknown key as exit 4
and a non-canonical manifest as exit 5. The tool and its interpretations are recorded in
[the release signing tool note](../docs/notes/2026-11-21-release-signing-tool.md).

`verify` has no test branch and reads no environment variable. The QEMU harness points it at a
temporary key file and a temporary trust directory, which is the production code path with test
material:

```sh
bittern-release init --key "$tmp/release.key" --pub-dir "$tmp/trust"
bittern-release sign "$tmp/manifest.json" --key "$tmp/release.key" --yes
bittern-release verify "$tmp/manifest.json" "$tmp/manifest.json.sig" \
  --trust-dir "$tmp/empty-image-store" --rotation-dir "$guest/etc/bittern/trust/release-keys"
```

## The embed asset copy

`templates/`, `defaults/`, `schema/`, and `ui/` live at the repository root for the reasons in
[ADR 0011](../docs/adr/0011-monorepo-layout-and-naming.md). `go:embed` cannot reach outside the
`agent/` module, so a build step copies them into the gitignored `agent/internal/assets/` tree:

```sh
make assets              # or: agent/scripts/sync-assets.sh
```

The script deletes and rewrites the copy on every run. It is a build input, not a second source of
truth: `internal/assets/assets_test.go` compares the embedded copy with root `templates/`,
`defaults/`, `schema/appliance.schema.json`, and `ui/` and fails the test run if the copy is stale. `make test`
and `make render` always depend on `make assets`, so a fresh clone works offline with Go present.

This copy is a known build wrinkle. It exists only because the module cannot embed files above its
own root; the root directories did not move.

## Renderer outputs

The renderer writes a slash-separated tree, sorted by path:

| Output | Family | Notes |
| --- | --- | --- |
| `networkd/10-lan0.network` | systemd-networkd | DHCP client; no static gateway |
| `networkd/10-cam0.network` | systemd-networkd | Static, gatewayless, IPv6 and link-local disabled |
| `networkd/10-lan0.link` | systemd-networkd | Only when `network.lan0.match` is present; `MACAddress=` or `Path=` |
| `networkd/10-cam0.link` | systemd-networkd | Only when `network.cam0.match` is present; `MACAddress=` or `Path=` |
| `nftables/bittern.nft` | nftables | One `inet bittern` table; input and forward deny by default |
| `dnsmasq/dnsmasq.conf` | dnsmasq | DHCP only on `cam0`; DNS disabled with `port=0` |
| `chrony/chrony.conf` | chrony | Client on `lan0`, NTP server for the camera subnet |
| `frigate/config.yml` | Frigate | Cameras, RTSP URLs or env placeholders, detector, decode, retention and loopback listeners; only when at least one camera is enabled |
| `compose/compose.yaml` | Docker Compose | Pinned image, host networking, selected devices and numeric group IDs, read-only config, media and cache mounts; only with the Frigate config |
| `systemd/bittern-frigate.service` | systemd | Oneshot `docker-compose up -d` and `down` wrapper with media and config conditions; only with the Frigate config |

Every file starts with:

```text
# Generated by Bittern from appliance.yaml; do not edit.
# appliance.yaml schema version: 2
```

Output is deterministic: templates and YAML encoders produce stable field and key order, the file
list is sorted, and no file contains a timestamp, a build-host hostname, or a random value. The golden suite renders every case
twice and compares the trees byte for byte.

## Apply transaction

The apply path is in `internal/apply`. It never edits a live file in place and never calls
`systemctl` outside its production `System` implementation.

| Step | What happens |
| --- | --- |
| Stage | Render into `<state-root>/generations/<n>/files/`, add the mode 0600 camera env file from `secrets.yaml`, and write a manifest |
| Validate | Run `nft -c`, `dnsmasq --test`, `chronyd -p`, and the pinned Frigate config check with `--env-file` when cameras need URL substitution |
| Write | Atomically replace only live files whose content, mode or owning unit changed, including the live camera env file |
| Udev | When the generation has `.link` files, run `udevadm control --reload`, trigger an add event for network devices, and wait up to 10 seconds for every parsed `Name=` |
| Units | Stop units whose files disappear, run `systemctl daemon-reload` when a unit or its config changed, enable every rendered unit and verify `is-enabled`, then restart an active changed unit or start an inactive one in firewall, DHCP, time, network, Frigate order |
| Health | Probe links, addresses with a bounded wait for the DHCP lease after a rename, the DHCP listener on `cam0`, chrony with the rendered config, the firewall table, the loopback Frigate version API when its unit is present, and any caller-supplied probe |
| Commit | Mark the generation applied and move `<state-root>/current` |

Unit enablement has two lifecycle owners. The installer enables every unit it installs through one
verified step. Apply enables a unit whose unit file it renders, which is currently
`bittern-frigate.service`; that unit does not exist at install time. Apply verifies `is-enabled`
before it starts the unit and disables the rendered unit when its files disappear. A disabled
rendered unit fails the apply with the reboot consequence named; it is never made active only for
the current boot.

The active journal at `<state-root>/journal.json` records the last completed step. A rerun continues
from that step. On a live failure the previous generation is written back and its units restarted;
when either generation has `.link` files, rollback also reloads udev, triggers another add event,
and waits for the restored names before restarting networkd. A rename is not fully reversible in the
running kernel without that trigger, and a previous generation with no `.link` files may need a
reboot to recover the stock names. A completed rollback is retained as
`<state-root>/apply/rollback-<timestamp>-g<generation>.json`; only the active journal is removed, so
the next run can start a new transaction while the failure stays inspectable. The result names the
failing step. `nft -c` netlink permission failures are recorded as skipped with the environment
reason. A syntax error in the ruleset is still a failure. `internal/apply.Inspect`
is the read-only state hook used by the status builder. The render section reports the applied
generation from `<state-root>/current` and the last transaction state from the latest manifest and
journal, including a failed or rolled-back attempt.

## UI apply path

The first-run handler's `ui.AgentApplier` loads the existing `appliance.yaml`, applies the fields the
wizard owns, validates the result, writes the document, and calls `apply.Apply`. A camera without a
`credentials_ref` gets a server-generated `cameras.<id>.rtsp_url` key. When the owner entered a
camera username and password, the applier writes the percent-encoded RTSP URL to `secrets.yaml`
atomically at mode 0600; otherwise it derives the no-auth URL from the camera address, port and
path. Existing keys are preserved unless the owner supplied a new credential. The credential key is
never a browser field. A camera that names a `credentials_ref` whose key is missing fails before
`appliance.yaml` is written. When the adopted address is inside the DHCP pool, the camera carries
the discovered MAC and the renderer writes the Bittern-owned reservation file. It is selected when the daemon runs with `--ui` and without `--ui-mock`; the mock mode and the UI
tests keep `ui.StubApplier`, which writes nothing. The opt-in browser check adds
`--ui-mock-apply`, which swaps that stub for a preview applier that returns success without writing
files so the wizard-done route is reachable. The non-mock setup checks use `ui.AgentChecks`,
which reads the status builder shared with the status page; `--ui-mock` keeps the sample
`ui.MockChecks` data. The UI listener is TLS by default; `--ui-listen` overrides the address for
tests, and `--ui-insecure-http` is a loopback-only development mode. The status API remains on its
separate loopback listener and is not mounted on the UI listener.

## First boot

`bittern first-boot` detects hardware, reuses or generates `identity.json` and
`/etc/bittern/secrets.yaml`, writes a minimal `appliance.yaml`, generates
`/etc/bittern/tls/server.crt`, renders and applies, then writes
`<state-root>/first-boot.json` only after the apply commits. The TLS private key lives only in the
`0600` secrets file, together with the generated Frigate admin password and the one-time setup
token. First boot also writes the token to `<state-root>/setup-token` at mode 0600 for operator
convenience. On a local console it prints a QR for
`https://<lan0-address>/ui/setup#token=<token>`, the grouped token, and a separate short code. The
unit sets `BITTERN_CONSOLE=/dev/tty1`; the block is written directly to that device, so the service
journal never contains the token. Without a local console, first boot prints the existing
instruction and `sudo bittern setup-token --qr` prints the same block on the caller's terminal.
`setup-token` without `--qr` keeps the plain token output for scripts. The first successful setup
removes both token copies and the short-code material; the short code expires after 10 minutes and
is single use. The certificate SANs contain the configured hostname and the
current `lan0` address when one exists; a rerun keeps the fingerprint, and an identity reset
regenerates it and the setup token.

First boot writes `network.lan0.match` and `network.cam0.match` into the generated
`appliance.yaml` whenever it can name both roles from wired NICs, using `mac` and falling back to
`pci`. Two wired NICs always get both matches. Exactly one live link makes the roles confident; two
live links or two dead links choose deterministically by PCI address and MAC, leave the roles
unconfirmed, and record an owner note to confirm or swap them in the wizard. Wireless and virtual
NICs are excluded from the two-role proposal. The renderer then emits `10-lan0.link` and
`10-cam0.link` through the same apply transaction, with the generated header. An unconfirmed
proposal still writes both matches and both link files, and records the owner confirmation in the
marker. Only an inventory with no usable wired NIC leaves a match absent. First boot never formats a
disk. A confident recording candidate sets the media mount point and names the detected drive by
`by_id`; otherwise the config has an empty `storage.recording.drives` list and reports recording as
unconfigured. The empty list is the version 2 form of
[OQ-24](../docs/open-questions.md), which is resolved. First boot also writes
`hardware.detector` and `hardware.video_decode.acceleration` from the detected accelerator and
render node.

The renderer emits the Frigate `config.yml`, `compose/compose.yaml`, and
`systemd/bittern-frigate.service` when at least one camera is enabled. Apply maps them to
`/var/lib/bittern/frigate/config.yml`, `/var/lib/bittern/compose/compose.yaml`, and
`/etc/systemd/system/bittern-frigate.service`. It reads the camera RTSP URLs from
`/etc/bittern/secrets.yaml`, stages `/var/lib/bittern/frigate/cameras.env` with mode 0600, passes the
pinned image and staged env file to the config check, writes the live env file only after validation,
reloads systemd, enables the rendered unit and verifies `is-enabled`, then restarts an active unit,
starts an inactive one, or stops and disables a unit whose files disappear. A missing secret
names the camera and fails the apply. The renderer never reads a secret; a camera with
`credentials_ref` renders a `{FRIGATE_CAMERA_<ID>_RTSP_URL}` placeholder, and the compose file points
at the generated env file. The first-boot unit is a packaging sample; systemd, the real boot ordering,
Docker and a real network remain unverified in this container.

## Deliberate omissions in BIT-003

- **No `.netdev` file.** Both interfaces are physical NICs matched by name. systemd `.netdev` units
  create virtual devices (bridges, bonds, VLANs, dummies); emitting one would invent a topology that
  ADR 0005 does not define. A `.netdev` belongs in the change that models a virtual device. This
  leaves the wording "`.network` and `.netdev`" in BIT-003 only partly exercised.
- **No recording mount unit or mergerfs service.** A systemd `.mount` unit needs `What=`, a device
  path, UUID, label, or role resolution, and optionally `Type=`. Schema version 2 now carries the
  `storage.recording.drives` member list, including `by_id`, `serial`, and `device_role` selectors,
  but the renderer does not emit member mount units or assemble the pool. Mount ordering, member
  health, the free-space reserve across members, and mergerfs remain follow-up work for the apply
  pipeline; ADR 0018 still requires the pool.
- **No secret value in generated files.** Camera entries are rendered into Frigate's `config.yml`,
  but a camera with `credentials_ref` gets only an env placeholder. The env file is generated by
  apply from `/etc/bittern/secrets.yaml`; no credential value is ever rendered.
- **No `OQ-15` toggle.** Port 8554 is closed on `lan0` and `cam0` by default. Exposing it needs a
  schema field and a decision on OQ-15, neither of which exists yet.

## Validation hooks

`internal/render` writes a temporary tree and runs each generated artifact through its native
checker:

| Artifact | Check | Result in this container (2026-11-14) |
| --- | --- | --- |
| `dnsmasq.conf` | `dnsmasq --test --conf-file=...` | Passes, dnsmasq 2.90 |
| `chrony.conf` | `chronyd -p -f ...` | Passes, chrony 4.3 |
| `bittern.nft` | `nft -c -f ...` | **Not validated.** `nft -c` needs CAP_NET_ADMIN to build its netlink cache; the container returns `cache initialization failed: Operation not permitted` even with sudo and an unprivileged network namespace |

The nftables test reports this as a skip with the environment error, not as a pass. The ruleset was
parsed but its syntax and semantics were not accepted by `nft -c`. Do not claim the firewall was
validated until the hook runs where CAP_NET_ADMIN is available.

## YAML behavior

The product parser is `gopkg.in/yaml.v3`. The reference validator uses PyYAML, and the two differ on
YAML 1.1 booleans and duplicate keys. The product rejects `yes`, `no`, `on`, and `off` as booleans,
and rejects duplicate mapping keys with a JSON path. The full contract and the fixture-suite parity
result are in [schema/validation.md](../schema/validation.md).

Related: [architecture](../docs/architecture.md), [ADR 0006 and ADR 0008](../docs/adr/README.md).
