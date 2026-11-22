# Agent language spike

Dated 2026-11-14. Evidence for [OQ-02](../open-questions.md), the language for `bitternd` and the
`bittern` CLI. The recommendation is in [ADR 0015](../adr/0015-agent-language.md), which remains
Proposed. This document does not decide OQ-02 or OQ-03.

The raw numbers below come from a one-session spike in this container. The measurement tables cite an
ID defined in the appendices, where the commands and raw output are reproduced. Spike sources and
binaries were kept under `/tmp/oq02` and are not committed.

## Scope and method

| Measured | Not measured |
| --- | --- |
| A 48-line `/api/status` server in each language, same JSON body. | The full agent, renderer, config store, or Frigate client. |
| Process start to first successful response, warm and evicted page cache. | systemd boot, watchdog, or `Type=notify` readiness. |
| RSS at rest after one request. | Memory under sustained camera or Frigate load. |
| Artifact size: static binary, Debian packages, venv, unit file. | Signed bundle size, compression, or A/B slot budget. |
| Build, offline build, cross-compilation, vendoring and wheelhouses. | mkosi image build or reproducibility of the OS image. |
| Update-step timeout, child cancellation, SIGTERM and rollback. | Docker, database migration, or a real bundle update. |
| YAML, JSON Schema and unit rendering, with microbenchmarks. | Full schema v1 (BIT-002 does not exist yet). |
| Contributor and package ecosystem counts. | Maintainer fluency, review speed, or hiring. |

Runtime environment:

| Item | Value | Source |
| --- | --- | --- |
| Host container | Debian GNU/Linux 12 (bookworm), kernel 7.2.15, 14 vCPU, 64 GiB RAM, overlayfs | M1 |
| Target userspace | Debian 13 (trixie) `minbase` chroot, debootstrap from `deb.debian.org` | M1 |
| Python | 3.13.5 (Debian `python3.13` 3.13.5-2+deb13u5) | M1 |
| Go | 1.27.4 official linux/amd64 tarball, SHA-256 `7226736c...24840` | M1 |
| Debian Go packages | `golang-go` 2:1.24~2, `golang-1.24-go` 1.24.4-1 | M1 |
| Debian Python libraries | PyYAML 6.0.2, jsonschema 4.19.2, Jinja2 3.1.6 | M1 |
| Upstream Python libraries | PyYAML 6.0.3, jsonschema 4.26.0, Jinja2 3.1.6 (venv test) | M8 |

Absolute timings are container measurements, not N100 measurements. The host has 14 vCPU, a fast
NVMe overlay, and no Frigate load. The ratios are the useful part; a slower appliance will move both
columns. Both servers bound `127.0.0.1`, used plain HTTP, and returned the same fixed JSON document.

## Summary

| Dimension | Go | Python | Ratio | Source |
| --- | --- | --- | --- | --- |
| Runtime artifact, with YAML and schema libraries | 7.45 MiB static binary | 26.19 MiB Debian packages (interpreter plus libraries) | 3.5x | M4, M5 |
| Same, with a venv instead of Debian packages | 7.45 MiB | 30.09 MiB (interpreter plus venv, no pip) | 4.0x | M5 |
| Start to first response, warm page cache | 8.13 ms | 128.56 ms | 15.8x | M3 |
| Start to first response, evicted page cache | 65.09 ms | 257.72 ms | 4.0x | M3 |
| RSS at rest | 9.66 MiB | 30.34 MiB | 3.1x | M3 |
| Validate and render 1000 configs | 46.18 ms validate, 0.71 ms render | 357.89 ms validate, 4.16 ms render | 7.7x, 5.9x | M8 |
| Runtime dependencies in the image | No interpreter or userland libraries; static binary | Python 3.13 plus 23 Debian packages | n/a | M4, M5 |
| Third-party modules to review | 3 modules, 1.17 MiB vendored | 8 wheels or 23 Debian packages | n/a | M5, M6 |
| Cold build | 8.07 s | No compile; 1.65 s venv plus 0.62 s offline install | n/a | M6 |
| Cross-compile to arm64 | 7.47 s, 5.81 MiB binary | Not supported; per-arch venv or packages | n/a | M6 |
| Contributor proxies | 2.62M GitHub repos, 74,453 Stack Overflow questions | 36.09M repos, 2,204,332 questions | 13.8x, 29.6x | M10 |

The size, memory and start numbers are real but individually modest on an appliance with 8 to 16 GiB
of RAM and a multi-gigabyte OS image. The load-bearing differences are elsewhere: a static binary has
no interpreter ABI contract with the OS, the app bundle can carry the whole runtime, and the update
transaction can use one cancellation model for HTTP, subprocesses and signals. Python's counterweight
is a larger ecosystem and more mature libraries, and the spike cannot measure the maintainer's
fluency in either language.

## Status API

Both servers are 48 lines and return the same byte-identical JSON body, SHA-256
`397b6c808dd6d6f54b1746656f56ea425fcff47d39aded637799ffe152cc7938`. Sources are in Appendix B.

