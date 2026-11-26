# 0035. The appliance is the time authority for the camera network

*Every camera on the camera segment takes its time from the appliance, through NTP where the camera
supports it and through an ONVIF time write where it does not, and a camera that refuses is still
adopted and reported.*

- Status: Accepted
- Date: 2026-11-26
- Deciders: maintainer
- Related: [ADR 0005](./0005-dual-nic-isolated-camera-network.md),
  [ADR 0016](./0016-bittern-owns-frigate-config.md),
  [ADR 0019](./0019-ui-stack.md),
  [ADR 0022](./0022-community-functionality-ux-wins.md),
  [ADR 0024](./0024-onvif-discovery-levels.md), [architecture](../architecture.md),
  [status contract](../status.md), [open questions](../open-questions.md),
  [the 2026-11-26 recon note](../notes/2026-11-26-time-sync-recon.md),
  [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability)

## Context

A camera network without one clock produces footage that cannot be lined up, overlays with the wrong
date, and camera event logs that cannot be matched to recordings. Cameras ship with their own clocks,
their own time zones, and their own idea of daylight saving. The camera segment has no internet
([ADR 0005](./0005-dual-nic-isolated-camera-network.md)), so a camera cannot reach a public time
server and cannot be corrected by a cloud service.

The appliance is already on the path to being the time authority. It renders `chrony` with the camera
address as the listening address and the camera subnet as the allowed client set, and `dnsmasq`
advertises it with DHCP option 42. It is not one yet: the render has no `local` reference, so an
appliance that has never synchronised does not serve usable time; the status contract has no clock
state; and nothing can set a camera's clock. The camera side is also bare: adoption collects an RTSP
URL and credentials, has no ONVIF account, and discovery has never sent an authenticated request.

The verification is in [the 2026-11-26 note](../notes/2026-11-26-time-sync-recon.md). The facts that
shape this decision, with every source accessed on 2026-11-26:

