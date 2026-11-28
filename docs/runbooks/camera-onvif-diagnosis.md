# Camera ONVIF diagnosis

- Date: 2026-11-28
- Status: ready for the next field run. The command and its fixtures are tested offline; no real
  camera has run the command yet.
- Related: [ONVIF compatibility matrix](../engineering/onvif-compatibility.md),
  [status contract](../status.md), [ADR 0024](../adr/0024-onvif-discovery-levels.md),
  [ADR 0035](../adr/0035-appliance-as-time-authority.md),
  [ADR 0037](../adr/0037-nvr-grid-per-pane-quality-and-full-screen.md),
  [OQ-47](../open-questions.md#oq-47), [OQ-49](../open-questions.md#oq-49),
  [OQ-51](../open-questions.md#oq-51),
  [the Amcrest media path note](../notes/2026-11-28-onvif-media-endpoint-diagnosis.md)

This runbook answers one question: when a camera's clock checks and sets over ONVIF but its stream
settings cannot be read, which call failed, at which URL, and why? It replaces guessing from the
owner-facing sentence with one read-only probe that prints the whole path.

The command is `bittern camera probe`. It sends only `Get` operations. It never sets the clock,
changes NTP, writes a profile, or changes camera configuration. It is safe to run on a working
camera; it does not adopt, apply, or restart anything.

## Preconditions

- [ ] The command is installed on the appliance. `sudo bittern camera probe --help` prints the
      usage. The camera must be on the camera network and reachable from `cam0`.

```sh
ip route get <camera-address>
```

- [ ] The camera is either in `appliance.yaml` and has a saved sign-in, or its address is known.
      For an address that is not in the configuration, name a saved `credentials_ref` with
      `--account`. Never put a camera password on the command line.

- [ ] The last status result is available. It names the stored state and, for a profile failure,
      the per-profile reasons in the camera page's Technical details. The probe explains those
      reasons; it does not replace them.

## Run the probe

For an adopted camera, use its identifier from `appliance.yaml`:

```sh
sudo bittern camera probe front_door
```

For a camera that is not in the configuration, use its address and the saved account:

```sh
sudo bittern camera probe 10.77.0.50 --account cameras.front_door.rtsp_url
```

Flags:

| Flag | Use |
| --- | --- |
| `--config PATH` | Read another `appliance.yaml`. Default `/etc/bittern/appliance.yaml`. |
| `--secrets PATH` | Read another `secrets.yaml`. Default `/etc/bittern/secrets.yaml`. |
| `--account REF` | Use this saved `credentials_ref` when the argument is an address. |
| `--endpoint URL` | Only when the device service is not on the standard port. The host must be the camera address. |
| `--timeout D` | Per-call timeout. Default `5s`. The whole probe is bounded. |
| `--json` | Print the same report as JSON for a support bundle. |

The report is redacted before it is printed. The probe registers the camera account values and the
reported serial number with the redactor, and refuses to print a report that still contains either
one. A fault string that echoes a password appears as `[REDACTED]`, not as the password.

## Read the report

The report has these sections, in order:

| Section | What it proves |
| --- | --- |
| `device endpoints` and `selected device service` | Which URL answered `GetSystemDateAndTime`. This is the path the clock uses. |
| `clock` | The camera's `DateTimeType`, UTC value and zone. An error here is a time failure, not a media failure. |
| `device information` | Manufacturer, model, firmware and hardware id from `GetDeviceInformation`. The serial is redacted. |
| `authentication` | The security flags and every account and scheme tried. A rejected password is an authentication failure, not a transport failure. |
| `GetCapabilities` | Every advertised service URL, printed verbatim. The `Media` line is the endpoint the probe uses. |
| `GetServices` | The `GetServices` list, kept beside the capabilities list. A media URL here that repeats the device service path is the common defect. |
| `media services` | Each advertised media endpoint, its profiles, encoder configuration, stream URI scheme and path, snapshot path, and the reason a call failed. |
| `calls` | One line per SOAP operation: operation name, URL, account or anonymous, HTTP status, failure class, fault code and reason verbatim, duration and size. |

Each `calls` line names the failure class:

| `failure=` | Meaning | Typical owner action |
| --- | --- | --- |
| `none` | The call answered. A SOAP fault can still be absent; the fault field is printed when present. | None. |
| `authentication` | The camera answered and rejected the account or the timestamp. | Check the camera sign-in, or add its ONVIF sign-in. |
| `transport` | No HTTP answer, a timeout, or a connection failure. | Only here is the power and cable check the right first step. |
| `refused` | The camera answered and refused the operation for another reason. | Read the fault code and reason. |

## Decide from the evidence

| Evidence | Diagnosis | Next step |
| --- | --- | --- |
| The clock and `GetDeviceInformation` answer, `GetCapabilities` names a media URL, and the media calls go to another path | The media endpoint was assumed instead of advertised. | Update to the build with this fix, then run the probe again. The media calls must use the `GetCapabilities` `Media` URL. |
| The media calls answer HTTP 404 on a URL that repeats `/onvif/device_service` | `GetServices` echoed the device path for the media service. | Record the model and firmware in the matrix. The fixed client uses the capabilities URL. |
| Every `GetStreamUri` returns an `rtsps://` URI | The camera offers only TLS RTSP. | Record `rtsps-unsupported`. Turn on the camera's unencrypted RTSP stream, or follow [OQ-51](../open-questions.md#oq-51). Do not expect the manual path to work while the camera is TLS-only. |
| Profiles are listed but every one is rejected: missing encoder, JPEG codec, no resolution, no stream URI, or duplicate URI | The media service answered and the profiles are unusable. | The report names each profile and the reason. Fix the camera's stream settings, then run setup again. |
| `GetProfiles` returns a fault such as `ter:NotAuthorized` | Authentication or authorization, not transport. | Check the account and its ONVIF user level. |
| Nothing answers, including the clock, and the calls show `no response` | The camera is not reachable at all. | Check power and the camera cable, then run the probe again. |
| The device service answers, but every media URL fails with `transport` | The media service is not listening or is disabled. | Check that the camera's ONVIF media service is on. Do not describe the camera as unreachable. |

## Record the result

1. Match the result to the stored state in the camera page's Technical details. If they disagree,
   record the probe output and the stored state in a dated note under `docs/notes/`.
2. Fill or update the camera's row in the [ONVIF compatibility matrix](../engineering/onvif-compatibility.md).
   Record the device service, the resolved media endpoint, the media version, the auth method and
   account role, the profile facts and the stream scheme. Never record a credential, a serial
   number, a hardware address, or a complete stream URL with user information.
3. If the camera needs a new quirk, write the quirk candidate with the evidence and a test. A
   candidate is not a quirk and is not applied without the register entry.
4. If the probe exposes a new reason the UI cannot explain, add the reason, its copy and its action
   in the same change, with a test.

## Verification

- [ ] The first line says `mode: read-only, Get operations only`.
- [ ] Every line under `calls` names an operation whose action contains `/Get`.
- [ ] The output contains no password, user name value, serial number, or complete URI with user
      information. A deliberate fault that echoes the password prints `[REDACTED]`.
- [ ] The `GetCapabilities` section shows the media URL the media calls actually used, or the report
      says why it did not.
- [ ] The stored camera page state and the probe result name the same failure class. If they do not,
      the stored result is stale; run setup again before recording the row.