| Metric | Go | Python | Source |
| --- | --- | --- | --- |
| Source lines, stdlib only | 48 | 48 | M2 |
| Source lines, with YAML, schema and template init | 74 | 68 | M2 |
| Binary or artifact | 6,611,104 B (6.30 MiB), static | 26,822 KiB Debian runtime (26.19 MiB) | M4, M5 |
| Start, warm, stdlib only | 4.41 ms | 42.35 ms | M3 |
| Start, evicted, stdlib only | 57.77 ms | 108.86 ms | M3 |
| Start, warm, with config libraries | 8.13 ms | 128.56 ms | M3 |
| Start, evicted, with config libraries | 65.09 ms | 257.72 ms | M3 |
| RSS, stdlib only | 6,646 KiB (6.49 MiB) | 18,602 KiB (18.17 MiB) | M3 |
| RSS, with config libraries | 9,890 KiB (9.66 MiB) | 31,070 KiB (30.34 MiB) | M3 |

The Python server uses `ThreadingHTTPServer` and the standard library; a production Python agent
would use aiohttp, FastAPI or similar, which adds packages, import time and RSS. The Python figures are
therefore a floor, not a typical deployment. The Go server uses `net/http` with a `ReadHeaderTimeout`
and graceful shutdown, which is production-adequate without a framework.

Cold figures evicted the binary, interpreter, shared libraries and site-packages with
`posix_fadvise(..., POSIX_FADV_DONTNEED)` before each run. The container masks
`/proc/sys/vm/drop_caches`, so these are best-effort cold-cache runs, not a controlled kernel cache
drop. Eviction was verified on the 70.7 MB Go tarball: 13.7 ms warm read versus 83.2 ms after eviction
(M3).
Variance is reported in Appendix C.

## Artifact size

| Item | Size | Source |
| --- | --- | --- |
| Go status server, stripped static | 6,611,104 B (6.30 MiB) | M4 |
| Go status server plus YAML, schema and template init | 7,815,328 B (7.45 MiB) | M4 |
| Go arm64 status server, cross-compiled | 6,095,008 B (5.81 MiB) | M6 |
| Go update-transaction spike binary | 1,953,952 B (1.86 MiB) | M4 |
| systemd unit, rendered by either template engine | 228 B, byte-identical | M8 |
| Python 3.13 interpreter and standard library only | 23,793 KiB (23.24 MiB) | M5 |
| Python YAML, schema and template libraries | 3,029 KiB (2.96 MiB) | M5 |
| Python Debian runtime total, `--no-install-recommends` | 26,822 KiB (26.19 MiB), 23 packages | M5 |
| Python Debian runtime with recommended packages | 76,527 KiB (74.74 MiB), 40 packages | M5 |
| Python venv, libraries only, no pip | 7,190,631 B (6.86 MiB) | M5 |
| Python venv total, no pip | 30.09 MiB (interpreter plus venv) | M5 |
| Python venv with pip | 17,460,410 B (16.65 MiB), 39.89 MiB total | M5 |
| Python wheelhouse for offline install | 1,533,717 B (1.46 MiB), 8 wheels | M6 |
| Go vendor directory | 1,231,436 B (1.17 MiB), 3 modules | M6 |
| UI fixture, 5,000,000 B; Go binary growth when embedded | 5,111,808 B (4.88 MiB) | M9 |

The Debian package figure is the sum of `Installed-Size` for the new packages, the standard apt
metric. It excludes shared libraries already present in the base image, such as libc and CA
certificates. The venv figure still needs the system interpreter; a venv is not standalone. The Go
binary needs no libc (verified with `ldd`: "not a dynamic executable") and no interpreter.

What the image must ship:

| Option | Image contents | Build host contents |
| --- | --- | --- |
| Go | `bitternd`, `bittern`, unit files | Go 1.27.x toolchain, git, make |
| Python, Debian packages | Python 3.13, its stdlib, 23 packages, agent source | Python 3, git, make; mkosi already needs Python |
| Python, venv | Python 3.13, its stdlib, venv with 8 wheels, agent source | Python 3, pip and wheel download, git, make |

mkosi itself is a Python program. Debian 13's `mkosi` 25.3-7 depends on `python3:any`,
`python3-cryptography` and `python3-pefile` (M11), so the build host already carries Python. Choosing
Go adds a second toolchain to the build host. Choosing Python does not add a runtime to the image if
the interpreter is already present, but the image currently has no Python unless the agent brings it.

## Build, offline, and cross-compilation

| Metric | Go | Python | Source |
| --- | --- | --- | --- |
| Cold build, status server | 7.89 s | No compile step | M6 |
| Cold build, status server plus libraries | 8.07 s | No compile step | M6 |
| Dependency setup | `go get` first fetch, 2.8 s network, then vendored | `pip download`, 4.92 s network, 8 wheels | M6 |
| Offline build | `GOPROXY=off` with `-mod=vendor`, succeeds | `pip install --no-index --find-links`, succeeds | M6 |
| Cross-compile amd64 to arm64 | `GOOS=linux GOARCH=arm64 CGO_ENABLED=0`, 7.47 s | Not supported; wheels and venv are arch-specific | M6 |
| Incremental rebuild | Standard Go build cache | None; `.pyc` cache only | M6 |
| Unit test runner | `go test`, standard library, no extra package | `unittest` standard library or pytest 8.3.5 | M6 |

