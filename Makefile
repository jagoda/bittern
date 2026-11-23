PYTHON ?= python3
GO ?= go
SCHEMA_VALIDATOR = tests/checks/validate_config.py
AGENT_DIR = agent
# Indirect reference: make -n must print this recipe, not execute it because the
# shell line also contains a recursive make invocation.
SUBMAKE = $(MAKE)

# Beta bundle metadata. The generation is the anti-rollback counter from ADR
# 0031 and must equal the suffix of the publish tag and the signed manifest.
GENERATION ?= 1
BUNDLE_VERSION ?= 0.0.0-beta.$(GENERATION)
PAYLOAD_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || printf 'unknown')
PAYLOAD_COMMIT_SHORT = $(shell printf '%s' '$(PAYLOAD_COMMIT)' | cut -c1-12)
# The kit baseline is not a published generation. Its version never matches a
# bundle built from the same commit, so the UI cannot offer the running factory
# version as an update. The full commit and generation 0 are in the kit VERSION
# stamp; the binary carries the short form.
BASELINE_VERSION ?= 0.0.0-factory-$(PAYLOAD_COMMIT_SHORT)
BUNDLE_CHANNEL ?= beta
PAYLOAD_DIR = build/payload
BUNDLE_STAGE = $(PAYLOAD_DIR)/bundle
APP_PAYLOAD = $(PAYLOAD_DIR)/bittern-app.tar.gz
MANIFEST = $(PAYLOAD_DIR)/manifest.json
BETA_KIT_NAME = bittern-beta-kit
BETA_KIT_STAGE = $(PAYLOAD_DIR)/kit/$(BETA_KIT_NAME)
BETA_KIT_ARCHIVE = $(PAYLOAD_DIR)/$(BETA_KIT_NAME).tar.gz
BETA_KIT_SUMS = $(PAYLOAD_DIR)/SHA256SUMS
PAYLOAD_EPOCH ?= 0
PAYLOAD_CREATED ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
# The CI test anchor is public. Its SHA-256 is the signing key id the manifest
# must name; the private seed is only in the protected beta environment. A QEMU
# run overrides KEY_PUB with a per-run ephemeral key so the kit carries that
# anchor instead of the real CI test key.
KEY_PUB ?= packaging/trust/beta-ci.pub
KEY_ID ?= $(shell sha256sum $(KEY_PUB) | cut -d' ' -f1)
# The beta compatibility rule. The provisional platform provider defines OS
# generation 1 as the Debian 13 amd64 base, so a beta test bundle claims that one
# generation and no wider range. See defaults/release.yaml and the phase two note.
OS_GENERATION_MIN ?= 1
OS_GENERATION_MAX ?= 1
SCHEMA_MIN ?= 1
SCHEMA_MAX ?= 2
UPDATER_MIN ?= 1

.PHONY: help check schema link-check style-check design-check bundle-check cleanup-test storage-test unit-enablement-check version-check release-preflight qemu-test qemu-firstboot qemu-firstboot-stock qemu-firstboot-clean qemu-firstboot-late-camera qemu-adoption qemu-update qemu-stop assets test render detect render-poc status ui-test ui-browser-check bitternd bittern-release apply-test first-boot-poc beta-payload

help:
	@printf '%s\n' \
		'Bittern repository tasks:' \
		'' \
		'  make check       Run every local check (link, schema, style, design, bundle, release, test)' \
		'  make schema      Validate the schema example and fixture suite' \
		'  make link-check  Check Markdown links and component READMEs' \
		'  make style-check Check Markdown style rules' \
		'  make design-check Check stylesheet tokens, casing, and !important rules' \
		'  make bundle-check Check the pinned Frigate fixture manifest' \
		'  make cleanup-test Test packaging/cleanup.sh against a temporary root' \
		'  make storage-test Test packaging/storage-setup.sh against fixture disks' \
		'  make unit-enablement-check  Check units enabled by the payload against the QEMU reboot assertions' \
		'  make version-check  Check the factory baseline and bundle version stamps stay distinct' \
		'  make release-preflight  Check the release preconditions and print the next generation' \
		'  make qemu-test   Boot the two-NIC QEMU harness and run its tests' \
		'  make qemu-firstboot  Run first boot, the TLS wizard and one camera in QEMU' \
		'  make qemu-firstboot-stock  Run first boot from stock NIC names in QEMU' \
		'  make qemu-firstboot-clean  Run install and first boot from no network or DNS in QEMU' \
		'  make qemu-firstboot-late-camera  Reboot with cam0 absent and assert first boot commits' \
		'  make qemu-adoption  Adopt a DHCP camera through discovery in QEMU' \
		'  make qemu-update  Install, update, roll back and reject bundles in QEMU' \
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
		'  make beta-payload Build the app bundle payload, manifest, beta kit and checksums' \
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
	$(PYTHON) tests/checks/release_asset_names.py || status=1; \
	$(PYTHON) tests/checks/tag_corpus.py || status=1; \
	$(PYTHON) tests/release/test_release_preflight.py || status=1; \
	$(PYTHON) tests/release/test_verify_release_draft.py || status=1; \
	$(SUBMAKE) version-check || status=1; \
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

