# Status contract

`bitternd` serves one authoritative, read-only status document at
`GET /api/status`. The UI, the CLI, and support read the same contract. Schema
version 1 is the first version and is versioned independently of
`appliance.yaml`. The document never includes a password, token, camera
credential, or secret value.

This build is the daemon foundation. It loads and validates `appliance.yaml`,
renders in memory, probes the recording software image, Frigate, storage, the
two interfaces and hardware, and reads the apply state from the state root. It serves the status API on loopback
and, with `--ui`, the first-run wizard and status pages over TLS. The daemon
does not run an apply transaction on its own; the wizard applier calls the
BIT-011 transaction. It does not update, reset, or discover cameras. Updates run
through the fixed `bittern-update.service` helper; the status `updates` section
reports their last committed state.

## Overall states

The overall `state` is exactly one of the values below. The `reason` string is
always present and explains a non-ready state, or confirms that every
configured check passed.

| State | Meaning | Typical cause |
| --- | --- | --- |
| `booting` | The daemon has not completed its first authoritative build | Startup, before probes settle |
| `ready` | Configuration and render are valid and every configured runtime check passed | Healthy appliance |
| `degraded` | Configuration is usable but at least one runtime check is unavailable or unhealthy | Frigate unreachable when a camera is configured, recording software absent or not ready when a camera is configured, recording drive not attached, attached but not mounted, or failing its write test, pool member missing, free space below reserve, interface missing, carrier down, no IPv4 address |
| `updating` | An update transaction owns the appliance state | BIT-009 transaction in progress |
| `failed` | The configuration or render is not trustworthy | Config missing, invalid config, render error, render validation failure |

`booting` and `updating` are transient. `degraded` never hides a failed
configuration: a config or render failure is always `failed`. An unreachable
Frigate with at least one enabled camera is deliberately `degraded`, because the
appliance still has a valid configuration and can recover without operator
repair. A configuration with no enabled camera has no Frigate service: the
section reports `configured: false`, `camera_count: 0`, and `state: unknown`,
and it does not degrade the appliance.

## Section states

Every section carries a `state` value. Section states are separate from the
overall state because a section can be unknown without an appliance fault.

| Section state | Meaning |
| --- | --- |
| `ok` | The section has fresh evidence and it is healthy |
| `degraded` | The section has fresh evidence and it is unavailable or unhealthy |
| `failed` | The section could not produce a trustworthy result |
| `unknown` | This build or probe cannot report the section; this is not a fault |

`unknown` is the documented answer for data that does not exist yet. It is not
an invitation to invent a value. A section state of `unknown` does not by
itself change a `ready` overall state. A degraded or failed `cameras` section
does change it, because a configured camera that cannot deliver a picture is an
appliance fault, not missing evidence.

The camera list is built from `appliance.yaml` and the last health result in
`<state-root>/camera-health.json`. Adoption, an edit apply, the periodic time
check, and the explicit Test picture action write that file; the status builder
only reads it. The file holds a section state, the check time, the JPEG size, a
non-secret detail, the last-seen time, and the cached camera time result. It
never holds a URL or a credential. `stream` is `ok` only when the last snapshot
succeeded and Frigate is healthy, `snapshot` is the last explicit result,
`recording` follows the Frigate section, and `time` is the last camera clock
check. `distinct_qualities` is always present and starts with `main`. `sub` is
included only when the camera's sub profile resolves to a different stream than
main. A camera with no profiles, or one whose sub profile names the same source,
reports `["main"]`. The UI does not call a camera for a list render.

## Document

The top level is stable for schema 1:

```json
{
  "schema": 1,
  "generated_at": "2026-11-14T12:00:00Z",
  "state": "degraded",
  "reason": "recording service is not responding, so recording is stopped",
  "sections": {}
}
```

`generated_at` is the UTC time of the build. `sections` always contains
`config`, `render`, `time`, `frigate`, `cameras`, `storage`, `network`,
`updates`, and `reset`. `hardware` is present when hardware detection succeeds.
It is omitted only when the probe fails.

