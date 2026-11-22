PYTHON ?= python3
GO ?= go
SCHEMA_VALIDATOR = tests/checks/validate_config.py
AGENT_DIR = agent
# Indirect reference: make -n must print this recipe, not execute it because the
# shell line also contains a recursive make invocation.
SUBMAKE = $(MAKE)

# POC payload metadata. The archive name carries the release tag, or 0.0.0-poc
# when there is no tag, plus the commit hash so a copied tarball is traceable.
PAYLOAD_VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || printf '0.0.0-poc')
PAYLOAD_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || printf 'unknown')
PAYLOAD_COMMIT_SHORT = $(shell printf '%s' '$(PAYLOAD_COMMIT)' | cut -c1-12)
PAYLOAD_NAME ?= bittern-poc-$(PAYLOAD_VERSION)-$(PAYLOAD_COMMIT_SHORT)
PAYLOAD_DIR = build/payload
PAYLOAD_STAGE = $(PAYLOAD_DIR)/stage/$(PAYLOAD_NAME)
PAYLOAD_ARCHIVE = $(PAYLOAD_DIR)/$(PAYLOAD_NAME).tar.gz
PAYLOAD_EPOCH ?= 0

.PHONY: help check schema link-check style-check design-check bundle-check cleanup-test storage-test unit-enablement-check qemu-test qemu-firstboot qemu-firstboot-stock qemu-firstboot-clean qemu-firstboot-late-camera qemu-adoption qemu-stop assets test render detect render-poc status ui-test ui-browser-check bitternd bittern-release apply-test first-boot-poc poc-payload

help:
	@printf '%s\n' \
		'Bittern repository tasks:' \
		'' \
		'  make check       Run every local check (link, schema, style, design, bundle, test)' \
		'  make schema      Validate the schema example and fixture suite' \
		'  make link-check  Check Markdown links and component READMEs' \
		'  make style-check Check Markdown style rules' \
		'  make design-check Check stylesheet tokens, casing, and !important rules' \
		'  make bundle-check Check the pinned Frigate fixture manifest' \
		'  make cleanup-test Test packaging/cleanup.sh against a temporary root' \
		'  make storage-test Test packaging/storage-setup.sh against fixture disks' \
		'  make unit-enablement-check  Check units enabled by the payload against the QEMU reboot assertions' \
		'  make qemu-test   Boot the two-NIC QEMU harness and run its tests' \
		'  make qemu-firstboot  Run first boot, the TLS wizard and one camera in QEMU' \
		'  make qemu-firstboot-stock  Run first boot from stock NIC names in QEMU' \
		'  make qemu-firstboot-clean  Run install and first boot from no network or DNS in QEMU' \
		'  make qemu-firstboot-late-camera  Reboot with cam0 absent and assert first boot commits' \
		'  make qemu-adoption  Adopt a DHCP camera through discovery in QEMU' \
		'  make qemu-stop   Stop the QEMU guests, including ones with missing pidfiles' \
		'  make test        Run the Go tests offline (go test ./...)' \
		'  make render      Run the golden render suite' \
		'  make status      Run the status contract and golden tests' \
		'  make ui-test     Run the first-run wizard and status page tests' \
		'  make ui-browser-check  Run the opt-in Playwright UI check at phone and desktop widths' \
		'  make detect      Print the read-only hardware inventory and proposal' \
		'  make render-poc  Render CONFIG=... into OUT=...' \
		'  make apply-test  Run the apply and first-boot tests' \
		'  make first-boot-poc FIRST_BOOT_ROOT=...  Run first boot offline in a temp root' \
		'  make poc-payload Build the POC bridge tarball under build/payload' \
		'  make bitternd    Build the daemon into agent/bin' \
		'  make bittern-release  Build the offline release signing tool into agent/bin' \
		'  make assets      Regenerate the embedded templates, defaults, schema, and UI assets' \
		'  make help        Show this help' \
		'' \
		'Checks run offline. make check needs Python 3, PyYAML, jsonschema, make and Go;' \
		'make test and make render need the Go toolchain.' \
		'make ui-browser-check is opt-in and needs Node, the pinned Playwright package,' \
		'and its Chromium; it is not part of make check.'

schema:
	@$(PYTHON) $(SCHEMA_VALIDATOR) schema/examples/four-cameras.yaml
	@$(PYTHON) $(SCHEMA_VALIDATOR) --suite tests/schema/cases.json

link-check:
	@$(PYTHON) tests/checks/link_check.py

style-check:
	@$(PYTHON) tests/checks/style_check.py

design-check:
	@$(PYTHON) tests/checks/design_check.py

