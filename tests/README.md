# tests

`tests/` is the home of the test strategy and its harnesses. It covers golden renders for every
template, a QEMU two-NIC boot harness, virtual cameras for onboarding and recording, update and
power-cut tests, hardware-in-the-loop soak tests, and the release gates that must pass before an
image or bundle is published. The release-gate details are defined in ADR 0012.

## Boundaries

In scope:

- Schema fixtures and expected outcomes for `appliance.yaml`, with a reference validator
- Fixtures and expected output for golden configuration renders
- QEMU images and scripts for a two-NIC appliance boot
- Virtual ONVIF/RTSP cameras and network isolation assertions
- Update, rollback, power-cut, and hardware-in-the-loop soak tests
- Release gates: Frigate advisories, CVE and SBOM checks, signatures, reproducibility

Out of scope:

- Frigate's own upstream test suite, which stays upstream
- Application code under test, which lives in the component directories
- Performance benchmarking of Frigate beyond the modest appliance targets
- Production monitoring and telemetry collection

Status: local checks, the BIT-016 POC cleanup test, schema version 1 and 2 fixtures with the v1
to v2 migration, the BIT-003
golden render suite including Frigate config, compose and unit output, the BIT-011 apply and
first-boot tests, the QEMU two-NIC harness, and the Frigate fixture bundle exist. The fixture config
and compose files are still hand-written, while its firewall is the renderer golden plus the
harness-only SSH rule. The renderer now has its own CPU, POC, Hailo plus QSV, and no-camera goldens;
the QEMU harness has not been switched to renderer output. The QEMU harness and the fixture bundle
were executed on 2026-11-14 in a KVM-enabled container; the results are in
[the QEMU harness note](../docs/notes/2026-11-14-qemu-harness.md). Virtual cameras for onboarding,
update tests, and hardware tests are not implemented yet.

Schema inputs and expected outcomes are in [tests/schema/cases.json](schema/cases.json), and the
reference validator is [tests/checks/validate_config.py](checks/validate_config.py). Its contract is
in [schema/validation.md](../schema/validation.md). The unit-enablement check in
[checks/unit_enablement.py](checks/unit_enablement.py) compares the units `install.sh` enables or
starts and apply renders with the units the QEMU harness asserts enabled after a reboot; it runs in
`make check` and fails naming the missing unit. The design check in
[checks/design_check.py](checks/design_check.py) parses `ui/static/*.css` and fails on a colour
literal or a raw `px` or `rem` value outside `:root`, on `text-transform: uppercase`, and on
`!important`; it runs in `make check` and fails closed on an unreadable or unparseable file. The
QEMU harness is documented in [qemu/README.md](qemu/README.md) and the Frigate fixture in
[fixtures/frigate/README.md](fixtures/frigate/README.md). The cleanup test in
[cleanup/test_cleanup.py](cleanup/test_cleanup.py) runs `packaging/cleanup.sh` against a temporary root
with fake systemd, Docker, nftables, apt-get and mount commands. It proves plan-only mode, the chrony
and nftables stock conffile restores, binary removal, conffile restore failure, the first cleanup,
the mount guard, the media purge confirmation, ownership refusal, and that a second run reports
already clean and changes nothing. The storage test in
[storage/test_storage_setup.py](storage/test_storage_setup.py) runs
`packaging/storage-setup.sh` against captured `lsblk` fixtures and fake mount, findmnt, blkid and
mkfs commands. It proves disk and partition discovery, zero-touch selection for one ext4 device
labelled `bittern-media`, interactive and non-interactive selection, typed format confirmation, the
fstab rewrite and verification, the optional `blkid` cross-check, and the stable `--list --json`
inventory. It never touches a real block device or runs a real mkfs.

The renderer contract is the golden suite in [tests/render](render/README.md). It holds the
dual-NIC, IPv6-off, four-camera CPU, POC, Hailo plus QSV, no-camera, and overlap cases, the native
validation hooks, the `.link` outputs, the Frigate config and compose files, and the exact expected
files.

The release manifest canonical vector is in
[fixtures/release/manifest-v1](fixtures/release/manifest-v1/README.md): the fixed canonical bytes,
their SHA-256, and the exact domain-prefixed signing input. Its key id is a placeholder, so it tests
the canonical form and not a signer.

The responsive UI browser check is in [tests/browser](browser/README.md). It is opt-in, starts its
own `bitternd`, and drives Chromium at phone and desktop widths. It is not part of `make check`, and
its screenshots stay under the gitignored `build/ui-screenshots/`.

The QEMU firstboot scenario decodes the setup QR from `bittern setup-token --qr` with the test-only
host `zbarimg` dependency, checks the grouped token and short code, completes setup from the decoded
fragment, and scans the journal, status, and captured logs for the token and short code. The Go tests
in `agent/internal/setupqr` add a qrencode-to-zbarimg roundtrip when those local tools exist and skip
it otherwise.

The first-boot goldens are in [tests/firstboot/minimal](firstboot/minimal/expected) and
[tests/firstboot/camera](firstboot/camera/expected). Secret values are replaced with placeholders and
the certificate body is omitted from the tree because it is not deterministic. The no-camera tree
has no Frigate env file or unit; the camera tree includes both, with the camera URL redacted.

Related: [architecture](../docs/architecture.md), [ADR 0012](../docs/adr/README.md).