| Section | Fields | Unknown or absent data |
| --- | --- | --- |
| `config` | `path`, `present`, `schema_version`, `valid`, `errors[]` with `path`, `rule`, `message` | `schema_version` is null when the document cannot be parsed far enough to read it |
| `render` | `generation`, `apply_state`, `file_count`, `last_render`, `validation[]` with `name`, `state`, `message` | `generation` is the applied generation from `<state-root>/current` and is null before the first successful apply; `last_render` is null when no successful in-memory render has run |
| `time` | `state`, `synchronised`, `source`, `stratum`, `offset_milliseconds`, `last_sync`, `zone`, `local_now`, `last_error` | `state` is `ok`, `never-synchronised`, `drifted`, or `unknown`; `source` is null when no upstream source is selected; `last_sync`, `stratum`, and `offset_milliseconds` are null until a first observation; `local_now` is the build time in the configured zone |
| `frigate` | `configured`, `reachable`, `version`, `camera_count`, `image.state`, `image.digest`, `image.last_error`, `last_error` | `configured` is false and `state` is `unknown` when no camera is enabled; `camera_count` is the number of enabled cameras from `appliance.yaml`, so an unreachable API still reports the configured count; `version` is null until the API answers; `image` is always present and its `digest` is the pinned sha256 value |
| `cameras` | `items[]` with `id`, `name`, `address`, `mac`, `vendor`, `model`, `enabled`, `stream`, `snapshot`, `recording`, `time`, `profiles`, `distinct_qualities`, `last_seen`, `detail`, `snapshot_at`, `snapshot_bytes` | The list comes from `appliance.yaml` and the camera health cache, not a live camera call. `snapshot` and `stream` are `unknown` until the wizard or an explicit test has run; `last_seen` is null until a check records it; `recording` follows the Frigate section; `time` is the camera clock state below; `profiles` is the stream-profile result below; `distinct_qualities` names the live qualities that render to different streams, so the UI can show one stream instead of offering a false main and sub choice |
| `storage` | `path`, `present`, `detail_state`, `free_bytes`, `reserve_bytes`, `member_count`, `missing_members[]` | `present` is evidence that a recording drive for the path is attached, not that the directory exists; `detail_state` is `ready`, `not-attached`, `not-mounted`, or `write-test-failed`, and is empty only when the section is `unknown` or the probe cannot read the mount table; `free_bytes` is null unless the path is mounted, so the OS filesystem is never measured as recording storage; `member_count` reports the single media path known to this build, so a configured multi-drive pool is not enumerated until the pool mount work lands |
| `network` | `lan0` and `cam0`, each with `address`, `carrier`, `last_error` | `address` and `carrier` are null when the interface or sysfs evidence is unavailable |
| `updates` | `state`, `current_slot`, `current_version`, `current_generation`, `running_generation`, `committed_generation`, `committed_version`, `channel`, `last_check`, `available_version`, `available_generation`, `rollback`, `last_operation`, `current_operation`, `base` | `current_slot` is always null until the OS A/B work claims it; `current_version` and `current_generation` are the running bundle and are null on a kit baseline; `committed_generation` is 0 on a kit baseline; `base` is null until the first update commits; `last_check.result` is `never` before the first check; `current_operation` is absent unless an update or rollback journal is running |
| `reset` | `level`, `completed_at` | Both are null until BIT-010 implements a reset record |
| `hardware` | `cpu.model`, `cpu.cores`, `memory_bytes`, `accelerators[]` | The whole section is omitted only when the hardware probe fails; missing files inside the detection roots produce empty fields and an empty accelerator list instead |

### Appliance clock

The `time` section is the appliance half of [ADR 0035](adr/0035-appliance-as-time-authority.md).
The probe runs `chronyc -n tracking` and `chronyc -n sources` with a one second
bound each, both read-only. It never sets the system clock or the hardware clock.
When `chronyc` is absent or cannot reach `chronyd`, the section is `unknown`
with `last_error`; a missing probe is not a fault.

The section uses its own state vocabulary because the owner action differs from
the four shared section states:

| State | Meaning | Primary copy | Next step |
| --- | --- | --- | --- |
| `ok` | The clock was set from a source and the last update is fresh | The appliance clock is set from the house network. | None. |
| `never-synchronised` | No successful update since `chronyd` started, or no source is reachable | The appliance has not been able to check the time since it started. Its clock may be wrong, and the cameras follow it. | Check the house network connection. |
| `drifted` | A source updated the clock before, but the last update is more than two hours old or the source was lost | The appliance last checked the time on 24 November 2026 at 21:53 CET (UTC+01:00). Its clock may be off by about two seconds. | Check the house network connection. |
| `unknown` | The probe cannot read the running daemon | Bittern cannot read the appliance clock. | Restart the appliance, then check again. |

