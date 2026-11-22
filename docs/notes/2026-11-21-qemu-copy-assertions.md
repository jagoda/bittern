# QEMU copy assertions and the wizard data hooks

- Date: 2026-11-21
- Status: wizard slice landed. The remaining QEMU copy-coupled checks are listed below and stay
  open under [BIT-035](../plan/implementation-plan.md#bit-035-decouple-qemu-assertions-from-rendered-copy).
- Scope: `tests/qemu/run.py` and the templates the wizard reads. The Go test copy coupling recorded
  as T-02 in [the test suite audit](2026-11-21-review-tests.md) is not changed here.
- Related: [EN-2](../principles.md#en-2-tests-assert-structure-and-values-not-user-facing-copy),
  [copy inventory](../design/copy-inventory.md#test-coupling),
  [components](../design/components.md#test-hooks), [QEMU README](../../tests/qemu/README.md#stable-data-hooks).

## What changed

The two `drive_wizard` phases now identify a screen by `data-step-name` and read values from
`data-camera-*`, `data-network-*`, `data-storage-*`, `data-discovery-*`, `data-result-state`, and
`data-camera-picture`. The harness helper is `data_values` in `tests/qemu/run.py`; `require_data`
turns a missing value into an error naming the attribute and the values it found.

The conversion covered 22 of the 48 copy-coupled QEMU literals in the audit inventory, at these
screens:

| Screen | Converted to |
| --- | --- |
| Welcome, network, storage, camera, review, done | `data-step-name`; live `data-network-*` and `data-storage-*` values |
| Discovery candidate and evidence | `data-camera-address`, `-mac`, `-model`, `-state`, `data-camera-sources`, `data-discovery-layer`, `-layer-state`, `-detail`, `data-discovery-state` |
| Review camera rows | `data-camera-address`, `-port`, `-path`, `-mac`, `-reservation`, `-credentials` |
| Apply, result, done | `data-result-state`; `data-camera-picture`; `data-camera-name` and `-address` on result rows |

The change also moved 13 address, MAC, port, path, model, and sample-data checks onto the same hooks.
Every converted check still fails when the value is wrong: `require_data` asserts the exact value,
and the discovery detail checks still require the failed ONVIF and RTSP evidence.

No template copy or handler behaviour changed. The hooks are invisible attributes; the rendered
sentences are the same.

The status and camera copy rewrite also gave the managed camera card its own hooks:
`data-camera-name`, `data-camera-address`, `data-camera-enabled`, `data-camera-stream-state`,
`data-camera-picture-state`, and `data-camera-recording-state`. The camera-list state assertions
now read those values instead of the badge words. The two flash sentences remain copy-coupled.

## Remaining copy-coupled assertions

These are the QEMU literals this tree has not converted. Line numbers are from this tree. The
installer and journal rows are evidence, not template prose; the remaining camera rows are flash
copy; the cleanup rows are operator output. Together they are 24 literals in 12 rows.

| File:line | Copy it depends on | Why it stays |
| --- | --- | --- |
| `tests/qemu/run.py:2077` | `installing Debian packages: docker.io docker-cli docker-compose` | Installer log line; the package list is the contract, not a template |
| `tests/qemu/run.py:2420` | `already has the Bittern network policy` | Installer rerun log line |
| `tests/qemu/run.py:2905` | `setup QR not shown` | Negative journal marker; fails open if the marker is reworded |
| `tests/qemu/run.py:3835`, `:3836`, `:3837` | `apply: apply udev:wait`; `apply: apply restart:systemd-networkd.service`; `health lan0-address: passed` | First-boot journal step and health keys |
| `tests/qemu/run.py:4064`, `:4065`, `:4066`, `:4067`, `:4068`, `:4069` | `network: management interface `; `network: no IPv4 address on `; `network: wrote .../90-bittern-bootstrap.network`; `network: seeded /etc/resolv.conf from the `; `network: getent hosts deb.debian.org resolved before apt`; `network: install network bootstrap ready` | Clean-network installer journal lines |
| `tests/qemu/run.py:4138` | `NIC roles are unconfirmed` | First-boot marker note, negative if reworded |
| `tests/qemu/run.py:4452`, `:4456` | `restart:dnsmasq.service failed`; `interface cam0 does not currently exist` | Late-camera failure text in the journal |
| `tests/qemu/run.py:4468`, `:4469`, `:4470` | `apply: apply restart:systemd-networkd.service`; `apply: apply wait:cam0-address`; `apply: apply restart:dnsmasq.service` | Late-camera journal ordering keys |
| `tests/qemu/run.py:5053` | `Edit camera`; `Apply camera change` | Camera edit page headings and action |
| `tests/qemu/run.py:5075` | `The picture test passed` | Explicit picture success flash; the state label is a hook |
| `tests/qemu/run.py:5123` | `The picture test failed` | Offline picture failure flash; the state labels are hooks |
| `tests/qemu/run.py:5205`, `:5207` | `remove Bittern dnsmasq file `; `manual dnsmasq reservation` | Cleanup plan output and negative manual-bridge check |

The original audit counted 48 literals. The wizard slice converted 22, leaving 26 from that
inventory. The late-camera rows added 5 literals that were not in the original count, and the camera
list rewrite converted 7 more, so the current table has 24. Do not delete an assertion to lower it.
Convert one table row at a time, keep the value check, and remove the row when its hook lands.

The [first-run copy rewrite](2026-11-21-wizard-copy-rewrite.md) converted no row in this table. The
wizard and adoption checks already read the hooks from `5066a34`, and the remaining rows are camera
list, journal and installer prose outside the wizard slice.

## Proof that a copy change no longer breaks the wizard assertions

The proof used the real templates and the same `data_values` and `require_data` helper as the QEMU
scenario, against a local `bitternd` mock with the wizard and mock apply enabled. This is the
unit-level review phase, not the full QEMU suite.

1. With the committed templates, the script walked welcome, network, storage, camera, review,
   apply result, and done and passed every hook assertion.
2. I changed the review heading from `Review configuration` to
   `Review configuration (copy proof)`, ran `make assets` and `make bitternd`, and reran the same
   script. It passed again. The captured review HTML differed in the heading (and the per-request
   CSRF token), and no value hook changed.
3. I restored the heading and ran `make assets` again. The committed template has the original copy.

Commands run:

```sh
make check
cd agent && CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=vendor \
  go test -count=1 ./internal/ui -run TestWizardPagesCarryStableDataHooks
python3 /tmp/prove_hooks.py /tmp/hook-proof-before
# temporary review heading edit, make assets, make bitternd
python3 /tmp/prove_hooks.py /tmp/hook-proof-after
diff -u /tmp/hook-proof-before/review.html /tmp/hook-proof-after/review.html
# revert, make assets
python3 tests/qemu/run.py firstboot
```

`make check` is green. The new Go test
[`data_hooks_test.go`](../../agent/internal/ui/data_hooks_test.go) asserts the camera, review,
result, and done hooks against rendered pages, so a missing or wrong attribute fails without reading
the copy.

I also ran `python3 tests/qemu/run.py firstboot`. The guests and install phases passed, then the
scenario stopped in `first-boot` before the wizard because `chrony.service` failed to start during
`systemctl start dnsmasq.service chrony.service` on this container host. That failure is before the
wizard assertions and is unrelated to the hooks. The QEMU firstboot and adoption scenarios still
need a clean host run; the local mock above is the evidence for the wizard slice.
