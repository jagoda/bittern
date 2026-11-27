# ONVIF compatibility assessment

Assessed 2026-11-27. Re-verify before relying on a version, namespace, operation, port or vendor
behaviour. This document records the specification and vendor evidence behind
[the compatibility strategy](../notes/2026-11-27-onvif-compatibility-strategy.md) and
[the compatibility matrix](../engineering/onvif-compatibility.md). It is research, not an
implementation decision.

Related: [ADR 0035](../adr/0035-appliance-as-time-authority.md),
[ADR 0024](../adr/0024-onvif-discovery-levels.md),
[BIT-027](../plan/implementation-plan.md#bit-027-robust-onvif-profiles-ptz-and-field-interoperability),
[the camera time note](../notes/2026-11-27-camera-time-onvif.md),
[the camera matrix](../notes/camera-matrix.md), [field parity](../engineering/field-parity.md).

## Scope and method

The questions are the ones BIT-027 must answer before it can claim multi-vendor interoperability:
which media service a camera speaks, how a client reads profiles and stream URIs, how it discovers
services, how it authenticates, how time operations differ, and how PTZ support is advertised.

"Verified" means the claim is quoted or directly derived from an ONVIF specification, an OASIS
specification, an IETF RFC, or the vendor's own document. "Vendor claim" means the vendor documents
its own product. "Unverified" means a field report, a client-library issue, or folklore. A claim
without an `S` reference is the author's synthesis and is labelled as such.

All sources were accessed on 2026-11-27 unless the row says otherwise. Every `S` reference resolves
to a URL and access date in the source table below.

## Sources

| ID | Source | Version or date | URL | Accessed |
| --- | --- | --- | --- | --- |
| S1 | ONVIF Core Specification | 26.06, June 2026 | <https://www.onvif.org/specs/core/ONVIF-Core-Specification.pdf> | 2026-11-27 |
| S2 | ONVIF Media2 Service Specification | 26.06, June 2026 | <https://www.onvif.org/specs/srv/media/ONVIF-Media2-Service-Spec.pdf> | 2026-11-27 |
| S3 | ONVIF Media Service Specification | 24.12, December 2024 | <https://www.onvif.org/specs/srv/media/ONVIF-Media-Service-Spec.pdf> | 2026-11-27 |
| S4 | ONVIF PTZ Service Specification | 26.06, June 2026 | <https://www.onvif.org/specs/srv/ptz/ONVIF-PTZ-Service-Spec.pdf> | 2026-11-27 |
| S5 | ONVIF Device service WSDL | 26.06 | <https://www.onvif.org/ver10/device/wsdl/devicemgmt.wsdl> | 2026-11-27 |
| S6 | ONVIF Media service WSDL | schema 21.06 | <https://www.onvif.org/ver10/media/wsdl/media.wsdl> | 2026-11-27 |
| S7 | ONVIF Media2 service WSDL | 26.06 | <https://www.onvif.org/ver20/media/wsdl/media.wsdl> | 2026-11-27 |
| S8 | ONVIF PTZ service WSDL | 26.06 | <https://www.onvif.org/ver20/ptz/wsdl/ptz.wsdl> | 2026-11-27 |
| S9 | ONVIF schema | 25.12 | <https://www.onvif.org/ver10/schema/onvif.xsd> | 2026-11-27 |
| S10 | ONVIF common schema | 25.12 | <https://www.onvif.org/ver10/schema/common.xsd> | 2026-11-27 |
| S11 | ONVIF Profile S Specification | 1.3, November 2019 | <https://www.onvif.org/wp-content/uploads/2019/12/ONVIF_Profile_-S_Specification_v1-3.pdf> | 2026-11-27 |
| S12 | ONVIF Profile T Specification | 1.0, 2018 | <https://www.onvif.org/wp-content/uploads/2018/09/ONVIF_Profile_T_Specification_v1-0.pdf> | 2026-11-27 |
| S13 | ONVIF Application Programmer's Guide | 1.0, 2016 | <https://www.onvif.org/wp-content/uploads/2016/12/ONVIF_WG-APG-Application_Programmers_Guide-1.pdf> | 2026-11-27 |
| S14 | OASIS WS-Security UsernameToken Profile | 1.1.1 | <https://docs.oasis-open.org/wss-m/wss/v1.1.1/os/wss-UsernameTokenProfile-v1.1.1-os.html> | 2026-11-27 |
| S15 | RFC 7616, HTTP Digest Access Authentication | June 2015 | <https://www.rfc-editor.org/rfc/rfc7616.html> | 2026-11-27 |
| S16 | RFC 9110, HTTP Semantics | June 2022 | <https://www.rfc-editor.org/rfc/rfc9110.html> | 2026-11-27 |
| S17 | Reolink, Introduction to ONVIF Protocol | vendor support article | <https://support.reolink.com/articles/360008718893-Introduction-to-ONVIF-Protocol> | 2026-11-27 |
| S18 | Hikvision, How to Enable ONVIF Protocol on IP Camera | v1.0, 2019-07-03 | <https://www.hikvision.com/content/dam/hikvision/en/support/how-to/how-to-document/network-cameras/How-to-Enable-ONVIF-Protocol-on-IP-Camera.pdf> | 2026-11-27 |
| S19 | Hikvision, How to Add IPC with ONVIF Protocol using HIKVISION NVR | vendor how-to | <https://www.hikvision.com/content/dam/hikvision/en/support/download/how-to/nvr/How%20to%20Add%20IPC%20with%20ONVIF%20Protocol%20using%20HIKVISION%20NVR.pdf> | 2026-11-27 |
| S20 | Dahua, Network Video Server User's Manual | V1.1.1, 2018 | <https://www.dahuasecurity.com/asset/upload/uploads/soft/20181107/Dahua-Network-Video-Server-Users-Manual-V1.1.1.pdf> | 2026-11-27 |
| S21 | ONVIF, Profile S end of support announcement | 2025-10-09 | <https://www.onvif.org/pressrelease/onvif-to-end-support-for-profile-s> | 2026-11-27 |
| S22 | ONVIF, specifications index | current page | <https://www.onvif.org/profiles/specifications/> | 2026-11-27 |
| S23 | Datastead, ONVIF port FAQ | third-party vendor FAQ | <https://www.datastead.com/faq/what-port-should-i-use-for-onvif> | 2026-11-27 |
| S24 | Network Optix, Troubleshoot ONVIF camera issues | vendor support article | <https://support.networkoptix.com/hc/en-us/articles/216517857-Troubleshoot-ONVIF-camera-issues> | 2026-11-27 |
| S25 | ONVIF client issue on Axis replay protection | issue report | <https://github.com/agsh/onvif/issues/172> | 2026-11-27 |
| S26 | Hikvision Network Camera User Manual | V5.5.90, 2022-12-23 | <https://www.hikvision.com/content/dam/hikvision/vn/product-datasheet/UD15501B-C_Baseline_User-Manual-of-Network-Camera_V5.5.90_20221223.pdf> | 2026-11-27 |

[S22] confirms that the Media, Media2, PTZ and Core documents above are the current published files
on the official specifications index. S21 is the current profile status announcement. S11 and S12 are
older than the service specifications and are used only for the profile function lists they state.

## 1. Media services

### 1.1 What each service exposes

Media1 and Media2 are separate services with separate namespaces, WSDLs, profile types and
operation shapes. A camera may implement one, the other, or both. [S2] Section 1 calls Media2 "the
second generation web service interface for configuration of the so called media profiles"; [S3]
Section 4.1 describes the Media1 profile model.

| Item | Media1 | Media2 | Evidence |
| --- | --- | --- | --- |
| Service namespace | `http://www.onvif.org/ver10/media/wsdl` | `http://www.onvif.org/ver20/media/wsdl` | [S3] Table 1; [S2] Table 1; [S6], [S7] `targetNamespace` |
| WSDL | `/ver10/media/wsdl/media.wsdl` | `/ver20/media/wsdl/media.wsdl` | [S6], [S7] |
| List profiles | `GetProfiles`, empty request, returns `tt:Profile` list | `GetProfiles` with optional `Token` and `Type` list, returns `tr2:MediaProfile` list | [S3] 5.2.2; [S2] 5.1.2 |
| Profile content | `tt:Profile` contains optional `VideoSourceConfiguration`, `VideoEncoderConfiguration`, `PTZConfiguration` and other configurations inline | `tr2:MediaProfile` contains `Name` and a `Configurations` set of actual configuration elements; without `Type` it carries no configuration information | [S9] `Profile`; [S7] `MediaProfile`, `ConfigurationSet`; [S2] 5.1.2 |
| Get one profile | `GetProfile` | `GetProfiles` with one `Token` | [S3] 5.2.3; [S2] 5.1.2 |
| Get encoder configuration | Embedded in the profile, or `GetVideoEncoderConfigurations` | Embedded when `Type` includes `VideoEncoder`, or `GetVideoEncoderConfigurations` with a profile token | [S3] 5.5.1; [S2] 5.3.2; [S7] `GetVideoEncoderConfigurations` |
| Stream URI | `GetStreamUri` with `StreamSetup` (`StreamType` RTP-Unicast or RTP-Multicast; `Transport/Protocol` UDP, RTSP or HTTP) | `GetStreamUri` with `Protocol` string (`RtspUnicast`, `RtspMulticast`, `RTSP`, `RtspsUnicast`, `RtspsMulticast`, `RtspOverHttp`) | [S3] 5.15.1; [S2] 5.4.1; [S9] `StreamType`, `TransportProtocol` |
| Stream URI result | `MediaUri` with `Uri`, `InvalidAfterConnect`, `InvalidAfterReboot`, `Timeout` | `Uri` | [S3] 5.15.1; [S2] 5.4.1 |
| Snapshot URI | `GetSnapshotUri`, returns `MediaUri`; image is always JPEG | `GetSnapshotUri`, returns a URI; image is always JPEG | [S3] 5.16.1; [S2] 5.5.1 |
| Service capabilities | `GetServiceCapabilities`, `trt:Capabilities` | `GetServiceCapabilities`, `tr2:Capabilities2` | [S3] 5.21; [S2] 5.14 |
| Access class, profiles and streams | `GetProfiles`, `GetStreamUri`, `GetSnapshotUri` are `READ_MEDIA` | Same | [S3] 5.2.2, 5.15.1, 5.16.1; [S2] 5.1.2, 5.4.1, 5.5.1 |
| Access class, service capabilities | `PRE_AUTH` | `PRE_AUTH` | [S3] 5.21; [S2] 5.14 |

The service namespaces used by the rest of this document are:

| Service | Namespace | WSDL | Evidence |
| --- | --- | --- | --- |
| Device | `http://www.onvif.org/ver10/device/wsdl` | `/ver10/device/wsdl/devicemgmt.wsdl` | [S5] `targetNamespace` |
| Media1 | `http://www.onvif.org/ver10/media/wsdl` | `/ver10/media/wsdl/media.wsdl` | [S3] Table 1; [S6] `targetNamespace` |
| Media2 | `http://www.onvif.org/ver20/media/wsdl` | `/ver20/media/wsdl/media.wsdl` | [S2] Table 1; [S7] `targetNamespace` |
| PTZ | `http://www.onvif.org/ver20/ptz/wsdl` | `/ver20/ptz/wsdl/ptz.wsdl` | [S4] Table 1; [S8] `targetNamespace` |

`READ_MEDIA` is not anonymous under the default access policy. [S1] Table 7 grants `PRE_AUTH` to
the Anonymous column and does not grant `READ_MEDIA` to it. A conformant camera may therefore reject
`GetProfiles` and `GetStreamUri` without credentials. A camera that permits them anonymously is
more permissive than the default, and a client must not depend on that.

Media1 uses the `tt:VideoEncoding` enumeration, whose values are `JPEG`, `MPEG4` and `H264`. [S9]
`VideoEncoding`. Media2 uses an IANA media subtype string in `VideoEncoder2Configuration/Encoding`,
which allows `H265` as well as `H264`. [S9] `VideoEncoder2Configuration`; [S7] `VideoEncoder2Configuration`.
This is a real capability difference, not just a shape difference.

### 1.2 How a client detects which service is available

1. `GetServices` returns the device's services. Each `tds:Service` carries a `tds:Namespace`, a
   `tds:XAddr`, a `tds:Version` and optional capabilities. A Media2 service is present when the
   namespace `http://www.onvif.org/ver20/media/wsdl` appears; Media1 is
   `http://www.onvif.org/ver10/media/wsdl`. [S1] 8.1.2.2; [S2] Table 1; [S3] Table 1.
2. `GetServices` is `PRE_AUTH`, so it can be read before authentication. [S1] 8.1.2.2.
3. A device must implement `GetServices` if any of its services implements
   `GetServiceCapabilities`. Media2 does implement `GetServiceCapabilities`, so a Media2-capable
   device has a standards-based way to announce itself. [S1] 8.1.2.2; [S2] 5.14.
4. `GetCapabilities` is the older, backward-compatible interface. It returns a `Media` element with
   an `XAddr`. It does not list a `Media2` element: the Core 26.06 capability list names
   `Media XAddr` but no Media2 address. [S1] 8.1.2.4, Annex A; [S9] `Capabilities`.
5. Therefore a client that only reads `GetCapabilities` can discover Media1 but not Media2. A client
   that reads `GetServices` can discover both. If `GetServices` is absent or malformed, the client
   can use the `GetCapabilities/Media/XAddr` endpoint and try Media1 operations; a Media2-only camera
   that omits `GetServices` is non-conformant, and the client must not assume Media2.
6. `GetServices` requires the returned `XAddr` to match the scheme and IP part of the request. [S1]
   8.1.2.2. A client must still validate the address against the camera segment before connecting.

### 1.3 When a camera advertises both

The specifications do not define precedence between Media1 and Media2, and they do not state that
the two services share profile tokens, profile names, encoder configurations or stream URIs. They
are separate services with separate WSDLs. A client cannot assume that a Media1 token exists in
Media2 or that the same token means the same stream.

The strategy proposed in the note is:

- Choose one service as authoritative for the camera. Prefer Media2 when it is advertised, because
  it is the service Profile T requires, it exposes typed configuration sets and H.265, and ONVIF is
  ending Profile S support in favour of Profile T. [S12] 7.2.3, 7.9.3; [S21].
- If Media2 answers but cannot produce a usable profile and stream URI, fall back to Media1 once and
  record the fallback. Do not combine a Media2 profile token with a Media1 `GetStreamUri`, or the
  reverse.
- If both work, keep the Media2 result and record the Media1 presence as evidence. If Media2 is
  absent, use Media1, which remains a current service specification even though Profile S is being
  retired. [S3]; [S21].
- Never infer "main" and "sub" from the service version or the token. Classify by the encoder
  attributes in Section 2.

The specification is silent on the equivalence question. The one-service rule is a client safety
rule, not a quotation.

### 1.4 Media2 traps

- **No `Type` means no configuration information.** [S2] 5.1.2: "If no Type is provided the
  returned profiles shall contain no configuration information." A client that calls `GetProfiles`
  with no `Type` gets profile shells and cannot classify them. The client must send `Type=All`, or
  the specific list of configuration types it needs.
- **`Type=All` returns the associated configurations.** [S2] 5.1.2: "If a single Type with value
  'All' is provided the returned profiles shall include all associated configurations." If a camera
  ignores this or returns an incomplete set, the client can call `GetVideoEncoderConfigurations`
  and `GetVideoSourceConfigurations` with the profile token. [S2] 5.3.2.
- **The `Type` values are an enumeration.** [S7] `ConfigurationEnumeration` defines `All`,
  `VideoSource`, `VideoEncoder`, `AudioSource`, `AudioEncoder`, `AudioOutput`, `AudioDecoder`,
  `Metadata`, `Analytics`, `PTZ` and `Receiver`. A vendor may add namespaced values. Request the
  smallest set that contains the profiles and encoders needed for classification.
- **The encoder is a `VideoEncoder2Configuration`, not the Media1 type.** It has `Encoding`,
  `Resolution`, optional `RateControl` with `FrameRateLimit` and `BitrateLimit`, and `Quality`. [S9]
  `VideoEncoder2Configuration`, `VideoRateControl2`; [S2] 5.2.3.
- **The stream protocol is a string, not a `StreamSetup`.** Use the exact values in [S2] 5.4.1.
  A Media1-style `StreamSetup` is not valid here.
- **`GetServiceCapabilities` is `PRE_AUTH`.** It can be read before authentication. [S2] 5.14.

## 2. Profile classification

### 2.1 What a profile carries

A profile is the mapping between a source, an encoder and the other configuration entities. [S3]
4.1: "A media profile maps a video and/or audio source to a video and/or an audio encoder, PTZ and
analytics configurations." [S2] 5.2.1: "A media profile consists of a set of media configurations."

The encoder attributes that matter for classification are:

| Attribute | Media1 | Media2 | Evidence |
| --- | --- | --- | --- |
| Encoding | `VideoEncoderConfiguration/Encoding`, enumeration `JPEG`, `MPEG4`, `H264` | `VideoEncoder2Configuration/Encoding`, an IANA video subtype string, so `H264` and `H265` are possible | [S9] `VideoEncoderConfiguration`, `VideoEncoding`, `VideoEncoder2Configuration`; [S2] 5.2.3 |
| Resolution | `Resolution/Width`, `Resolution/Height` | `Resolution/Width`, `Resolution/Height` | [S9] `VideoResolution`, `VideoResolution2` |
| Frame rate limit | `RateControl/FrameRateLimit`, "Maximum output framerate in fps" | `RateControl/FrameRateLimit`, "Desired frame rate in fps" | [S9] `VideoRateControl`, `VideoRateControl2` |
| Bitrate limit | `RateControl/BitrateLimit`, "The maximum output bitrate in kbps" | `RateControl/BitrateLimit`, "the maximum output bitrate in kbps"; optional `AverageBitRate` | [S9] `VideoRateControl`, `VideoRateControl2` |
| Quality | `Quality`, a relative value; "A high value within supported quality range means higher quality" | Same | [S9] `VideoEncoderConfiguration`, `VideoEncoder2Configuration` |

`RateControl` is optional in both models. [S9] `VideoEncoderConfiguration`, `VideoRateControl2`.
When it is absent the bitrate and frame rate are undefined, not zero. The classification rule in
the note treats a missing value as unknown and ranks it below any reported positive value; it does
not claim the stream is low quality.

### 2.2 Why classification is by attributes, not by token naming

The specification does not contain a sentence that says "classify profiles by attributes, not by
token names." It is silent on that exact wording. What it does define is the role of the
identifiers and the meaning of the configuration:

- `Profile@token` is "Unique identifier of the profile." [S9] `Profile`.
- `Profile/Name` is "User readable name of the profile." [S9] `Profile`.
- `tt:ReferenceToken` is "Unique identifier for a physical or logical resource. Tokens should be
  assigned such that they are unique within a device. Tokens must be at least unique within its
  class." [S10] `ReferenceToken`.
- `tt:Name` is "User readable name. Length up to 64 characters." [S10] `Name`.
- The encoder configuration carries the attributes in Section 2.1. [S9]; [S2] 5.2.3.
- Media2 `GetProfiles` lets the client choose configuration types with the `Type` parameter, not by
  interpreting a token: "The Type parameter controls which configurations are returned and has no
  effect on the number of profiles returned." [S2] 5.1.2.

The client therefore treats the token and the name as identifiers, and classifies by resolution,
bitrate limit, frame rate limit and encoding. The token is used only as a final deterministic
tie-break when two profiles have identical attributes. This is a client rule derived from the
schema, not a quoted mandate.

The specification is also silent on profile order. `GetProfiles` returns a list; neither [S3] 5.2.2
nor [S2] 5.1.2 defines a first, second or preferred profile. A client must not assume that the first
profile is the main stream.

### 2.3 Shared encoders and shared profiles

Media1 `ConfigurationEntity` has a `UseCount` field: "Number of internal references currently using
this configuration." [S9] `ConfigurationEntity`. Media2 deprecates `UseCount` but keeps the concept:
"Read-only property indicating the number of referenced profiles. Deprecated in Media2." [S2] 5.2.1.
A profile may therefore share one encoder with another profile, and one encoder may be used by
several profiles. The client must dedupe by encoder token before classifying, or it will count the
same stream twice.

Media2 also has virtual profiles for multitrack streaming. A virtual profile token bundles several
profiles into one RTSP session. [S2] 5.1.6. It is not a single video source and encoder pair, so it
is not eligible for the high/low rule in this proposal.

## 3. Service discovery and endpoints

### 3.1 GetCapabilities and GetServices

| Operation | Purpose | Access class | What it returns for media | Evidence |
| --- | --- | --- | --- | --- |
| `GetCapabilities` | Backward-compatible capability query | `PRE_AUTH` | `Capabilities/Media/XAddr`, `Media/StreamingCapabilities`, `Media/ProfileCapabilities/MaximumNumberOfProfiles` | [S1] 8.1.2.4, Annex A; [S9] `Capabilities` |
| `GetServices` | Full service list with optional capabilities | `PRE_AUTH` | `Service/Namespace`, `Service/XAddr`, `Service/Version`, `Service/Capabilities` for each service | [S1] 8.1.2.2 |
| `GetServiceCapabilities` (Device) | Device service capabilities | `PRE_AUTH` | Security and network flags, including `UsernameToken`, `HttpDigest` and `Network/NTP` | [S1] 8.1.2.3, Table 9 |
| `GetServiceCapabilities` (Media) | Media service capabilities | `PRE_AUTH` | `MaximumNumberOfProfiles`, `ConfigurationsSupported`, snapshot, streaming flags | [S3] 5.21; [S2] 5.14 |

`GetCapabilities` is explicitly the backward-compatible interface: "Refer to GetServices for a full
set of capabilities." [S1] 8.1.2.4. A client should read both when it can, and treat `GetServices`
as the authoritative service inventory.

### 3.2 The device service path is fixed

[S1] 5.1.1 states: "The entry point for the device management service is fixed to:
/onvif/device_service." This is a normative statement about the device service path. It does not
say that the host, scheme or port are fixed, and it does not cover the Media, PTZ, Events or Imaging
service paths.

The ONVIF Application Programmer's Guide adds: "Service addresses are not fixed. They vary according
to the vendor and device, and might require different handling. Therefore, this information must be
updated dynamically." [S13] Section 5.1.4. Read together, the safe interpretation is:

- The device service path is `/onvif/device_service`.
- The device service host, scheme and port may differ from the camera's RTSP endpoint and may be
  non-standard. Use the manual address plus an optional ONVIF port, or the WS-Discovery `XAddrs`.
- The Media, PTZ, Events and Imaging service addresses must come from `GetCapabilities` or
  `GetServices`, not from a guessed path.
- A device service path other than `/onvif/device_service` is non-conformant. It is a quirk
  candidate with evidence, not a design assumption.

No vendor document found on 2026-11-27 states a different device service path. The vendor documents
below state a port, not a path.

### 3.3 Ports

| Port | What it is | Evidence | Use in the strategy |
| --- | --- | --- | --- |
| 80/tcp | HTTP, the usual ONVIF and web port | [S1] 7.3.2.3 says a device should provide a port 80 device service entry for firewall traversal; [S19] says Hikvision uses HTTP port 80 by default; [S20] says Dahua HTTP default is 80 | First default attempt, at `/onvif/device_service` |
| 443/tcp | HTTPS, the usual secure web and ONVIF port | [S20] lists HTTPS default 443 | Second default attempt, with normal TLS verification |
| 8000/tcp | Reolink's documented default ONVIF port | [S17] states "The default port for ONVIF is 8000" | A per-camera ONVIF port field or the discovered XAddr |
| 8000/tcp | Hikvision's device or server port, not its ONVIF port | [S18] is about ONVIF enablement; [S19] says Hikvision uses HTTP port 80 by default; the Hikvision camera manual lists Server Port 8000 separately from HTTP 80 [S26] | Never assume 8000 is ONVIF on a Hikvision camera |
| 8080/tcp | An alternate HTTP port sometimes used for ONVIF | [S23] says the HTTP/ONVIF port is usually 80 or 8080; this is a third-party FAQ, not a vendor specification | A documented field candidate, not a default |
| 37777/tcp | Dahua's private TCP/SDK port | [S20] lists TCP port 37777 as the private protocol default, separate from HTTP 80 | Never probe it as ONVIF |
| 554/tcp | RTSP | [S20] lists RTSP default 554 | The stream port, not the control port |

The strategy does not scan ports. A port scan is slow, noisy, may trip a camera's login lock, and
cannot distinguish ONVIF from a vendor SDK. The camera form needs an optional ONVIF port, or
discovery must supply the device service `XAddr`. The current camera schema has no ONVIF endpoint or
port, which is why the current client can only try 80 and 443 at the fixed path.

## 4. Authentication

### 4.1 The mechanisms

ONVIF Core 5.9.1 makes HTTP digest authentication the baseline for services over HTTP and HTTPS,
with these exceptions: legacy devices that support WS-Security UsernameToken, TLS client
authorization, and JWT client authorization over HTTPS. [S1] 5.9.1. Profile S makes WS-UsernameToken
mandatory for devices and HTTP Digest optional; Profile T makes HTTP Digest mandatory. [S11] 7.1.3;
[S12] 7.1.3. Core 26.06 cites RFC 2617 for HTTP Digest; RFC 7616 is the current specification and
obsoletes it, so the client should accept the digest algorithms the camera challenges for, including
the MD5 legacy algorithm. [S1] 5.9.1; [S15] 1, 3. A camera may therefore accept one, the other, or
both.

| Mechanism | Form | Replay and clock behaviour | Evidence |
| --- | --- | --- | --- |
| WS-Security UsernameToken, `PasswordDigest` | `Password_Digest = Base64(SHA-1(nonce + created + password))`; `Created` is a UTC timestamp; nonce is random bytes | The camera compares `Created` with its own clock; a large skew can reject the token. ONVIF requires both nonce and timestamp. | [S14] 3.1; [S1] 5.9.5 |
| WS-Security UsernameToken, `PasswordText` | The password, a password hash, or a derived password, sent in the SOAP header | The password is exposed to the SOAP layer. The OASIS profile recommends passing a password only over a secure transport or inside an encrypted token. | [S14] 3.1 |
| HTTP Digest | HTTP `401` with `WWW-Authenticate: Digest`, then an `Authorization: Digest` header; RFC 7616 | No `Created` timestamp, so it does not depend on clock agreement. Algorithms and `qop` are negotiated by the challenge. | [S1] 5.9.1; [S15] 3 |
| HTTP Basic | HTTP `Authorization: Basic` | Not an ONVIF authentication method in Core 5.9.1. It exposes the password unless TLS protects it. | [S1] 5.9.1 |

The OASIS profile gives the exact digest input: "concatenate the nonce, creation timestamp, and the
password ... digest the combination using the SHA-1 hash algorithm, then include the Base64 encoding
of that result as the password (digest)." It also says the nonce is hashed using the octet sequence
of its decoded value, and the timestamp is hashed using the UTF-8 octets of its text. [S14] 3.1.
The ONVIF Core adds: "A client shall use both nonce and timestamps as defined in [WS-UsernameToken].
The server shall reject any Username Token not using both nonce and creation timestamps." [S1]
5.9.5. A client that sends only a password, or a digest without both nonce and timestamp, is not
conformant.

HTTP Digest is not only a fallback for older cameras. It is the Core baseline, it is mandatory in
Profile T, and it avoids the clock-skew failure mode entirely. ONVIF announced on 2025-10-09 that
it is ending Profile S support and recommends Profile T, citing Profile S's authentication
mechanisms as no longer consistent with current security recommendations. [S21]. That is a reason
to prefer HTTP Digest when a camera advertises it, while still supporting WS-UsernameToken for the
legacy cameras that only offer it.

### 4.2 Which operations may be unauthenticated

The Core access classes and the default access policy in Table 7 are:

| Operation | Access class | Anonymous allowed by default | Evidence |
| --- | --- | --- | --- |
| `GetCapabilities` | `PRE_AUTH` | Yes | [S1] 8.1.2.4, Table 7 |
| `GetServices` | `PRE_AUTH` | Yes | [S1] 8.1.2.2, Table 7 |
| `GetServiceCapabilities` (Device, Media, PTZ) | `PRE_AUTH` | Yes | [S1] 8.1.2.3; [S3] 5.21; [S2] 5.14; [S4] 5.10 |
| `GetSystemDateAndTime` | `PRE_AUTH` | Yes | [S1] 8.3.6 |
| `GetWsdlUrl` | `PRE_AUTH` | Yes | [S1] 8.1.1 |
| `GetEndpointReference` | `PRE_AUTH` | Yes | [S1] 8.4.9 |
| `GetDeviceInformation` | `READ_SYSTEM` | No | [S1] 8.3.1, Table 7 |
| `GetProfiles` | `READ_MEDIA` | No | [S3] 5.2.2; [S2] 5.1.2 |
| `GetStreamUri` | `READ_MEDIA` | No | [S3] 5.15.1; [S2] 5.4.1 |
| `GetSnapshotUri` | `READ_MEDIA` | No | [S3] 5.16.1; [S2] 5.5.1 |
| `GetNTP` | `READ_SYSTEM` | No | [S1] 8.2.6 |
| `SetSystemDateAndTime` | `WRITE_SYSTEM` | No | [S1] 8.3.7 |
| `SetNTP` | `WRITE_SYSTEM` | No | [S1] 8.2.7 |
| PTZ `GetNodes`, `GetPresets`, `GetStatus` | `READ_MEDIA` | No | [S4] 5.1.2, 5.4.2, 5.3.7 |
| PTZ move and preset writes | `ACTUATE` | No | [S4] 5.3, 5.4 |

Table 7 grants `PRE_AUTH` to Administrator, Operator, User and Anonymous. It does not grant
`READ_MEDIA` to Anonymous. [S1] Table 7. A device may configure a more permissive access policy, so
an anonymous `GetProfiles` success is allowed, but a client must not require it. A `401` on a
`READ_MEDIA` operation is normal and means "authenticate", not "the camera is broken."

### 4.3 The clock skew limit

The only numeric freshness figure in the specifications read for this assessment is in the OASIS
UsernameToken profile:

> "It is RECOMMENDED that web service producers provide a timestamp freshness limitation, and that
> any UsernameToken with stale timestamps be rejected. As a guideline, a value of five minutes can
> be used as a minimum to detect, and thus reject, replays." [S14] 3.1.

This is a recommendation and a guideline, not a mandated maximum. It says five minutes is a
reasonable minimum window; a device may use a smaller or larger window, or no window at all. ONVIF
Core does not define a numeric skew limit. It requires nonce and timestamp, and says the server
shall reject a token that lacks them, but it does not say how old a timestamp may be. [S1] 5.9.5.
The specification is silent on the exact window.

The practical consequence is that a camera whose clock is far from the timestamp in the request can
reject an otherwise correct password. The exact window is vendor-specific and undocumented in the
sources found. A client library issue reports about ten seconds of replay protection on Axis
cameras; a support article describes ONVIF authentication failures when clocks differ but does not
state a window. The exact figures are unverified: [S25] is a client-library report and [S24] is a
vendor support article that does not state a window. The safe rule is not "assume five
minutes"; the safe rule is "timestamp from the camera's own clock, and treat a timestamp rejection
as a distinct failure."

HTTP Digest has no timestamp and is therefore the reliable escape when a camera's clock is wrong
but its HTTP authentication works. [S1] 5.9.1; [S15] 3.

The HTTP `Date` header is a second, coarser source of the camera's clock. RFC 9110 requires an
origin server with a clock to generate `Date` in 2xx, 3xx and 4xx responses. [S16] 6.6.1. A 401 from
the camera therefore normally carries the camera's own time, which is exactly the value needed for
a WS-UsernameToken `Created`. The header has one-second resolution and is not an ONVIF guarantee,
but it is a documented HTTP behaviour and a useful bootstrap when `GetSystemDateAndTime` is refused.
The strategy uses it only to build the first authenticated timestamp, never to set the appliance
clock.

## 5. Time operations

| Operation | Requirement | Device profile requirement | Access class | What it does | Evidence |
| --- | --- | --- | --- | --- | --- |
| `GetSystemDateAndTime` | Required; the device shall support the return of the DST setting and the manual or NTP time | Mandatory for Profile S and Profile T devices | `PRE_AUTH` | Returns `DateTimeType` (`Manual` or `NTP`), `DaylightSavings`, optional POSIX `TimeZone`, optional `UTCDateTime`, optional `LocalDateTime`; the device shall provide `UTCDateTime` despite the optional marking | [S1] 8.3.6; [S11] 7.5.3; [S12] 7.5.3 |
| `SetSystemDateAndTime` | Required; the device shall support configuring DST and the manual time (if applicable) or NTP indication (if applicable) | Mandatory for Profile S and Profile T devices | `WRITE_SYSTEM` | Sets `DateTimeType`, `DaylightSavings`, optional POSIX `TimeZone`, optional `UTCDateTime`; `UTCDateTime` has no meaning in NTP mode; faults include `InvalidTimeZone`, `InvalidDateTime` and `NtpServerUndefined` | [S1] 8.3.7; [S11] 7.5.3; [S12] 7.5.3 |
| `GetNTP` | Conditional on the `Network.NTP` capability; the value is the maximum number of NTP servers | Conditional on NTP support; device M if supported | `READ_SYSTEM` | Returns `FromDHCP`, `NTPFromDHCP`, `NTPManual` | [S1] 8.2.6, 8.1.2.3 Table 9; [S12] 8.8.3 |
| `SetNTP` | Conditional on the `Network.NTP` capability | Conditional on NTP support; device M if supported | `WRITE_SYSTEM` | Sets the NTP server list; changing the list does not change the clock mode; `SetSystemDateAndTime` activates NTP | [S1] 8.2.7; [S12] 8.8.3 |

The `Network.NTP` capability is an integer that states the maximum number of NTP servers. A value of
zero or an absent `NTP` element means the camera does not advertise NTP support. [S1] 8.1.2.3 Table
9; [S5] `NetworkCapabilities/NTP`. The Device service capabilities do not define a flag for time
setting itself. [S1] 8.1.2.3 Table 9. The presence of `SetSystemDateAndTime` in the WSDL is not
evidence that a camera accepts a manual time or NTP mode; the modes are conditional on the device's
implementation.

### 5.1 What a client does when only one operation works

- **Read works, set fails:** The client can use the camera clock for authentication timestamps and
  can report the camera's mode and zone, but it cannot correct the clock. Record `read-only` or
  `refused`, keep the camera adopted, and show the owner action to set the clock in the camera's own
  UI. Do not retry the write on every check.
- **Set works, read fails:** The client can push a manual time, but it cannot verify the result or
  read the mode. This is non-conformant because `GetSystemDateAndTime` is required. Push once,
  attempt a read-back, and record `unverified` if the read still fails. Do not claim success.
- **NTP works, manual does not:** Use `SetNTP` followed by `SetSystemDateAndTime` with
  `DateTimeType=NTP`. Changing the NTP list alone does not activate NTP. [S1] 8.2.7. If the mode
  switch fails with `NtpServerUndefined`, the NTP list was not accepted; record it.
- **Manual works, NTP does not:** Push `SetSystemDateAndTime` with `DateTimeType=Manual`, the UTC
  instant and a POSIX zone. Verify by reading the returned UTC value back. This is the ADR 0035
  fallback. [ADR 0035](../adr/0035-appliance-as-time-authority.md).
- **Neither works:** Record `refused`, keep the camera adopted, and show the owner action. A camera
  that refuses time still streams, records and produces snapshots. [ADR 0035](../adr/0035-appliance-as-time-authority.md).
- **Only `GetNTP`/`SetNTP` work, but the mode cannot be switched:** The NTP list is stored but not
  active. Record `ntp-inactive`; do not report success.

The specification defines the operations and the conditional NTP capability. It does not define a
fallback between them; ADR 0035 chooses NTP first and a pushed manual time second.

## 6. PTZ

### 6.1 Operations

| Operation | Requirement | Access class | Notes | Evidence |
| --- | --- | --- | --- | --- |
| `GetServiceCapabilities` | PTZ service capability query | `PRE_AUTH` | `EFlip`, `Reverse`, `GetCompatibleConfigurations`, `MoveStatus`, `StatusPosition`, `MoveAndTrack` | [S4] 5.10; [S8] |
| `GetNodes` and `GetNode` | A PTZ-capable device shall implement both | `READ_MEDIA` | Each node has `SupportedPTZSpaces`, `MaximumNumberOfPresets`, `HomeSupported`, `AuxiliaryCommands`, `MaximumNumberOfPresetTours` | [S4] 5.1.1, 5.1.2, 5.1.3 |
| `GetConfigurations`, `GetConfiguration`, `GetConfigurationOptions` | A PTZ-capable device shall implement them | `READ_MEDIA` | Configuration options include the supported spaces and, for continuous movement, the accepted timeout range | [S4] 5.2.2, 5.2.3, 5.2.4 |
| `ContinuousMove` | A PTZ-capable device shall support continuous movements | `ACTUATE` | Velocity has optional pan/tilt and zoom components; optional timeout; zero on an axis stops that axis | [S4] 5.3.3 |
| `Stop` | A PTZ-capable device shall support Stop | `ACTUATE` | Optional `PanTilt` and `Zoom` filters, both default true | [S4] 5.3.5 |
| `AbsoluteMove` | Required if the node supports absolute pan/tilt or absolute zoom movements | `ACTUATE` | Position has optional pan/tilt and zoom; speed optional | [S4] 5.3.1 |
| `RelativeMove` | Required if the node supports relative pan/tilt or relative zoom movements | `ACTUATE` | Translation has optional pan/tilt and zoom; speed optional; zero translation stops | [S4] 5.3.2 |
| `GetStatus` | A PTZ-capable device shall be able to report status | `READ_MEDIA` | Position if `StatusPosition`; MoveStatus if `MoveStatus`; UTC time | [S4] 5.3.7 |
| `GetPresets` | Required for nodes supporting presets | `READ_MEDIA` | Returns token, optional name, optional absolute position | [S4] 5.4, 5.4.2 |
| `SetPreset`, `GotoPreset`, `RemovePreset` | Required for nodes supporting presets | `ACTUATE` | `SetPreset` can create or update; `GotoPreset` can take a speed; `RemovePreset` deletes | [S4] 5.4.1, 5.4.3, 5.4.4 |
| `GotoHomePosition`, `SetHomePosition` | Required if `HomeSupported` is true | `ACTUATE` | `SetHomePosition` fails if the home position is fixed | [S4] 5.1.1, 5.5 |

### 6.2 Zoom

Zoom is not a separate operation. It is an optional component of the move and status types:

- `ContinuousMove/Velocity/Zoom` for continuous zoom.
- `AbsoluteMove/Position/Zoom` for absolute zoom.
- `RelativeMove/Translation/Zoom` for relative zoom.
- `Stop/Zoom` to stop only the zoom axis.
- `GetPresets` returns the zoom position when the node supports absolute zoom.
- `GetStatus` returns the zoom position when the profile has PTZ and the node supports it.

A PTZ node may support pan/tilt only, zoom only, or both. [S4] 4, 5.3.1, 5.3.2, 5.3.3. The node's
`SupportedPTZSpaces` lists `AbsoluteZoomPositionSpace`, `RelativeZoomTranslationSpace`,
`ContinuousZoomVelocitySpace` and `ZoomSpeedSpace` when the corresponding zoom movement is
available. [S4] 5.1.1, 5.7. If the space group is absent, the corresponding move operation is not
available for that node. [S4] 4: "If the PTZ node does not support the coordinate systems of a
certain group, the corresponding move operation will not be available for this PTZ node."

### 6.3 Presets, home and status

- If `MaximumNumberOfPresets` is greater than zero, all preset operations shall be available for
  that node. [S4] 5.1.1. If it is zero, no preset operation is required, and a camera with PTZ but
  no presets is valid.
- `HomeSupported` true means the home operations are available. [S4] 5.1.1.
- `MoveStatus` and `StatusPosition` are separate capabilities. A camera can report movement without
  reporting position, or position without movement. [S4] 5.10. A device that supports Profile T
  continuous PTZ move must return `MoveStatus` true, because the profile makes continuous PTZ
  conditional and requires the status capability when the feature is implemented. [S12] 7.22.1.
- `GetStatus` can fail with `NoStatus` when the profile has PTZ but no status is available. [S4]
  5.3.7. That is a capability gap, not a broken camera.

### 6.4 How a client detects PTZ support

1. Read `GetServices` for the namespace `http://www.onvif.org/ver20/ptz/wsdl`, or
   `GetCapabilities` for `PTZ/XAddr`. [S1] 8.1.2.2, Annex A; [S4] Table 1.
2. Read the selected media profile. A Media1 profile has an optional `PTZConfiguration`; a Media2
   profile has an optional `PTZ` entry in its configuration set. [S9] `Profile`, `ConfigurationSet`.
3. If the profile has a PTZ configuration, read `GetNodes` or `GetNode` for `SupportedPTZSpaces`,
   `MaximumNumberOfPresets`, `HomeSupported` and `AuxiliaryCommands`. [S4] 5.1.1.
4. Read `GetConfigurationOptions` for the profile's PTZ configuration; it returns the spaces and
   the timeout range for continuous movement. [S4] 5.2.4.
5. Read `GetServiceCapabilities` for `MoveStatus`, `StatusPosition`, `EFlip`, `Reverse`,
   `GetCompatibleConfigurations` and `MoveAndTrack`. [S4] 5.10.
6. Only then enable the controls the node actually supports. A PTZ service without a PTZ
   configuration in the chosen profile gives no control for that profile; another profile may have
   one.

This is research for BIT-027. It is not implemented in this change.

## 7. What the specification leaves open

| Question | What the specification says | What it does not say |
| --- | --- | --- |
| Media1 versus Media2 precedence | Both are separate services; Profile T uses Media2, Profile S uses Media1. [S2], [S3], [S11], [S12] | It does not say which to prefer when both are present, or whether their profile tokens or configurations are equivalent |
| Profile order | `GetProfiles` returns a list. [S3] 5.2.2; [S2] 5.1.2 | It does not define a first, main or preferred profile |
| Profile token meaning | The token is a unique identifier. [S10] `ReferenceToken`; [S9] `Profile` | It does not assign semantics to names such as `main`, `sub` or `profile_1` |
| Clock skew window | Nonce and timestamp are required; stale timestamps should be rejected; five minutes is a guideline. [S1] 5.9.5; [S14] 3.1 | It does not define a numeric maximum window, and vendors do not publish one in the sources found |
| Authentication preference | HTTP Digest is the Core baseline; WS-UsernameToken is a legacy exception; Profile S and T differ. [S1] 5.9.1; [S11] 7.1.3; [S12] 7.1.3 | It does not say which to try first when both are supported |
| Non-standard ports | A device should provide a port 80 device service entry. [S1] 7.3.2.3 | It does not enumerate vendor ports or require a port 8000 service |
| Device service path | Fixed to `/onvif/device_service`. [S1] 5.1.1 | It does not allow a vendor-specific path, and no vendor document found documents one |
| HTTP `Date` as a clock source | An origin server with a clock must send `Date` in 2xx, 3xx and 4xx responses. [S16] 6.6.1 | It is not an ONVIF operation and is not guaranteed to match the ONVIF device clock |
| PTZ availability | ContinuousMove and Stop are mandatory for a PTZ-capable device; presets, zoom, home, absolute and relative moves are conditional. [S4] 5.1.1, 5.3, 5.4, 5.7 | It does not guarantee that a camera advertising PTZ implements every feature |
| Media2 `Type` behaviour | No `Type` means no configuration information; `Type=All` includes all associated configurations. [S2] 5.1.2 | It does not define what a camera should do if it cannot return a requested type; the general response is to omit it without an error |

## 8. Current client gap

The ONVIF client added for camera time is a Device-service-only client:

| Current behaviour | Where | What the compatibility work needs |
| --- | --- | --- |
| Device service path fixed to `/onvif/device_service`; HTTP 80 then HTTPS 443 only | `agent/internal/onvif/endpoint.go:16`, `:23-34` | An optional ONVIF endpoint or port, and discovery `XAddr` support, for cameras such as Reolink on 8000 |
| SOAP 1.2 only | `agent/internal/onvif/client.go:19` | SOAP 1.2 is the specified binding ([S1] 5.3); a SOAP 1.1 fallback is a quirk, not a default |
| WS-Security UsernameToken digest only | `agent/internal/onvif/auth.go:16`, `:24-53` | HTTP Digest support, security capability detection, and a bounded fallback order |
| Device service operations only | `agent/internal/onvif/client.go` | Media1 and Media2 clients, profile enumeration, stream and snapshot URIs, PTZ later |
| Authenticated `GetServiceCapabilities` | `agent/internal/onvif/client.go:179-180` | The operation is `PRE_AUTH`; read it unauthenticated first, and authenticate only if the camera requires it |
| No Media2 `GetProfiles` `Type` handling | not present | Send `Type=All`, and fetch per-profile configurations when a camera returns a partial set |

The current client is correct for the time slice: it timestamps the first authenticated request from
the camera's unauthenticated clock and records a rejection. The compatibility work extends it; it
does not replace the time behaviour.

## 9. Consequences for the strategy

- Media1 support is not optional. Media2 is the future direction, but Media1 remains a current
  service specification and a large installed base. [S3]; [S21].
- A client must read `GetServices` to find Media2. `GetCapabilities` alone is not enough.
- Profile enumeration requires `READ_MEDIA` authentication on a conformant camera. Capability
  discovery can be unauthenticated; profiles cannot be assumed to be.
- HTTP Digest is the first choice when advertised, not only a fallback, because it is the Core
  baseline, Profile T mandatory, and immune to clock skew.
- WS-UsernameToken still has to work, with both nonce and timestamp, because Profile S cameras and
  many legacy devices use it.
- The clock problem is solved with three layers: the camera's unauthenticated time, the HTTP `Date`
  header, and HTTP Digest. The appliance clock is the last resort, and a rejection is reported, not
  hidden.
- The device service path is fixed; ports and service `XAddr` values are not. A client must not scan
  ports or guess paths.
- Profile classification must use resolution, bitrate, frame rate and encoding, and must not use
  token names or response order.
- PTZ support is a set of conditional capabilities, not a boolean. Detect the node, the spaces, the
  preset count and the status capabilities before showing a control.