`last_sync` is chrony's reference time, the last successful update. A daemon
that has never had one reports the Unix epoch and is `never-synchronised`; a
daemon that had one and lost every source keeps the old reference time and is
`drifted`. Two hours is several missed updates at chrony's longest default poll
interval (about 17 minutes), so a late poll is not reported as drift.

`source` is the selected upstream address, or null for the local reference and
for a never-synchronised clock. `stratum` and `offset_milliseconds` are
technical details; the offset sign is positive when the system clock is ahead
of its source. `zone` is the configured `identity.timezone`, and `local_now` is
the build time in that zone. Both follow the display rule: a timestamp always
carries its zone label and offset, or an explicit `UTC`.

A non-`ok` clock does not change the overall state. An appliance with no
internet is a supported configuration and still gives every camera one
consistent clock through the local reference, which is the trade-off ADR 0035
records. The time section carries the warning and the next action on its own.
The `time` probe is not one of the apply-time native checks; `nft -c`,
`dnsmasq --test`, `chronyd -p`, and the Frigate config check remain apply-time
gates.

### Camera clock

The `time` entry on each camera is the camera half of
[ADR 0035](adr/0035-appliance-as-time-authority.md). It distinguishes the
states because the owner action differs:

| State | Meaning | Primary copy | Next step |
| --- | --- | --- | --- |
| `ok` | The camera clock agrees with the appliance within two seconds | This camera's clock is set by the appliance. | None. |
| `never-synchronised` | No successful set or confirmation yet, including a camera just switched to NTP mode | Bittern has not set this camera's clock yet. | None yet; the next check will try. |
| `drifted` | The camera clock differs from the appliance by more than two seconds | This camera's clock is four minutes behind. Bittern will correct it at the next check. | None if it corrects itself; otherwise check the camera. |
| `refused` | The camera rejected the account or the time operation | This camera will not let Bittern set its clock. Set the time on the camera itself, or check the camera's sign-in. | Use the camera's own settings. |
| `unreachable` | The camera did not answer the time request | Bittern cannot reach this camera to check its clock. | Check the camera's power and connection. |

The two second threshold is ADR 0035 section 5: the check reads the camera at
most every 15 minutes and writes only when the mode is wrong, the UTC differs
by more than two seconds, or the pushed zone offset changed. It is the same
threshold used to report `drifted`, so the state and the correction rule cannot
disagree. The mechanism, the NTP and push capability booleans, the last check
time, and the last confirmed sync are technical details; the state and its
detail are the owner copy. The camera time result never changes the overall
appliance state by itself, because a camera that refuses time still records
video.

### Camera stream profiles

The `profiles` entry on each camera is the adoption-time ONVIF Media result
from [ADR 0037](adr/0037-nvr-grid-per-pane-quality-and-full-screen.md) and
[BIT-027](plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability).
It records what the camera advertises, which media service answered, which
scheme and account worked, where the first authenticated timestamp came from,
and the reason a sub stream is absent. It never holds a complete URI or a
credential. A camera is probed once at adoption; the renderer never probes.

| State | Meaning | Primary copy | Next step |
| --- | --- | --- | --- |
| `ok` | Two usable profiles were selected | This camera offers two streams. Bittern uses the sharper one for one camera and the lighter one for the camera wall. | None. |
| `one-stream` | One usable profile, or two profiles that share an encoder | This camera offers one stream. Bittern uses it for both views. | Turn on a second, lighter stream in the camera's own settings, then run setup again. |
| `onvif-disabled` | The camera does not answer ONVIF, or has it switched off | This camera does not answer ONVIF. It records through the stream address you entered. | Turn on ONVIF in the camera's own settings, then run setup again to use its lighter stream. |
| `sign-in-rejected` | The camera refused every sign-in scheme and account | This camera refused the sign-in Bittern used for its stream settings. | Check the camera sign-in, or add the camera's ONVIF sign-in on the camera form. |
| `no-media-service` | ONVIF answers but advertises no Media1 or Media2 service | This camera answers ONVIF but does not offer its stream settings. | Turn on the camera's media or ONVIF profile service, then run setup again. |
| `no-profile` | Profiles are empty or none is an eligible H.264 or H.265 stream | This camera did not list a video profile Bittern can use. | Check the camera's stream settings, or add the stream address on the camera form. |
| `no-stream` | Profiles exist but no stream URI passed validation | This camera did not return a usable stream address. | Check the camera's stream settings, or add the stream address on the camera form. |
| `unreachable` | The camera did not answer the ONVIF endpoint | Bittern could not reach this camera to read its stream settings. | Check the camera's power and network cable, then run setup again. |
| `clock-unavailable` | The camera rejected the first authenticated timestamp | Bittern could not read this camera's clock, so the camera refused its stream settings. | Set the camera's clock in its own settings, then run setup again. |
| `malformed` | The camera answered with XML or a service list the client could not read | This camera returned stream settings Bittern could not read. | Update the camera firmware, or add the stream address on the camera form. |
| `not-probed` | No probe has run for this camera | Bittern has not read this camera's stream profiles yet. | Run setup or edit the camera again. |