Go builds did not need a C compiler because `CGO_ENABLED=0`. The Debian package `golang-go` in trixie
is 1.24.4, outside the upstream two-release support window on 2026-11-14; the image build pins its own
toolchain (1.27.4 in this spike) rather than using the trixie package. Python has no compile step, but
a release venv must be built for the target architecture and Python ABI; cross-building an arm64 venv
on an amd64 host needs a target interpreter or qemu, and native extensions such as `rpds-py` are
arch-specific wheels.

Golden renders work in both. Go uses `text/template` and reads fixtures from `testdata/`; Python uses
Jinja2 and reads the same fixtures. Two concrete differences matter for byte-exact output:

- Jinja2 drops one trailing newline by default. `keep_trailing_newline=True` produced the same 228-byte
  unit as Go; the default produced 227 bytes (M8).
- PyYAML resolves `yes`, `no`, `on` and `off` as booleans, while `yaml.v3` resolves them as strings when
  decoded into an untyped map. PyYAML also accepts duplicate mapping keys and keeps the last value;
  `yaml.v3` rejects duplicates (M8). A Python agent needs `ruamel.yaml` or a custom loader to get strict
  YAML 1.2 behavior.

The QEMU harness is language-neutral for the agent: it boots an image, drives the agent over HTTP and
the CLI, and checks the guest. It can remain Python regardless of this decision, and it can also run a
cross-compiled Go test binary inside the guest. The harness was not built or run in this spike.

From `make`, the shape is the same in both cases: `make test` runs `go test ./...` or
`python3 -m pytest`; a golden update flag (`-update` or `--update-golden`) regenerates fixtures; and
`make qemu` boots the image and drives the guest over its console and API. None of these targets
exists yet; BIT-001 adds `make check`.

## Update transaction and concurrency

A transaction step was implemented in each language: a preflight, a backup, a long start step, a
health gate, a timeout, a child process, and a rollback marker. The start step ran `sleep 5`; the
timeout was 300 ms; rollback slept 50 ms and wrote a marker.

| Metric | Go | Python | Source |
| --- | --- | --- | --- |
| Code lines, timeout, signal and rollback | 53 | 60 | M7 |
| Timeout: in-program elapsed | 300 to 301 ms | 350 to 352 ms (includes 50 ms rollback) | M7 |
| Timeout: wall time to exit | 354 to 356 ms | 382 to 396 ms | M7 |
| Timeout: exit code, child reaped, marker written | 3, yes, yes | 3, yes, yes | M7 |
| SIGTERM: signal to process exit | 50 to 51 ms | 50 to 51 ms | M7 |
| SIGTERM: wall time after signal | 52 to 53 ms | 56 ms | M7 |
| SIGTERM: rollback marker written | yes | yes | M7 |

The mechanisms:

- Go uses `context.WithTimeout` per step, `exec.CommandContext` to kill the child on cancellation, and
  `signal.NotifyContext` to turn SIGTERM into context cancellation. The same context can bound an HTTP
  call to Frigate, a `docker` invocation, a database step and a health probe. A mutex or a single
  owning goroutine serialises the transaction.
- Python uses `subprocess.run(timeout=...)`, which kills and reaps the child on `TimeoutExpired`, and a
  signal handler that sets a `threading.Event`. An asyncio implementation would use
  `asyncio.create_subprocess_exec` and `asyncio.wait_for`. The same event loop can bound HTTP calls,
  but cancellation has to be threaded through each await point.

Both are adequate for the transaction. Go's advantage is not raw speed at cancellation; the measured
difference is dominated by Python's process startup and the 50 ms rollback sleep in both. The
advantage is that one context value propagates through steps, HTTP, subprocesses and signals without
adapter code, which makes the health gate and power-cut recovery state machine easier to review.

## Libraries

| Concern | Go | Python | Source |
| --- | --- | --- | --- |
| YAML | `gopkg.in/yaml.v3` v3.0.1, released 2022-05-27, last commit 2025-04-01 | PyYAML 6.0.2 in Debian, 6.0.3 upstream, released 2025-09-25 | M8 |
| JSON Schema | `github.com/santhosh-tekuri/jsonschema/v6` v6.0.3, released 2026-06-28, last commit 2026-11-12 | `jsonschema` 4.19.2 in Debian, 4.26.0 upstream, released 2026-01-07 | M8 |
| Templates | `text/template`, standard library | Jinja2 3.1.6, released 2025-03-05 | M8 |
| Draft 2020-12 validation | Works, tested | Works, tested | M8 |
| Validate 1000 configs, median of 3 | 46.18 ms | 357.89 ms (Debian), 394.89 ms (upstream venv) | M8 |
| Render 1000 units, median of 3 | 0.71 ms | 4.16 ms | M8 |

