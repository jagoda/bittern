# 0015. Agent language

*Implement `bitternd` and the `bittern` CLI in Go as statically linked binaries built from vendored
modules.*

- Status: Accepted
- Date: 2026-11-14
- Decided: 2026-11-14 (language)
- Deciders: maintainer
- Related: [OQ-02](../open-questions.md#resolved-questions),
  [agent language spike](../research/agent-language-spike.md),
  [ADR-0006](./0006-declarative-config-and-renderer.md),
  [ADR-0007](./0007-mkosi-os-image-and-releases.md),
  [ADR-0008](./0008-updates-rollback-and-reset.md),
  [ADR-0010](./0010-security-and-privacy-posture.md),
  [ADR-0011](./0011-monorepo-layout-and-naming.md),
  [ADR-0012](./0012-testing-and-release-gates.md)

## Context

OQ-02 blocked application code beyond the schema until this ADR resolved it. The agent is a root
daemon that serves the status API, validates `appliance.yaml`, renders systemd-networkd, nftables,
dnsmasq, chrony, Compose and Frigate fragments, applies and rolls back generations, drives camera
discovery, runs the app-bundle update transaction, and performs reset and support-bundle work. The
CLI is a thin operator front end over the same API. The agent also has to behave correctly across an
A/B OS update, an app-bundle rollback, a power cut, and a signal during a long-running step.

The runtime facts, dated 2026-11-14:

- The target is Debian 13. Its `python3` metapackage is 3.13.5-1 and its interpreter package is
  3.13.5-2+deb13u5. Its `golang-go` is 2:1.24~2 and `golang-1.24-go` is 1.24.4-1. Go 1.24 is outside
  the upstream two-release support window; current releases were 1.27.4 and 1.26.11.
- mkosi, the image build tool from [ADR-0007](./0007-mkosi-os-image-and-releases.md), is a Python
  program and already needs Python 3 on the build host. Nothing else in the image definition requires
  Python, so the target image has none unless the agent adds it.
- The [agent language spike](../research/agent-language-spike.md) measured a status server, artifact
  size, start time, RSS, schema validation, unit rendering, build and cross-compilation, update-step
  cancellation, and library maturity in both languages. It kept the spike out of the repository and
  did not build the full agent.
- Measured medians from the spike: a Go status server with YAML and schema libraries is 7.45 MiB and
  9.66 MiB RSS; the Python equivalent needs 26.19 MiB of Debian packages and 30.34 MiB RSS. Start to
  first response was 8.13 ms in Go and 128.56 ms in Python with a warm page cache, and 65.09 ms versus
  257.72 ms with an evicted cache. Validation of 1000 configs took 46.18 ms in Go and 357.89 ms in
  Python. Python has the larger ecosystem: 2,204,332 Stack Overflow questions tagged Python versus
  74,453 tagged Go, and 36.09M versus 2.62M GitHub repositories.
- The update transaction can be expressed in either language. The spike measured a 300 ms step
  timeout, child cancellation, SIGTERM handling and rollback in both. Go's advantage is that one
  `context.Context` propagates through HTTP, subprocesses, health probes and signals; Python needs a
  subprocess timeout, an event or signal handler, and explicit cancellation at each await point.

The decision is the language, not the schema dialect, the UI stack, the update mechanism, or the
test harness language. Those remain separate.

## Decision

**Bittern implements `bitternd` and the `bittern` CLI in Go.**

- The Go module lives under `agent/` and builds `bitternd` and `bittern`. Whether that is two binaries
  or one binary with modes is left to the agent design.
- Build with `CGO_ENABLED=0`, `-trimpath` and stripped symbols. The result is a static binary with no
  libc or interpreter dependency. The image ships the binaries and their unit files; it does not ship
  a Python interpreter or venv for the agent.
- Pin a supported Go toolchain in the image build and record its version in the image manifest and
  SBOM. At the time of writing that is Go 1.27.x. Do not use Debian 13's 1.24.4 package as the build
  toolchain, because 1.24 is outside upstream support.
- Vendor every module with `go mod vendor`, review the vendor diff, and build with `GOPROXY=off`. The
  build must work without network access once the source tree and toolchain are present.
- Use `text/template` for generated systemd units and other plain-text files. The YAML and JSON Schema
  libraries are provisional in this ADR: the spike used `gopkg.in/yaml.v3` v3.0.1 and
  `github.com/santhosh-tekuri/jsonschema/v6` v6.0.3. BIT-003 chooses and pins the final pair and must
  test strict decoding, duplicate keys, and the `yes`/`no`/`on`/`off` behavior. BIT-002 is already
  merged and its Python validator is a development reference, not the product; BIT-003 must prove the
  Go parser matches the fixture suite.
- The QEMU harness talks to the agent over HTTP and the CLI and is not constrained by this decision.
- Out of scope: the UI stack (OQ-03), the OS A/B mechanism (OQ-04), the test runner choice, and the
  exact split between daemon and CLI.

## Consequences

### Positive

- The app bundle carries its whole runtime. An OS update changes Python, libc or system libraries
  without changing the agent's ABI, and an app-bundle rollback is a file replacement.
- The image does not need a Python interpreter, a venv, or a pip resolution step on the device. For the
  status-server fixture the measured difference is 7.45 MiB versus 26.19 MiB, or 30.09 MiB with a
  no-pip venv.
- The status-server fixture starts in 8.13 ms warm and 65.09 ms with an evicted cache, against
  128.56 ms and 257.72 ms for Python, and uses 9.66 MiB RSS against 30.34 MiB. Those differences are
  not the main reason for the decision, but they remove a class of resource questions on a small
  appliance.
- One `context.Context` covers the update transaction, subprocess timeouts, HTTP calls to Frigate and
  health probes. SIGTERM cancels the same context, so rollback has one path to test.
- `go build`, `go test` and cross-compilation to arm64 work offline with a vendored tree. The spike
  cross-compiled the status server to arm64 in 7.47 s without a C toolchain.
- Schema validation and golden rendering were about 7.7x and 5.9x faster in the spike, which keeps a
  large golden suite cheap.

### Negative / costs

- The maintainer's fluency is the largest risk this evidence cannot measure. Go's ecosystem proxies
  are much smaller than Python's: 74,453 versus 2,204,332 Stack Overflow questions, and 2.62M versus
  36.09M GitHub repositories. A single-maintainer project should not choose a language the maintainer
  cannot review quickly, even if its binaries are smaller.
- A static Go binary does not receive Debian security updates. A Go runtime or module advisory needs
  a rebuild, a new signature and a new app bundle. The release gates in
  [ADR-0012](./0012-testing-and-release-gates.md) must include the Go toolchain and the module SBOM.
- `gopkg.in/yaml.v3` has not cut a release since 2022 and last saw a commit in April 2025. It is stable
  and still used, but `goccy/go-yaml` is the fallback if it stops working. That would be a library
  change, not a language change.
- YAML behavior differs from PyYAML. Untyped `yes`, `no`, `on` and `off` are strings in `yaml.v3` but
  booleans in PyYAML, and `yaml.v3` rejects duplicate keys while PyYAML keeps the last value. The
  schema and documentation must make this explicit, and the renderer must not depend on implicit YAML
  1.1 booleans.
- `text/template` is less expressive than Jinja2 for complex units and fragments. The renderer should
  keep templates simple; if it needs Jinja2 features, that is a design smell rather than a reason to
  change language.
- The build host needs a Go toolchain in addition to the Python that mkosi already needs. The toolchain
  must be pinned and updated on the same cadence as the Go security releases.
- Embedding the UI in the binary costs roughly the asset size again: a 5,000,000-byte fixture grew the
  binary by 5,111,808 bytes. Whether to embed is a UI packaging decision, not part of this ADR.

### Follow-ups

- BIT-003: choose and pin the YAML and JSON Schema library pair; add strict decoding tests,
  duplicate-key tests, and documented YAML 1.1/1.2 behavior. BIT-002 is already merged and its Python
  validator is a development reference, not the product; BIT-003 must prove the Go parser matches the
  fixture suite.
- Image build: pin the Go toolchain, verify the vendor tree, add Go modules to the SBOM, and add the
  toolchain and modules to the CVE scan.
- BIT-006: define the status contract and the state machine in Go, with context timeouts and a single
  owner for the transaction.
- BIT-009: implement the update transaction with context cancellation and signal tests, then run the
  QEMU failure and power-cut cases.
- Add golden render tests and a `make` target that runs `go test` offline.
- Decide the QEMU harness language separately; it consumes the agent API either way.
- Revisit this ADR after the first vertical slice, and sooner if the maintainer's Go velocity is
  materially worse than Python, if `yaml.v3` becomes unusable, or if the Go support window forces a
  toolchain jump that the image build cannot absorb.

## Alternatives considered

- **Python with Debian packages.** This is the strongest alternative and the fallback. It has the
  smallest Python footprint (26.19 MiB including the interpreter), mature libraries (`jsonschema`,
  PyYAML, Jinja2), Debian security updates for the interpreter and libraries, the larger contributor
  pool, and the same language as mkosi, which is also a natural fit for the QEMU harness. It was not
  chosen because the agent's runtime and libraries then move with the OS image, so a library security
  fix can require the disruptive OS loop, and because the interpreter ABI couples the app bundle to the
  Debian release.
  Those are architectural costs, not benchmark losses; a maintainer who is much faster in Python can
  reasonably choose this option.
- **Python with a venv in the app bundle.** Not chosen. The venv is tied to the target architecture
  and the OS Python minor and ABI, so an OS update can break it. The image cost is 30.09 MiB for the
  interpreter plus a no-pip venv, and the bundle would carry pip, wheel resolution and a second
  interpreter copy. The spike measured the size but did not test a venv across an OS update.
- **Python with FastAPI, aiohttp or another framework.** Not chosen for the same reasons as Debian
  Python, plus more packages, import time and RSS than the stdlib floor measured here. It would make
  the Python figures worse, not better, for an appliance that needs a small status API.
- **Go for the daemon and Python for the CLI.** Not chosen. Two toolchains, two sets of schema and
  renderer code, and a shared contract that has to be tested twice. The CLI is a thin API client, so it
  should use the same language and client types as the daemon.
- **Rust.** Not evaluated. It is a credible appliance language, but it was not in the OQ-02 shortlist
  and a serious comparison needs its own spike, toolchain and contributor analysis. This ADR does not
  claim Go is better than Rust.
- **Leave OQ-02 open.** Not viable. It blocks code beyond schema and invites accidental decisions in
  component skeletons.

## References

- [Agent language spike](../research/agent-language-spike.md), dated 2026-11-14, with commands and
  raw output.
- [OQ-02](../open-questions.md), [architecture](../architecture.md),
  [implementation plan](../plan/implementation-plan.md)
- [ADR-0006](./0006-declarative-config-and-renderer.md),
  [ADR-0007](./0007-mkosi-os-image-and-releases.md),
  [ADR-0008](./0008-updates-rollback-and-reset.md),
  [ADR-0010](./0010-security-and-privacy-posture.md),
  [ADR-0011](./0011-monorepo-layout-and-naming.md),
  [ADR-0012](./0012-testing-and-release-gates.md)
- Go release policy: <https://go.dev/doc/devel/release#policy>
- Go downloads and support window: <https://go.dev/dl/>
- `yaml.v3`: <https://pkg.go.dev/gopkg.in/yaml.v3>
- `santhosh-tekuri/jsonschema` v6: <https://github.com/santhosh-tekuri/jsonschema>
- Debian 13 Go package: <https://packages.debian.org/trixie/golang-go>
- Debian security archive: <https://security.debian.org/>
- Stack Overflow tag counts: <https://stackoverflow.com/tags/go/info> and
  <https://stackoverflow.com/tags/python/info>