# A factory baseline and a published bundle from the same commit must never
# share a version string. The baseline must not look like a generation tag.
version-check:
	@set -eu; \
	baseline='$(BASELINE_VERSION)'; \
	bundle='$(BUNDLE_VERSION)-$(PAYLOAD_COMMIT_SHORT)'; \
	[ "$$baseline" != "$$bundle" ] || { \
		printf 'version stamp collision: baseline and bundle are both %s\n' "$$baseline" >&2; exit 1; }; \
	case "$$baseline" in \
		0.0.0-factory-*) ;; \
		*) printf 'baseline version %s is not a factory baseline\n' "$$baseline" >&2; exit 1 ;; \
	esac; \
	case "$$bundle" in \
		0.0.0-beta.*) ;; \
		*) printf 'bundle version %s is not a published beta version\n' "$$bundle" >&2; exit 1 ;; \
	esac; \
	printf 'version stamp: baseline %s; bundle %s\n' "$$baseline" "$$bundle"

# Release preflight per ADR 0033. This target is read-only: it verifies the
# main check run, the beta-draft secret name, the environment ref policies,
# computes the next generation, validates the tag with the Python tag rule
# checked against tests/fixtures/release/tag-cases.json, and prints the next
# action. It needs only python3 and gh; it does not build or invoke the Go
# tool. It creates no tag, draft, or release. Pass RELEASE_PREFLIGHT_ARGS=--plan
# for the explicit plan banner, or RELEASE_PREFLIGHT_ARGS=--tag-path for the
# tag-push fallback checks.
RELEASE_PREFLIGHT_ARGS ?=
release-preflight:
	@$(PYTHON) packaging/release_preflight.py $(RELEASE_PREFLIGHT_ARGS)

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

qemu-update:
	@$(PYTHON) tests/qemu/run.py update

qemu-stop:
	@$(PYTHON) tests/qemu/run.py stop

# The Go module is under agent/, while templates/, defaults/, schema/ and ui/ live
# at the repository root. go:embed cannot reach outside the module, so copy the
# repository-owned inputs into agent/internal/assets/ before every Go command.
assets:
	@agent/scripts/sync-assets.sh

