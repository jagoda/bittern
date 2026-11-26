# Time synchronisation recon: appliance and camera clocks

- Date: 2026-11-26
- Status: wave 1 fact verification for [ADR 0035](../adr/0035-appliance-as-time-authority.md). No
  product code changed.
- Type: research and fact verification
- Related: [ADR 0005](../adr/0005-dual-nic-isolated-camera-network.md),
  [ADR 0016](../adr/0016-bittern-owns-frigate-config.md),
  [ADR 0024](../adr/0024-onvif-discovery-levels.md),
  [BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0),
  [BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability),
  [BIT-017](../plan/implementation-plan.md#bit-017-resolver-installation-and-first-boot-dns),
  [OQ-20](../open-questions.md), [camera matrix](camera-matrix.md),
  [Frigate assessment](../research/frigate-assessment.md), [status contract](../status.md),
  [architecture](../architecture.md)

Wave 1 verifies what the repository and the upstream standards actually support before the ADR fixes
the design. Every repository claim below carries a path and a line. Every upstream claim carries a
URL and the access date 2026-11-26. Items that could not be verified are listed as unverified.

## 1. The appliance's chrony configuration today

| Question | Answer | Evidence |
| --- | --- | --- |
| Which template renders it? | `templates/chrony/chrony.conf.tmpl`, rendered to `chrony/chrony.conf` and installed as `/etc/chrony/chrony.conf` with unit `chrony.service`. | `templates/chrony/chrony.conf.tmpl:1-12`, `agent/internal/render/render.go:123-133`, `agent/internal/apply/layout.go:286-287` |
| What sources does it declare? | `pool pool.ntp.org iburst`, a provisional renderer default because the schema has no time-source field. Client traffic leaves on `lan0`, the only default route. | `templates/chrony/chrony.conf.tmpl:6-8`, `docs/open-questions.md:96` (OQ-20), `docs/architecture.md:24`, `docs/architecture.md:80-85` |
| Does it step or slew at boot? | `makestep 1.0 3` steps when the correction is larger than 1.0 s, but only during the first three clock updates. Otherwise it slews. `rtcsync` copies the system clock to the RTC every 11 minutes on Linux. | `templates/chrony/chrony.conf.tmpl:3-4`; chrony 4.6 `makestep`, `maxslewrate`, `rtcsync` (accessed 2026-11-26) |
| Does it serve time? | Yes, only to the `cam0` subnet, bound in the template to the `cam0` address. The renderer passes the `cam0` address and masked subnet. There is no `local` directive, so a never-synchronised server is not a usable source for cameras. | `templates/chrony/chrony.conf.tmpl:10-12`, `agent/internal/render/render.go:127-128`; chrony 4.6 `local`, `allow`, `bindaddress` (accessed 2026-11-26) |
| What opens the port? | `udp dport 123` is accepted from `cam0` only. `lan0` has no 123 accept. The output chain is policy accept, so client requests to upstream servers leave and their replies return as established traffic. | `templates/nftables/bittern.nft.tmpl:7`, `:37-39`, `:48-50` |
| What advertises the server? | DHCP option 42 points cameras at the configured NTP server, which must sit inside `cam0`; the example uses `10.77.0.1`. | `templates/dnsmasq/dnsmasq.conf.tmpl:11`, `agent/internal/config/semantic.go:72-79`, `schema/examples/poc-gk41.yaml:35` |
| What does the installer enable? | `chrony` is in the package list and `chrony.service` is enabled with a verified `systemctl is-enabled` check. | `packaging/install.sh:18`, `:764-775` |
| Is first boot ordered after time sync? | No. First boot runs after local filesystems and before `bitternd`; it has no ordering against `chrony.service`. | `agent/packaging/bittern-first-boot.service:3-9` |
| How is the rendered file validated? | Apply runs `chronyd -p -f` on the staged file before install. The status builder does not run it and reports native checks as unknown. | `agent/internal/apply/apply.go:60`, `agent/internal/apply/system.go` (`chronyCheckFile`), `agent/internal/render/hooks_test.go:126-135`, `agent/internal/status/builder.go:328-332` |

The two facts that matter for the design: the appliance already claims to be the camera time server,
and it cannot be one on a device with no internet because the rendered config has no local reference.

## 2. Is the appliance timezone set, and to what?

- `identity.timezone` is required by the schema, is an IANA name, and defaults to `UTC`
  (`schema/appliance.schema.json:64-83`).
- First boot writes `UTC` unconditionally (`agent/internal/firstboot/config.go:153`).
- Nothing applies the value. A repository search for `localtime`, `timedatectl`, `tzdata` and
  `zoneinfo` returns no product code outside documentation, and no code loads a location.
- The browser surface formats every timestamp in UTC and appends the literal `UTC`
  (`agent/internal/ui/view.go:54-62`, `ui/templates/status_panel.html:7`, `:247`,
  `ui/templates/cameras.html:24`, `:71`, `ui/templates/operation_result.html:55-57`, `:118-120`).

So the appliance stores a timezone it never uses. The kernel clock and the RTC are UTC, which is
correct, but the field is dead configuration and the owner sees UTC everywhere.

Timezone data is present but not owned by Bittern today. Debian 13 carries tzdata 2026f and chrony
depends on tzdata, so the package is installed as a transitive dependency rather than as a stated
requirement. The rules still change: IANA 2026f records that Cuba will not fall back on 2026-11-01.
Go's `time/tzdata` package is used only when the system database is absent, so a chosen copy must be
loaded explicitly with `time.LoadLocationFromTZData`.

- <https://www.iana.org/time-zones>
- <https://packages.debian.org/trixie/tzdata>
- <https://packages.debian.org/trixie/chrony>
- <https://pkg.go.dev/time/tzdata>
- <https://pkg.go.dev/time#LoadLocationFromTZData>

All accessed 2026-11-26.

## 3. What depends on correct time

| Dependency | Where | Time-sensitive? |
| --- | --- | --- |
| UI TLS certificate | Minted at first boot with `NotBefore` five minutes before the clock and `NotAfter` ten years later (`agent/internal/firstboot/certificate.go:69-70`). It is reused while the identity serial and public key match, and the match does not check dates (`:102-116`). | Yes, and sharply. A first boot before synchronisation mints a window from the wrong clock, and the certificate is never re-minted for a clock change. A device that first booted at the epoch can keep an expired certificate for the life of its identity. Browsers validate it against their own clock. |
| Setup token | Single use, removed after setup, no expiry (`agent/internal/auth/auth.go:192-253`). | No. |
| Short setup code | Ten minute lifetime, compared against the appliance clock (`agent/internal/auth/auth.go:44-46`, `:282`). | Yes. A forward step invalidates it early, a backward step extends it. |
| Administrator session | Twelve hour absolute lifetime, in memory, compared against the appliance clock (`agent/internal/auth/auth.go:42-43`, `:331-340`, `:352-360`). | Yes, weakly. A forward step signs people out early; a backward step extends a session past twelve real hours. |
| Session cookie | `Expires` is absolute and `MaxAge` is relative (`agent/internal/auth/auth.go:392-399`). | No on the appliance side. The browser applies its own clock and prefers `MaxAge`. |
| ONVIF discovery | WS-Discovery, unauthenticated `GetDeviceInformation` and RTSP `OPTIONS` carry no timestamp (`agent/internal/discovery/discovery.go:1-8`, `layers.go:108-111`, `:235-250`). | No today. Authenticated ONVIF is different; see the bootstrap finding below. |
| mDNS | Bittern does not use it. Frigate's host networking needs it per the dated assessment (`docs/research/frigate-assessment.md:124`). | No. |
| Log ordering | journald stamps entries with the system clock. | Operationally yes. A backward step breaks order and makes the support bundle harder to read; no code depends on the sequence. |
| TLS to cameras | The XAddr validator accepts `https` and the client uses default verification, with no camera CA or pinning (`agent/internal/discovery/layers.go:211-232`). | No in the current code. A camera with a self-signed certificate would fail on trust, not on time. There is no supported camera TLS path. |
| TLS to GitHub during an update | The channel client uses an `http.Client` with system roots (`agent/internal/update/channel.go:53-60`). The update preflight also rejects a clock before 2020 (`agent/internal/update/steps.go:33-36`). | Yes, hard. No update check or download works without a plausible clock and a valid certificate window. |
| dnsmasq leases | Expiry is computed from the clock. | Yes, minor. A large step can expire or renew leases early. |
| Frigate recordings | Segment timestamps come from the recorder. | Yes for the archive. A step reorders clips and breaks cross-camera alignment. The camera clock affects the camera's own overlay and event log; whether Frigate uses any camera-supplied time in recording metadata is unverified (section 12). |

Surprises, in order of severity:

1. The UI certificate is minted before any time sync and is never re-minted for a clock change.
2. Upgrade TLS and the update preflight are the only hard time gates in the product, and they fail
   closed, so an offline appliance cannot update itself once its clock is wrong enough.
3. The camera-side services have no time gate at all, which is why a wrong camera clock can persist
   forever without anyone noticing.

## 4. Does the status contract expose time state?

No. The document has `config`, `render`, `frigate`, `cameras`, `storage`, `network`, `updates`,
`reset`, and optionally `hardware` (`docs/status.md:81-96`, `agent/internal/status/status.go:61-71`).
There is no clock, chrony or time section. The status builder explicitly does not run the native
checks, including chrony (`agent/internal/status/builder.go:328-332`). The only time data is document
metadata: `generated_at`, operation timestamps, and the camera health timestamps, all formatted UTC.
The `network` section reports address, carrier and last error only (`docs/status.md:93`).

## 5. The camera network from the appliance

- `cam0` is static `10.77.0.1/24`, gatewayless, IPv6 disabled, with
  `ConfigureWithoutCarrier=yes` so `dnsmasq` and `chrony` can bind before a camera is present
  (`templates/networkd/cam0.network.tmpl:1-13`, ADR 0005).
- Cameras are `10.77.0.0/24`. `dnsmasq` serves DHCP only: pool `.50` to `.199`, reservations `.10`
  to `.49`, gateway `10.77.0.1`, option 42 the same address, and no DNS
  (`templates/dnsmasq/dnsmasq.conf.tmpl:1-15`).
- The input chain drops by default, accepts `udp 67` and `udp 123` from `cam0`, and accepts
  established and related traffic. The forward chain drops and there is no NAT
  (`templates/nftables/bittern.nft.tmpl:1-50`).
- A camera reaches the appliance at `10.77.0.1`. The address cameras see is that same address, from
  the DHCP gateway option and option 42. Nothing has to be opened in the firewall for a camera to use
  the appliance as a time server today; the rule already exists on `cam0` and does not exist on
  `lan0`.
- The appliance reaches camera ONVIF and HTTP ports from the output chain, which is policy accept.
  No input rule is involved, and the replies return as established traffic.

## 6. What exists for ONVIF today

- Discovery is layered and read only: WS-Discovery on `cam0`, the dnsmasq lease, the ARP table, an
  unauthenticated ONVIF `GetDeviceInformation` when WS-Discovery returns an endpoint, and an RTSP
  `OPTIONS` probe (`agent/internal/discovery/discovery.go:1-8`, `layers.go:108-181`, `:235-260`).
- There is no ONVIF Media, PTZ, Events or Imaging client, no authenticated ONVIF call, and no ONVIF
  credential. Discovery never sends credentials (ADR 0024:114).
- Adoption collects the display name, address, RTSP port, RTSP path, optional hardware address and
  the RTSP username and password (`agent/internal/ui/wizard.go:510-518`, `:985-1034`). The credential
  becomes the RTSP URL under `cameras.<id>.rtsp_url`.
- The camera schema has no ONVIF field: `id`, `name`, `address`, `enabled`, `rtsp_port`, `rtsp_path`,
  `mac`, `vendor`, `model`, `credentials_ref` (`schema/appliance.schema.json:195-260`).
- Adoption therefore collects nothing ONVIF beyond the display vendor and model and the hardware
  address. There is no ONVIF endpoint, account or time setting to reuse.
- BIT-008 is partial: the adoption note says the WS-Discovery and unauthenticated Device probe "are
  not a claim of full ONVIF support" (`docs/notes/2026-11-15-camera-adoption.md`). BIT-027 owns
  profiles, PTZ and field interoperability.

## 7. Upstream ONVIF time operations

All ONVIF sources were accessed on 2026-11-26. The current Core specification is version 26.06. The
Device service WSDL is `devicemgmt.wsdl`; the schema is `onvif.xsd`.

| Operation | Service | Request | Access class | Conditionality | Source |
| --- | --- | --- | --- | --- | --- |
| `GetSystemDateAndTime` | Device | Empty. Returns `DateTimeType` (`Manual` or `NTP`), `DaylightSavings`, optional `TimeZone` in POSIX form, optional `UTCDateTime`, `LocalDateTime`. A device must provide `UTCDateTime`. | `PRE_AUTH` | Required by the Device service. | Core 26.06 8.3.6; WSDL `GetSystemDateAndTime`; `onvif.xsd` `SystemDateTime` |
| `SetSystemDateAndTime` | Device | `DateTimeType`, `DaylightSavings`, optional `TimeZone` (POSIX), optional `UTCDateTime` (no meaning in NTP mode). | `WRITE_SYSTEM` | The operation is required; manual and NTP modes are conditional on device support. The Device service capabilities define no flag for time setting. | Core 26.06 8.3.7; Core 19.12 Table 45; WSDL element |
| `GetNTP` | Device | Empty. Returns `FromDHCP`, `NTPFromDHCP[]`, `NTPManual[]`. | `READ_SYSTEM` | Conditional on NTP support. | Core 26.06 8.2.6; Core 19.12 Table 19; WSDL |
| `SetNTP` | Device | `FromDHCP`, `NTPManual[]` of `NetworkHost`. Accepts RFC 1123 or RFC 952 host strings. Does not change `DateTimeType`; `SetSystemDateAndTime` activates NTP. | `WRITE_SYSTEM` | Conditional on the `Network.NTP` capability (an integer: the maximum number of NTP servers). | Core 26.06 8.2.7; Core 19.12 Table 20; WSDL; Profile Q Device Test Specification 21.06, step 5 |

The time operations are Device service operations in the Core specification, not profile features. A
camera that claims a streaming profile does not thereby promise that it can be told the time. The NTP
capability is the only capability flag that bears on time, and it covers `SetNTP`/`GetNTP` only.

Sources:

- ONVIF Core Specification 26.06: <https://www.onvif.org/specs/core/ONVIF-Core-Specification.pdf>
- ONVIF Core Specification 19.12, used for the explicit access class tables:
  <https://www.onvif.org/specs/core/ONVIF-Core-Specification-v1912.pdf>
- Device service WSDL: <https://www.onvif.org/ver10/device/wsdl/devicemgmt.wsdl>
- ONVIF schema: <https://www.onvif.org/ver10/schema/onvif.xsd>
- Profile Q Device Test Specification 21.06:
  <https://www.onvif.org/wp-content/uploads/2021/06/ONVIF_Profile_Q_Device_Test_Specification_21.06.pdf>

## 8. What a camera does in NTP mode when the server is unreachable

ONVIF defines the configuration operation, not the failure behaviour. `SetNTP` sets the server list;
the specification does not say what the camera does while the server does not answer. Vendor
documentation therefore decides, and the sources found do not describe an automatic fallback:

- Hikvision's network camera manual says that if the camera fails to synchronise its time with the
  network, the time must be set manually. Hikvision's NTP and DST note documents a configurable NTP
  interval from 1 to 10080 minutes with 60 minutes as the default for its devices.
  - <https://www.hikvision.com/content/dam/hikvision/vn/product-datasheet/UD15501B-C_Baseline_User-Manual-of-Network-Camera_V5.5.90_20221223.pdf>
  - <https://www.hikvision.com/content/dam/hikvision/en/support/download/how-to/nvr/How%20to%20configure%20NTP%20and%20DST.pdf>
- Axis documents NTP with one or two servers and fallback servers, and a separate manual or custom
  time setting, but no automatic fallback statement was found:
  <https://help.axis.com/download/um_p1378_le_t10135686_2505.pdf>

The safe conclusion: do not assume a camera in NTP mode recovers by itself. It keeps running on its
own oscillator, and the only reliable check is to read `GetSystemDateAndTime` and compare it with the
appliance.

## 9. Does ONVIF time need authentication, and at what level?

- The read is `PRE_AUTH`: `GetSystemDateAndTime` must answer before authentication, because a client
  needs the device time to build a WS-Security `UsernameToken` timestamp.
- The writes are `WRITE_SYSTEM`: `SetSystemDateAndTime` and `SetNTP`. `GetNTP` is `READ_SYSTEM`.
- The Core default access policy grants `WRITE_SYSTEM` to the Administrator level only (Core 26.06
  Table 7); `Operator`, `User` and anonymous accounts do not get it.
- The ONVIF Profile Q device test specification, section 5.2.4, verifies that operations in the
  `UNRECOVERABLE`, `WRITE_SYSTEM` and `READ_SYSTEM_SECRET` access classes cannot be accessed below
  the Administrator level, and its test steps invoke `SetSystemDateAndTime` with a `User` level
  account and expect HTTP 401 Unauthorized. An administrator-class ONVIF account is therefore
  normally required for time changes.
- Vendor practice agrees: Network Optix tells operators to confirm the account has ONVIF
  administrator privileges and to synchronise clocks because ONVIF requests are timestamped
  (<https://support.networkoptix.com/hc/en-us/articles/216517857-Troubleshoot-ONVIF-camera-issues>).
- Axis cameras have replay attack protection on by default and reject ONVIF requests whose timestamp
  is more than about ten seconds from the camera clock. The figure comes from a client library issue
  report, not from Axis documentation, so treat the exact window as unverified per model:
  <https://github.com/agsh/onvif/issues/172>

The bootstrap problem follows. A camera whose clock is hours off will reject a normal authenticated
request as stale. `GetSystemDateAndTime` answers without authentication, so the first authenticated
write must be timestamped from the camera's own clock, not the appliance's. The exact accepted window
is unverified per camera.

## 10. Chrony behaviour worth relying on

All chrony sources were accessed on 2026-11-26. Debian 13 ships chrony 4.6.1-3+deb13u2
(<https://packages.debian.org/trixie/chrony>), and the 4.6 manual pages are used below.

| Question | Answer | Source |
| --- | --- | --- |
| What happens with no reachable source? | `chronyd` keeps running. `tracking` reports `Leap status: Not synchronised`, and `sources` marks the source `?` (not selectable, for example unreachable, not synchronised, or not enough measurements). The clock is not corrected and drifts at the rate recorded in the drift file. | chronyc 4.6 `tracking`, `sources` |
| Can the clock be stepped after start? | Automatic stepping is limited to the first `limit` clock updates in `makestep` (`1.0 3` means the first three updates). After that, corrections are slews, at most `maxslewrate`, whose default is 83333.333 ppm. `chronyc makestep` can force a step at any time, and the manual warns that jumps seriously affect some software. | chrony.conf 4.6 `makestep`, `maxslewrate`; chronyc 4.6 `makestep` |
| What does it report as a source state? | `sources` symbols: `*` best, `+` combined, `-` selectable but not selected, `x` falseticker, `~` too much variability, `?` not selectable. `tracking` reports reference ID, stratum, last offset, RMS offset, skew, update interval and leap status (`Normal`, `Insert second`, `Delete second`, `Not synchronised`). | chronyc 4.6 `sources`, `tracking` |
| Can it serve time when it has never synchronised? | Only with the `local` directive, which makes the server appear synchronised to clients even when it never was, for exactly the isolated-network case. The default local stratum is 10 and the default distance is 1 second. Without `local`, a never-synchronised server is not a usable source. | chrony.conf 4.6 `local` |
| Which sockets open? | The server port opens only when an address is allowed by `allow`, or a peer or broadcast mode is configured. `bindaddress` binds the listening socket to one local address per protocol; `port` defaults to 123. | chrony.conf 4.6 `allow`, `bindaddress`, `port` |

Sources:

- <https://chrony-project.org/doc/4.6/chrony.conf.html>
- <https://chrony-project.org/doc/4.6/chronyc.html>

## 11. Findings that change the design

1. First boot can mint an expired TLS certificate, and nothing re-mints it when the clock is fixed
   (`agent/internal/firstboot/certificate.go:69-70`, `:102-116`, and the unit has no ordering after
   `chrony.service`). This is a separate defect to fix, not just a time-sync detail.
2. The appliance already advertises itself as the camera time server but cannot serve time when it
   has never synchronised, because the render has no `local`. An offline appliance leaves every
   camera to drift on its own clock while option 42 points at a server that will not answer.
3. `makestep 1.0 3` counts updates, not wall time. An appliance that boots offline and finds a source
   a week later can still step its clock a week after boot, which can expire sessions and jump the
   recording timeline.
4. The timezone field has been required since schema v1 and is always written as UTC. No code applies
   it, and every surface prints UTC.
5. Update TLS is a hard time gate; camera TLS is effectively absent. The clock matters most for the
   update path, not for cameras.
6. Adoption has no ONVIF credential, so camera time sync needs a credential path before it can
   authenticate. Reusing the existing `credentials_ref` is the smallest option; a separate ONVIF
   account needs a second key under the same reference.
7. A camera that is hours off rejects authenticated ONVIF requests, so the client must timestamp the
   first write from the camera's unauthenticated clock read. This is part of the design, not a retry.

## 12. Unverified

- Which cameras support `SetNTP`, `SetSystemDateAndTime`, both, or neither. The camera matrix has no
  tested product row yet (`docs/notes/camera-matrix.md`).
- Whether cameras honour DHCP option 42, and whether they use it automatically after being switched
  to NTP mode.
- The accepted WS-Security clock skew per camera model. The ten second Axis figure is from a client
  library issue, not vendor documentation.
- Whether cameras accept a POSIX timezone string that carries a fixed offset and no DST rule, and
  whether they apply DST from the `TimeZone` string when `DaylightSavings` is true.
- Whether a camera in NTP mode applies its own DST correctly when its zone is right but its DST
  setting is off.
- Whether an `https` ONVIF endpoint with a camera self-signed certificate can work at all with the
  current verifier. The code accepts the scheme but has no camera CA or pinning.
- Whether Frigate uses any camera-supplied timestamp for recording metadata, or always the recorder
  clock. The Frigate facts are dated 2026-11-14 and were not re-verified in this wave.
- Whether the camera keeps its NTP configuration across a reboot, a firmware update, or a factory
  reset. The periodic re-assert is designed to cover this, but no camera has been tested.

## 13. Next step

[ADR 0035](../adr/0035-appliance-as-time-authority.md) fixes the design: the appliance is the time
authority for the camera network, with NTP preferred and an ONVIF time push as the fallback, detected
per camera and re-checked on a bounded interval. Wave 2 implements the renderer, the status time
section, the ONVIF time client with four camera fixtures, the bundle tzdata and the local-time UI.
The required tests are listed in the ADR consequences.