| Fact | Source |
| --- | --- |
| `GetSystemDateAndTime` is a required Device service operation with access class `PRE_AUTH`. It returns `DateTimeType` (`Manual` or `NTP`), `DaylightSavings`, an optional POSIX `TimeZone`, and `UTCDateTime`. | ONVIF Core Specification 26.06, 8.3.6: <https://www.onvif.org/specs/core/ONVIF-Core-Specification.pdf>; Device WSDL: <https://www.onvif.org/ver10/device/wsdl/devicemgmt.wsdl> |
| `SetSystemDateAndTime` takes `DateTimeType`, `DaylightSavings`, optional POSIX `TimeZone` and optional `UTCDateTime`. Access class `WRITE_SYSTEM`. The operation is required; the manual and NTP modes are conditional on device support, and the Device service capabilities define no flag for time setting. | Core 26.06, 8.3.7 and Table 7; Core 19.12, Table 45: <https://www.onvif.org/specs/core/ONVIF-Core-Specification-v1912.pdf>; WSDL |
| `GetNTP` and `SetNTP` are conditional on the `Network.NTP` capability, an integer that states the maximum number of NTP servers. `SetNTP` has access class `WRITE_SYSTEM`; `GetNTP` is `READ_SYSTEM`. Changing the NTP list does not change the clock mode; `SetSystemDateAndTime` activates NTP. | Core 26.06, 8.2.6 and 8.2.7; Core 19.12, Tables 19 and 20; WSDL; Profile Q Device Test Specification 21.06: <https://www.onvif.org/wp-content/uploads/2021/06/ONVIF_Profile_Q_Device_Test_Specification_21.06.pdf> |
| A `User` level ONVIF account is rejected with HTTP 401 for `SetSystemDateAndTime`. The Core default access policy grants `WRITE_SYSTEM` to the Administrator level only, so time writes need an administrator-class ONVIF account. | Profile Q Device Test Specification 21.06, 5.2.4; Core 26.06 Table 7: <https://www.onvif.org/specs/core/ONVIF-Core-Specification.pdf> |
| ONVIF defines no fallback for a camera in NTP mode whose server is unreachable. Hikvision documents that the time must then be set manually, and documents a 1 to 10080 minute NTP interval with a 60 minute default for its devices. | Hikvision camera manual: <https://www.hikvision.com/content/dam/hikvision/vn/product-datasheet/UD15501B-C_Baseline_User-Manual-of-Network-Camera_V5.5.90_20221223.pdf>; Hikvision NTP and DST note: <https://www.hikvision.com/content/dam/hikvision/en/support/download/how-to/nvr/How%20to%20configure%20NTP%20and%20DST.pdf> |
| Cameras commonly reject authenticated ONVIF requests whose timestamp is far from their own clock; Axis replay protection is reported at about ten seconds. The exact window is not vendor-documented in the source found, so it stays unverified. | Client library issue report: <https://github.com/agsh/onvif/issues/172>; Network Optix support: <https://support.networkoptix.com/hc/en-us/articles/216517857-Troubleshoot-ONVIF-camera-issues> |
| `chronyd` keeps running with no reachable source, reports `Leap status: Not synchronised`, and does not correct the clock. Automatic stepping is limited to the first `makestep` updates, then corrections slew at `maxslewrate`, default 83333.333 ppm. `chronyc makestep` can force a step but warns that jumps break software. | chrony 4.6: <https://chrony-project.org/doc/4.6/chrony.conf.html>, <https://chrony-project.org/doc/4.6/chronyc.html> |
| The `local` directive makes a server appear synchronised to clients even when it never was, for isolated networks; the default local stratum is 10. Without it a never-synchronised server is not a usable source. | chrony 4.6 `local`: <https://chrony-project.org/doc/4.6/chrony.conf.html> |
| DST rules still change. IANA released 2026f, which records that Cuba will not fall back on 2026-11-01. Debian 13 carries tzdata 2026f and chrony 4.6.1, and chrony depends on tzdata. | <https://www.iana.org/time-zones>; <https://packages.debian.org/trixie/tzdata>; <https://packages.debian.org/trixie/chrony> |
| Go's `time/tzdata` is used only when the system database is absent; explicit loading from a chosen copy uses `time.LoadLocationFromTZData`. | <https://pkg.go.dev/time/tzdata>; <https://pkg.go.dev/time#LoadLocationFromTZData> |

Two repository facts complete the context. First boot is ordered after local filesystems and before
`bitternd`, not after `chrony.service`, and it mints the UI certificate from the current clock with a
ten year lifetime and no later re-mint for a clock change
(`agent/internal/firstboot/certificate.go:69-70`, `:102-116`). The status builder does not run the
native checks, including chrony (`agent/internal/status/builder.go:328-332`). Both are covered by the
follow-ups.

## Decision

**1. The appliance consumes time on the management network and serves it on the camera network, on
the camera interface only.**

- `chrony` is a client on `lan0` and a server on `cam0`. Client sockets are bound to `lan0`. The
  server listens on the `cam0` address only, with the `cam0` subnet as the only allowed client set.
  The rendered configuration carries `bindacqdevice lan0`, `bindaddress <cam0 address>`,
  `allow <cam0 subnet>`, and `local`.
- The appliance never answers NTP on `lan0`, never on `tailscale0`, and never relays NTP between the
  two segments. A management-network client that asks the appliance for time gets no answer.
- The firewall consequence is explicit: the only `udp dport 123` accept is on `cam0`, and no rule is
  added for `lan0`. The existing rule at `templates/nftables/bittern.nft.tmpl:39` is sufficient for
  cameras, and its absence on `lan0` is the enforcement, not an oversight.
- DHCP option 42 stays pointed at the appliance, and `cam0` still has no DNS and no forwarding.

**2. The appliance steps on first contact with a source, and only then.**

