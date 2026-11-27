# Appliance time: chrony serving, the zone, and the clock status

- Date: 2026-11-27
- Status: landed, appliance half of [ADR 0035](../adr/0035-appliance-as-time-authority.md). The camera
  half is not started.
- Related: [ADR 0035](../adr/0035-appliance-as-time-authority.md),
  [time sync recon](2026-11-26-time-sync-recon.md), [status contract](../status.md),
  [architecture](../architecture.md), [OQ-20](../open-questions.md),
  [the render suite](../../tests/render/README.md).

## What changed

The rendered chrony configuration now matches the decision:

| Directive | Reason |
| --- | --- |
| `bindacqdevice lan0` | Upstream queries leave on the house network only |
| `bindaddress <cam0 address>` | The NTP server socket exists on the camera address only |
| `allow <cam0 subnet>` | The camera subnet is the only client set |
| `local stratum 10` | A never-synchronised appliance still serves one clock to cameras |
| `makestep 1.0 3` | A large correction is stepped only in the first three updates |
| `rtcsync` | The RTC copies the UTC system clock; no code writes a local time |
| `port 0` when there is no camera address | The server is disabled instead of binding every address |

The nftables template keeps exactly one NTP accept, `iifname "cam0" udp dport 123 accept`, and
removes it when the camera role has no address. There is still no accept on `lan0` or `tailscale0`.
The installer now installs `tzdata` explicitly and disables `chrony-wait.service` and
`systemd-time-wait-sync.service`, so no boot unit waits for a source.

`identity.timezone` is now used. The setup review step captures the browser's
`Intl.DateTimeFormat().resolvedOptions().timeZone`, shows it, allows an override, and falls back to
`UTC` when JavaScript is off or the value is unreadable. The applier normalizes the value through the
IANA database and writes the existing field. There is no schema change and no migration. The system
and hardware clocks stay UTC; a zone only changes what is displayed.

Every browser-facing timestamp renders with its zone and offset, for example
`26 Nov 2026, 21:53 CET (UTC+01:00)`, and a UTC value is labelled once. Daylight saving comes from
`time.LoadLocation` (the Debian tzdata package, with the embedded Go database as a fallback), never
from arithmetic.

The status document gains a `time` section with `ok`, `never-synchronised`, `drifted`, and `unknown`.
It reports the last successful update, the selected source, the offset, the zone, and the local
time. The status page shows the primary copy and the next action from ADR 0035 section 7. A non-`ok`
clock does not change the overall state: an offline appliance is supported and still gives every
camera one consistent clock.

## What happens without a source

`chronyd` keeps running, does not correct the clock, and reports `Not synchronised`. The status
section is `never-synchronised` when there has never been an update and `drifted` when there was one
and it is now more than two hours old or the source was lost. Cameras still receive the local
reference at stratum 10, so they agree with each other and with the recording timeline. The owner is
told the clock is unchecked, not shown a false healthy state.

No boot path waits for synchronisation. First boot has no ordering against `chrony.service`, and the
wait units are disabled by the installer. A clock before 2020 still fails the update preflight, as
before.

## Tests

| Test | Assertion |
| --- | --- |
| `TestChronyServesCameraNetworkOnly` | Acquisition on `lan0`, server on the camera address, camera subnet only, local reference present |
| `TestChronyDisablesServerWithoutCameraRole` | `port 0`, no `bindaddress`, no `allow` |
| `TestNftablesAllowsNTPOnCameraInterfaceOnly` | One NTP accept on `cam0`; none on `lan0`, the tailnet, or an absent camera role |
| `TestGoldenCameraRoleAbsent` | The chrony and nftables goldens for a document with no camera address |
| `TestDaylightSavingComesFromIANA` | Fixed clock at both European transitions, asserting displayed time and offset |
| `TestHalfHourDaylightSavingComesFromIANA` | A 30 minute transition (Lord Howe) is resolved by the database |
| `TestNoCodeSetsLocalClock` | No `timedatectl`, `hwclock`, `date -s`, `settimeofday`, `clock_settime`, or `time.Local` in product code |
| `TestWizardCapturesBrowserTimezone` | The submitted zone reaches the apply request |
| `TestWizardTimezoneFallsBackToUTC` | No field, an empty field, and an unreadable value all apply as UTC |
| `TestAgentApplierTimezoneFallback` | The written `appliance.yaml` carries UTC for an unusable value and the valid name otherwise |
| `TestClockState` | `ok`, `never-synchronised`, and `drifted` from the observation |
| `TestTimeSectionForEveryState` | The status fields for every clock state |
| `TestStatusClockCardShowsPlainLanguage` | The primary copy and next action for every state |

## Verification

The QEMU `firstboot` scenario carries the `time` and `time-after-reboot` phases with the serving and
reboot assertions. On 2026-11-26 both passed: `10.77.0.1:123` was the only NTP listener, a request
from the camera address was answered and one from the house address was not, the only firewall NTP
accept was on `cam0`, the wait units were disabled, and the status time section was `ok` in the
`Europe/Berlin` zone. The evidence is
`build/qemu/logs/firstboot-20261126-182742/time-serving.txt`. The full scenario did not finish green
in that container: the live-view sign-in expectation was corrected to the current device-state
landing, and a later rerun reached the 15 minute image-prewarm deadline on a slow registry pull.
Neither is a time fault.

## Unfinished

- The camera half: authenticated ONVIF time reads and writes, the derived mechanism per camera, the
  15 minute check, and the camera time state in the status contract.
- The tzdata copy in the signed app bundle and the IANA release in the manifest. The current code
  uses the Debian package and the Go fallback; the bundle copy is a manifest v2 follow-up.
- The TLS certificate re-mint when the clock first becomes plausible is still the separate
  first-boot follow-up from the recon note.

Next concrete step: implement the camera time client on top of this serving model, starting with the
endpoint validation and the unauthenticated `GetSystemDateAndTime` bootstrap.
