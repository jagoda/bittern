# Time synchronisation beta test

- Date: 2026-11-27
- Status: maintainer checklist for the generation 4 time synchronisation beta. Not run yet. The
  appliance and camera halves are verified by Go tests and the QEMU firstboot fixture; no real camera
  has run this code.
- Bench: Debian 13 amd64, one management NIC on the house LAN, the camera NIC on the camera network,
  one to four cameras, a local console, and a phone or a second clock on the house LAN.
- Payload: the published `app-beta-000004` release and the device's installed generation 3
  (`app-beta-000003`). Generation 4 is the first bundle that applies rendered configuration inside
  the update transaction, and it adds the appliance time authority, per-camera clock
  synchronisation, and zone-aware timestamps.
- Related: [ADR 0035](../adr/0035-appliance-as-time-authority.md),
  [ADR 0036](../adr/0036-update-bundles-deliver-subsystem-changes.md),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability),
  [appliance time note](../notes/2026-11-27-appliance-time.md),
  [camera time note](../notes/2026-11-27-camera-time-onvif.md),
  [time sync recon](../notes/2026-11-26-time-sync-recon.md),
  [apply on update](../notes/2026-11-27-apply-on-update.md),
  [status contract](../status.md), [camera matrix](../notes/camera-matrix.md),
  [OQ-41](../open-questions.md), [OQ-42](../open-questions.md), [OQ-43](../open-questions.md),
  [live view beta test](live-view-beta-test.md),
  [beta bench rollout](beta-bench-rollout.md)

This runbook answers one question: after a device on generation 3 updates to generation 4, do the
appliance and every adopted camera agree on the time, does the appliance serve time only to the
camera network, and does the update apply and roll back its configuration as one transaction? It
starts where the [live view beta test](live-view-beta-test.md) leaves the device: on generation 3,
signed in, with at least one camera configured and adopted.

The time design is [ADR 0035](../adr/0035-appliance-as-time-authority.md). The appliance consumes
time on the house network and serves it on the camera address only. Each camera is detected
individually: NTP first, a pushed ONVIF time second, and a camera that refuses is still adopted and
reported. A camera that cannot be reached or refuses the operation does not fail the adoption or
stop recording.

## Preconditions

Run the checks in order and do not work around a failed one.

- [ ] The device is on generation 3 and the pointer names 3. Do not rebuild the device from the
      generation 4 kit for this run; the update path is part of what is being tested.

```sh
sudo bittern update status
sudo cat /var/lib/bittern/app/current
curl -fsS http://127.0.0.1:8080/api/version
```

Expected: `committed: generation 3 (0.0.0-beta.3-<short>), channel beta` and
`running: generation 3 (0.0.0-beta.3-<short>)`; the pointer file contains `3`; the version endpoint
reports `baseline: false` and the generation 3 version. If the pointer contains `4`, the device is
not on the starting generation for this run.

- [ ] The maintainer is signed in at `https://<lan0-address>/ui/status` and the Updates card shows
      generation 3 as the installed version.

- [ ] At least one camera is configured and adopted, and its latest picture is visible on the
      Cameras page.

Two cameras are better: one is enough for the update and the appliance checks, but the per-camera
state table cannot be covered with one camera unless it changes class during the run. Record each
camera model, firmware, and address in the run record. If the bench has only one camera, record the
uncovered classes as not covered rather than inferring them.

- [ ] The camera network is connected and the camera is reachable on `cam0`.

```sh
ip -brief address show cam0
ip route get <camera-address>
```

Expected: `cam0` is up and carries the camera address, normally `10.77.0.1/24`, and the route to the
camera goes through `cam0`, not `lan0`. The picture on the Cameras page is the practical reachability
check; ICMP may be blocked by the camera.

- [ ] The management network has internet. The update check and download need GitHub; the Frigate
      image pull needs GHCR.

```sh
getent hosts github.com
getent hosts ghcr.io
```

Expected: both print an address. If either fails, fix the house network before starting. The update
cannot reach the release or the image, and the time card cannot reach an upstream source.