- The system clock and the hardware clock are UTC. `chrony` keeps `driftfile`, `rtcsync` and
  `makestep 1.0 3`: a correction larger than one second is stepped, but only within the first three
  clock updates after `chronyd` starts. After that the clock is slewed at the configured maximum.
- No other path sets the clock. There is no set-time action in the UI or CLI, no manual `date`,
  `timedatectl` or `hwclock` call, and no code writes a local time into the RTC. `rtcsync` writes UTC.
- The three-update window counts updates, not wall time. An appliance that boots offline and finds a
  source a week later may still step. That is accepted because the alternative is a slew measured in
  hours, and because the status change is visible. The follow-ups require the step to be recorded
  with the offset before and after.
- The upstream source set remains [OQ-20](../open-questions.md). This decision does not choose
  between the public pool, DHCP-provided servers, a vendor set or owner-supplied addresses. Every
  option is consumed on `lan0`, and this decision is compatible with all of them.

**3. With no internet at all, the appliance still gives every camera one clock.**

- The render carries `local stratum 10`, so `chronyd` serves time to cameras even when it has never
  been synchronised or when its last update is old. Consistency across cameras and the recorder
  matters more than absolute accuracy, and a camera that follows the appliance agrees with the
  recording timeline. The local reference is not gated on a plausible clock: a pre-2020 clock is
  already a visible fault and an update blocker, and one consistent wrong clock is still better for
  the recording timeline than three different wrong ones.
- The appliance does not pretend the clock is right. The status `time` section reports
  `never-synchronised`, the last successful update, the offset and the source, and the primary copy
  says the appliance has not checked the time and the cameras follow it.
- The existing update gate stays: a clock before 2020 is refused by the update preflight
  (`agent/internal/update/steps.go:33-36`) and is a visible fault, not a silent one. A camera that
  was set from a wrong appliance clock is corrected within one sync interval after the appliance
  clock becomes right.

**4. The camera policy is NTP first, an ONVIF time push second, and the mechanism is detected per
camera.**

- Preferred mechanism: point the camera at the appliance's NTP server. Read the current
  `SystemDateAndTime` first. If it already reports `NTP`, only verify the clock. Otherwise
  `SetNTP(FromDHCP=false, NTPManual=[cam0 address])`, then
  `SetSystemDateAndTime(NTP, DaylightSavings=<current>, TimeZone=<current if returned>)`. The camera
  keeps its own time zone; Bittern changes the clock source and the mode, not the display zone.
  Verify by reading `DateTimeType == NTP`.
- Fallback mechanism: push the time with `SetSystemDateAndTime(Manual, DaylightSavings=false,
  TimeZone=<the appliance zone offset in force now, in POSIX form>, UTCDateTime=<now UTC>)`. Sending
  the UTC instant with the offset that applies now makes the camera's local time equal the
  appliance's local time, and it does not depend on the camera's own DST rules. Verify by reading the
  returned `UTCDateTime` back.
- Detection is per camera and is never assumed. The order is: unauthenticated
  `GetSystemDateAndTime` (does the camera expose time at all), authenticated `GetServiceCapabilities`
  (is `Network.NTP` present), then the two attempts above. If the unauthenticated read fails, the
  writes are still attempted with the appliance clock, and a rejected timestamp is recorded as
  `refused`. A streaming profile claim, a model name, or a vendor list is not evidence that a camera
  supports NTP or time writes.
- The detected mechanism is derived state, not configuration. It is cached with the camera health
  record, never written into `appliance.yaml`, and re-detected after a failure or a reported mode
  change. The owner is never asked to choose it.

**5. Synchronisation runs at adoption and periodically after it.**

- At adoption: after the camera transaction commits and its credential exists, one bounded time sync
  runs outside the transaction. It never extends or fails the adoption.
- Thereafter: one check per camera every 15 minutes. The check reads the camera's mode and clock,
  and writes only when the camera is in the wrong mode, its UTC differs from the appliance by more
  than 2 seconds, or the zone offset in force has changed since the last push.
