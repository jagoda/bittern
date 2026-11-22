# Render golden suite

`tests/render/` holds the committed contract for the BIT-003 renderer. Each valid case pairs an
`appliance.yaml` with an expected output tree. The Go test in `agent/internal/render` renders into a
temporary directory and compares the tree byte for byte.

| Case | Input | Expected output | Checks |
| --- | --- | --- | --- |
| `four-cameras` | `schema/examples/four-cameras.yaml` | `four-cameras/expected/` | The dual-NIC example with four cameras, a recording mount point, CPU detection, no video decode, and no NIC match, so no `.link` file is emitted |
| `ipv6-off` | `ipv6-off/appliance.yaml` | `ipv6-off/expected/` | `cam0` is IPv4-only; Tailscale adds no inbound tailnet service rule; `lan0` matches by MAC and `cam0` by PCI path in the `.link` files |
| `poc-gk41` | `schema/examples/poc-gk41.yaml` | `poc-gk41/expected/` | Coral USB detection plus VAAPI decode: `/dev/bus/usb`, `/dev/dri`, and the numeric `video` (44) and `render` (992) GIDs |
| `hailo-qsv` | `hailo-qsv/appliance.yaml` | `hailo-qsv/expected/` | Hailo-8 detection plus QSV decode: `/dev/hailo0` and `/dev/dri`, with no `/dev/bus/usb`; direct RTSP URLs and no env file |
| `no-cameras` | `no-cameras/appliance.yaml` | `no-cameras/expected/` | No Frigate config, compose file, or unit, because Frigate with zero cameras is not a valid service |
| `reserved-camera` | `reserved-camera/appliance.yaml` | `reserved-camera/expected/` | A camera inside the dynamic pool with a MAC renders `dnsmasq/reservations.conf` with one `dhcp-host` entry; a camera without a MAC is rejected |
| `overlap` | `overlap/appliance.yaml` | none | `lan0` and `cam0` overlap; validation fails at `$.network.lan0.subnet` with `network.subnet-overlap` and no file is written |

The `overlap` case deliberately has no `expected/` directory. The test asserts that validation fails
and that the output directory stays empty. `networkd/10-lan0.link` and `networkd/10-cam0.link` are
renderer output when the document has `network.*.match`; the files carry the same do-not-edit header
as every other generated file.

## Frigate outputs

A case with a camera MAC also has `dnsmasq/reservations.conf`, mapped to
`/etc/dnsmasq.d/bittern-reservation.conf`. The file carries the generated header and one
`dhcp-host` line per reserved camera. A camera with no MAC and an address inside the pool is a
validation error.

A case with at least one enabled camera also has:

| Path | Contract |
| --- | --- |
| `frigate/config.yml` | Cameras, direct RTSP URLs or `{FRIGATE_CAMERA_<ID>_RTSP_URL}` placeholders, detector, decode, continuous and motion recording plus event retention from `retention_days`, snapshots, database path, and loopback listeners |
| `compose/compose.yaml` | Pinned tag and digest, `pull_policy: missing`, host networking, selected devices and numeric group IDs, writable config directory with the managed config file mounted read-only inside it, media mount, cache tmpfs, restart policy, log rotation, and `no-new-privileges` |
| `systemd/bittern-frigate.service` | Oneshot `/usr/bin/docker-compose --file ... up --detach` on start and `down` on stop, with config, compose and media conditions |

`pull_policy: missing` keeps the image download out of the apply. The prewarm unit
(`bittern-image-prewarm.service`) downloads the pinned reference in the background, and the compose
start uses the local image.

The POC and four-camera goldens use `credentials_ref`, so their configs contain env placeholders and
their compose files reference `/var/lib/bittern/frigate/cameras.env`. The Hailo case has no
`credentials_ref`, so its configs contain direct URLs and its compose file has no `env_file`. The
goldens never contain a credential value or a full URL for a secret-backed camera.

The `hailo-qsv` case sets `hardware.detector.device` to a PCI address to prove the renderer uses
Frigate's `hailo8l` detector with the product transport (`PCIe`) rather than copying a PCI address
into a field Frigate does not accept.

## Running

```sh
make render
```

This regenerates the embedded asset copy, including `defaults/frigate.yaml`, and runs the golden
suite offline. The suite also renders each valid case a second time from a fresh load and asserts the
two trees are byte-identical.

Regenerate the expected files after an intentional renderer change:

```sh
cd agent && go test ./internal/render -update
```

Review the diff before committing. Do not hand-edit a file under an `expected/` directory; the
renderer is the source of truth and the test fails on any one-character drift.

## Validation hooks

The same test writes the rendered files to a temporary tree and runs the native checkers when they
are installed:

- `dnsmasq --test` on `dnsmasq/dnsmasq.conf`.
- `nft -c` on `nftables/bittern.nft`. In this container the check skips with
  `cache initialization failed: Operation not permitted`; the ruleset is not validated.
- `chronyd -p` on `chrony/chrony.conf`.

A missing checker skips that hook. A checker that rejects the output fails the test. Do not treat a
skip as a pass for the firewall.

The Frigate config check is an apply-time hook because it needs the pinned container image and the
generated camera env file. The golden suite does not run Docker, so `frigate/config.yml` is checked
against the pinned upstream source and reference config rather than through
`python3 -m frigate --validate-config` here. The fixture bundle documents the upstream invocation for
the QEMU guest.

Related: [tests](../README.md), [agent](../../agent/README.md),
[schema validation](../../schema/validation.md), [defaults](../../defaults/README.md).