- [ ] The appliance can reach a time source. This is the same management link, but record the result
      before the update so a later failure is attributable.

```sh
chronyc tracking
chronyc sources
```

Expected: `Reference ID` names a source, `Leap status` is `Normal`, and `System time` is small. On a
device that has not synchronised yet, record `Leap status: Not synchronised` and the last error
instead of treating it as a failed precondition; the clock card check after the update names the
actual state.

- [ ] The install channel is beta.

```sh
sudo cat /etc/bittern/install-channel
```

Expected: `beta`.

- [ ] The generation 4 release is published and approved, and the Updates card offers update number
      4. A draft is not a release. Use the [beta bench rollout](beta-bench-rollout.md) for the
      publish and approval path.

- [ ] A phone or a second clock on the house LAN is available for the displayed-time comparison. It
      must be a clock the maintainer trusts, not the same appliance page.

Record the run as you go:

| Field | Value |
| --- | --- |
| Date and maintainer | |
| Bench box and management address | |
| Camera address on `cam0` | |
| Generation 3 version | |
| Generation 4 version and release URL | |
| Generation 4 release run URL | |
| Generation 4 publish run URL | |
| Update result and running image digest | |
| Appliance clock state, source, offset, zone | |
| Appliance time before and after reboot | |
| Camera model, firmware, mechanism, state | |
| Camera refused result and action taken | |
| Daylight saving boundary result | |
| NTP listener result on `cam0` and `lan0` | |
| Rollback or apply failure result, if any | |

## What generation 4 changes

| Area | Generation 3 | Generation 4 |
| --- | --- | --- |
| Update transaction | Files-only bundle; rendered configuration waited for a later apply | Candidate renderer, native checks, and changed live files applied inside the transaction before commit; the saved-settings migration is persisted after commit |
| Appliance time | `chrony` rendered without a selected server address or local reference | Client on `lan0`, server on the `cam0` address only, camera subnet only, `local stratum 10`, and a first-contact step bounded to the first three updates |
| NTP exposure | No NTP accept on the camera interface | One `udp dport 123` accept on `cam0`; none on `lan0` or the tailnet |
| Status | No clock section | Appliance clock card with state, source, stratum, offset, last check, zone, and local time |
| Camera list | No clock state | Per-camera clock badge, plain-language copy, next step, and mechanism in Technical details |
| Camera time | Adoption collected an RTSP URL only | ONVIF Device service detection, NTP configuration or pushed UTC time, a 15 minute verified check, backoff, and a cached derived mechanism |
| Camera sign-in | RTSP credential only | Optional separate ONVIF account under the same credential reference for time operations |
| Time zone | Stored but not rendered | Setup captures the browser zone, timestamps render with zone and offset, and the system and hardware clocks stay UTC |
| tzdata | Debian package via the `chrony` dependency | Installed explicitly; the application uses the system database with the embedded Go fallback |

The update transaction change is [ADR 0036](../adr/0036-update-bundles-deliver-subsystem-changes.md).
Generation 4 is the first beta bundle that runs the candidate renderer and the native `nft -c`,
`dnsmasq --test`, `chronyd -p`, and Frigate `--validate-config` checks inside the update. It writes
only the changed live targets, then starts the new agent and the recording service. This is why the
chrony and nftables changes reach a device on generation 3 without a manual camera apply.

The bundle carries no schema change. The update reports `migration: none`, and the saved
`appliance.yaml` keeps schema version 1. A device that does report a migration or `config-pending` is
outside this runbook; stop and collect the update state.

## Install generation 4

### Before

The Updates card shows the installed generation 3 version. Select Check for updates so the card is
current.

Expected: "Update available", "Version `0.0.0-beta.4-<short>` is available. The appliance is running
version `0.0.0-beta.3-<short>`.", update number 4, and an Install update button. The release is public
and the check is not rate limited.

If the card offers a different version, stop. The tag is computed from the published releases, and a
different generation means the channel or the device is not in the state this runbook expects.

