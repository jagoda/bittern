# Frigate fixture bundle

This directory is a runtime fixture for BIT-005. It proves that the pinned upstream image starts with
one synthetic camera, that the config check rejects bad config before start, and that the firewall, not
Docker port publishing, is the boundary. The compose files and config are hand-written; the firewall is
the renderer's four-camera golden plus one QEMU-only SSH rule. It is not the product bundle.

Status: executed inside the QEMU appliance guest on 2026-11-14. The config check, compose start, health
wait, authenticated snapshot, unauthenticated rejection, and the host firewall probe all pass. The
Frigate UI and internal API bind loopback only (ADR 0016). The image was pulled in the guest from the
pinned digest. It is still a fixture, not a product bundle. The product renderer now emits its own
Frigate config, compose file, and unit; this hand-written bundle is deliberately kept as the
independent QEMU check until a separate change switches the harness to renderer output.

## Files

| Path | Purpose |
| --- | --- |
| `compose.yaml` | Hardware-oriented fixture: host networking, `/dev/dri`, `/dev/bus/usb`, numeric `group_add` GIDs, `shm_size`, restart policy, log rotation. |
| `compose.qemu.yaml` | QEMU fixture: same image and runtime policy without GPU or USB devices, because the harness guest has no passthrough devices. |
| `config/config.yml` | Hand-written one-camera Frigate config. Deliberately not rendered. |
| `manifest.json` | Tag plus digest, resolution command, config-check command, smoke status, and SBOM source. |
| `check.py` | Static bundle check: missing or floating digests, host networking, no published ports, loopback listeners, and the renderer firewall contract. |
| `check-config.sh` | Runs the upstream `python3 -m frigate --validate-config` invocation in the pinned image. |
| `smoke.sh` | Starts the QEMU-safe compose file, waits for health, logs in on loopback 8971, and fetches an authenticated `latest.jpg`. |

`shm_size` is set to 1 GiB as a conservative fixture budget for four 1080p detect streams. The product
renderer will own the camera-count calculation and will raise it when the configured streams need more.

There is no committed copy of the firewall under this bundle. `tests/qemu/run.py firewall` reads the
renderer golden at `tests/render/four-cameras/expected/nftables/bittern.nft`, adds exactly one SSH rule
on `lan0` so the harness can drive the guest, and loads that. The renderer does not emit the SSH rule.
The only product inbound port on `lan0` is the Bittern UI on 443.

The Frigate database path is `/media/frigate/frigate.db`, following ADR 0014: recordings, snapshots,
and the media database live on the recording drive, not the OS or config drive. This is the layout
BIT-005 is meant to exercise. It is unverified until the first successful fixture run; if Frigate
cannot keep the database on that mount, record the concrete failure instead of silently moving it
back to `/config`.

## Image pin

The image is pinned by tag and digest in `manifest.json`:

```text
ghcr.io/blakeblackshear/frigate:0.18.2@sha256:fd60f99ffd4eb6c42360ed4e14953e270586f239309ab3983e55b2e97ffac09a
```

The digest was resolved on 2026-11-14 through the OCI registry API with curl and an anonymous GHCR
token, not by Docker and not from a blog post:

```sh
TOKEN=$(curl -fsS 'https://ghcr.io/token?scope=repository:blakeblackshear/frigate:pull&service=ghcr.io' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')
curl -fsSI -H "Authorization: Bearer $TOKEN" \
  -H 'Accept: application/vnd.oci.image.index.v1+json' \
  https://ghcr.io/v2/blakeblackshear/frigate/manifests/0.18.2
```

The response included:

```text
content-type: application/vnd.oci.image.index.v1+json
docker-content-digest: sha256:fd60f99ffd4eb6c42360ed4e14953e270586f239309ab3983e55b2e97ffac09a
```

The amd64 platform manifest is `sha256:9ebab4801be69ab4ebc7f6ab1d582c859ff7412d457a3fa550056894e8444dad`
and its config blob is `sha256:d8c79fd30635c830c5972dbc61c8c6c789af2373c654e7f4de1073eef435c982`.
The manifest records these for audit; the compose files use the index digest. There is no Docker CLI
in this container, so the image has not been pulled by Docker here.

## Config check before start

The check is the upstream invocation, with the fixture config mounted read-only at `/config`:

```sh
docker run --rm --network none --platform linux/amd64 \
  -v "$PWD/config:/config:ro" \
  --entrypoint python3 \
  ghcr.io/blakeblackshear/frigate:0.18.2@sha256:fd60f99ffd4eb6c42360ed4e14953e270586f239309ab3983e55b2e97ffac09a \
  -u -m frigate --validate-config
```

The check below was run in the appliance guest on 2026-11-14 and printed "Your config file is valid".
That run also pulled the image; the check itself is a few seconds, the whole command took 225 s.
Frigate logs `Config file is read-only, unable to migrate config file.` because the mount is `:ro`;
that is expected for the check and does not change the verdict.

## Smoke test

On a Docker host or inside the QEMU appliance guest, from this directory:

```sh
./check-config.sh
./smoke.sh
```

`smoke.sh` uses `compose.qemu.yaml` by default. On hardware with the passed-through devices, use:

```sh
FRIGATE_COMPOSE_FILE=compose.yaml ./smoke.sh
```