The schema and render benchmarks are a fixed YAML document, a 20-line Draft 2020-12 schema, and a
12-line systemd unit. They measure library overhead, not the full renderer. Go's validation is about
7.7x faster and rendering about 5.9x faster in this fixture. Both are fast enough; the difference
matters only for a large golden suite.

Library maturity is Python's strongest technical argument. `jsonschema`, PyYAML and Jinja2 are widely
used, well documented, and packaged by Debian. Go's `jsonschema/v6` is active and adequate, but
`yaml.v3` has not cut a release since 2022, its README promises API stability, and its last commit
was 2025-04-01. `goccy/go-yaml` v1.19.2 (2026-01-08) is an active alternative if `yaml.v3` becomes a
problem. Python also has `ruamel.yaml` for strict YAML 1.2 behavior, at the cost of another package.

## Serving the UI

Both options can serve the UI:

- Go can embed static assets with `//go:embed` or serve them from the bundle directory. Embedding
  5,000,000 B of fixtures grew the status binary by 5,111,808 B (M9), so embedding costs roughly the
  asset size again. It makes the binary the whole UI artifact and removes file-path drift.
- Python serves files from disk with the web framework in use, or with a reverse proxy. The assets
  stay in the bundle and cost their own size, not a copy inside a binary. The agent's web framework
  choice adds its own packages.

This does not decide OQ-03. A React or HTMX SPA can be served by either runtime. The one OQ-03 option
that would couple the decisions is server-rendered Go templates, which presupposes a Go server; that is
an argument inside OQ-03, not evidence for OQ-02. Whether to embed assets in the binary or ship them
beside it is also a packaging decision for the UI workstream.

## Contributor pool and review burden

| Proxy | Go | Python | Ratio | Source |
| --- | --- | --- | --- | --- |
| Stack Overflow questions, tag count | 74,453 | 2,204,332 | 29.6x | M10 |
| GitHub repositories by primary language | 2,619,929 | 36,090,679 | 13.8x | M10 |
| Debian package names, `golang-*` versus `python3-*` | 2,398 | 5,330 | 2.2x | M10 |
| Third-party modules for this spike | 3 modules (yaml, schema, x/text) | 8 wheels or 23 Debian packages | n/a | M5, M6 |
| Spike code lines, status with config init plus transaction | 127 | 128 | n/a | M2, M7 |

These proxies measure ecosystem size, not maintainer skill. The project is explicitly not ready for
external contributions, and the implementation plan lists the single-maintainer bus factor as a top
risk. For one maintainer, Go's smaller dependency surface and static typing reduce the amount of
third-party code to review and pin, while Python's larger pool makes it easier to find help and
reviewers later. The maintainer's own fluency is the factor this spike cannot measure and the ADR
treats it as the main reason to choose Python instead.

## Debian 13 packages and security updates

| Package | trixie version | Source |
| --- | --- | --- |
| `python3` metapackage | 3.13.5-1 | M11 |
| `python3.13` interpreter | 3.13.5-2+deb13u5 | M11 |
| `python3-yaml` | 6.0.2-1+b2 | M11 |
| `python3-jsonschema` | 4.19.2-6 | M11 |
| `python3-jinja2` | 3.1.6-1 | M11 |
| `python3-pytest` | 8.3.5-2 | M11 |
| `python3-systemd` | 235-1+b6 | M11 |
| `golang-go` | 2:1.24~2 | M11 |
| `golang-1.24-go` | 1.24.4-1 | M11 |
| `mkosi` | 25.3-7, depends on `python3:any` | M11 |

The security update paths differ:

| Option | How a runtime CVE is fixed | Coupling to the OS loop |
| --- | --- | --- |
| Go static binary in the app bundle | Rebuild the binary with a fixed toolchain or module, sign a new bundle, apply through loop 2 | None; the OS Python or libc version does not matter |
| Python from Debian packages | `apt` update in the OS image; the agent code may live in the bundle | The agent's runtime and libraries move with the OS image, so a library fix waits for loop 1 |
| Python venv in the app bundle | Rebuild the venv with fixed wheels and ship a new bundle | The venv is tied to the OS Python minor and ABI; an OS update can break it |

The trixie security archive is live: its Release file is dated 2026-11-14, and the `python3.13`
interpreter package is `3.13.5-2+deb13u5`, a stable-update revision of the 3.13.5 package (M11). Debian's
Go package stayed at `1.24.4-1`; in any case, apt cannot patch a statically linked Go binary that was
shipped in an app bundle. A Go choice therefore moves runtime CVE response into the
release pipeline: a Go or module advisory needs a rebuild and a signed bundle. That is the same shape
as the existing Frigate digest and bundle release gates, but it must be explicit.

Python's advantage is that Debian patches the interpreter and libraries without Bittern rebuilding
anything, provided the agent uses distro packages and accepts OS-loop coupling. That advantage
disappears for a venv in the app bundle, which is one reason the ADR does not recommend that shape.

## What was not measured

- No QEMU boot, systemd unit verification, watchdog, or `Type=notify` readiness.
- No full update transaction: no Docker stop/start, database backup or restore, signature check, or
  power cut.