### During

Select Install update.

Expected: "Installing update `0.0.0-beta.4-<short>`", the current step, "N of M steps completed", and
a two-second refresh. The configuration apply runs after "preparing the recording software" and
before "starting the appliance". The card labels it "updating the appliance" because the apply step
does not have its own label yet. The apply validates the staged files before it writes any live file,
then restarts the units whose files changed. `chrony` is one of them.

The step budget is 60 minutes. Leave the page open. Do not start a second install, do not reboot, and
do not run a camera apply during the update.

The update restarts `bitternd`. The card can show "Bittern is restarting. Reconnecting when it is
ready.", then the sign-in page. Sign in again. The session ending is expected and not a failed
update.

### If the configuration apply fails

A failure at the configuration apply is an update failure, not a partial success. The transaction
rolls back the bundle pointer, the generated configuration, and the units together, and restarts the
generation 3 agent and recording service. The box stays on generation 3.

Expected on the card: "Update failed", "The update did not finish", "Stopped while updating the
appliance", and "The update stopped while updating the appliance. The previous version is still
running." In Technical details, "Last update failing step" names `apply-config` and "Last update
reported reason" names the native check that failed, for example a `chronyd` or `dnsmasq` check. The
Updates card may offer a rollback to generation 2; do not use it. The failed update has already
restored generation 3.

Collect and confirm:

```sh
sudo bittern update status
sudo cat /var/lib/bittern/app/current
sudo journalctl -u bittern-update.service -n 100 --no-pager
```

Expected: `committed: generation 3` and `running: generation 3`; the pointer contains `3`; the last
operation result is `rolled-back`; the last apply is `rolled-back` with the internal failing apply
step and reason. Do not edit the chrony, nftables, or dnsmasq file by hand, and do not apply a camera
change to force the configuration through. Fix the named cause in the bundle, publish a new
generation, and run the update again. If the cause cannot be fixed in the bundle, stop and record the
failure.

### After

Expected: the card reads "Up to date", the installed version is `0.0.0-beta.4-<short>`, and the card
offers "Roll back to version `0.0.0-beta.3-<short>`". Collect:

```sh
sudo bittern update status
sudo cat /var/lib/bittern/app/current
curl -fsS http://127.0.0.1:8080/api/version
```

Expected: `committed: generation 4 (0.0.0-beta.4-<short>)` and
`running: generation 4 (0.0.0-beta.4-<short>)`; the pointer file contains `4`; the version endpoint
reports `baseline: false` and the generation 4 version. `bittern update status` also reports
`last apply: applied, configuration generation <n> (<m> files changed)` and `migration: none
(schema 1 to 1)`. The newest history record is `committed`.

A committed update with `last apply: not-run` is a failure of the apply-on-update path. The time
configuration may still be old. Do not run a camera apply to hide it; record it and stop.

Open Technical details and confirm the recording software digest is still the generation 3 digest.
Generation 4 does not change the Frigate pin.

## Appliance time checks

Open `https://<lan0-address>/ui/status`. The Clock card is below the network and storage cards.

### 1. The clock card is synchronised with a source and an offset

Expected on the card: the badge reads "Checked" and the sentence reads "The appliance clock is set
from the house network." The card shows "Appliance time" with a recent local time, the configured
"Time zone", and a recent "Last checked". Open Technical details and confirm:

| Field | Expected |
| --- | --- |
| Clock state | `ok` |
| Clock set from a source | `Yes` |
| Time source | An upstream address, not empty and not the local reference |
| Time source level | A stratum lower than 10, normally 1 to 4 |
| Clock offset | A value in milliseconds, normally a few milliseconds; an exactly zero offset may omit the row |
| Clock zone | The configured IANA zone, for example `Europe/Berlin` |
| Clock read error | Absent |

The same facts from the command line:

```sh
chronyc tracking
chronyc sources
```

Expected: `Leap status: Normal`, a selected source marked `^*` in `sources`, and a small `System
time`. `chronyc tracking` reports `System time` as the offset; the card rounds it to milliseconds.