Every failure state is a capability record, not an adoption failure. Adoption
commits the camera, its reservation, and its credential; the manual stream
address remains in use. The status and the setup wizard both show the primary
copy and the next step, because a camera with ONVIF switched off or a separate
ONVIF account is common and the owner must see the action rather than find it
in a log.

### Timestamps in the browser

Every timestamp the UI renders uses the configured zone with its abbreviation
and offset, for example `26 Nov 2026, 21:53 CET (UTC+01:00)`. A UTC timestamp is
labelled once, for example `26 Nov 2026, 12:00 UTC`; a raw UTC value is never
shown unlabelled. The pages that show a timestamp are the status page
(`generated_at`, `render.last_render`, `reset.completed_at`, and the clock
fields), the camera list (last seen and last picture), the operation progress
and result pages (started, finished, and time taken), the update card (last
check), and the test-picture caption.

Daylight saving comes from the IANA database through the configured zone name,
never from a stored offset or arithmetic. A transition changes what is
displayed; it does not change the clock, the RTC, or any recorded instant. The
status document's `generated_at`, `last_sync`, and `local_now` carry the same
information for the CLI and support.

Hardware detection is read-only. It reads `/proc`, `/sys`, and `/dev`, never
writes those trees, and needs no elevated privileges. The `accelerators` list
contains the supported classes that are present: `openvino` for a DRM render
node, `coral_usb` for a USB Coral, and `hailo8` for a Hailo-8. CPU model, core
count, and total memory come from `/proc/cpuinfo` and `/proc/meminfo`.

`render.apply_state` reports the last apply transaction, read by
`internal/apply.Inspect` from `<state-root>/current`, `journal.json`, and the
latest generation manifest. It is one of `not-applied`, `applied`, `applying`,
`rolling-back`, `failed`, or `rolled-back`, and is always present. It is
independent of the overall state: a failed or rolled-back attempt leaves the
previous applied generation running, so the overall state still reflects the
current configuration and runtime. `render.generation` names the applied
generation only; it does not advance until the transaction commits. An update transaction runs the
same apply pipeline inside its own transaction, so `render.generation` and `render.apply_state` can
advance to the applied configuration generation while `updates.committed_generation` still names the
previous bundle. The update's own commit point is `updates.committed_generation`; the applied
configuration generation is `last_operation.apply.generation`.

`render.validation` reports only checks the daemon can run in memory: file
count, sorted and unique relative paths, and the generated file header. The
native `nft -c`, `dnsmasq --test`, `chronyd -p`, and Frigate config checks are
apply-time gates, not status-time commands, and appear as an `unknown`
`native-checkers` result.

