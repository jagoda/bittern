# Copy inventory

- Date: 2026-11-21
- Status: Proposed work list for the rewrite. Every row is a current violation of [copy.md](copy.md)
  with a proposed replacement. The rewrite lands as one change, after
  [BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy)
  decouples the QEMU assertions listed under [test coupling](#test-coupling).
- Applies to: `ui/templates/` and the owner-facing strings in `agent/internal/ui/`. It does not
  change routes, layout, fields, or tests. A row that needs a template or handler change says so
  under [dependencies](#dependencies-on-a-template-or-handler-change).
- Companion documents: [audience.md](audience.md), [copy.md](copy.md), [patterns.md](patterns.md),
  [components.md](components.md), [CHANGELOG.md](CHANGELOG.md).

Line numbers are from commit `2c5e484`. Re-check them before the rewrite; another change is editing
the apply path in the same templates. The order is the first-run flow, then the camera surfaces,
then status, then the shared error pages.

## How to read a row

`Where` is one of:

| Value | Meaning |
| --- | --- |
| Primary | Rewritten in place, visible by default. |
| Disclosure | Moved into a named `details` element that belongs to the task. |
| Technical | Moved into the "Technical details" disclosure, the support bundle, or the status JSON. |
| Remove | Deleted. The object or state is already clear without it. |
| Conditional | Primary copy that depends on the state, so both sentences are written. |

Two line numbers after one path mean the same string or the same fix appears in both places.

## Summary

| Surface | Rows |
| --- | --- |
| Shell and footer | 2 |
| Setup and sign-in | 10 |
| Welcome and network | 7 |
| Recording drive | 7 |
| Camera add and edit | 29 |
| Review | 10 |
| Apply result | 18 |
| Status | 23 |
| Camera list | 14 |
| Remove camera | 3 |
| Shared error pages | 4 |
| Total | 127 |

## Shell and footer

| ID | Location | Current | Proposed | Where |
| --- | --- | --- | --- | --- |
| C-01 | [base.html](../../ui/templates/base.html):39 | "TLS certificate SHA-256 fingerprint: `<value>`" | "Certificate fingerprint" with the value visible; "TLS" and "SHA-256" in the technical line | Primary and Technical |
| C-02 | [base.html](../../ui/templates/base.html):48 | "Preview build: sample data only, no system files are written." | "Preview build: sample data only. Nothing on the appliance changes." | Primary |

## Setup and sign-in

| ID | Location | Current | Proposed | Where |
| --- | --- | --- | --- | --- |
| C-03 | [setup.html](../../ui/templates/setup.html):3 | "First run is locked until you enter the one-time setup token or its short fallback code. Scan the QR shown on the appliance console with this device, or read the token on the appliance terminal with `sudo bittern setup-token --qr`." | "First run is locked until you enter the setup code. Scan the QR on the appliance console with this device, or type the backup code shown there." The command moves to a technical line. | Primary and Technical |
| C-04 | [setup.html](../../ui/templates/setup.html):14 | "One-time setup token or short code" | "Setup code or backup code" | Primary |
| C-05 | [setup.html](../../ui/templates/setup.html):17 | "Scan the console QR on this device, or type the grouped token or the short code from the console. The token is in a root-only file; the short code is single use and expires in 10 minutes." | "Scan the QR on the appliance console with this device, or type the code exactly as the console shows it. A backup code works once and expires after 10 minutes." The file and its permissions move to a technical line. | Primary and Technical |
| C-06 | [setup.html](../../ui/templates/setup.html):25 | "Use at least 8 characters. The appliance stores only a salted PBKDF2-HMAC-SHA256 hash." | "Use at least 8 characters. The appliance never stores the password itself." The algorithm moves to a technical line. | Primary and Technical |
| C-07 | [setup.html](../../ui/templates/setup.html):40 | "The appliance uses a self-signed certificate, so the browser will show a warning before this page loads. That warning is expected. Compare the certificate fingerprint shown by the browser with this value before you continue." | "The appliance uses its own security certificate, so the browser shows a warning before this page loads. That warning is expected. Compare the fingerprint the browser shows with the one below before you continue." | Primary and Technical |
| C-08 | [setup.html](../../ui/templates/setup.html):43 | "SHA-256 fingerprint" | "Fingerprint"; the algorithm is named in the technical line beside it | Primary and Technical |
| C-09 | [setup.html](../../ui/templates/setup.html):47 | "The certificate fingerprint is unavailable on this plain-HTTP connection." | "The fingerprint is not shown on this connection. Open the appliance address with https:// to compare it." | Primary |
| C-10 | [auth.go](../../agent/internal/ui/auth.go):193 | "The setup token or short code is not valid. Check the console block, or run sudo bittern setup-token --qr on the appliance. A short code expires after 10 minutes and can be used once." | "That setup code is not valid. Check the code on the appliance console, or show it again there. A backup code works once and expires after 10 minutes." The command is not repeated in an error. | Primary |
| C-11 | [auth.go](../../agent/internal/ui/auth.go):230 | "This page needs a fresh form token before it can change anything." | "The form expired before it could save anything." | Primary |
| C-12 | [auth.go](../../agent/internal/ui/auth.go):238 | "Bittern could not create a private browser session." and "Restart bitternd and open the appliance address again. Check the journal if it still fails." | "Bittern could not start a private session for this browser." and "Reload the page. If it still fails, restart the appliance." | Primary and Technical |

## Welcome and network

| ID | Location | Current | Proposed | Where |
| --- | --- | --- | --- | --- |
| C-13 | [welcome.html](../../ui/templates/welcome.html):9 | "Connect the house LAN cable to the lan0 port." | "Connect the house network cable to the LAN port." The port label moves to a disclosure. | Primary and Disclosure |
| C-14 | [welcome.html](../../ui/templates/welcome.html):10 | "Connect the camera switch to the cam0 port." | "Connect the camera switch to the camera port." | Primary |
| C-15 | [welcome.html](../../ui/templates/welcome.html):11 | "Have the camera address and its RTSP details ready." | "Have the camera address ready in case Bittern cannot find it." | Primary |
| C-16 | [network.html](../../ui/templates/network.html):7 to 15 | "Links found", caption "Network link status", columns "Link" and "Carrier" | "Connections found", caption "Network connections", columns "Connection" and "Cable" | Primary |
| C-17 | [network.html](../../ui/templates/network.html):20 | "lan0 (house LAN)" | "House network" | Primary |
| C-18 | [network.html](../../ui/templates/network.html):25 | "cam0 (camera network)" | "Camera network"; "The appliance labels its ports lan0 and cam0" in the disclosure | Primary and Disclosure |
| C-19 | [network.html](../../ui/templates/network.html):32, from [checks.go](../../agent/internal/ui/checks.go):48,152,169 | "The network check is degraded. lan0 has 192.168.1.50/24 (carrier up). cam0 has 10.77.0.1/24 (carrier up)." and "The network status is unavailable (unknown): `<reason>`." | "House network: address 192.168.1.50, cable connected. Camera network: address 10.77.0.1, cable connected." On failure: "Bittern could not check the network. Open status for the reason, then try again." | Primary and Technical |

## Recording drive

| ID | Location | Current | Proposed | Where |
| --- | --- | --- | --- | --- |
| C-20 | [storage.html](../../ui/templates/storage.html):3 to 4, [base.html](../../ui/templates/base.html):55 | Heading "Storage check" and stepper label "Storage"; "Recordings and camera snapshots belong on a dedicated drive, separate from the operating system." | Heading "Recording drive" and stepper label "Recording"; "Recordings belong on their own drive, separate from the one the appliance runs from." | Primary |
| C-21 | [storage.html](../../ui/templates/storage.html):9 | "Media path" and `/var/lib/bittern/media` | "Drive" and "Connected" or "Not found"; the path moves to the technical line | Primary and Technical |
| C-22 | [storage.html](../../ui/templates/storage.html):11 | "Drive present" | "Drive connected" | Primary |
| C-23 | [storage.html](../../ui/templates/storage.html):15 | "Free-space reserve" | "Space kept free" | Primary |
| C-24 | [storage.html](../../ui/templates/storage.html):18, from [checks.go](../../agent/internal/ui/checks.go):69 to 92 | "The recording drive is attached but /var/lib/bittern/media is not mounted. ... Check the fstab line and the mount, then rerun storage-setup.sh." and the attached, write-test, and pool sentences | Plain sentences: "The recording drive is connected but Bittern cannot write to it. Recording is stopped. Check the drive, then run the storage setup again." The mount, fstab, script, and pool move to the technical line. | Primary and Technical |
| C-25 | [checks.go](../../agent/internal/ui/checks.go):163 | "The pool reports 1 member." and "The pool reports `<n>` members." | "Bittern sees one recording drive." and "Bittern sees `<n>` recording drives." | Primary |
| C-26 | [wizard.go](../../agent/internal/ui/wizard.go):77 | "The recording drive is separate from the operating system and has room for new recordings." | "The recording drive is separate from the one the appliance runs from and has room for new recordings." This is preview data and appears in screenshots. | Primary |

## Camera add and edit

| ID | Location | Current | Proposed | Where |
| --- | --- | --- | --- | --- |
| C-27 | [camera.html](../../ui/templates/camera.html):7 | "Choose a camera Bittern found on cam0, or type its address yourself." | "Choose a camera Bittern found on the camera network, or type its address yourself." | Primary |
| C-28 | [camera.html](../../ui/templates/camera.html):8 to 11 | Summary "How discovery works"; "Bittern checks the cam0 DHCP lease and ARP table, asks ONVIF devices to identify themselves, and tests whether the camera answers on RTSP. It never sends a password." | Summary "How Bittern finds cameras"; the lease, ARP, ONVIF, and RTSP sentences move to technical details. "Bittern never sends the camera password." stays visible. | Disclosure and Technical |
| C-29 | [camera.html](../../ui/templates/camera.html):22 | "Discovery did not complete" | "Bittern could not search the camera network" | Primary |
| C-30 | [camera.html](../../ui/templates/camera.html):23, from [wizard.go](../../agent/internal/ui/wizard.go):634 | "Discovery on cam0 did not finish: `<error>`. You can still enter the camera address by hand." | "The search on the camera network did not finish. You can still type the camera address below." The raw error moves to the technical line. | Primary and Technical |
| C-31 | [camera.html](../../ui/templates/camera.html):27 | Busy label "Discovering..." | "Searching..." | Primary |
| C-32 | [camera.html](../../ui/templates/camera.html):29 | "Discover cameras again" | "Search again" | Primary |
| C-33 | [camera.html](../../ui/templates/camera.html):34 | "Cameras found on cam0" | "Cameras found" | Primary |
| C-34 | [camera.html](../../ui/templates/camera.html):35 | "Discovery ran a moment ago, so this is the previous result." | "Bittern searched a moment ago, so this is the previous result." | Primary |
| C-35 | [camera.html](../../ui/templates/camera.html):36 | "Choose the camera to add, then use this camera to fill its address and MAC." | "Choose the camera to add, then use this camera to fill in its address." | Primary |
| C-36 | [camera.html](../../ui/templates/camera.html):46,48 | The hardware address in the candidate row, and "via dhcp lease, onvif" | Both move inside the candidate's technical details. Keep the address, the state, and the vendor or model visible. | Technical |
| C-37 | [camera.html](../../ui/templates/camera.html):46 | "MAC not seen" | Removed; the absence of a hardware address is not a finding in the row | Remove |
| C-38 | [camera.html](../../ui/templates/camera.html):52 | Summary "Discovery evidence" | Summary "Technical details" | Technical |
| C-39 | [camera.html](../../ui/templates/camera.html):70 | "No camera answered on cam0. Connect the camera to cam0 and discover again, or type its address below." | "No camera answered on the camera network. Connect it to the camera port and search again, or type its address below." | Primary |
| C-40 | [camera.html](../../ui/templates/camera.html):93 | "The IPv4 address on the camera network." and "Choose a camera above, or type its IPv4 address on the camera network." | "The address on the camera network, for example 10.77.0.20." and "Choose a camera above, or type its address on the camera network." | Primary |
| C-41 | [camera.html](../../ui/templates/camera.html):101 to 102 | "RTSP port" and "Usually 554." | "Stream port" and "Usually 554. Change it only when the camera manual gives another port." | Primary |
| C-42 | [camera.html](../../ui/templates/camera.html):107 to 108 | "RTSP path" and "Usually /, or the path from the camera manual." | "Stream path" and "Usually /. The camera manual gives the path when it is not /." | Primary |
| C-43 | [camera.html](../../ui/templates/camera.html):113 | Summary "MAC address and sign-in" | Summary "Hardware address and sign-in" | Primary |
| C-44 | [camera.html](../../ui/templates/camera.html):115 | "Optional. Add the MAC only when discovery did not see it, and the camera account only when the camera needs one." | "Optional. Add the hardware address only when Bittern did not find the camera, and the camera sign-in only when the camera needs one." | Primary |
| C-45 | [camera.html](../../ui/templates/camera.html):119 | "Camera MAC address" | "Hardware address (MAC)" | Disclosure |
| C-46 | [camera.html](../../ui/templates/camera.html):120 | "Only needed when discovery did not see it and the address is in the DHCP pool." | "Only needed when Bittern did not find the camera and its address is in the range Bittern hands out." | Primary |
| C-47 | [wizard.go](../../agent/internal/ui/wizard.go):483 | "Enter a MAC address such as aa:bb:cc:dd:ee:50, or leave this blank to use discovery." | "Enter the hardware address, for example aa:bb:cc:dd:ee:50, or leave it blank and let Bittern find the camera." | Primary |
| C-48 | [wizard.go](../../agent/internal/ui/wizard.go):497 | "Enter the camera IPv4 address, for example 10.77.0.20." | "Enter the camera address on the camera network, for example 10.77.0.20." | Primary |
| C-49 | [wizard.go](../../agent/internal/ui/wizard.go):499 | "Enter an IPv4 address such as 10.77.0.20." | "Enter an address such as 10.77.0.20, with no letters or spaces." | Primary |
| C-50 | [wizard.go](../../agent/internal/ui/wizard.go):501 | "Use the camera address on the camera network, not a loopback, multicast, or unspecified address." | "Use the camera's own address on the camera network." | Primary |
| C-51 | [wizard.go](../../agent/internal/ui/wizard.go):506 | "Enter the RTSP port, usually 554." | "Enter the stream port, usually 554." | Primary |
| C-52 | [wizard.go](../../agent/internal/ui/wizard.go):508 | "Enter a port from 1 to 65535, usually 554." | "Enter a port number from 1 to 65535, usually 554." | Primary |
| C-53 | [wizard.go](../../agent/internal/ui/wizard.go):515 | "Enter the RTSP path, beginning with a slash. Use / when the camera has no path." | "Enter the stream path, beginning with /. Use / when the camera manual gives no path." | Primary |
| C-54 | [wizard.go](../../agent/internal/ui/wizard.go):517 | "Start the RTSP path with a slash, for example /stream1." | "Start the stream path with /, for example /stream1." | Primary |
| C-55 | [wizard.go](../../agent/internal/ui/wizard.go):542 | "This address is inside the DHCP pool, so Bittern needs its MAC for a reservation. Enter the camera MAC in advanced stream settings, choose a discovered camera, or use a static address outside the pool." | "This address is in the range Bittern hands out automatically, so Bittern needs the camera's hardware address to keep it fixed. Choose a camera Bittern found, enter the hardware address, or use an address outside that range." | Primary and Disclosure |

## Review

| ID | Location | Current | Proposed | Where |
| --- | --- | --- | --- | --- |
| C-56 | [review.html](../../ui/templates/review.html):15 | "Configuration file" and `/etc/bittern/appliance.yaml` | "Configuration" and "Not saved yet" or "Saved on the appliance"; the path moves to the technical line | Primary and Technical |
| C-57 | [review.html](../../ui/templates/review.html):19 to 20 | "House LAN (lan0)" and "DHCP, currently 192.168.1.50/24" | "House network" and "Automatic address, currently 192.168.1.50" | Primary |
| C-58 | [review.html](../../ui/templates/review.html):21 to 22 | "Camera network (cam0)" | "Camera network" | Primary |
| C-59 | [review.html](../../ui/templates/review.html):23 to 24 | "Recording storage" and `/var/lib/bittern/media` | "Recording drive" and "Separate drive"; the path moves to the technical line | Primary and Technical |
| C-60 | [review.html](../../ui/templates/review.html):32 | "Identifier" and `camera_1` | Moved to the technical line; the camera name is the identifier the owner uses | Technical |
| C-61 | [review.html](../../ui/templates/review.html):36,38 | "RTSP port" and "RTSP path" | "Stream port" and "Stream path" | Primary |
| C-62 | [review.html](../../ui/templates/review.html):40 to 41 | "MAC address" | Moved to the technical line | Technical |
| C-63 | [review.html](../../ui/templates/review.html):42 to 43 | "DHCP reservation" and "A dhcp-host entry pins 10.77.0.20 to aa:bb:cc:dd:ee:50 on the camera network." | "Address reservation" and "Bittern keeps 10.77.0.20 for this camera on the camera network." | Primary and Technical |
| C-64 | [review.html](../../ui/templates/review.html):44 to 45 | "Credentials" | "Sign-in" | Primary |
| C-65 | [review.html](../../ui/templates/review.html):53 | "Applying asks the appliance agent to validate, render, and write this configuration. The preview build stops at the apply interface and writes no files." | "Bittern checks the configuration, saves it, and starts the recording service. If a check fails, nothing changes." The preview sentence moves to a technical line, and only in a preview build. | Primary and Technical |

## Apply result

| ID | Location | Current | Proposed | Where |
| --- | --- | --- | --- | --- |
| C-67 | [done.html](../../ui/templates/done.html):3, from [wizard.go](../../agent/internal/ui/wizard.go):361 | "Setup complete" in every state, including a failed camera check | Conditional. All checks passed: "Setup complete". Any check failed: "Setup finished with a problem". | Conditional |
| C-68 | [done.html](../../ui/templates/done.html):4 | "The configuration was accepted. The appliance can now continue with its normal startup checks." | "Bittern saved the configuration and started the recording service. The checks below show what it found." | Primary |
| C-69 | [done.html](../../ui/templates/done.html):11 | "Frigate returned a picture from camera_1 (image/jpeg, 48213 bytes)." | "Front door is sending pictures." The component, the content type, the size, and the identifier move to the technical line. | Primary and Technical |
| C-70 | [done.html](../../ui/templates/done.html):14 | "camera_1 has not delivered a picture yet" | "Front door has not sent a picture yet" | Primary |
| C-71 | [done.html](../../ui/templates/done.html):15 | "`<error>`. The configuration is applied; check the camera power, the network link, and the RTSP path, then run setup again." | "The configuration is applied. Check the camera power, the cable, and the stream path, then run setup again." The raw error moves to the technical line. | Primary and Technical |
| C-72 | [done.html](../../ui/templates/done.html):24 | "Open the status page to see the appliance state and its sections." | "Open status to see whether the appliance and its recordings are healthy." | Primary |
| C-73 | [done.html](../../ui/templates/done.html):26 | "Return to setup if you need to change the camera details." | "Change a camera from the camera list." The link points at the camera list. | Primary |
| C-74 | [done.html](../../ui/templates/done.html):31 | "Back to setup" | Removed; after apply the wizard is not the edit surface | Remove |
| C-75 | [snapshot.go](../../agent/internal/ui/snapshot.go):76 | "Frigate did not return a camera picture yet" | Moved to the technical line | Technical |
| C-76 | [snapshot.go](../../agent/internal/ui/snapshot.go):90 | "the camera check ran out of time before Frigate delivered a picture" | Moved to the technical line | Technical |
| C-77 | [snapshot.go](../../agent/internal/ui/snapshot.go):101 | "the camera check request could not be built" | Moved to the technical line | Technical |
| C-78 | [snapshot.go](../../agent/internal/ui/snapshot.go):106 | "Frigate is not reachable on the appliance loopback address yet" | Moved to the technical line | Technical |
| C-79 | [snapshot.go](../../agent/internal/ui/snapshot.go):111 | "Frigate answered the camera check with HTTP `<code>`" | Moved to the technical line | Technical |
| C-80 | [snapshot.go](../../agent/internal/ui/snapshot.go):116 | "Frigate answered the camera check without an image" | Moved to the technical line | Technical |
| C-81 | [snapshot.go](../../agent/internal/ui/snapshot.go):121,125 | "the camera picture could not be read" and "Frigate returned an empty or oversized camera picture" | Moved to the technical line | Technical |
| C-82 | [wizard.go](../../agent/internal/ui/wizard.go):430 | "apply is not implemented. Nothing on the appliance has changed. The apply pipeline arrives with BIT-011; try again after that update." | "This build cannot apply a configuration yet. Nothing on the appliance has changed." The ticket and the pipeline move to the technical line. | Primary and Technical |
| C-83 | [wizard.go](../../agent/internal/ui/wizard.go):436 | "The appliance reported: `<error>`. The apply transaction did not commit; a previous generation was restored when one existed. Check the status page, then try again." | "The configuration was not applied. The previous configuration is still running. Check status, then try again." The raw cause and the transaction terms move to the technical line. | Primary and Technical |
| C-84 | [wizard.go](../../agent/internal/ui/wizard.go):376 | "The setup session could not start", "Bittern could not create a private browser session.", "Reload this page. If it still fails, restart bitternd and open setup again." | "Bittern could not start a private session for this browser." and "Reload the page. If it still fails, restart the appliance and open setup again." | Primary and Technical |

## Status

| ID | Location | Current | Proposed | Where |
| --- | --- | --- | --- | --- |
| C-85 | [status.html](../../ui/templates/status.html):3 | "This page shows the read-only status document from the appliance agent. Unknown values stay unknown until a check reports them." | "This page shows what the appliance last checked. A value stays unknown until a check reports it." | Primary |
| C-86 | [status_panel.html](../../ui/templates/status_panel.html):27 to 34 | "Path", "File present", "Schema version", "Valid" and their values | "Configuration saved" and "Checked" in primary; the path, the schema version, and the raw validity move to the technical line | Primary and Technical |
| C-87 | [status_panel.html](../../ui/templates/status_panel.html):39 to 53 | Caption "Configuration errors", columns "Path", "Rule", "Message" | Caption "Configuration problems", columns "Setting", "Problem", "Detail" | Primary and Technical |
| C-88 | [status_panel.html](../../ui/templates/status_panel.html):66 | "Render and validation" | "Configuration files" | Primary |
| C-89 | [status_panel.html](../../ui/templates/status_panel.html):74 | "Generation" | "Saved version" | Primary |
| C-90 | [status_panel.html](../../ui/templates/status_panel.html):76 | "Files rendered" | "Files written" | Primary |
| C-91 | [status_panel.html](../../ui/templates/status_panel.html):78 | "Last render" | "Last written" | Primary |
| C-92 | [status_panel.html](../../ui/templates/status_panel.html):83 | "In-memory render checks" | "Configuration file checks" | Primary |
| C-97 | [status_panel.html](../../ui/templates/status_panel.html):139 | "Media path" and `/var/lib/bittern/media` | "Recording drive" and "Connected"; the path moves to the technical line | Primary and Technical |
| C-98 | [status_panel.html](../../ui/templates/status_panel.html):146 | "Pool members known" and "Missing members" | "Drives known" and "Drives missing" | Primary |
| C-99 | [status_panel.html](../../ui/templates/status_panel.html):152 | "The UI, network, and camera management still work; recording is stopped. Connect the drive and reboot, then check storage again." | "The web pages, the network, and camera management still work; recording is stopped. Connect the drive and restart the appliance, then check again." | Primary |
| C-100 | [status_panel.html](../../ui/templates/status_panel.html):154 | "The recording drive is attached but `/var/lib/bittern/media` is not mounted. ... Check the fstab line and the mount, then rerun `storage-setup.sh`." | "The recording drive is connected but Bittern cannot write to it. Recording is stopped. Check the drive, then run the storage setup again." The mount and the script move to the technical line. | Primary and Technical |
| C-101 | [status_panel.html](../../ui/templates/status_panel.html):156 | "The recording media is mounted but failed its write test. ... recording fails until the drive is repaired or replaced." | "The recording drive is connected but Bittern could not write to it. Recording fails until the drive is repaired or replaced." | Primary |
| C-102 | [status_panel.html](../../ui/templates/status_panel.html):173 to 179 | Caption "Interface status", column "Interface" with lan0 and cam0, column "Carrier" | "Network status", "Connection", "Cable" | Primary |
| C-103 | [status_panel.html](../../ui/templates/status_panel.html):207 | "Current slot" | "Installed version" | Primary |
| C-104 | [status_panel.html](../../ui/templates/status_panel.html):209 | "Bundle generation" | "Update"; the bundle and the generation move to the technical line | Primary and Technical |
| C-105 | [status_panel.html](../../ui/templates/status_panel.html):215 to 223 | "Reset", "Last reset level", "Completed at" | "Reset", "Last reset", "Completed"; the level word moves to the technical line | Primary and Technical |
| C-106 | [status_panel.html](../../ui/templates/status_panel.html):233 to 242 | "CPU", "Cores", "Memory", "Accelerators" and their values | A one-line summary in primary; the processor model, cores, and accelerators move to the technical line | Primary and Technical |
| C-107 | [status_panel.html](../../ui/templates/status_panel.html):244 | "Hardware detection did not report on this build." | "This build did not report the hardware." | Primary |
| C-108 | [status.go](../../agent/internal/ui/status.go):30,155 | "The daemon is starting and has not completed its first status check." and "An update transaction is running. Status checks resume when it finishes." | "Bittern is starting and has not finished its first check." and "An update is running. Checks resume when it finishes." | Primary |
| C-110 | [status.go](../../agent/internal/ui/status.go):161 | "Configuration is invalid: network.lan0.subnet must be an IPv4 CIDR." | "The configuration has a problem: the house network address is not valid." The key and the format move to the technical line. | Primary and Technical |
| C-111 | [status.go](../../agent/internal/ui/status.go):24 | "The status builder is not configured yet." | "Status is not available yet." | Primary |
| C-112 | [checks.go](../../agent/internal/ui/checks.go):169 to 176 | "The `<section>` status is unavailable (`<state>`): `<reason>`." | Moved to the technical line; primary says "Bittern could not check this section." | Technical |

## Camera list

| ID | Location | Current | Proposed | Where |
| --- | --- | --- | --- | --- |
| C-113 | [cameras.html](../../ui/templates/cameras.html):3 | "The cameras Bittern manages. This list reads the appliance status contract; a snapshot test runs only when you ask for it." | "The cameras Bittern manages. Bittern tests a camera only when you ask it to." | Primary |
| C-114 | [cameras.html](../../ui/templates/cameras.html):9 to 11 | "No cameras yet", "Run setup to discover a camera on the camera network, or add one by address.", action "Open setup" | "No cameras yet", "Bittern has no camera to record. Add one and it appears here.", action "Add camera" | Primary |
| C-115 | [cameras.html](../../ui/templates/cameras.html):34 | "Frigate degraded" | "Recording degraded" | Primary |
| C-116 | [cameras.html](../../ui/templates/cameras.html):44 | "Test snapshot" | "Test picture" | Primary |
| C-117 | [cameras.html](../../ui/templates/cameras.html):49 | Summary "Details" | Summary "Technical details" | Technical |
| C-118 | [cameras.html](../../ui/templates/cameras.html):54 | "Stream unknown" | "Stream not checked" | Primary |
| C-119 | [cameras.html](../../ui/templates/cameras.html):56 | "Recording unknown" | "Recording not checked" | Primary |
| C-120 | [cameras.html](../../ui/templates/cameras.html):59 | "Last snapshot" | "Last picture" | Primary |
| C-121 | [cameras.html](../../ui/templates/cameras.html):61 | "MAC" and the value, or "Not recorded" | "Hardware address" and the value, inside the technical details | Technical |
| C-122 | [cameras.go](../../agent/internal/ui/cameras.go):320 | "The camera change was applied. The list below reflects the new configuration." | "The camera change was applied." | Primary |
| C-123 | [cameras.go](../../agent/internal/ui/cameras.go):322 | "The camera was removed and its saved credential was deleted." | "The camera was removed and its saved sign-in was deleted." | Primary |
| C-124 | [cameras.go](../../agent/internal/ui/cameras.go):324 | "The snapshot test succeeded. The camera returned a picture through Frigate." | "The picture test passed. The camera sent a picture." | Primary |
| C-125 | [cameras.go](../../agent/internal/ui/cameras.go):326 | "The snapshot test failed. The row below shows the reason Frigate reported." | "The picture test failed. The camera's row shows why." | Primary |
| C-126 | [view.go](../../agent/internal/ui/view.go):136 | "Frigate degraded" | "Recording degraded" | Primary |

## Remove camera

| ID | Location | Current | Proposed | Where |
| --- | --- | --- | --- | --- |
| C-127 | [camera-remove.html](../../ui/templates/camera-remove.html):18 | "Removing the camera applies a new configuration without it, stops its address reservation and video stream, and deletes its saved credential. Recordings already on disk are not deleted." | "Removing `<camera>` stops its recording and live view, releases the address Bittern reserved for it, and deletes the sign-in saved on the appliance. Recordings already on the drive are kept." | Primary |
| C-128 | [cameras.go](../../agent/internal/ui/cameras.go):165 to 167 | "The credential could not be deleted", "The camera was removed from the configuration, but its saved credential is still in /etc/bittern/secrets.yaml.", "Remove that camera key from /etc/bittern/secrets.yaml by hand, then reload the camera list." | "The camera was removed but its sign-in was not deleted", "The camera is no longer in the configuration, but the sign-in saved for it is still on the appliance.", and a next step that does not name a file. See [finding F-2](#findings). | Primary and Technical |
| C-129 | [cameras.go](../../agent/internal/ui/cameras.go):189 | "That camera is not in the appliance configuration." and "Open the camera list and choose a camera that is still configured." | "That camera is no longer in the configuration. Open the camera list and choose another camera." | Primary |

## Shared error pages

| ID | Location | Current | Proposed | Where |
| --- | --- | --- | --- | --- |
| C-130 | [ui.go](../../agent/internal/ui/ui.go):219 | "The page could not be rendered. Restart bitternd and try again." | "Bittern could not build this page. Restart the appliance and try again." | Primary and Technical |
| C-131 | [ui.go](../../agent/internal/ui/ui.go):243 | "That page is not part of the setup flow." and "Open setup to continue, open status to see the appliance state, or check the address in the browser." | "That page is not part of Bittern." and "Open status to see the appliance, or check the address in the browser." | Primary |
| C-132 | [wizard.go](../../agent/internal/ui/wizard.go):240 | "The camera details did not arrive in a usable form." and "Return to the camera step, check the fields, and submit the form again." | "The camera details did not reach the appliance. Nothing was saved." and "Go back to the camera step, check the details, and send the form again." | Primary |
| C-133 | [applier.go](../../agent/internal/ui/applier.go):46,78,85,94,100 and [camera_secrets.go](../../agent/internal/ui/camera_secrets.go):35,55,75,90,93,144 | "ui: cannot load /etc/bittern/appliance.yaml: ...", "ui: encode appliance.yaml: ...", "ui: the setup changes are not valid: ...", "ui: write appliance.yaml: ...", and the "ui: camera ... needs an RTSP URL with credentials at secret key ..." family | Never shown in primary copy. The plain sentence is C-83; these strings move to the technical line and the support bundle, with the `ui:` prefix removed and no secret path or key in the message. | Technical |

## Findings

The rules cannot fix these with copy alone. Each one is recorded here rather than invented in a
template. The note for this audit is
[2026-11-21 audience and copy audit](../notes/2026-11-21-audience-and-copy-audit.md).

| ID | Finding | Evidence | Destination |
| --- | --- | --- | --- |
| F-1 | There is no screen that changes the house network or the camera network after setup. The review shows both as read-only facts, and the apply request always writes DHCP. | [wizard.go](../../agent/internal/ui/wizard.go):419 (`LANMode: "DHCP"`) and [review.html](../../ui/templates/review.html):19 to 22 | Product scope. No v1 ticket; ask before building a settings surface. |
| F-2 | An orphaned camera sign-in has no automatic cleanup. When the secret delete fails, the error page asks the owner to edit a YAML file by hand. | [cameras.go](../../agent/internal/ui/cameras.go):165 to 167 | Needs a retry or cleanup action; copy alone cannot remove the manual step. |
| F-3 | Showing the setup code again needs a shell command on the console. The primary path depends on `sudo bittern setup-token --qr`. | [setup.html](../../ui/templates/setup.html):3 and [auth.go](../../agent/internal/ui/auth.go):193 | Needs the console to keep showing the code, or a re-issue action in the UI. Until then the command stays in technical details. |
| F-4 | [patterns.md](patterns.md) says the required stream port and path sit inside "Advanced settings" whose hint says "Optional". The current form keeps them outside the disclosure, and the disclosure is "MAC address and sign-in". | [camera.html](../../ui/templates/camera.html):98 to 140, the edit-form section in [patterns.md](patterns.md), and the fieldset and retire-list entries in [components.md](components.md) | Correct the three stale claims with the rewrite; do not move required fields back into a disclosure. |
| F-5 | The done screen sends the owner back to setup after apply, against pattern rule W7. | [done.html](../../ui/templates/done.html):26,31 | [BIT-036](../plan/implementation-plan.md#bit-036-first-run-and-flow-contradictions-from-the-flow-layer) owns the flow; the rewrite removes the labels only with that ticket. |
| F-6 | The firstboot QEMU assertion for the storage sentence already matches prose that no longer exists, so the scenario cannot pass on the current tree. | [run.py](../../tests/qemu/run.py):2933, against [checks.go](../../agent/internal/ui/checks.go):75, changed in `2c5e484` | [BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy) is the prerequisite; this is the coupling, already live. |
| F-7 | Hiding the media path removes the only identifier of which drive is in use. The status document has no drive model, label, or serial number to show instead. | [status_panel.html](../../ui/templates/status_panel.html):139 and the `StorageSection` fields in [agent/internal/status/status.go](../../agent/internal/status/status.go):156 to 166; `path` is the only drive identifier | Needs a drive label in the status contract, or the path stays visible. Ask before changing the contract. |

## Dependencies on a template or handler change

These rows are copy, but the sentence cannot be true without one small non-copy change. Keep them in
the same rewrite so the copy does not describe a state the page cannot show.

| Row | Change |
| --- | --- |
| C-01 | Needs a technical details block on the status page, or one in the footer, to hold the fingerprint. The value is not deleted. |
| C-67 | The done title needs the snapshot result. All succeeded: "Setup complete"; any failed: "Setup finished with a problem". |
| C-69, C-70 | The camera name replaces the identifier. `SnapshotResult` carries only `CameraID`, so the handler must add the name or the template must map it. |
| C-114 | The empty state's action points at the camera step (`/ui/wizard/camera`), not at the wizard welcome page. The route exists. |
| C-21, C-56, C-59, C-97 | The path value moves into the technical details. The plain value is a state word, not a path. |
| C-36, C-38, C-117 | The existing `details` elements are renamed to "Technical details" and extended, not duplicated. |
| C-73, C-74 | The done screen stops sending the owner back to setup. The link target and the label change together, and wait for BIT-036. |
| C-104 | A second label replaces one word; the value does not change. |

## Test coupling

[BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy) is the
prerequisite for this rewrite. The QEMU scenarios assert rendered sentences, so an editorial change
fails the suite and looks like a product regression. Do not edit these assertions in the rewrite;
land the stable ids and the value lookups first, then change the copy. The principle is EN-2 in
[principles.md](../principles.md#en-2-tests-assert-structure-and-values-not-user-facing-copy).

The wizard rows below are now read through the hooks in
[the QEMU README](../../tests/qemu/README.md#stable-data-hooks); the remaining QEMU rows and the Go
rows are still open and are inventoried in
[the wizard note](../notes/2026-11-21-qemu-copy-assertions.md#remaining-copy-coupled-assertions).

The table lists every assertion site that matches owner-facing prose, and says whether the rewrite
changes it. Values such as addresses, MACs, camera names, and routes are omitted because the
rewrite does not change them.

| Assertion | Matches | Copy rows | What the rewrite changes |
| --- | --- | --- | --- |
| [run.py](../../tests/qemu/run.py):2915 | "Welcome to Bittern" | none | The heading stays; listed for the firstboot flow |
| [run.py](../../tests/qemu/run.py):2919 | "Network check" | C-16 to C-19 | The heading stays; the body copy around it changes |
| [run.py](../../tests/qemu/run.py):2926 | "carrier up" | C-19 | The sentence becomes "cable connected" |
| [run.py](../../tests/qemu/run.py):2930 | "Storage check" | C-20 to C-24 | The heading becomes "Recording drive" |
| [run.py](../../tests/qemu/run.py):2933 | "The recording drive is present", "The recording drive at" | C-24 | Already stale: both strings were removed in `2c5e484`, so this assertion cannot pass today |
| [run.py](../../tests/qemu/run.py):2935 | "The recording drive is separate from the operating system" | C-26 | "separate from the one the appliance runs from" |
| [run.py](../../tests/qemu/run.py):2939 | "Add a camera" | none | The heading stays |
| [run.py](../../tests/qemu/run.py):2957 | "Setup complete" | C-67 | Conditional on the check result |
| [run.py](../../tests/qemu/run.py):4565 | "Welcome to Bittern" | none | Adoption flow, same heading |
| [run.py](../../tests/qemu/run.py):4569 | "Network check", "carrier up" | C-16 to C-19 | Same as the firstboot rows; the heading stays |
| [run.py](../../tests/qemu/run.py):4573 | "Storage check" | C-20 to C-24 | The heading becomes "Recording drive", same as the firstboot row |
| [run.py](../../tests/qemu/run.py):4585 to 4588 | "dhcp lease", "No ONVIF XAddr", "RTSP OPTIONS" | C-28, C-36, C-38 | The layer evidence moves inside "Technical details"; the strings come from discovery, not the template |
| [run.py](../../tests/qemu/run.py):4594 | "Discovery did not complete" | C-29 | "Bittern could not search the camera network" |
| [run.py](../../tests/qemu/run.py):4607,4608 | "Online", "ws-discovery" | C-36 | "Online" stays; the source moves inside technical details |
| [run.py](../../tests/qemu/run.py):4636,4637 | "DHCP reservation", "Stored on the appliance when the configuration is applied" | C-63, C-64 | "Address reservation" and the sign-in sentence |
| [run.py](../../tests/qemu/run.py):4647 | "Setup complete" | C-67 | Conditional on the check result |
| [run.py](../../tests/qemu/run.py):4649 | "Frigate returned a picture" | C-69 | "is sending pictures" |
| [run.py](../../tests/qemu/run.py):4668,4669,4670 | "Picture received", "Recording", "Enabled" | none | All three stay; the list's unknown labels change under C-118 and C-119 |
| [run.py](../../tests/qemu/run.py):4679 | "Edit camera", "Apply camera change" | C-40 to C-42, C-61 | The heading and the button stay; the field labels change around them |
| [run.py](../../tests/qemu/run.py):4693 | "Picture received" | none | The badge stays; the success flash changes under C-124 |
| [run.py](../../tests/qemu/run.py):4701 | "The snapshot test succeeded", "Picture received" | C-124 | "The picture test passed" |
| [run.py](../../tests/qemu/run.py):4747 | "Snapshot failed", "Not streaming" | none | Both stay; the unknown labels change under C-118 and C-119 |
| [run.py](../../tests/qemu/run.py):4750 | "The snapshot test failed" | C-125 | "The picture test failed" |

That is 23 assertion sites and 27 distinct prose strings. Four sites (2915, 2939, 4565, 4679)
match headings the rewrite keeps. The other 19 match prose the rewrite changes or relocates;
several keep passing only because the string survives inside the new technical details, which is
the accident BIT-035 removes.

## What this inventory does not change

- Routes, redirects, and field names. The form posts `rtsp_port` and `rtsp_path`; only the labels
  change.
- The status JSON contract. The section keys stay `frigate`, `render`, and `storage`; only the
  labels change.
- A secret, a password, a setup code, or a backup code. No row adds one to a screen, a URL, or a
  log.
- Product scope. The findings above are recorded, not built.

Related: [audience.md](audience.md), [copy.md](copy.md), [patterns.md](patterns.md),
[components.md](components.md),
[BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy),
[the audit note](../notes/2026-11-21-audience-and-copy-audit.md).