A failure looks like the badge "Not checked" or "May be off", the sentence "The appliance has not
been able to check the time since it started", an empty Time source, a source level of 10, no Clock
offset row while `chronyc tracking` also has no update, a "Clock read error", or `chronyc` reporting
`Not synchronised`. A source level of 10 with state `never-synchronised` means the appliance is
serving its local reference, which keeps the cameras consistent but is not a checked clock.

### 2. The displayed time is correct, not an hour out

Compare the "Appliance time" on the card with the trusted phone or wall clock. They must agree to the
minute. Then compare the same instant rendered on the appliance in the configured zone:

```sh
TZ=Europe/Berlin date '+%d %b %Y, %H:%M %Z (UTC%:z)'
```

Substitute the zone shown on the card. Expected: the same minute and the same zone abbreviation and
offset as the card, for example `27 Nov 2026, 21:53 CET (UTC+01:00)`. The offset must be the one in
force now, not the standard offset.

A failure looks like a time exactly one hour or 30 minutes away from the trusted clock, a card that
shows a UTC time without saying UTC, a card that shows `CET` while `date` shows `CEST`, a missing
zone abbreviation or offset, or timestamps on the status, update, and camera pages that disagree
with each other. An hour out is usually a zone or daylight saving fault, not a wrong system clock;
check the zone before changing anything.

### 3. The clock is still correct after a reboot

Reboot the appliance and wait for it to return.

```sh
sudo reboot
```

Sign in again and repeat checks 1 and 2. Allow up to five minutes after boot for the first chrony
update; the appliance does not wait for a time source at boot.

Expected: the Clock card returns to "Checked", the source and offset are present, the displayed time
still matches the trusted clock and the configured zone, and the camera states are still checked or
are rechecked within one 15 minute interval. The NTP listener check below still passes.

A failure looks like the card staying "Not checked" after five minutes on a working house network, a
clock that is wrong after boot, the zone reverting to `UTC`, no NTP listener after boot, or every
camera falling to "Clock not set" and staying there after two check intervals. Record the boot time,
the card, and `chronyc tracking` before rebooting again.

## Camera time checks

Open `https://<lan0-address>/ui/cameras`. Each enabled camera shows a clock badge and one plain
sentence. The mechanism is in that camera's Technical details, not in the badge.

The first pass runs when `bitternd` starts after the update, so the first result should appear within
about 30 seconds for a reachable camera. The periodic check is every 15 minutes. A camera that was
just switched to NTP mode may report "Clock not set" until it polls; that is not a failure yet.

| Badge | State | What it means | What to do |
| --- | --- | --- | --- |
| Clock checked | `ok` | The camera clock agrees with the appliance within two seconds, or was just set and verified | Nothing |
| Clock not set | `never-synchronised` | No successful set or confirmation yet, including a camera just switched to NTP mode | Wait one 15 minute check. If it is still not set after two checks, treat it as a failure and collect the state |
| Clock off | `drifted` | The camera differs from the appliance by more than two seconds | Wait one check; Bittern writes the correction. If it is still off after two checks, check the camera's time settings and sign-in |
| Clock refused | `refused` | The camera rejected the sign-in or the time operation | Use the camera's own settings, as described below |
| Camera offline | `unreachable` | The camera did not answer the time request | Check the camera's power and the `cam0` link |

A camera that reports "Clock checked" must also show a recent "Clock checked" time in Technical
details. A stale check time with a current badge is a status bug; record it.

### How to tell which mechanism the camera chose

Open the camera's Technical details and read the "Clock" row. The mechanism follows the state:

| Technical details | Meaning |
| --- | --- |
| `Clock checked via NTP` | The camera advertises NTP, accepted the appliance as its server, and reports NTP mode. This is the preferred mechanism |
| `Clock checked via Pushed time` | The camera has no usable NTP capability. Bittern writes the UTC time and the offset in force, and re-pushes when the offset changes |
| `Clock checked via Time setting refused` | The camera accepted neither mechanism. The badge should be "Clock refused" |
| `Clock checked via Not detected` | Detection has not completed or the cached mechanism was cleared. Edit and save the camera without changing a value to wake a new pass, then check again |