- No Frigate API calls, camera discovery, RTSP, or nftables/dnsmasq validation.
- No renderer, schema v1, migrations, or golden suite at scale.
- No TLS, authentication, rate limiting, or concurrent client load.
- No arm64 execution; the arm64 binary was cross-compiled but not run.
- No N100, eMMC/SSD, or real appliance measurements.
- No maintainer productivity trial. That is the main open input for the decision.

## Recommendation

The measurements favor Go on artifact size, start time, RSS and validation throughput, although each
is individually modest on this hardware. The stronger argument is architectural: a static binary
removes the interpreter ABI contract between the OS loop and the app loop. The recommendation assumes
the maintainer can work in Go at least as effectively as in Python; if that is not true, Python with
Debian packages is the defensible alternative and the size and speed differences are not large enough
to override it. The full argument is in [ADR 0015](../adr/0015-agent-language.md).

## Appendix A: environment and versions

Commands, run on 2026-11-14:

```console
$ uname -r && nproc
7.2.15-76070215-generic
14
$ /tmp/oq02/go1.27/bin/go version
go version go1.27.4 linux/amd64
$ sha256sum /tmp/oq02/go127.tar.gz
7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840  go127.tar.gz
$ sudo chroot /tmp/trixie /usr/bin/python3 --version
Python 3.13.5
$ sudo chroot /tmp/trixie /usr/bin/python3 -c \
    'import importlib.metadata as m; print(m.version("PyYAML"), m.version("jsonschema"), m.version("Jinja2"))'
6.0.2 4.19.2 3.1.6
```

The trixie chroot was created with:

```console
$ sudo /usr/sbin/debootstrap --variant=minbase \
    --include=python3,python3-yaml,python3-jsonschema,python3-jinja2,ca-certificates \
    trixie /tmp/trixie http://deb.debian.org/debian
```

`/tmp/trixie-clean` is a second, package-free `minbase` chroot used only for package size sums.
`mkosi` and the package versions in the security table came from `apt-cache policy` and
`apt-cache show` in that chroot. The Go toolchain is the official tarball, not the Debian package;
the Debian package version is recorded for availability, not used for the measurements.

Source: M1.

## Appendix B: status servers

Both servers return the same body. The body hash was produced by the harness for every run and was
identical for Go and Python. The servers were compiled or copied into `/tmp/trixie/srv` and run under
`chroot` so both used the Debian 13 userspace.

Go (`status-go/main.go`, 48 lines):

```go
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var doc = map[string]any{
	"schema_version": 1,
	"state":          "ready",
	"started_at":     "2026-11-14T00:00:00Z",
	"app":            map[string]any{"version": "0.0.0-spike", "generation": 1},
	"frigate":        map[string]any{"state": "up", "api": "ok"},
	"storage":        map[string]any{"state": "ok", "free_bytes": 1000000000},
	"network":        map[string]any{"lan": "up", "cam": "up"},
}

func main() {
	addr := "127.0.0.1:" + os.Getenv("PORT")
	if os.Getenv("PORT") == "" {
		addr = "127.0.0.1:8080"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(doc)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
```

Python (`status-py/server.py`, 48 lines):

```python
import json
import os
import signal
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

DOC = {
    "schema_version": 1,
    "state": "ready",
    "started_at": "2026-11-14T00:00:00Z",
    "app": {"version": "0.0.0-spike", "generation": 1},
    "frigate": {"state": "up", "api": "ok"},
    "storage": {"state": "ok", "free_bytes": 1000000000},
    "network": {"lan": "up", "cam": "up"},
}
BODY = json.dumps(DOC, sort_keys=True, separators=(",", ":")).encode()


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/api/status":
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(BODY)))
        self.end_headers()
        self.wfile.write(BODY)

    def log_message(self, *args):
        pass


def main():
    port = int(os.environ.get("PORT", "8080"))
    srv = ThreadingHTTPServer(("127.0.0.1", port), Handler)

    def stop(signum, frame):
        threading.Thread(target=srv.shutdown, daemon=True).start()

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    srv.serve_forever()


if __name__ == "__main__":
    main()
```

Build and line count:

```console
$ wc -l status-go/main.go status-py/server.py
48 status-go/main.go
48 status-py/server.py
$ CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/status-go ./status-go
$ ldd bin/status-go-deps127
	not a dynamic executable
```

Source: M2.

## Appendix C: start and RSS harness

Method: 20 warm runs and 10 evicted runs per label, one process per run, loopback only. The harness
starts the server, polls `GET /api/status` every 1 ms until a 200 response, records the monotonic time
from `Popen` to the completed body, waits 2 s, reads `VmRSS` from `/proc/PID/status`, then terminates
the process. The evicted mode applies `posix_fadvise(..., POSIX_FADV_DONTNEED)` to the binary and, for
Python, to `/usr/bin/python3.13`, `/usr/lib/python3.13`, `/usr/lib/x86_64-linux-gnu` and
`/usr/lib/python3/dist-packages`. The 70.7 MB Go tarball was read before and after the same eviction
call to check that it works; the appendix has the numbers.