check:
	@status=0; \
	$(PYTHON) tests/checks/link_check.py || status=1; \
	$(PYTHON) $(SCHEMA_VALIDATOR) schema/examples/four-cameras.yaml || status=1; \
	$(PYTHON) $(SCHEMA_VALIDATOR) --suite tests/schema/cases.json || status=1; \
	$(PYTHON) tests/checks/style_check.py || status=1; \
	$(PYTHON) tests/checks/design_check.py || status=1; \
	$(PYTHON) tests/fixtures/frigate/check.py || status=1; \
	$(PYTHON) tests/cleanup/test_cleanup.py || status=1; \
	$(PYTHON) tests/storage/test_storage_setup.py || status=1; \
	$(PYTHON) tests/checks/unit_enablement.py || status=1; \
	$(SUBMAKE) test || status=1; \
	exit $$status

bundle-check:
	@$(PYTHON) tests/fixtures/frigate/check.py

cleanup-test:
	@$(PYTHON) tests/cleanup/test_cleanup.py

storage-test:
	@$(PYTHON) tests/storage/test_storage_setup.py

unit-enablement-check:
	@$(PYTHON) tests/checks/unit_enablement.py

qemu-test:
	@$(PYTHON) tests/qemu/run.py test

qemu-firstboot:
	@$(PYTHON) tests/qemu/run.py firstboot

qemu-firstboot-stock:
	@$(PYTHON) tests/qemu/run.py firstboot --stock-names

qemu-firstboot-clean:
	@$(PYTHON) tests/qemu/run.py firstboot --clean-network

qemu-firstboot-late-camera:
	@$(PYTHON) tests/qemu/run.py firstboot --late-camera

qemu-adoption:
	@$(PYTHON) tests/qemu/run.py adoption

qemu-stop:
	@$(PYTHON) tests/qemu/run.py stop

# The Go module is under agent/, while templates/, defaults/, schema/ and ui/ live
# at the repository root. go:embed cannot reach outside the module, so copy the
# repository-owned inputs into agent/internal/assets/ before every Go command.
assets:
	@agent/scripts/sync-assets.sh