"via NTP" and "via Pushed time" are the only successful mechanisms. A camera that accepts both is
reported as NTP, because NTP is preferred and is restored before the result is recorded. The
mechanism is cached with the camera health record, not written into `appliance.yaml`, and it is
re-detected after an edit, an address change, or a failure.

A camera in NTP mode keeps its own time zone. Bittern changes the source and the mode, not the
display zone. A camera in push mode is given the appliance's offset in force, so its local time
should equal the appliance's local time even if its own zone is different.

### A camera that refuses time operations

A refused camera is still adopted, still records, and still appears in the camera list. It is not a
failed update and not a reason to remove the camera.

1. Open the camera's own web interface on the camera network, or the native camera page if the
   support path provides one. Set the clock manually, or enable NTP and point it at the appliance's
   `cam0` address. Save the setting.
2. Check the account Bittern uses. Time writes need an administrator-class ONVIF account; a
   view-only or operator account is rejected. Most cameras use the same account for RTSP and ONVIF.
3. If the RTSP account is not an administrator, add the separate account in the camera form under
   "Camera time sign-in". It is stored under the same credential reference, not a second store.
4. Save the camera. This is a normal configuration apply and wakes a new time detection.
5. Record the model, firmware, mechanism, the exact refused sentence, and the action taken in the
   [camera matrix](../notes/camera-matrix.md). A refusal is a matrix row, not a reason to hide the
   camera.

Do not disable TLS verification, lower the ONVIF account to a weaker one, or retry in a loop. A
camera with a self-signed HTTPS certificate is reported as "Camera offline" rather than refused; that
is the separate TLS trust limit in the camera time note.

## Daylight saving

The appliance clock is UTC. The zone only changes what is displayed and what a push-mode camera is
told. Daylight saving comes from the IANA database, never from adding or subtracting an hour.

### The current offset