The script waits for the image health check, reads the generated admin password from the container
logs, logs in at `/api/login` on loopback 8971, and fetches `/api/cam0/latest.jpg` with the
authenticated cookie. It checks the JPEG status, content type, and size. It also checks that the same
snapshot request without a session is not served. The password is kept in a temporary file and is
never printed or committed.

The health check goes green before Frigate has created the admin user, so the script polls the logs
for the password for up to 60 s instead of reading them once. The first 2026-11-14 run returned
`smoke: ok: healthy, authenticated snapshot HTTP 200, 51868 bytes, image/jpeg` in 10.1 s with the
image already pulled, and 4 min 5 s including the pull. The warm run after the M1 join returned
11519 bytes in 3.3 s with the loopback-only config.

The fixture config sets `auth.reset_admin_password: true` so each start prints a fresh password for
the smoke test. That is a fixture convenience, not a product default. The product config pins
`reset_admin_password: false`, stores the admin password as a secret, scrubs it from logs, and
rotates it at first boot as required by ADR 0010. Reconciling the stored secret with Frigate's user
table remains apply wiring.

## Ports and firewall

Frigate uses host networking, so 5000, 1984, 8554, and 8971 are listening sockets in the appliance
guest, not Docker-published ports. Nothing is "unpublished" by Docker. The authenticated UI and the
internal API bind `127.0.0.1`; go2rtc's 1984 and 8554 listeners stay upstream behavior and are blocked
by the firewall. The boundary is the host firewall.

The firewall policy is the renderer output:

- default drop for input and forward;
- established, related, and loopback traffic allowed;
- the Bittern UI on 443 is allowed on `lan0`, and on `tailscale0` when Tailscale is enabled;
- Frigate's UI on 8971 and its internal API on 5000 are not reachable on `lan0`;
- 1984, 5000, 8554, 8555, and 8971 are explicitly dropped on both `lan0` and `cam0`, with 8555
  dropped for both TCP and UDP;
- `cam0` gets DHCP and NTP only, and there is no forwarding between `lan0` and `cam0`.

Port 8554 stays closed until OQ-15 decides whether the RTSP restream is exposed. Port 8555 stays
closed until OQ-03 decides the live-video path, including whether WebRTC is exposed or proxied. The
renderer comment names both questions and does not choose an answer.

The only `lan0` inbound rule the harness adds is the management SSH rule. It is not in the renderer
golden and is not a product rule. The Bittern UI on 443 is a rendered product rule, not a harness
rule.

The rendered rules were loaded in the appliance guest on 2026-11-14. `nft -c -f` accepts them with
nftables 1.1.3. With them loaded, 8971 answered on `127.0.0.1` inside the guest and 5000, 1984,
8554, and 8555 gave no answer through the host forwards. The guest's live table matched the golden
except for the substituted SSH rule. `ss -lntp` showed Frigate's UI and internal API on
`127.0.0.1:8971` and `127.0.0.1:5000`, and go2rtc on 1984 and 8554 on all interfaces. That is the
firewall being the boundary, not Docker.

## SBOM

No SBOM is committed or invented. The release bundle will generate one from the pinned OCI image and
its layers at bundle-build time, for example with Syft or the registry SBOM support, and attach it to
the signed bundle. The manifest records `sbom.status: not-generated` and the intended source. A
hand-written list of guessed packages would be worse than no SBOM.

## QEMU usage

The harness guest has no `/dev/dri` or `/dev/bus/usb` passthrough. After `make qemu-test` has booted
and snapshotted the guests, copy this directory in with the harness SSH key and run the smoke script:

```sh
tar -C tests/fixtures -cf - frigate \
  | ssh -i build/qemu/ssh/id_ed25519 -p 2222 bittern@127.0.0.1 \
      'rm -rf /tmp/frigate && tar -C /tmp -xf -'
ssh -i build/qemu/ssh/id_ed25519 -p 2222 bittern@127.0.0.1 \
  'cd /tmp/frigate && ./smoke.sh'
```

From the host, after Frigate is healthy, the harness firewall probe applies the renderer golden in the
guest and checks the loopback listener and the forwarded ports:

```sh
python3 tests/qemu/run.py firewall
```

That command expects the login page on `127.0.0.1:8971` inside the guest, and 5000, 1984, 8554, and
8555 blocked from `lan0`. The rendered golden also allows 443 on `lan0` for the Bittern UI; the
firstboot scenario checks that path with no harness UI rule. There is no host forward for 8971, so a
browser on the host cannot reach the Frigate UI. The probe is application-level because QEMU user
networking accepts the host connection even when the guest drops the packet; a bare `connect()`
cannot tell them apart.

## What is unverified

- The hardware compose file has not run on hardware; it needs `/dev/dri` and `/dev/bus/usb`.
- The QEMU harness still consumes this hand-written bundle rather than the product renderer output.
  The product renderer's Frigate config has not been run through the upstream config check or started
  here, because this container has no Docker daemon.
- No SBOM has been generated. The manifest records the intended source rather than a fabricated list.
- The recording-drive layout from ADR 0014 is only approximated by `./state/media`. A real
  recording drive, mount options, and the crypto-erase path are still untested.

Related: [QEMU harness](../../qemu/README.md), [Frigate assessment](../../../docs/research/frigate-assessment.md),
[ADR 0004](../../../docs/adr/0004-docker-on-bare-metal-debian.md),
[ADR 0010](../../../docs/adr/0010-security-and-privacy-posture.md),
[ADR 0016](../../../docs/adr/0016-bittern-owns-frigate-config.md).