- The interval is chosen for daylight saving. A transition changes the offset by an hour, or by 30
  minutes in a zone such as Lord Howe, and a 15 minute interval bounds how long a camera can show the
  wrong local time. It is 96 reads per camera per day, and writes only on a real difference, so it is
  not a retry storm.
- Bounds: 5 seconds per ONVIF operation, 15 seconds per camera, four cameras in parallel, one pass at
  a time. A failed camera does not retry inside the cycle. After three consecutive failures it backs
  off to hourly, after 24 hours to six-hourly, and a success resets the schedule. Cancellation stops
  the pass.
- A camera that has just been switched to NTP mode may not have polled yet. The first check records
  `never-synchronised` and waits for the next cycle; it does not report a failure.

**6. The safety rules are part of the decision.**

- Credentials are required. The ONVIF account is the camera's existing `credentials_ref`. When a
  camera needs a separate ONVIF account, it gets a second key under the same reference, never a
  second secret store. The client tries the camera's existing sign-in first, because most cameras use
  one account for RTSP and ONVIF. A camera that rejects it gets one optional second sign-in on the
  camera form, stored under the same reference; the camera is adopted whether or not it is supplied.
  Discovery stays unauthenticated; a credential is used only against the selected camera, after the
  owner has entered it. No credential, complete URL, or endpoint with user information appears in
  status, logs, audit events, the support bundle, or a test.
- Every request validates the endpoint against the camera segment before connecting: the camera's own
  IPv4 address, `http` or `https`, no user information, no redirects, no other host. The existing
  `allowedXAddr` rules apply (`agent/internal/discovery/layers.go:211-232`).
- The first authenticated request is timestamped from the camera's own clock, read without
  authentication. A camera that is hours off rejects a timestamp from the appliance, and then it
  could never be fixed. When the unauthenticated read is unavailable, the write uses the appliance
  clock and a rejection is reported, not retried.
- A camera that refuses, times out or is offline changes nothing else. Adoption still commits, the
  camera keeps recording, and the failure is reported with the operation that failed.

**7. What the owner sees, in plain language.**

The status contract gains a `time` section for the appliance and a time entry per camera. The
appliance states are `ok`, `never-synchronised` (never checked or no source), and `drifted` (last
check too old). The camera states are `ok`, `never-synchronised`, `drifted`, `refused` and `unreachable`,
because they need different actions.

| State | Primary copy | Next action |
| --- | --- | --- |
| Appliance `ok` | The appliance clock is set from the house network. | None. |
| Appliance `never-synchronised` | The appliance has not been able to check the time since it started. Its clock may be wrong, and the cameras follow it. | Check the house network connection. |
| Appliance `drifted` | The appliance last checked the time on 24 November at 21:53 CET. Its clock may be off by about two seconds. | Check the house network connection. |
| Camera `ok` | This camera's clock is set by the appliance. | None. |
| Camera `never-synchronised` | Bittern has not set this camera's clock yet. | None yet; the next check will try. |
| Camera `drifted` | This camera's clock is four minutes behind. Bittern will correct it at the next check. | None if it corrects itself; otherwise check the camera. |
| Camera `refused` | This camera will not let Bittern set its clock. Set the time on the camera itself, or check the camera's sign-in. | Use the camera's own settings. |
| Camera `unreachable` | Bittern cannot reach this camera to check its clock. | Check the camera's power and connection. |

Mechanism names such as NTP, ONVIF and POSIX belong in the technical details, not in the primary
copy. UTC is a zone label, not a mechanism, and appears when UTC is the displayed zone. A timestamp
always carries its zone: the configured zone with its abbreviation and offset, or an explicit `UTC`
label. A raw UTC timestamp is never shown without saying so.

**8. Daylight saving and the time zone are handled once, from a UTC clock.**

- The system clock and the hardware clock are always UTC and are never set to local time. The time
  zone is a display and camera-time concern resolved through the IANA database, so a DST transition
  changes what is shown and what is pushed, not what the machine believes the time is.