Check 2 above is the live test: the card's abbreviation and offset must match `TZ=<configured zone>
date` at the same minute. In winter, `Europe/Berlin` shows `CET (UTC+01:00)`; in summer it shows
`CEST (UTC+02:00)`. A zone with a 30 minute transition, such as `Australia/Lord_Howe`, must show the
30 minute shift (for example `+10:30` to `+11:00`), not a whole hour.

### The next boundary without waiting

Use a fixed UTC instant on each side of the zone's next transition. For `Europe/Berlin`, the spring
transition is at 01:00 UTC on 28 March 2027:

```sh
TZ=Europe/Berlin date -d '2027-03-28 00:30 UTC' '+%d %b %Y, %H:%M %Z (UTC%:z)'
TZ=Europe/Berlin date -d '2027-03-28 01:30 UTC' '+%d %b %Y, %H:%M %Z (UTC%:z)'
```

Expected: `28 Mar 2027, 01:30 CET (UTC+01:00)` and `28 Mar 2027, 03:30 CEST (UTC+02:00)`. The second
result skips 02:00 to 03:00, which is the transition. Repeat with the zone on the card; for a zone
without daylight saving, both lines show the same offset and the result is correct.

This proves the zone database used by the appliance. The card proves the live display path. If the
card and the command disagree at the current instant, the display path is wrong; the boundary result
does not excuse it.

### A zone on the other side of a boundary

If the bench has a second zone available on the other side of a daylight saving boundary, compare the
same UTC instant in both. For example, a configured `Europe/Berlin` in January is on standard time
while `Australia/Sydney` is on daylight time:

```sh
TZ=Europe/Berlin date -d '2027-01-15 12:00 UTC' '+%Z %:z %H:%M'
TZ=Australia/Sydney date -d '2027-01-15 12:00 UTC' '+%Z %:z %H:%M'
```

Expected: `CET +01:00 13:00` and `AEDT +11:00 23:00`. If a second appliance or a phone with a second
zone is available, check its card or clock against the same instant. Do not change the appliance's
configured zone just to create this case.

A wrong result looks like both zones showing the same offset, each zone showing the other's offset,
a whole-hour shift in a zone whose transition is 30 minutes, or a zone abbreviation that does not
match the offset. For a push-mode camera, compare the camera overlay with the appliance local time; an hour
difference at the current instant means the pushed offset is wrong. An NTP-mode camera keeps its own
zone, so a difference from the appliance is expected when the zones differ.

## The NTP serving check

The appliance must answer NTP on the camera address and nowhere else. This is the one command that
demonstrates the listener:

```sh
sudo ss -lunp 'sport = :123'
```

Expected: one line, bound to the camera address, for example:

```text
UNCONN 0 0 10.77.0.1:123 0.0.0.0:* users:(("chronyd",pid=...,fd=...))
```

The exact `pid` and `fd` vary. What matters is the local address. `10.77.0.1` is the default `cam0`
address; substitute the address from `ip -brief address show cam0` if this device uses another
camera subnet.

A failure looks like `0.0.0.0:123`, `[::]:123`, the management address instead of the camera address,
more than one listener, no listener at all, or a listener owned by a process other than `chronyd`.
`0.0.0.0:123` is the worst case: it answers the house LAN. Do not accept "it works from the camera"
as proof that the house LAN is closed; the bind address is the proof.

Supporting evidence, not a substitute for the listener:

```sh
sudo nft list ruleset | grep -F 'udp dport 123'
```

Expected: one rule with `iifname "cam0"` and no rule for `lan0` or the tailnet. If the listener is
correct but the firewall rule is missing, the camera still gets time and the house LAN is protected
only by the bind; record both facts.

### A camera pointed at the appliance

For a camera whose Technical details say "via NTP", open its own NTP page. The server should be the
appliance's `cam0` address, and the camera should show a recent successful synchronisation. On the
appliance:

```sh
sudo chronyc clients
```

Expected: after the camera has polled, its address appears in the client list. A camera can poll as
infrequently as every 60 minutes on some models, so an empty list immediately after the update is not
a failure. The camera's own "last synchronisation" time is the better immediate evidence.

For a camera whose mechanism is "Pushed time", there is no NTP client entry. Its clock is checked and
corrected through ONVIF, and "Clock checked via Pushed time" is the evidence. The listener check is
still required.

A failure looks like an NTP-mode camera that lists a public server or no server, a camera whose clock
still drifts after two 15 minute checks, a camera that reports NTP mode but never appears in
`chronyc clients` after its poll interval, or a camera whose local time is an hour from the appliance
in push mode.

### The house LAN must not get time

The listener command is the check. From a house LAN client, the appliance's `lan0` address must not
answer NTP and must not appear as a server. Do not open a firewall hole to make a LAN client work;
the camera network is the only intended client set.

A failure looks like the listener on `lan0`, a `0.0.0.0` bind, a LAN client receiving time, or an
`nftables` accept for `udp dport 123` without `iifname "cam0"`. Record the `ss` output and the
`nftables` rule before changing anything.

## If something fails

Stop at the failed check and collect the evidence before changing anything. Do not retry the update,
re-tag a generation, or edit the device state to make the card look clean.

Collect:

- The Clock card and its Technical details. Copy the badge, the sentence, the appliance time, the
  zone, the last checked time, the clock state, the source, the source level, the offset, and any
  read error. A screenshot is useful, but copy the text so it can be searched.
- The clock commands:

```sh
chronyc tracking
chronyc sources
```

- The chrony and agent logs:

```sh
sudo journalctl -u chrony -n 50 --no-pager
sudo journalctl -u bitternd -n 100 --no-pager
```

- The per-camera state. For each camera, record its name, address, model, firmware, clock badge,
  sentence, next step, and Technical details clock row. The same facts are in the camera health
  cache:

```sh
sudo cat /var/lib/bittern/camera-health.json
```

- The update state if the failure happened during or after the update:

```sh
sudo bittern update status
sudo journalctl -u bittern-update.service -n 100 --no-pager
```

- The run URLs: the generation 4 release run, the called publish run, and the release URL. The
  release URL is `https://github.com/jagoda/bittern/releases/tag/app-beta-000004`.

