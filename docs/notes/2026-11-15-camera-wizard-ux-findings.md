# Camera wizard UX findings

The GK41 Level 1 clean install passed, and the TLS wizard, camera discovery, adoption, apply, and
snapshot completed. This note records that run and the camera onboarding UX gaps it exposed. It is
evidence for [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) and
[BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing), not a
code change.

Related: [clean-install runbook](../runbooks/gk41-clean-install.md),
[GK41 hardware run](2026-11-15-gk41-hardware-run.md),
[BIT-008](../plan/implementation-plan.md#bit-008-camera-discovery-and-adoption-on-cam0),
[BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress),
[BIT-020](../plan/implementation-plan.md#bit-020-responsive-and-mobile-friendly-web-ui),
[BIT-021](../plan/implementation-plan.md#bit-021-camera-native-ui-proxy),
[ADR 0022](../adr/0022-community-functionality-ux-wins.md).

## Successful run

The Level 1 sequence in the clean-install runbook completed on the GK41. Cleanup reached a verified
clean state, the payload installed, first boot completed, the wizard accepted the setup token and
administrator password, and the camera was discovered on `cam0` and adopted with its credentials.
Bittern wrote the DHCP reservation, the apply committed, and Frigate returned a JPEG snapshot. The
post-reboot checks passed with the rendered network, resolver, firewall, and one camera still
present.

The Amcrest camera stayed on DHCP. No manual camera reservation, static address, network file, or
resolver was needed. The camera adoption path is now the automatic path described in the runbook.

The run used the two named temporary exceptions from the runbook:

- The recording drive was formatted and mounted at `/var/lib/bittern/media` before first boot. This
  is the [BIT-012](../plan/implementation-plan.md#bit-012-storage-selection-and-preparation)
  storage exception.
- The setup token was copied from the appliance console to the browser. This is the
  [BIT-019](../plan/implementation-plan.md#bit-019-setup-token-qr-exchange) setup exception.

This is a Level 1 result only. It does not prove Level 2 on a fresh Debian install, and it does not
prove a literal no-manual-input install while BIT-012 and BIT-019 are open.

## Findings

The successful path still required the owner to interpret failed probes and type values the
appliance already knew. The six findings below are in the order the run exposed them.

### 1. An authentication-required probe reads as a failure

The ONVIF device probe runs before the wizard asks for camera credentials. The camera requires
authentication, so the probe receives HTTP 401 and records the layer as `failed` with an
"authentication required" detail. The candidate remains in the list because another layer found it,
but its summary is only `online` or `seen`, and the failed ONVIF layer appears beside the successful
layers.

The owner cannot tell "the camera was found and needs a login" from "the camera is unreachable or
broken". The RTSP probe treats a 401 as `ok`, while the ONVIF device layer treats it as `failed`, so
the same authentication requirement reads differently depending on the layer.

Evidence: the unauthenticated request and the 401 and 403 handling in
[`probeOnvifDevice`](../../agent/internal/discovery/layers.go), the `online` or `seen` summary in
[`Discover`](../../agent/internal/discovery/discovery.go), and the per-layer failed badge in
[camera.html](../../ui/templates/camera.html).

### 2. Optional fields are only identified in fine print, and discovered fields are not prefilled

The camera form marks the MAC address as optional only inside its hint paragraph. The username and
password fields are also optional for a camera without authentication, but that is explained in
prose rather than marked on the fields. Required fields rely on the HTML `required` attribute and
have no visible required marker. The owner has to read every hint to know what can be left blank.

Discovery does not prefill the fields it can supply. `submitCamera` copies the selected candidate's
address into the form and stores vendor and model on the camera, but the port and path still come
from the form defaults, and the owner sees no profile, stream URI, or snapshot URI from discovery.

Evidence: the optional wording in [camera.html](../../ui/templates/camera.html), the `554` and `/`
defaults in [`formFromCamera`](../../agent/internal/ui/wizard.go), the address-only candidate fill
in [`submitCamera`](../../agent/internal/ui/wizard.go), and the candidate fields in
[discovery.go](../../agent/internal/discovery/discovery.go).

### 3. The RTSP path shows the form default instead of the discovered stream URI

The camera form always starts with RTSP port `554` and path `/`. Selecting a discovered camera does
not replace those values, because the discovery candidate has no profile token, stream URI, or
snapshot URI. The owner has to know the camera's stream path from the manual or another tool, even
though the camera can often report it through ONVIF Media.

Evidence: the `cameraForm{Port: "554", Path: "/"}` default in
[`formFromCamera`](../../agent/internal/ui/wizard.go), the absence of profile and stream fields in
[`Candidate`](../../agent/internal/discovery/discovery.go), and the RTSP path field in
[camera.html](../../ui/templates/camera.html).

### 4. The radio selector above a fully expanded details card is a confusing layout

Each candidate is one large label containing a radio button and the full evidence card. Every
candidate renders all of its layer results whether or not it is selected, and the form sits below
the whole list. The owner selects a camera at the top, then scrolls through long, visually similar
cards to reach the fields they need to complete.

The card layout is not master-detail and not stepwise. Selection and editing are separated by the
full discovery evidence, which makes it easy to lose which camera is selected.

Evidence: the radio and expanded candidate markup in [camera.html](../../ui/templates/camera.html)
and the candidate card layout in [bittern.css](../../ui/static/bittern.css).

### 5. There is no progress or status during probing or applying

Discovery runs synchronously before the camera page is rendered. The owner sees no "searching"
state, no per-layer progress, and no elapsed time. Apply is a blocking POST: the review page holds
the request through validation, rendering, live writes, service restarts, and the health gate, then
redirects or shows an error. There is no operation view to reconnect to after a refresh or a closed
tab.

This is the gap [BIT-014](../plan/implementation-plan.md#bit-014-long-running-operation-progress)
owns. The runbook already warns that the review page can hold the browser request for several
minutes during a cold image pull or slow apply.

Evidence: the synchronous [`showCamera`](../../agent/internal/ui/wizard.go) discovery call, the
blocking [`apply`](../../agent/internal/ui/wizard.go) request, and the "Apply progress" limitation
in the [clean-install runbook](../runbooks/gk41-clean-install.md).

### 6. The single-camera flow does not work for several cameras, and the listing has no add action

The wizard session holds one camera pointer, and the apply request appends at most that one camera.
The camera list can show several cameras from the configuration, but once it is no longer empty the
only actions are edit, test snapshot, and remove. There is no add action on the listing, so the
owner has to return to the first-run setup wizard to add another camera.

That is not a multi-camera inventory. It also conflicts with the MVP definition of four cameras
onboarded from the UI in under 30 minutes.

Evidence: the single `Camera *config.Camera` field and one-camera `applyRequest` in
[wizard.go](../../agent/internal/ui/wizard.go), and the empty-state-only setup link in
[cameras.html](../../ui/templates/cameras.html).

## Product consequence

The protocol path is proven, but the owner path is not. A successful install currently depends on
the owner understanding probe internals, knowing a stream path the camera can report, and returning
to the setup wizard for every additional camera. That is a UX defect under
[ADR 0022](../adr/0022-community-functionality-ux-wins.md): the fix belongs in the smallest
Bittern-specific layer, not in a fork or a new discovery stack.

The consequence for the plan is two tickets:

- [BIT-028](../plan/implementation-plan.md#bit-028-camera-onboarding-ux) makes onboarding
  discovery-first, adds explicit found, authentication-needed, unreachable, and unsupported states,
  collects credentials before validation, prefills discovered values, moves optional fields into an
  advanced disclosure, and replaces the radio-plus-card layout. It also wires probe and apply
  progress through BIT-014 and enforces BIT-020 at phone and desktop widths.
- [BIT-029](../plan/implementation-plan.md#bit-029-multi-camera-inventory-and-add-from-listing)
  keeps the setup wizard as the first-run path, adds an add action to the camera listing that reuses
  the BIT-028 flow, and makes the inventory work for several cameras.

Neither ticket preempts the QEMU clean-install lifecycle, the setup token QR, the responsive work,
or the hardware Level 2 gate. They follow the near-term items because a clean install that still
needs the owner to decode probe failures is not an owner-ready camera path.