Harness (`/tmp/oq02/harness/measure.py`):

```python
"""Cold/warm start and RSS harness for the OQ-02 spike.

Run as root: sudo python3 measure.py --label go --runs 10 --cold -- \
    /usr/sbin/chroot /tmp/trixie /srv/status-go
"""
import argparse
import http.client
import json
import os
import statistics
import subprocess
import sys
import time


EVICT = []


def fadvise_dontneed(path):
    try:
        fd = os.open(path, os.O_RDONLY)
    except OSError:
        return
    try:
        os.posix_fadvise(fd, 0, 0, os.POSIX_FADV_DONTNEED)
    except OSError:
        pass
    finally:
        os.close(fd)


def evict(paths):
    for root in paths:
        if os.path.isdir(root):
            for dirpath, _dirnames, filenames in os.walk(root):
                for name in filenames:
                    fadvise_dontneed(os.path.join(dirpath, name))
        else:
            fadvise_dontneed(root)


def rss_kb(pid):
    with open(f"/proc/{pid}/status") as f:
        for line in f:
            if line.startswith("VmRSS:"):
                return int(line.split()[1])
    return -1


def one_run(cmd, port, cold):
    if cold:
        evict(EVICT)
    env = dict(os.environ, PORT=str(port))
    t0 = time.monotonic_ns()
    proc = subprocess.Popen(cmd, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        deadline = time.monotonic() + 10
        while True:
            try:
                conn = http.client.HTTPConnection("127.0.0.1", port, timeout=0.2)
                conn.request("GET", "/api/status")
                resp = conn.getresponse()
                body = resp.read()
                if resp.status == 200:
                    break
                raise RuntimeError(f"status {resp.status}")
            except (ConnectionError, OSError):
                if time.monotonic() > deadline:
                    raise
                time.sleep(0.001)
        start_ms = (time.monotonic_ns() - t0) / 1e6
        doc = json.loads(body)
        assert doc["state"] == "ready", doc
        time.sleep(2.0)
        rss = rss_kb(proc.pid)
        return start_ms, rss, body
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--label", required=True)
    ap.add_argument("--runs", type=int, default=10)
    ap.add_argument("--port", type=int, default=18080)
    ap.add_argument("--cold", action="store_true")
    ap.add_argument("--evict", action="append", default=[])
    ap.add_argument("cmd", nargs=argparse.REMAINDER)
    args = ap.parse_args()
    cmd = args.cmd
    if cmd and cmd[0] == "--":
        cmd = cmd[1:]
    EVICT.extend(args.evict)
    rows = []
    body = b""
    for i in range(args.runs):
        start_ms, rss, body = one_run(cmd, args.port, args.cold)
        rows.append((i + 1, start_ms, rss))
        print(f"{args.label},{i + 1},{start_ms:.2f},{rss}", flush=True)
    starts = [r[1] for r in rows]
    rsses = [r[2] for r in rows]
    import hashlib

    print(
        f"SUMMARY {args.label} start_ms min={min(starts):.2f} "
        f"median={statistics.median(starts):.2f} max={max(starts):.2f} "
        f"rss_kb median={statistics.median(rsses):.0f} max={max(rsses)} "
        f"body_sha256={hashlib.sha256(body).hexdigest()}",
        flush=True,
    )


if __name__ == "__main__":
    sys.exit(main())
```

Invocations:

```console
$ sudo python3 harness/measure.py --label go127-warm --runs 20 --port 18100 -- \
    /usr/sbin/chroot /tmp/trixie /srv/status-go127
$ sudo python3 harness/measure.py --label go127-cold --runs 10 --cold --port 18101 \
    --evict /tmp/trixie/srv/status-go127 -- /usr/sbin/chroot /tmp/trixie /srv/status-go127
$ sudo python3 harness/measure.py --label go127-deps-warm --runs 20 --port 18102 -- \
    /usr/sbin/chroot /tmp/trixie /srv/status-go-deps127
$ sudo python3 harness/measure.py --label go127-deps-cold --runs 10 --cold --port 18103 \
    --evict /tmp/trixie/srv/status-go-deps127 -- /usr/sbin/chroot /tmp/trixie /srv/status-go-deps127
$ sudo python3 harness/measure.py --label py-warm --runs 20 --port 18086 -- \
    /usr/sbin/chroot /tmp/trixie /usr/bin/python3 /srv/server.py
$ sudo python3 harness/measure.py --label py-cold --runs 10 --cold --port 18087 \
    --evict /tmp/trixie/srv/server.py --evict /tmp/trixie/usr/bin/python3.13 \
    --evict /tmp/trixie/usr/lib/python3.13 --evict /tmp/trixie/usr/lib/x86_64-linux-gnu -- \
    /usr/sbin/chroot /tmp/trixie /usr/bin/python3 /srv/server.py
$ sudo python3 harness/measure.py --label py-deps-warm --runs 20 --port 18088 -- \
    /usr/sbin/chroot /tmp/trixie /usr/bin/python3 /srv/server-deps.py
$ sudo python3 harness/measure.py --label py-deps-cold --runs 10 --cold --port 18089 \
    --evict /tmp/trixie/srv/server-deps.py --evict /tmp/trixie/usr/bin/python3.13 \
    --evict /tmp/trixie/usr/lib/python3.13 --evict /tmp/trixie/usr/lib/x86_64-linux-gnu \
    --evict /tmp/trixie/usr/lib/python3/dist-packages -- \
    /usr/sbin/chroot /tmp/trixie /usr/bin/python3 /srv/server-deps.py
```

