# Third-party notices

Bittern's own code and documentation are licensed under the MIT License (see [LICENSE](LICENSE)).
This file lists the separate works that Bittern distributes with, fetches, or runs alongside, and
points to their licenses. Bittern does not relicense these components, and it never bundles Frigate+
models. When a component is fetched at runtime instead of shipping in the image, the upstream license
still governs its use. This file is informational and does not replace the upstream license texts.

| Component | License | How it is used |
| --- | --- | --- |
| Frigate | MIT | Used unmodified as a container image; its license text and notices ship with the upstream image. |
| go2rtc | MIT | Streaming component bundled inside the Frigate image. |
| Default detection models | Apache-2.0 | Models Frigate ships by default; verify each model before redistributing it in a Bittern image. |
| Google Coral and OpenVINO tooling and models | Apache-2.0, as applicable | Hardware runtimes and model formats; the specifics are documented upstream. |
| ffmpeg | GPL | Bundled in the upstream Frigate image; Bittern does not relicense or rebuild it, and distribution must honor the applicable source-offer obligations. |
| MediaMTX | MIT | Test-only RTSP server fetched by the QEMU camera fixture at a pinned version and checksum; not shipped in the image or app bundle. |
| HTMX 2.0.11 | Zero-Clause BSD (0BSD) | Vendored as `ui/static/htmx-2.0.11.min.js` with SHA-256 `d6fdc75f204e6bdefa99b69bf1e6d4ac69b8a364f77929f45c13476b4000f717`; license text is in [ui/static/htmx-2.0.11.LICENSE](ui/static/htmx-2.0.11.LICENSE). Never loaded from the network. |
| `qrencode` | LGPL-2.1-or-later | Debian package in the appliance image package set and the POC payload; the setup block pipes the URL to it on standard input and never passes or stores the token as an argument or file. The Debian package ships its own copyright file. |
| `zbar-tools` (`zbarimg`) | LGPL-2.1-or-later | Test-only host dependency for the QEMU setup-token scenario, which decodes the terminal QR; not shipped in the appliance image or app bundle. |
| Playwright 1.65.1 (`@playwright/test`, `playwright`, `playwright-core`) | Apache-2.0 | Test-only browser driver for `make ui-browser-check`; installed with `npm ci` under `tests/browser/node_modules`; not shipped in the image, app bundle, or device SBOM. |
| Chromium for Testing 159.0.8154.6 (Playwright revision 1255) | Chromium BSD-3-Clause and bundled third-party notices | Test-only browser downloaded by `npx playwright install --with-deps chromium`; not distributed by Bittern and not shipped in the image, app bundle, or device SBOM. |
| Frigate+ models | Proprietary | Licensed only for cameras the licensee owns or operates; excluded from the default open distribution and never bundled by Bittern. |
| Debian packages in the OS image | Per package | Each package keeps its own license; the image ships the corresponding copyright files. |
| Go toolchain and runtime | BSD-3-Clause | Builds the agent; the Go runtime is linked into Bittern's static binaries. |
| `gopkg.in/yaml.v3` | MIT and Apache-2.0 | Vendored YAML parser for `appliance.yaml`; license files are in `agent/vendor/gopkg.in/yaml.v3/`. |
| `github.com/santhosh-tekuri/jsonschema/v6` | Apache-2.0 | Vendored JSON Schema validator; license file is in `agent/vendor/github.com/santhosh-tekuri/jsonschema/v6/`. |
| `golang.org/x/text` | BSD-3-Clause | Transitive dependency of the JSON Schema validator, vendored for localized messages; license files are in `agent/vendor/golang.org/x/text/`. |
| PyYAML and jsonschema (`python3-yaml`, `python3-jsonschema`) | MIT | Development-only schema validation for local checks; not shipped in the appliance image or runtime. |

## Maintenance

Update this file whenever a runtime component is added, removed, or changes license. When HTMX is
updated, change its filename, checksum, `ui/README.md`, and this row together. Generate an SBOM for
every release and attach it to the release artifacts, as required by
[ADR 0012](docs/adr/0012-testing-and-release-gates.md).

Third-party license texts and copyright notices remain the property of their respective owners.