# POC bridge payload. This target is deliberately not part of make check: it
# needs the Go toolchain and produces a gitignored artifact. The two binaries
# are static linux/amd64 builds with stripped symbols. File order and mtimes are
# fixed so the same tree produces the same tarball.
poc-payload: assets
	@set -eu; \
	stage="$(PAYLOAD_STAGE)"; \
	stage_root="$(PAYLOAD_DIR)/stage"; \
	archive="$(PAYLOAD_ARCHIVE)"; \
	trap 'rm -rf "$$stage_root"' EXIT; \
	rm -rf "$$stage_root"; \
	mkdir -p "$$stage" "$(PAYLOAD_DIR)"; \
	rm -f "$(PAYLOAD_DIR)"/bittern-poc-*.tar.gz; \
	(cd $(AGENT_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOPROXY=off GOFLAGS=-mod=vendor \
		$(GO) build -trimpath -buildvcs=false -ldflags='-s -w' \
		-o "$(abspath $(PAYLOAD_STAGE))/bittern" ./cmd/bittern); \
	(cd $(AGENT_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOPROXY=off GOFLAGS=-mod=vendor \
		$(GO) build -trimpath -buildvcs=false \
		-ldflags='-s -w -X main.version=$(PAYLOAD_VERSION)-$(PAYLOAD_COMMIT_SHORT)' \
		-o "$(abspath $(PAYLOAD_STAGE))/bitternd" ./cmd/bitternd); \
	install -m 0644 agent/packaging/bittern-first-boot.service "$$stage/bittern-first-boot.service"; \
	install -m 0644 agent/packaging/bittern-image-prewarm.service "$$stage/bittern-image-prewarm.service"; \
	install -m 0644 agent/packaging/bittern-update.service "$$stage/bittern-update.service"; \
	install -m 0644 agent/packaging/bitternd.service "$$stage/bitternd.service"; \
	install -m 0755 packaging/install.sh "$$stage/install.sh"; \
	install -m 0755 packaging/cleanup.sh "$$stage/cleanup.sh"; \
	install -m 0755 packaging/storage-setup.sh "$$stage/storage-setup.sh"; \
	sed -e 's/@PAYLOAD_VERSION@/$(PAYLOAD_VERSION)/g' \
		-e 's/@PAYLOAD_COMMIT@/$(PAYLOAD_COMMIT)/g' \
		-e 's/@PAYLOAD_COMMIT_SHORT@/$(PAYLOAD_COMMIT_SHORT)/g' \
		packaging/README-payload.md > "$$stage/README.md"; \
	(cd "$$stage" && \
		sha256sum bittern bitternd bittern-first-boot.service bittern-image-prewarm.service bittern-update.service bitternd.service install.sh cleanup.sh storage-setup.sh README.md > SHA256SUMS); \
	tar_file="$$archive.tar"; \
	rm -f "$$tar_file" "$$archive"; \
	tar --sort=name --mtime="@$(PAYLOAD_EPOCH)" --owner=0 --group=0 --numeric-owner \
		-C "$(PAYLOAD_DIR)/stage" -cf "$$tar_file" "$(PAYLOAD_NAME)"; \
	gzip -9n -c "$$tar_file" > "$$archive"; \
	rm -f "$$tar_file"; \
	printf 'payload: %s\n' "$$archive"; \
	printf 'contents:\n'; \
	tar -tzf "$$archive"; \
	printf 'sha256: '; sha256sum "$$archive" | cut -d' ' -f1; \
	printf 'size: '; du -h "$$archive" | cut -f1

test: assets
	@cd $(AGENT_DIR) && CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=vendor $(GO) test -count=1 ./...

render: assets
	@cd $(AGENT_DIR) && CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=vendor $(GO) test -count=1 ./internal/render -run 'TestGolden|TestDiffDetectsOneCharacterDrift'

status: assets
	@cd $(AGENT_DIR) && CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=vendor $(GO) test -count=1 ./internal/status

ui-test: assets
	@cd $(AGENT_DIR) && CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=vendor $(GO) test -count=1 ./internal/ui

# Opt-in Playwright check for the responsive UI (ADR 0025). It is deliberately
# not a dependency of check, test, schema, style-check, link-check, or ui-test.
# The browser and Node package are provisioned by hand; see tests/browser/README.md.
ui-browser-check: assets
	@command -v node >/dev/null 2>&1 || { \
		printf '%s\n' 'ui-browser-check needs Node.js 20 or newer; install Node, then run: cd tests/browser && npm ci && npx playwright install --with-deps chromium' >&2; \
		exit 2; \
	}
	@node -e 'process.exit(Number(process.versions.node.split(".")[0]) >= 20 ? 0 : 1)' >/dev/null 2>&1 || { \
		printf '%s\n' "ui-browser-check needs Node.js 20 or newer (found $$(node --version)); install Node, then run: cd tests/browser && npm ci && npx playwright install --with-deps chromium" >&2; \
		exit 2; \
	}
	@test -d tests/browser/node_modules/@playwright/test || { \
		printf '%s\n' 'ui-browser-check needs the pinned Playwright package; run: cd tests/browser && npm ci' >&2; \
		exit 2; \
	}
	@mkdir -p build/browser
	@cd $(AGENT_DIR) && CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=vendor $(GO) build -trimpath -o ../build/browser/bitternd ./cmd/bitternd
	@BITTERND_BIN="$(abspath build/browser/bitternd)" node tests/browser/check.mjs

detect: assets
	@cd $(AGENT_DIR) && CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=vendor $(GO) run ./cmd/bittern detect

render-poc: assets
	@test -n "$(CONFIG)" || { printf '%s\n' 'render-poc needs CONFIG=... OUT=...' >&2; exit 2; }
	@test -n "$(OUT)" || { printf '%s\n' 'render-poc needs CONFIG=... OUT=...' >&2; exit 2; }
	@cd $(AGENT_DIR) && CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=vendor $(GO) run ./cmd/bittern render --config "$(abspath $(CONFIG))" --out "$(abspath $(OUT))"

bitternd: assets
	@mkdir -p $(AGENT_DIR)/bin
	@cd $(AGENT_DIR) && CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=vendor $(GO) build -trimpath -ldflags='-s -w' -o bin/bitternd ./cmd/bitternd

# The release signing tool runs on an offline signing machine. It is not part of
# the appliance payload or the image, so it has no assets to embed and no
# payload target builds it.
bittern-release:
	@mkdir -p $(AGENT_DIR)/bin
	@cd $(AGENT_DIR) && CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=vendor $(GO) build -trimpath -buildvcs=false -ldflags='-s -w' -o bin/bittern-release ./cmd/bittern-release

FIRST_BOOT_ROOT ?= build/first-boot

apply-test: assets
	@cd $(AGENT_DIR) && CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=vendor $(GO) test -count=1 ./internal/apply ./internal/firstboot

first-boot-poc: assets
	@cd $(AGENT_DIR) && CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=vendor $(GO) run ./cmd/bittern first-boot --offline \
		--state-root "$(abspath $(FIRST_BOOT_ROOT))/state" \
		--config-root "$(abspath $(FIRST_BOOT_ROOT))/etc" \
		--live-root "$(abspath $(FIRST_BOOT_ROOT))/live"