Medians and ranges (ms, RSS in KiB):

| Label | Runs | Start median | Start min | Start max | RSS median |
| --- | --- | --- | --- | --- | --- |
| go127-warm | 20 | 4.41 | 3.36 | 5.64 | 6,646 |
| go127-cold | 10 | 57.77 | 46.02 | 112.25 | 6,214 |
| go127-deps-warm | 20 | 8.13 | 6.81 | 10.58 | 9,890 |
| go127-deps-cold | 10 | 65.09 | 12.81 | 78.40 | 9,556 |
| py-warm | 20 | 42.35 | 37.52 | 49.88 | 18,602 |
| py-cold | 10 | 108.86 | 84.44 | 171.34 | 17,994 |
| py-deps-warm | 20 | 128.56 | 119.17 | 153.48 | 31,070 |
| py-deps-cold | 10 | 257.72 | 202.35 | 339.66 | 30,586 |

Source: M3.

The fadvise check, used to confirm the cold-start method:

```console
$ python3 - <<'EOF'
import os, time
p = '/tmp/oq02/go127.tar.gz'
def readall():
    t = time.monotonic()
    with open(p, 'rb') as f:
        while f.read(1 << 20):
            pass
    return (time.monotonic() - t) * 1000
print('warm read ms', readall())
fd = os.open(p, os.O_RDONLY)
os.posix_fadvise(fd, 0, 0, os.POSIX_FADV_DONTNEED)
os.close(fd)
print('after fadvise ms', readall())
EOF
warm read ms 13.745702000051097
after fadvise ms 83.18636718583107
# second run
warm read ms 4.043969999656838
after fadvise ms 86.85917300044821
```

Source: M3.

## Appendix D: update transaction

The Go step used `context.WithTimeout` and `exec.CommandContext`; the Python step used
`subprocess.run(timeout=...)` and a signal handler. Core of the Go timeout path:

```go
stepCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
defer cancel()
cmd := exec.CommandContext(stepCtx, "sleep", "5")
err := cmd.Run()
if stepCtx.Err() == context.DeadlineExceeded {
	rollback()
	os.Exit(3)
}
```

Core of the Python timeout path:

```python
try:
    subprocess.run(["sleep", "5"], timeout=0.3, check=True)
except subprocess.TimeoutExpired:
    rollback()
    sys.exit(3)
```

The full spike also had a signal mode: start `sleep 5`, send SIGTERM after 100 ms, kill and reap the
child, run the 50 ms rollback, and exit 0. Results:

```text
go,timeout,exit=3,wall_ms=353.8,mode=timeout cancelled=1 rollback=1 elapsed_ms=300
go,signal,exit=0,sig_to_exit_wall_ms=52.6,mode=signal rollback=1 signal_to_exit_ms=50
py,timeout,exit=3,wall_ms=381.5,mode=timeout cancelled=1 rollback=1 elapsed_ms=351
py,signal,exit=0,sig_to_exit_wall_ms=56.2,mode=signal rollback=1 signal_to_exit_ms=50
```

Each row was repeated three times; the table in the main text reports the ranges. Rollback markers
were present after every run and the child `sleep` was reaped. Source: M7.

## Appendix E: sizes, builds, and libraries

Binary sizes and static linkage:

```console
$ stat -c '%s %n' bin/status-go127 bin/status-go-deps127 bin/status-go127-arm64 \
    bin/deps-go127 bin/txn-go127 bin/status-go-ui127
6611104 bin/status-go127
7815328 bin/status-go-deps127
6095008 bin/status-go127-arm64
5509280 bin/deps-go127
1953952 bin/txn-go127
11722912 bin/status-go-ui127
$ ldd bin/status-go-deps127
	not a dynamic executable
$ du -sb deps-go/vendor
1231436 deps-go/vendor
```

The UI fixture was 5,000,000 bytes; the embedded build grew by 5,111,808 bytes. Source: M4, M9.

Python package sums used `apt-get -s --no-install-recommends install python3 python3-yaml
python3-jsonschema python3-jinja2`, then summed `Installed-Size` for every `Inst` package:

```text
packages=23
installed_size_kb=26822
interpreter_only_kb=23793
libraries_only_kb=3029
```

With recommended packages the same command produced 40 packages and 76,527 KiB. Venv sizes:

```text
base_venv_bytes=10283874
full_venv_bytes=17460410
nopip_venv_bytes=7190631
wheelhouse_bytes=1533717
```

