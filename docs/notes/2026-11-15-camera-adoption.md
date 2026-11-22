# Camera adoption POC slice and managed camera list

- Date: 2026-11-15
- Status: implemented in the wizard, status contract `cameras` section, renderer, apply, cleanup test,
  and the QEMU `adoption` scenario

## What changed

- `internal/discovery` discovers on `cam0` with layered, best-effort evidence: the dnsmasq lease,
  the ARP table, ONVIF WS-Discovery (UDP 3702, socket bound to the interface), an unauthenticated
  ONVIF `GetDeviceInformation` attempt when WS-Discovery returns an endpoint, and an RTSP `OPTIONS`
  probe on 554 and 8554. It never probes `lan0`, bounds every layer, caps candidates, and rate limits
  repeated runs. ONVIF is an optimization, not a requirement: a failed layer is recorded on the
  candidate and shown in the wizard, and it never removes the candidate. Candidates carry address,
  MAC, lease hostname, vendor and model when reported, per-layer results, and an online or seen
  summary. Discovery never sends credentials, refuses an XAddr outside `cam0`, and refuses redirects.
- The wizard camera step lists the candidates with that evidence and a manual address fallback. The
  manual path always works: it does not depend on ONVIF, and an optional MAC field covers an in-pool
  address that discovery has not seen. The wizard collects a camera username and password, builds the
  complete RTSP URL with percent-encoded credentials, and writes it to `/etc/bittern/secrets.yaml`
  at mode 0600 under a server-generated `cameras.<id>.rtsp_url` key. The key is not a browser field
  and the password is never rendered.
- A camera inside the dynamic pool now requires a MAC. The wizard takes the MAC from its own
  discovery result, and the renderer writes a `dhcp-host` entry in
  `/etc/dnsmasq.d/bittern-reservation.conf`. The file carries the generated header, so
  `packaging/cleanup.sh` removes it as Bittern-owned; a manual reservation in another file is left
  and fails the clean-state check.
- The status contract has a `cameras` section built from `appliance.yaml` and the non-secret
  `<state-root>/camera-health.json`, never a live camera call. `/ui/cameras` shows name, address,
  MAC, vendor/model, stream and snapshot health, recording state, last seen, and enabled state, with
  Edit, Test snapshot, and Remove. Edit and remove use the same apply transaction; remove deletes
  the secret. The native camera UI action is not in this list and stays with the proxy ticket.
- The QEMU `adoption` scenario starts the camera on DHCP, runs an authenticated MediaMTX fixture, and
  proves the manual adoption with WS-Discovery off, the failed ONVIF layers in the fallback page, the
  online WS-Discovery layers after the responder starts, the reservation, encoded secret, snapshot,
  managed list, edit, explicit snapshot test, and an unreachable camera state.

## Tested

- `make check` passes: schema cases including a reserved in-pool camera and duplicate/invalid MAC,
  the render golden with `dnsmasq/reservations.conf`, the cleanup tests that remove the Bittern
  reservation and leave a manual one, the status camera section and health cache, the UI list and
  edit tests, and the discovery layer and SSRF guard tests.
- `make qemu-test` passes in 24.8 s on 2026-11-15.
- `make qemu-firstboot` passes on 2026-11-15 after the wizard camera form and status section changes.
- `python3 tests/qemu/run.py adoption` passes on 2026-11-15. It proves the DHCP camera at a pool
  address, the manual adoption with WS-Discovery off and the failed ONVIF layers visible, the online
  layer after the responder starts, the Bittern reservation accepted by `dnsmasq --test`, the
  credential in `secrets.yaml` at mode 0600, the Frigate JPEG, the managed list, a name edit, an
  explicit snapshot test, and the offline camera list and degraded status section. The wizard phase
  takes about 250 s because the cold guest pulls the pinned Frigate image. The logs pass the
  no-token and no-camera-credential checks.

## Unfinished

- The full BIT-008 scope still needs a bounded unicast sweep across the camera subnet, authenticated
  ONVIF Media profiles and stream URIs, re-probing an auth-enabled camera, a packet capture that
  shows no traffic outside `cam0`, and a failed-adoption atomicity proof. The WS-Discovery and
  unauthenticated Device probe are not a claim of full ONVIF support.
- The native camera UI proxy is a separate ticket. The list does not wait for it.
- A production camera compatibility matrix still depends on field evidence (OQ-10).

## Next step

Repeat the GK41 clean install with the camera on DHCP and the wizard taking the place of the old
static-address exception. Use the `adoption` capture set as the expected evidence, and add field
quirks to the OQ-10 matrix rather than assuming full ONVIF support.
