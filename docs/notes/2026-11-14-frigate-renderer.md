# Frigate config, compose, and unit rendering

- Date: 2026-11-14
- Related: [ADR 0004](../adr/0004-docker-on-bare-metal-debian.md),
  [ADR 0014](../adr/0014-dedicated-recording-drive.md),
  [ADR 0016](../adr/0016-bittern-owns-frigate-config.md),
  [ADR 0018](../adr/0018-recording-capacity-pool.md),
  [Frigate assessment](../research/frigate-assessment.md),
  [schema validation](../../schema/validation.md)

## What landed

The BIT-003 renderer now emits `frigate/config.yml`, `compose/compose.yaml`, and
`systemd/bittern-frigate.service` for a configuration with at least one enabled camera. The product
policy lives in [defaults/frigate.yaml](../../defaults/frigate.yaml) and is embedded through the
asset copy step with the templates and schema.

- The image is `ghcr.io/blakeblackshear/frigate:0.18.2` pinned by the index digest
  `sha256:fd60f99ffd4eb6c42360ed4e14953e270586f239309ab3983e55b2e97ffac09a`, re-verified through
  the anonymous GHCR OCI registry API on 2026-11-14. The fixture manifest carries the same pin.
- The compose file uses host networking, a writable `/config` directory with the managed
  `config.yml` mounted read-only inside it, the configured media mount, a cache tmpfs,
  `no-new-privileges`, bounded json-file logs, and only the devices and numeric group IDs selected
  by the detector and video decode policy. The group IDs are numeric because Docker resolves a
  `group_add` name in the image; see [the group ID note](2026-11-15-frigate-group-gids.md).
- The unit is a oneshot. `ExecStart` runs `docker compose --file ... up --detach`, `ExecStop` runs
  `down`, and `ConditionPathExists` covers the config, compose file, and media path.
- `config.yml` carries the do-not-edit header, loopback listeners from ADR 0016, the database at
  `/media/frigate/frigate.db`, continuous and motion recording plus alert and detection events, all
  retained from `storage.recording.retention_days`. Snapshots are a separate Frigate section.
- Authentication is on, `auth.reset_admin_password` is pinned false, and `objects.track: [person]`
  remains marked provisional until the schema exposes object selection.
- A configuration with no enabled cameras emits none of the three files. Frigate with zero cameras
  is not a valid service, and first boot with an empty inventory stays independent of Frigate.

Golden cases now cover the four-camera CPU build, the POC GK41 with `coral_usb` and `vaapi`, a Hailo
plus QSV build, and a no-camera document. The golden suite asserts byte-identical repeat renders,
the shared header, direct camera URLs when no secret is referenced, and the device list per
detector.

## RTSP URL secret mechanism

Frigate 0.18.2 supports `{FRIGATE_*}` substitution only in fields typed as `EnvString`. The camera
input path is one of them: `frigate/config/env.py` implements `substitute_frigate_vars`, and
`frigate/config/camera/ffmpeg.py` types `CameraInput.path` as `EnvString`. The 0.18.2 reference
config documents that `cameras[].ffmpeg.inputs[].path` may contain a `FRIGATE_` environment
variable or Docker secret. Source and docs were read at tag `v0.18.2`
(`cfa08c56b23d23280c3d0a218e5a43182c97445b`).

The rendered config therefore uses a whole-URL placeholder, not a username or password fragment:

```yaml
cameras:
  front_door:
    ffmpeg:
      inputs:
        - path: '{FRIGATE_CAMERA_FRONT_DOOR_RTSP_URL}'
          roles:
            - detect
            - record
```

The compose file references `/var/lib/bittern/frigate/cameras.env` when at least one camera has
`credentials_ref`. Apply writes that file with mode 0600. Each line is:

```text
FRIGATE_CAMERA_<CAMERA_ID_UPPER>_RTSP_URL=<complete RTSP URL>
```

`render.FrigateCameraSecrets` exposes the camera id, the `credentials_ref` key, and the env var name
so the apply task does not have to reimplement the mapping. `render.FrigateCameraEnvPath` is the
shared path. The value at `credentials_ref` in `/etc/bittern/secrets.yaml` is a complete RTSP URL,
never a password. `schema/validation.md` records this contract.

## Apply wiring

The renderer adds default targets for the Frigate config, compose file, env file, and unit. The
apply transaction now resolves the camera secret mapping, stages `frigate/cameras.env` with mode
0600, runs the pinned config check with that file, writes the live env file only after validation
passes, reloads systemd, and starts or restarts the unit after the network and firewall units. The
last camera removed is a stop transition. See
[the Frigate apply note](2026-11-14-frigate-apply.md) for the transaction and the exact check
command.

## Verification

`make check`, `make test`, `make render`, and `make schema` pass, and the apply and first-boot
suites cover the camera env file, config check, unit lifecycle, rollback, and a cut before start. The
QEMU two-NIC harness was re-run on 2026-11-14 after the renderer change: the addresses,
camera-reachable, cam0-isolated, and snapshot-reset tests passed in 43.3 s on KVM. The QEMU
firstboot scenario then ran the rendered config and compose against the pinned image: generation 2,
Frigate healthy, one camera, and a JPEG snapshot. See
[the first-boot note](2026-11-14-qemu-firstboot.md).

The harness tests still use the hand-written Frigate fixture under `tests/fixtures/frigate/`; the
firstboot scenario uses renderer output.

## What remains unverified

There is no Docker daemon in this container, so the golden suite cannot run the upstream config
check or start the compose file here. The QEMU run covered the config check, the env-placeholder
substitution against the virtual camera, the unit start, and the media database on the recording
mount. Still unverified:

- The oneshot stop transition under systemd.
- A failed health probe or killed container rolling back to the previous unit and files.
- Device access on hardware for Coral, OpenVINO, Hailo, VAAPI, and QSV.