Wheelhouse contents: 8 wheels (`attrs`, `jinja2`, `jsonschema`, `jsonschema_specifications`,
`markupsafe`, `pyyaml`, `referencing`, `rpds_py`). Offline install with `pip install --no-index
--find-links /wheels pyyaml jsonschema jinja2` succeeded in 0.62 s; venv creation took 1.65 s. The
wheel download took 4.918 s over the network. Source: M5, M6.

Build and cross-compilation:

```console
$ time CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/status-go127 .
real    0m7.888s
$ time CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/status-go-deps127 .
real    0m8.069s
$ time GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' \
    -o bin/status-go127-arm64 .
real    0m7.474s
$ GOPROXY=off GOFLAGS=-mod=vendor go build -trimpath -ldflags='-s -w' \
    -o bin/deps-go127-offline .
$ echo $?
0
$ cat deps-go/vendor/modules.txt | grep '^# '
# github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
# golang.org/x/text v0.14.0
# gopkg.in/yaml.v3 v3.0.1
$ wc -l status-go-deps/main.go txn-go/main.go status-py-deps/server.py txn-py/txn.py
74 status-go-deps/main.go
53 txn-go/main.go
68 status-py-deps/server.py
60 txn-py/txn.py
```

Source: M6.

`go get` (with the earlier Go 1.24.10 toolchain) fetched the three modules in 2.814 s on the first run;
later builds used the module cache and the vendor directory. The offline vendor build above exits 0.
Source: M6.

Library versions and benchmarks:

```console
$ curl -sS 'https://go.dev/dl/?mode=json' | jq -r '.[].version' | head -2
go1.27.4
go1.26.11
$ curl -sS https://proxy.golang.org/gopkg.in/yaml.v3/@latest
{"Version":"v3.0.1","Time":"2022-05-27T08:35:30Z"}
$ curl -sS https://proxy.golang.org/github.com/santhosh-tekuri/jsonschema/v6/@latest
{"Version":"v6.0.3","Time":"2026-06-28T17:38:00Z", ...}
$ sudo chroot /tmp/trixie /srv/deps-go127
validate_1000_ms=40.80
render_1000_ms=0.70
unit_bytes=228
$ sudo chroot /tmp/trixie /usr/bin/python3 /srv/check.py
validate_1000_ms=357.89
render_1000_ms=4.70
unit_bytes=228
```

The medians in the main table come from three runs each: Go validation 40.80, 46.18, 52.37 ms and
render 0.70, 0.71, 0.90 ms; Python validation 337.88, 357.89, 359.81 ms and render 3.88, 4.16, 4.70
ms. The upstream-venv Python run produced validation medians near 394.89 ms, not materially
different. Source: M8.

YAML semantics test, same document in both:

```text
Go:     yes_value=string(yes)  no_value=string(no)  on_value=string(on)  off_value=string(off)
        octal=int(10)  duplicate keys: error "mapping key already defined"
Python: yes_value=bool(True)   no_value=bool(False) on_value=bool(True)  off_value=bool(False)
        octal=int(10)  duplicate keys: last value wins, no error
```

Source: M8.

## Appendix F: contributor and package counts

```console
$ curl -sS 'https://api.stackexchange.com/2.3/tags/python;go/info?site=stackoverflow' \
    | jq -r '.items[] | "\(.name) \(.count)"'
python 2204332
go 74453
$ gh api -X GET search/repositories -f q='language:go' -f per_page=1 --jq .total_count
2619929
$ gh api -X GET search/repositories -f q='language:python' -f per_page=1 --jq .total_count
36090679
$ apt-cache search --names-only '^python3-' | wc -l
5330
$ apt-cache search --names-only '^golang-' | wc -l
2398
```

The Stack Overflow and GitHub counts are ecosystem size proxies retrieved on 2026-11-14; they do not
measure quality, review capacity, or the maintainer's skill. Source: M10.

## Appendix G: Debian 13 package and security facts

```console
$ apt-cache policy python3 python3.13 python3-yaml python3-jsonschema python3-jinja2 \
    python3-pytest python3-systemd golang-go golang-1.24-go mkosi
python3:            3.13.5-1
python3.13:         3.13.5-2+deb13u5
python3-yaml:       6.0.2-1+b2
python3-jsonschema: 4.19.2-6
python3-jinja2:     3.1.6-1
python3-pytest:     8.3.5-2
python3-systemd:    235-1+b6
golang-go:          2:1.24~2
golang-1.24-go:     1.24.4-1
mkosi:              25.3-7, Depends: ... python3:any ...
$ curl -sS http://deb.debian.org/debian-security/dists/trixie-security/Release | head -5
Origin: Debian
Label: Debian-Security
Suite: stable-security
Codename: trixie-security
Date: Sat, 14 Nov 2026 04:03:02 UTC
```

The policy block is an abridged copy of the full `apt-cache policy` output; versions and the
`python3:any` dependency were copied from it. The security archive was added to the clean chroot
before the candidate check. Go's release policy (<https://go.dev/doc/devel/release#policy>) supports
each major release until two newer majors exist, so Go 1.24 is outside support on 2026-11-14; the
current releases were 1.27.4 and 1.26.11 (M1). Source: M11.