- The zone is the existing `identity.timezone` IANA name. Setup prefills it from the browser's
  `Intl.DateTimeFormat().resolvedOptions().timeZone`, shows it on the review step, and allows an
  override. When the browser gives nothing usable, or JavaScript is off, the value is `UTC` and the
  flow still completes.
- No schema change and no migration are required. `identity.timezone` has been required since schema
  version 1 and first boot already writes `UTC` (`schema/appliance.schema.json:64-83`,
  `schema/versions/v1/appliance.schema.json:59-80`, `agent/internal/firstboot/config.go:153`). Every
  existing device already carries a value. The next completed setup replaces `UTC` with the browser
  zone; until then `UTC` is a working default. A configuration with the field missing is invalid, not
  silently defaulted, and a truly pre-field device cannot exist because no schema version lacked the
  field.
- The appliance ships tzdata twice. The Debian package is present because `chrony` depends on it, and
  it serves the OS and third-party software at the image cadence. The signed application bundle also
  carries a copy of the IANA database and loads it explicitly with `time.LoadLocationFromTZData`, so
  the rules used for display and for camera pushes update with the application bundle, which is the
  loop that can ship a rule change in days without an OS update. There is no runtime download of
  timezone data. A device that receives no bundle update keeps the rules in its last bundle; that is
  the honest limit, and the IANA release in use is reported in status and the support bundle, so
  support can see a device that has missed a release. The release check records the IANA release in
  the bundle metadata.
- The UI shows local time with the correct offset and abbreviation, for example `26 Nov 2026, 21:53
  CET (UTC+01:00)`. `UTC` appears only when labelled. The change applies to the status panel, the
  camera list, the operation and update views, and every future timestamp.
- On the camera side, NTP mode keeps the camera's own zone. Push mode writes the offset that applies
  now and re-pushes when it changes, so a DST transition self-corrects within one 15 minute interval
  rather than persisting until someone notices. The ONVIF `TimeZone` is a POSIX string, not an IANA
  name, so the push uses a fixed offset string and does not ask the camera to apply a DST rule.

**9. A camera that cannot be synchronised does not block adoption.**

- Adoption commits the camera, its reservation and its secret as it does today. The time sync runs
  after the commit and cannot fail the transaction.
- The reasons: a wrong camera clock does not stop video, snapshots or recording, and adoption would
  not fix it; [ADR 0024](./0024-onvif-discovery-levels.md) already makes an optional service a
  degradation rather than a failed adoption, and time is another optional service; and blocking would
  turn a camera setting the owner can fix in the camera's own UI into a failed installation, which the
  owner cannot fix during adoption anyway.
- The camera's own clock still affects its overlay, its event log and any camera-side retention, so a
  refused camera is reported, not hidden.

**10. Out of scope.**

- Frigate's own time zone, its recording metadata and its database timestamps. The pinned Frigate
  image is unmodified and its timestamp behaviour is not changed here.
- Camera time zones in NTP mode, beyond preserving what the camera reports.
- GPS or radio clock hardware, NTS, leap second policy beyond the UTC clock and the IANA data, and
  time for containers other than the host clock.
- A per-camera time source UI, a fleet time policy, and any time service on the management network.

## Consequences

### Positive

- One clock authority for the camera segment, with a mechanism that matches what the camera actually
  supports instead of a vendor assumption.
- An offline appliance still gives every camera one consistent clock, and the owner is told the clock
  is unchecked rather than shown a false healthy state.
- A camera that refuses time no longer silently drifts; the owner gets a named state and an action.
- DST is automatic for the appliance and self-correcting for cameras, with no manual step at a
  transition.
- The 15 minute check is mostly reads, so the traffic and the credential exposure are small.

### Negative / costs