Never include `/etc/bittern/secrets.yaml`, a camera URL with credentials, the ONVIF password, the
administrator password, the setup token, or a support-bundle secret in a report. Read the daemon log
before sharing it. `camera-health.json` contains no credential, but it does contain camera addresses;
treat it as internal.

## What is unproven

| Unproven | Why it matters | Where it is tracked |
| --- | --- | --- |
| The camera capability matrix | No product camera has a tested row. The QEMU virtual camera has no ONVIF time endpoint, so the NTP, push, both, and neither paths are tested only against a Go fake endpoint | [OQ-41](../open-questions.md), [camera matrix](../notes/camera-matrix.md), [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability) |
| No real camera has run this code | Every camera result in this runbook is a first field result for generation 4. A passing fake endpoint is not evidence that a vendor accepts `SetNTP` or `SetSystemDateAndTime` | [camera time note](../notes/2026-11-27-camera-time-onvif.md) |
| Accepted clock skew | The window in which a camera accepts an authenticated ONVIF request timestamped from its own unauthenticated clock is unmeasured. The ten second Axis figure is a client library report, not vendor documentation | [OQ-42](../open-questions.md) |
| Fixed-offset zone handling | The push path sends a POSIX fixed-offset string and the offset in force. Whether a camera accepts that string, ignores the DST flag, and re-pushes correctly at a transition is unmeasured | [OQ-43](../open-questions.md) |
| Persistence across a camera reboot or factory reset | A camera may forget the NTP server, the mode, or the pushed time. Bittern rechecks every 15 minutes and re-detects after a failure, but survival across a camera reboot or factory reset has not been measured | This runbook, [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability) |
| DHCP option 42 acceptance | `dnsmasq` advertises the appliance, but no tested camera row proves that a model honours option 42 instead of a manual NTP address | [OQ-41](../open-questions.md) |
| Camera NTP poll behaviour and offline fallback | A camera in NTP mode may poll hourly and may keep its last time or drift when the appliance is unreachable. The vendor behaviour and the interval are unmeasured | This runbook, [camera time note](../notes/2026-11-27-camera-time-onvif.md) |
| ONVIF endpoint assumptions | The service is derived from the camera address on port 80 or 443 at `/onvif/device_service`. A camera on another port, another address, or with a self-signed certificate is reported as unreachable | [camera time note](../notes/2026-11-27-camera-time-onvif.md) |
| The tzdata bundle copy | Generation 4 uses the Debian `tzdata` package and the embedded Go fallback. The signed-bundle tzdata copy and the IANA release in the manifest are not in this generation | [ADR 0035](../adr/0035-appliance-as-time-authority.md), [appliance time note](../notes/2026-11-27-appliance-time.md) |
| A real daylight saving transition | The boundary check renders fixed instants through the zone database. The live behaviour of a push-mode camera across an actual transition, and the re-push within 15 minutes, are not observed here | [OQ-43](../open-questions.md) |
| Long-run stability | The checks are minutes long, not days. A camera that drifts, a source that flaps, and a long offline period are not measured | This runbook |

## What this runbook does not cover

- The publish, approval, and release verification path. Use the
  [beta bench rollout](beta-bench-rollout.md) for the dispatch, the `beta` environment gate, and the
  release checks.
- The OS image, A/B slots, bootloader, and hardware release gate.
- Frigate's own time zone, recording metadata, and database timestamps. The Frigate image is
  unmodified and its timestamps are not changed here.
- Multiple camera vendors, ONVIF profiles, PTZ, audio, playback, or events. This run is time only.
- A power cut during the update. The update engine tests cover the transaction steps; this run does
  not cut power.
- A camera except the one on the bench. Record what the bench camera does and leave the matrix row
  open until a second model is tested.