# Beta release assets. This target is deliberately not part of make check: it
# needs the Go toolchain and produces gitignored artifacts. It builds the two
# static linux/amd64 binaries, packs the files-only application payload and its
# canonical manifest, then builds the unsigned beta preparation kit and its
# published SHA256SUMS. The manifest is not signed here; sign is a separate
# step and this target never reads a private key.
beta-payload: assets bittern-release
	@set -eu; \
	bundle="$(BUNDLE_STAGE)"; \
	kit_root="$(abspath $(PAYLOAD_DIR))/kit"; \
	kit="$(abspath $(BETA_KIT_STAGE))"; \
	archive="$(BETA_KIT_ARCHIVE)"; \
	payload_out="$(abspath $(PAYLOAD_DIR))"; \
	trap 'rm -rf "$$bundle" "$$kit_root"' EXIT; \
	rm -rf "$$bundle" "$$kit_root"; \
	mkdir -p "$$bundle/usr/bin" "$$kit/templates" "$$kit/trust/release-keys" "$(PAYLOAD_DIR)"; \
	(cd $(AGENT_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOPROXY=off GOFLAGS=-mod=vendor \
		$(GO) build -trimpath -buildvcs=false -ldflags='-s -w' \
		-o "$$payload_out/bundle/usr/bin/bittern" ./cmd/bittern); \
	(cd $(AGENT_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOPROXY=off GOFLAGS=-mod=vendor \
		$(GO) build -trimpath -buildvcs=false \
		-ldflags='-s -w -X main.version=$(BUNDLE_VERSION)-$(PAYLOAD_COMMIT_SHORT)' \
		-o "$$payload_out/bundle/usr/bin/bitternd" ./cmd/bitternd); \
	(cd $(AGENT_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOPROXY=off GOFLAGS=-mod=vendor \
		$(GO) build -trimpath -buildvcs=false \
		-ldflags='-s -w -X main.version=$(BASELINE_VERSION) -X main.factoryBaseline=true' \
		-o "$$kit/bitternd" ./cmd/bitternd); \
	(cd $(AGENT_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOPROXY=off GOFLAGS=-mod=vendor \
		$(GO) build -trimpath -buildvcs=false -ldflags='-s -w' \
		-o "$$kit/bittern-launch" ./cmd/bittern-launch); \
	agent/bin/bittern-release pack --tree "$$bundle" --out "$$payload_out" \
		--version "$(BUNDLE_VERSION)" --generation "$(GENERATION)" --channel "$(BUNDLE_CHANNEL)" \
		--source-commit "$(PAYLOAD_COMMIT)" --key-id "$(KEY_ID)" \
		--os-generation-min "$(OS_GENERATION_MIN)" --os-generation-max "$(OS_GENERATION_MAX)" \
		--schema-min "$(SCHEMA_MIN)" --schema-max "$(SCHEMA_MAX)" --updater-min "$(UPDATER_MIN)" \
		--created "$(PAYLOAD_CREATED)" --epoch "$(PAYLOAD_EPOCH)"; \
	install -m 0755 "$$bundle/usr/bin/bittern" "$$kit/bittern"; \
	install -m 0644 packaging/platform.json "$$kit/platform.json"; \
	install -m 0644 agent/packaging/bittern-first-boot.service "$$kit/bittern-first-boot.service"; \
	install -m 0644 agent/packaging/bittern-image-prewarm.service "$$kit/bittern-image-prewarm.service"; \
	install -m 0644 agent/packaging/bittern-update.service "$$kit/bittern-update.service"; \
	install -m 0644 agent/packaging/bitternd.service "$$kit/bitternd.service"; \
	install -m 0755 packaging/install.sh "$$kit/install.sh"; \
	install -m 0755 packaging/cleanup.sh "$$kit/cleanup.sh"; \
	install -m 0755 packaging/storage-setup.sh "$$kit/storage-setup.sh"; \
	install -m 0644 "$(KEY_PUB)" "$$kit/trust/release-keys/$(KEY_ID).pub"; \
	(cd templates && find . -type f ! -name README.md -print | while IFS= read -r file; do \
		install -d -m 0755 "$$kit/templates/$$(dirname "$$file")"; \
		install -m 0644 "$$file" "$$kit/templates/$$file"; \
	done); \
	sed -e 's/@BUNDLE_VERSION@/$(BUNDLE_VERSION)/g' \
		-e 's/@GENERATION@/$(GENERATION)/g' \
		-e 's/@PAYLOAD_COMMIT@/$(PAYLOAD_COMMIT)/g' \
		-e 's/@PAYLOAD_COMMIT_SHORT@/$(PAYLOAD_COMMIT_SHORT)/g' \
		-e 's/@BASELINE_VERSION@/$(BASELINE_VERSION)/g' \
		-e 's/@KEY_ID@/$(KEY_ID)/g' \
		packaging/README-beta-kit.md > "$$kit/README.md"; \
	{ \
		printf 'product: bittern\n'; \
		printf 'baseline: true\n'; \
		printf 'channel: %s\n' "$(BUNDLE_CHANNEL)"; \
		printf 'generation: 0\n'; \
		printf 'version: %s\n' "$(BASELINE_VERSION)"; \
		printf 'source_commit: %s\n' "$(PAYLOAD_COMMIT)"; \
		printf 'payload_sha256: '; sha256sum "$(APP_PAYLOAD)" | cut -d' ' -f1; \
	} > "$$kit/VERSION"; \
	(cd "$$kit" && find . -type f ! -name SHA256SUMS -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | sed 's|  \./|  |' > SHA256SUMS); \
	rm -f "$$archive" "$$archive.tar"; \
	tar --sort=name --mtime="@$(PAYLOAD_EPOCH)" --owner=0 --group=0 --numeric-owner \
		-C "$$kit_root" -cf "$$archive.tar" "$(BETA_KIT_NAME)"; \
	gzip -9n -c "$$archive.tar" > "$$archive"; \
	rm -f "$$archive.tar"; \
	(cd "$(PAYLOAD_DIR)" && sha256sum "$(BETA_KIT_NAME).tar.gz" > SHA256SUMS); \
	printf 'payload: %s\n' "$(APP_PAYLOAD)"; \
	sha256sum "$(APP_PAYLOAD)"; \
	printf 'manifest: %s\n' "$(MANIFEST)"; \
	sha256sum "$(MANIFEST)"; \
	printf 'kit: %s\n' "$$archive"; \
	sha256sum "$$archive"; \
	printf 'checksums: %s\n' "$(BETA_KIT_SUMS)"; \
	cat "$(BETA_KIT_SUMS)"; \
	printf 'kit contents:\n'; \
	tar -tzf "$$archive"

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