- A second authenticated protocol client in `bittern`, with a credential use, a new failure surface,
  and an SSRF boundary to hold. It reuses the ONVIF implementation BIT-008 selects; a new library
  needs the [ADR 0024](./0024-onvif-discovery-levels.md) review. It needs its own tests.
- The push path depends on a POSIX time zone string and on camera tolerance for a fixed offset. The
  exact acceptance is unverified until the camera matrix has rows.
- Some cameras will refuse time operations. The product will show a permanent state for them, and the
  owner may have to set the camera clock by hand until a firmware or configuration fix is found.
- The appliance can still step its clock after a long offline period, which can sign sessions out and
  reorder the recording timeline once. The status change makes it visible but does not prevent it.
- The `local` reference means cameras trust an appliance clock that may be wrong. Consistency is
  chosen over accuracy, and the status must carry that truth.
- Carrying the IANA database in the bundle adds a release step and a few hundred kilobytes.

### Follow-ups

- Renderer: `bindacqdevice lan0`, `bindaddress <cam0>`, `allow <cam0 subnet>`, `local stratum 10`,
  and no server on `lan0`. Golden renders and a firewall test that `udp 123` is accepted on `cam0`
  only.
- Status: add the `time` section and the per-camera time fields to the contract and the golden
  status, and keep every secret out. Update [status.md](../status.md) in the same change.
- First boot: write the browser zone. The certificate is re-minted, not waited for: first boot never
  blocks on the clock, and when the clock first becomes plausible and the current certificate window
  does not cover it, the agent re-mints the certificate with the same identity and key. A bounded
  wait before the first mint is allowed but not required. The expired-certificate finding is a defect
  in its own right.
- ONVIF time client: authenticated Device service calls, endpoint validation, camera-clock
  timestamps, bounded timeouts, per-camera backoff, and the derived mechanism cached with the camera
  health record.
- UI: local time rendering with zone and abbreviation, the status time block, and the camera time
  state in the camera list, all following [ADR 0026](./0026-design-system-and-ui-review.md) and
  [the copy rules](../design/copy.md).
- tzdata: add `tzdata` explicitly to `image/packages.txt` and `packaging/install.sh`, carry the IANA
  copy in the bundle, load it explicitly, and report its release in status and the support bundle.
- Camera matrix: add columns for the time mechanism, the NTP result, the pushed result, and the
  evidence, starting with the Axis reference device in BIT-027.
- Tests, all required before this decision is implemented:
  - A fixed clock at both DST boundaries on a zone with a spring forward and a fall back, asserting
    the displayed local time, the pushed camera time and the offset across the transition.
  - An assertion that no code path sets the system clock or the hardware clock to anything but UTC,
    including a static check for `timedatectl set-time`, `hwclock --set`, `date -s`, `settimeofday`
    and `clock_settime` outside `chrony`.
  - Four camera fixtures: one that accepts NTP configuration, one that accepts only a pushed time,
    one that refuses both, and one that is offline at the time of synchronisation. Adoption commits
    in all four, the status distinguishes all four states, and the request count proves there is no
    retry storm.
  - A device configuration written before setup collected the zone (`timezone: UTC`), which loads and
    keeps working, and whose next completed setup writes the browser zone, with no schema migration.
  - An offline appliance that serves `local` stratum 10, reports `never-synchronised`, and corrects every
    camera within one interval after the clock becomes right.
  - Redaction: no credential, complete URL, or endpoint with user information in status, logs, the
    support bundle, or test output.

## Alternatives considered

- **Assume every camera supports NTP.** Rejected: `SetNTP` is conditional on the `Network.NTP`
  capability, the Device service capabilities define no flag for time setting at all, and a streaming
  profile claim is not evidence. Assuming support produces a camera that looks configured and never
  syncs.
- **Push the time only.** Rejected: it is a write on every correction, needs an authenticated
  administrator-class account on every cycle, and cannot be self-correcting when the appliance is
  offline. It also depends on the appliance being a time source anyway, so NTP is strictly better
  where the camera supports it.