`updates.base` is the base fingerprint recorded by the update transaction. It
covers the Debian version, the kernel version, the Docker version, the presence
of the devices the appliance needs, and firmware presence. The base fingerprint
is recorded evidence for support and compatibility review, not a trust input; the
transaction checks the root-owned OS generation metadata instead. The transaction
writes only under `/var/lib/bittern` and the bounded subsystem allowlist in
[ADR 0036](adr/0036-update-bundles-deliver-subsystem-changes.md); it does not assume a general
writable root or knowledge of OS slots. The platform generation is supplied by a provider that
reads `/usr/lib/bittern/platform.json`, so image metadata can supply it now and
an installer fingerprint can supply it later without changing the process. The
daemon does not host the update worker, so it does not set the overall `updating`
state; the separate `bittern-update.service` owns the transaction and the journal. The status
probe reads the active journal read-only so the browser can show the current step; it never starts,
resumes, or changes the operation.
See [the architecture update flow](architecture.md#update-flow) and
[BIT-009](plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback).

### App bundle state, channel check, and rollback

The status probe is read-only. It reads
`<state-root>/update/state.json`, the activation pointer `<state-root>/app/current`,
the running bundle manifest, the retained bundle directories, the check record
`<state-root>/update/check.json`, and the newest update history record. It never
calls GitHub and never takes the update transaction lock. The check itself is an
on-demand command; the status page does not trigger one.

A kit-prepared appliance is the baseline. It has no `state.json` and no activation
pointer, and the fixed launcher runs `/usr/bin/bitternd`. The `updates` section is
`ok`, not `unknown`: `current_version` is the baseline build version,
`current_generation` and `running_generation` are null, `committed_generation` is
0, `committed_version` is null, `rollback.available` is false, and
`last_check.result` is `never`. The first published release is a real update from
generation 0 to generation 1.

| Field | Meaning |
| --- | --- |
| `current_version` | Version of the bundle named by the activation pointer, or the baseline build version when the pointer is 0 or absent |
| `current_generation` | Generation named by the activation pointer; null on the baseline |
| `running_generation` | Explicit name for the activation pointer generation; null on the baseline |
| `committed_generation` | Committed anti-rollback generation from `state.json`; 0 on the baseline |
| `committed_version` | Version committed in `state.json`; null before the first update |
| `channel` | Device channel from `appliance.yaml`, or the last committed channel when the config cannot be read |
| `last_check.checked_at` | UTC RFC 3339 time of the last check attempt; null before the first check |
| `last_check.result` | `never`, `update-available`, `up-to-date`, `rate-limited`, `offline`, or `failed` |
| `last_check.reason` | Redacted explanation for a non-success result; empty on success |
| `last_check.cached` | True when the check returned a stored record without a network request |
| `available_version` | Verified candidate version; only set for `update-available` |
| `available_generation` | Verified candidate generation; only set for `update-available` |
| `rollback.available` | A rollback target exists while the running generation equals the committed generation |
| `rollback.generation` | Retained target generation, or 0 for the baseline |
| `rollback.version` | Retained target version; null for the baseline |
| `last_operation.operation_id` | Newest history record |
| `last_operation.kind` | `app-bundle-update` or `app-bundle-rollback` |
| `last_operation.result` | `committed`, `rolled-back`, `rejected`, `failed`, or `state-unknown` |
| `last_operation.failed_step` | Failing step when the operation failed |
| `last_operation.reason` | Redacted failure or rollback reason |
| `last_operation.finished_at` | UTC RFC 3339 completion time |
| `last_operation.from_generation` | Generation before the operation |
| `last_operation.to_generation` | Candidate or restored generation |
| `last_operation.restored_generation` | Generation restored by a rollback, 0 for a baseline revert |
| `last_operation.baseline` | True when the rollback target was the baseline |
| `last_operation.apply.result` | `applied`, `failed`, `rolled-back`, or `not-run`; absent on a manual bundle rollback, which runs no apply |
| `last_operation.apply.generation` | Configuration apply generation the update wrote; null when no apply ran |
| `last_operation.apply.changed_files` | Number of live targets the apply wrote or removed |
| `last_operation.apply.failed_step` | Failing apply-internal step, such as `check:dnsmasq/dnsmasq.conf` |
| `last_operation.apply.reason` | Redacted apply failure |
| `last_operation.migration.result` | `none`, `applied`, `config-pending`, or `discarded` |
| `last_operation.migration.from` | Source appliance schema version, or null |
| `last_operation.migration.to` | Target appliance schema version, or null |
| `last_operation.migration.reason` | Redacted reason when the record is still pending |
| `current_operation.operation_id` | Operation id from the running journal |
| `current_operation.kind` | `app-bundle-update` or `app-bundle-rollback` |
| `current_operation.phase` | `install`, `rollback`, or `manual-rollback` |
| `current_operation.step` | The journal's next step key, or the last completed step when the next step is empty |
| `current_operation.steps_completed` | Steps before the named step, measured from the fixed plan |
| `current_operation.steps_total` | Number of steps in the fixed plan |
| `current_operation.started_at` | Journal creation time in UTC RFC 3339 |
| `current_operation.from_generation` | Generation before the operation |
| `current_operation.to_generation` | Candidate generation for an update, or restored generation for a rollback |
| `current_operation.baseline` | True when the rollback target is the baseline |

After a manual rollback, `current_generation` and `running_generation` name the
retained previous bundle, while `committed_generation` and `committed_version`
stay at the newer committed release. The next update must have a generation
greater than the committed generation. After a baseline revert,
`current_generation` and `running_generation` are null, `current_version` is the
baseline build version, `committed_generation` stays at the newer value, and
`last_operation.baseline` is true.

The channel check record is schema 1 at mode 0644. It contains the channel, the
check time, the result, a redacted reason, and the verified candidate version,
generation, tag, manifest digest, key id, and source commit. A failure record
contains no candidate. A rate limit, an offline device, a 404, a malformed
response, a bad signature, a tag mismatch, a channel mismatch, or an incompatible
manifest never reports `up-to-date`. The check result is a status fact only; an
install is a separate explicit command or UI action. Automatic periodic checking
remains open under [OQ-12](open-questions.md).

The Frigate section is derived from the validated configuration and the
rendered state. `camera_count` counts enabled cameras, and `configured` is true
only when at least one is enabled, because that is when the renderer emits the
Frigate config, compose file, and unit. With no enabled camera the section is
`unknown` and the probe is not called. The apply transaction uses the same
loopback `GET /api/version` endpoint as its health gate, but the decisions stay
separate: a failed apply health gate rolls the transaction back, while the
status API reports an unreachable configured Frigate as `degraded` without
changing the applied generation.

### Recording software image

`frigate.image.state` is one of `absent`, `downloading`, `ready`, or `failed`.
Docker's local image database is authoritative for `ready`: the probe runs
`docker image inspect` on the pinned reference, so a tag that moved is not
reported as the pinned image. The state file `<state-root>/image-prewarm.json`,
written by `bittern-image-prewarm.service`, explains `downloading` and `failed`
and carries the last download error. A `downloading` record older than the
unit's 30 minute timeout is reported as `failed`, so a killed process is not
reported as a live download. `frigate.image.digest` is the pinned sha256 value
and belongs in technical details. The raw image reference is not part of the
status document.

The image state is independent of the camera count, so it is visible before the
first camera exists. With no enabled camera it does not change the overall state.
With a camera configured, `absent`, `downloading`, or `failed` degrades the
Frigate section with a reason and no live write happens on apply until the image
is `ready`.

Schema version 2 names recording members in `storage.recording.drives` and
keeps an optional `mount_point` for the single media path. This status document
keeps the same field paths and values: `path` is the configured mount point, or
the product media path `/var/lib/bittern/media` when none is set. Pool member
enumeration and per-member health wait for the mount and mergerfs work required
by [ADR 0018](adr/0018-recording-capacity-pool.md). No status field path changed
for schema version 2.

The storage section carries the explicit detail state from
[ADR 0028](adr/0028-missing-recording-drive-and-boot.md). Boot does not depend
on the drive: the media fstab line carries `nofail` and an
`x-systemd.device-timeout`, so a missing enclosure leaves the media directory on
the OS filesystem and the appliance still boots and serves the UI.

| Detail state | Evidence | Meaning |
| --- | --- | --- |
| `not-attached` | `present: false`, `free_bytes: null`, `missing_members` may name the expected device | No eligible recording device is present. The media path may exist as a directory. Connect the drive and reboot, or rerun `storage-setup.sh` |
| `not-mounted` | `present: true`, `free_bytes: null` | A recording device is present, but the media path is not a mount point. Check the fstab line and the mount |
| `write-test-failed` | `present: true`, `free_bytes` measured | The media path is a mount point, but the probe file cannot be written, synced, or removed |
| `ready` | `present: true`, `free_bytes` measured, `state: ok` | The media path is a mount point and the write test passes |

The three failure details set the section to `degraded`, because the appliance is
still reachable and the owner can act; the overall state is `degraded` and the
Frigate section names the stopped service. `detail_state` is empty only when the
probe cannot read the mount table; the section is then `degraded` and `reason`
names the probe error. The attached case comes from the block inventory, never
from the media path: a plain directory on the OS disk is `not-attached` or
`not-mounted`, never `ready`, and its free space is never recorded capacity. The
write test creates and removes one small file and caches a pass for five
minutes, so a status poll does not keep the recording disk busy; a failure is
reported on the next build. The UI gives each detail state its own sentence and
next action. `detail_state` is additive to status schema 1: no existing field
path or value changed for a healthy appliance, and a reader must tolerate the
new field as it must tolerate any unknown field.

## API

| Endpoint | Response |
| --- | --- |
| `GET /api/status` | The status document, schema 1 |
| `GET /api/version` | `{"version":"dev","status_schema":1,"baseline":false}` |

Both endpoints are read-only and reject other methods with `405`.

`baseline` is true only for the preparation kit factory build. That build reports a
`0.0.0-factory-<commit>` version and generation 0, and it is not a published generation. A bundle
built from the same commit reports its `0.0.0-beta.<generation>-<commit>` version with
`baseline: false`, so a reader never has to guess whether two equal-looking versions are the same
artifact.

`bitternd` defaults to `--listen 127.0.0.1:8080` and
`--config /etc/bittern/appliance.yaml`. A non-loopback `--listen` address is
rejected. The status API has no TLS and stays on loopback. The browser-facing UI
is a separate listener: server-rendered Go templates with HTMX over TLS on
`0.0.0.0:443` by default, per [ADR 0019](adr/0019-ui-stack.md). `--ui-listen`
overrides the address for tests, and `--ui-insecure-http` selects plain HTTP
only for loopback development. The product service does not use the insecure
mode.

The UI certificate is read from `/etc/bittern/tls/server.crt` and its private
key from `tls.private_key_pem` in `/etc/bittern/secrets.yaml`. `--ui-certificate`
and `--ui-secrets` override those paths for tests. A missing certificate, a
missing key, or a mismatched pair stops startup; there is no HTTP fallback. The
daemon logs the certificate SHA-256 fingerprint at startup and every UI page
shows it so the owner can compare the browser warning. The setup page explains
why the self-signed certificate is not publicly trusted.

Frigate is probed at `http://127.0.0.1:5000` with a two-second client timeout,
per [ADR 0016](adr/0016-bittern-owns-frigate-config.md).

## Browser authentication

First boot generates a one-time setup token and stores the authoritative copy
as `setup_token` in `/etc/bittern/secrets.yaml`. It also writes an operator
convenience copy to `/var/lib/bittern/setup-token` at mode 0600. The local
console shows the setup block: a QR for
`https://<lan0-address>/ui/setup#token=<token>`, the token in readable groups,
and a separate short one-time code. The token is in the URL fragment, so the
browser never sends it in an HTTP request, access log, proxy log, or `Referer`.

The setup page reads the fragment locally, clears it with
`history.replaceState` before submitting, and posts the token once in the form
body. With JavaScript disabled the grouped token or short code is entered by
hand. The short code is a separate random value, never a prefix or truncation
of the token. It is stored only as a domain-separated SHA-256 hash and expiry in
`/etc/bittern/secrets.yaml`, expires after 10 minutes, is valid once, and uses
the same per-address and global setup rate limit as the token.

The token and short code are excluded from first-boot results, the status
document, service logs, and support output. The first UI visit requires one of
them. On success the owner sets the administrator password, both token copies
and the short-code material are removed, and an authenticated session is
created. Later visits use the login page. The only interactive account name is
`admin`.

If no local console is available, first boot prints the existing instruction
and the owner runs `sudo bittern setup-token --qr`. That command prints the
block to its caller and is not a service log.

The password is stored in `/var/lib/bittern/auth.json` (mode 0600) as a salted
PBKDF2-HMAC-SHA256 hash with 600,000 iterations, using only `crypto/hmac` and
`crypto/sha256`. No plaintext password or reversible encoding is stored. The
iteration count is recorded with each hash so it can be raised later.

Sessions are server-side and expire after 12 hours. The cookie is `Secure`,
`HttpOnly`, and `SameSite=Strict`, scoped to `/ui`. Every session carries a
separate CSRF token, and every mutating UI request must submit it. Guest
sessions carry the CSRF token for the login and setup forms. The setup and login
flows work without JavaScript.

A normal navigation with no valid session gets a 303 to the login route with
`next=<original page>`. An HTMX request, including a fragment poll, gets a 401
with `HX-Redirect` to the login route with `next=<owning page>`, and the
response contains no login or setup HTML. HTMX therefore performs one
top-level navigation instead of swapping the login page into the live region.
The same rule covers a fragment request while setup is still required.

The session store is in memory, so a daemon restart signs out every browser.
[BIT-045](plan/implementation-plan.md#bit-045-keep-the-administrator-session-across-a-daemon-restart)
is blocked on [OQ-37](open-questions.md#register), which decides whether sessions are persisted and
where.

Failed login and setup attempts are limited per remote address and globally
with a short lockout: five failures per address or 20 globally in a 10 minute
window trigger a five minute lockout. Unknown user and wrong password return the
same message. The limiter state is in memory and resets when the daemon
restarts.

Security headers on every UI response are a self-only CSP, `nosniff`,
`X-Frame-Options: DENY` with `frame-ancestors 'none'`, and `Referrer-Policy:
no-referrer`. HSTS is deliberately absent because the certificate is
self-signed; pinning it would make a renamed or re-addressed appliance
unreachable.

## Redaction

`agent/internal/redact` is the single redaction helper. The status builder,
the structured logger, the apply transaction, and first boot run text through
it. Known secret values are replaced with `[REDACTED]`, and the common textual
forms are masked even without prior registration:

- sensitive key/value assignments such as `password:`, `token=`, or `"api_key":`;
- bearer tokens;
- credentials embedded in RTSP or HTTP URLs;
- PEM private keys.

A `credentials_ref` value is a key name, not a credential, and is not treated
as a secret. The status document never includes raw `appliance.yaml` content,
rendered file bodies, secrets, or environment dumps. It never includes the setup
token, the administrator password hash, a session identifier, or a CSRF token.

The setup token is never printed by first boot's stdout or by the daemon. It
joins the redactor's literal values as soon as first boot reads the secrets
file, so a token value cannot appear in status output, a structured log line,
or support output. The local console block is written directly to the console
device, not to the service journal. The explicit `bittern setup-token --qr`
command writes only to the caller's terminal; under systemd nothing invokes it.
The short code is printed once in the same block and only its hash is stored.

Logs are one JSON object per event on one line. The daemon logs
`daemon.start`, `daemon.stopping`, `daemon.stopped`, `daemon.failed`,
`http.request`, and shutdown errors. Every attribute is redacted before it is
written.

## Packaging

[`agent/packaging/bitternd.service`](../agent/packaging/bitternd.service) runs
the UI on 443 with `CAP_NET_BIND_SERVICE`, keeps the status API on loopback,
makes the system read-only except `/etc` and `/var/lib/bittern`, and applies a
system-call filter. The wizard calls apply in this process, so the unit also
keeps `CAP_NET_ADMIN` and `AF_NETLINK` for `nft -c`, and `CAP_NET_RAW` only to
bind camera discovery to `cam0`; `/etc` is writable because
the transaction atomically replaces `/etc/nftables.conf` and writes the
generated network, DHCP, chrony and Frigate unit files. The nftables ruleset is
the network boundary for the UI listener.

[`agent/packaging/bittern-first-boot.service`](../agent/packaging/bittern-first-boot.service)
is the oneshot that runs `bittern first-boot` after local filesystems and before
`bitternd`. It skips itself when `/var/lib/bittern/first-boot.json` exists. It
grants `/etc` and `/var/lib/bittern` because the transaction atomically replaces
`/etc/nftables.conf` and writes the generated network, DHCP and chrony files,
plus `CAP_NET_ADMIN` and `AF_NETLINK` for `nft -c` and the firewall health probe, and
`CAP_NET_RAW` for the camera discovery socket. It sets
`BITTERN_CONSOLE=/dev/tty1` so `bittern first-boot` writes the setup QR, grouped
token and short code directly to the local console instead of the journal. The
image and POC package set installs `qrencode` for that block. The QEMU
first-boot scenario installs and runs both units.

Related: [architecture](architecture.md), [agent README](../agent/README.md),
[ADR 0012](adr/0012-testing-and-release-gates.md),
[ADR 0016](adr/0016-bittern-owns-frigate-config.md),
[ADR 0017](adr/0017-reset-levels-and-decommissioning.md),
[ADR 0018](adr/0018-recording-capacity-pool.md),
[ADR 0019](adr/0019-ui-stack.md).