- **Run a separate time server.** Rejected: another always-on device to power, patch, secure and
  explain, on a segment with no internet. The appliance is already the only device with a foot in
  both segments and already advertises itself with option 42.
- **Point cameras at the house router or a public pool.** Rejected: the camera segment has no route
  to either, and a public pool would require opening camera egress that
  [ADR 0005](./0005-dual-nic-isolated-camera-network.md) forbids.
- **Serve NTP on both interfaces.** Rejected: it adds an exposed service on the management network
  with no product need, against the isolation of [ADR 0005](./0005-dual-nic-isolated-camera-network.md).
  If a LAN time service is ever wanted, it needs its own ADR.
- **Push time through vendor HTTP APIs.** Rejected: per-vendor code, per-vendor credentials and
  per-vendor bugs for a function the open standard already defines. It also duplicates the ONVIF work
  BIT-027 needs anyway.
- **Do nothing and let cameras use their defaults.** Rejected: cameras ship pointed at public servers
  they cannot reach, and the owner gets drifting overlays and unmatched event logs with no state and
  no action.

## Open questions

- **OQ-41:** which cameras support NTP mode, pushed time, both, or neither, and which honour DHCP
  option 42? The camera matrix has no tested product row. This is the capability matrix the decision
  depends on.
- **OQ-42:** what clock skew do cameras accept on authenticated ONVIF requests, and must the
  authenticated header always be timestamped from the camera's unauthenticated time read? The ten
  second Axis figure is unverified.
- **OQ-43:** do cameras accept a fixed-offset POSIX time zone string and apply the DST flag, or must
  Bittern send a DST rule string? The answer decides whether the push path stays fixed-offset.

## References

All accessed 2026-11-26.

- ONVIF Core Specification 26.06: <https://www.onvif.org/specs/core/ONVIF-Core-Specification.pdf>
- ONVIF Core Specification 19.12, for the explicit access class tables:
  <https://www.onvif.org/specs/core/ONVIF-Core-Specification-v1912.pdf>
- ONVIF Device service WSDL: <https://www.onvif.org/ver10/device/wsdl/devicemgmt.wsdl>
- ONVIF schema: <https://www.onvif.org/ver10/schema/onvif.xsd>
- ONVIF Profile Q Device Test Specification 21.06:
  <https://www.onvif.org/wp-content/uploads/2021/06/ONVIF_Profile_Q_Device_Test_Specification_21.06.pdf>
- chrony 4.6 configuration: <https://chrony-project.org/doc/4.6/chrony.conf.html>
- chrony 4.6 control and monitoring: <https://chrony-project.org/doc/4.6/chronyc.html>
- Debian 13 chrony package: <https://packages.debian.org/trixie/chrony>
- Debian 13 tzdata package: <https://packages.debian.org/trixie/tzdata>
- IANA time zone database: <https://www.iana.org/time-zones>
- Go `time/tzdata`: <https://pkg.go.dev/time/tzdata>
- Go `time.LoadLocationFromTZData`: <https://pkg.go.dev/time#LoadLocationFromTZData>
- Hikvision network camera manual:
  <https://www.hikvision.com/content/dam/hikvision/vn/product-datasheet/UD15501B-C_Baseline_User-Manual-of-Network-Camera_V5.5.90_20221223.pdf>
- Hikvision NTP and DST note:
  <https://www.hikvision.com/content/dam/hikvision/en/support/download/how-to/nvr/How%20to%20configure%20NTP%20and%20DST.pdf>
- Axis P13 series manual: <https://help.axis.com/download/um_p1378_le_t10135686_2505.pdf>
- Network Optix ONVIF troubleshooting:
  <https://support.networkoptix.com/hc/en-us/articles/216517857-Troubleshoot-ONVIF-camera-issues>
- ONVIF client issue on Axis replay protection: <https://github.com/agsh/onvif/issues/172>
- [The 2026-11-26 recon note](../notes/2026-11-26-time-sync-recon.md)
