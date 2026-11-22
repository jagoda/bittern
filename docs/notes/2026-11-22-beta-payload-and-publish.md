# Beta payload, preparation kit, and tag publish workflow

- Date: 2026-11-22
- Status: implementation note. The bundle packer, `make beta-payload`, and the tag-driven publish
  workflow landed. No tag was created and no release was published. The QEMU update scenario and the
  production offline signing ceremony remain open.
- Related: [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline),
  [BIT-009](../plan/implementation-plan.md#bit-009-app-bundle-update-transaction-with-rollback),
  [update transaction specification](2026-11-22-update-transaction-spec.md),
  [channel, CI, and publication](2026-11-22-channel-ci-and-publication.md),
  [packer](../../agent/cmd/bittern-release/pack.go),
  [tag parser](../../agent/internal/release/tag.go),
  [publish workflow](../../.github/workflows/publish.yml),
  [beta kit README](../../packaging/README-beta-kit.md),
  [release defaults](../../defaults/release.yaml)

## What landed

| Area | Change |
| --- | --- |
| Packer | `bittern-release pack` turns a built tree into a deterministic files-only `bittern-app.tar.gz` and a canonical `manifest.json`. The manifest is built from the archive bytes, so `payload.filename`, `payload.size`, `payload.sha256`, and `files` describe exactly the archive that ships. |
| Determinism | Entries are sorted by path, the tar and gzip timestamps come from `--epoch` (default 0), and the owner and group are 0. A second run with the same tree and arguments produces identical payload and manifest bytes. |
| Signing | The packer never reads a private key. `bittern-release sign` is still the only signing step, and it refuses a manifest whose `signing.key_id` is not the key file's id. |
| Tag parser | `agent/internal/release.ParseBetaTag` and `bittern-release tag` implement the ADR 0031 rule: `app-beta-` plus exactly six lowercase hexadecimal characters, parsed as base 16, with generation 0 rejected. The publish workflow calls this command instead of reimplementing the rule in shell. |
| Kit target | `make beta-payload` supersedes `make poc-payload`. It builds the two static binaries, packs the application payload and manifest, builds `bittern-beta-kit.tar.gz`, and writes the published `SHA256SUMS` for the kit. Every caller, runbook, and help line now uses `make beta-payload`. |
| Kit contents | The kit carries the packaging install, cleanup, and storage scripts, the four units including `bittern-update.service`, all templates, `trust/release-keys/<key_id>.pub` for `/etc/bittern/trust/release-keys`, a README, a VERSION stamp naming the generation and source commit, and an internal `SHA256SUMS`. It is unsigned, digest only, and is not an update payload. |
| Installer | `packaging/install.sh` installs the CI test anchor when `trust/release-keys` is present. It never installs a release anchor. The README and runbooks verify the kit with `sha256sum -c SHA256SUMS` before extracting or running it. |
| Publish workflow | `.github/workflows/publish.yml` runs on `app-beta-*`, validates the exact tag rule first, calls the phase one check workflow as a reusable check, and then uses two release jobs. The draft job has `contents: write` and no environment: it downloads the unsigned assets, checks the tag generation and source commit against the manifest, signs with the CI test key, verifies the signature, creates a draft with exactly the five release assets, and records their digests. The publish job requires the `beta` environment, takes no build action, re-verifies the draft's five asset names and recorded digests, and publishes the existing draft. |
| Permissions and pins | The workflow has `contents: read` at workflow level. The draft and publish jobs each have `contents: write`; only the publish job has the `beta` environment. Every action is GitHub-owned and pinned by full commit SHA. |
| Checks | `check.yml` is now reusable, and its build job produces the unsigned beta payload, manifest, kit, and checksums as `bittern-beta-unsigned-test-build`. |

The five release assets are `manifest.json`, `manifest.json.sig`, `bittern-app.tar.gz`,
`bittern-beta-kit.tar.gz`, and `SHA256SUMS`. The kit's internal checksum file covers its own files;
the published `SHA256SUMS` covers the kit tarball.

## Compatibility rule for beta test bundles

`make beta-payload` sets `os_generation_min` and `os_generation_max` to 1, and
[`defaults/release.yaml`](../../defaults/release.yaml) records the same rule with `schema_min` 1,
`schema_max` 2, and `updater_min` 1.

The platform provider is provisional. It reads `/usr/lib/bittern/platform.json`, and the beta kit
now installs that file with `schema_version` 1 and `generation` 1, mode 0644 and root-owned. No image
build writes it yet, so generation 1 is a kit and bench value until the image ADR fixes the image
layout. The rule defines generation 1 as the Debian 13 amd64 base that the beta kit and the bench
runs use. A beta bundle therefore claims exactly one OS generation, not a range from 0 to the
maximum. That is deliberate:

- A range that accepts every generation would silently claim compatibility with an OS that was
  never tested. The verifier would accept the bundle on an unknown platform and the claim would be
  false.
- A missing metadata file fails closed at preflight with `platform.missing`, so a device that cannot
  prove its generation does not update. A file that is not root-owned, is a symlink, or is group- or
  other-writable fails closed for the same reason.
- When the image writes generation 1 metadata under ADR 0030 and a later generation is tested, the
  range widens in a reviewed change with dated evidence. Widening it before that is not a default.
- The manifest integer is authoritative. The tag is only the publish selector and the workflow fails
  when the tag generation and the manifest generation disagree.

This is a design delta, not a permanent compatibility matrix. [OQ-35](../open-questions.md) still
owns the supported delivery model and base.

The installer writes the metadata file during install, so a kit-prepared bench box passes preflight
instead of failing every bundle with `platform.missing`. Generation 1 remains provisional: the image
ADR owns the final layout, path, and generation scheme, and this provisional value changes in one
constant, one installed file, and one test when that decision lands.

## Workflow behavior

The draft job has `contents: write` and no environment. It builds, signs, creates the draft, and
uploads the five assets before any approval. An unapproved, rejected, or cancelled run therefore
leaves a draft that a maintainer can inspect, which is the ADR 0032 point 7 behaviour. The publish
job needs the `beta` environment, takes no build action, and publishes only after it has checked
the draft's five asset names and the digests recorded by the draft job. It downloads the draft
assets and runs `sha256sum -c` against that record, so a replaced or incomplete asset cannot be
published under the approved run.

The CI test key is held only by the draft job. Because a job without an environment cannot read an
environment secret, `BITTERN_CI_SIGNING_KEY` must be a repository secret for this workflow. The
`beta` environment still gates the visible publication, and the key is decoded to a 0600 file in
the runner's temporary directory, removed by a trap, and removed again by an `always()` step. The
ADR 0032 follow-up records this scope and the two-job split. The key is still the CI test key: it is
never the release key, and a field device never trusts it.

The environment's tag policy is the documented `app-beta-*` glob, which is broader than the exact
six-hex rule. The workflow's first job applies the exact rule with the Go parser before any check,
build, or release step, so the policy cannot admit a malformed tag into the release path.

## Local result

`make beta-payload` was run twice with `PAYLOAD_CREATED=2026-11-22T00:00:00Z`. The payload and kit
bytes were identical across the two runs. The manifest digest below is for that fixed creation time;
a CI run uses its own `created` value and therefore its own manifest digest.

| Asset | Size | SHA-256 |
| --- | --- | --- |
| `bittern-app.tar.gz` | 9882692 | `c023ae4ad9082cfc4d7e7955216e13df15edf3d3f670c49d06c4433d2ab7c8b1` |
| `manifest.json` | 834 | `3e3cbefa2a0d2e748fc11b2017e801d3537655df2f64989d7fee3807e2a74b61` |
| `bittern-beta-kit.tar.gz` | 10522077 | `a849dde678e21d1d4e48b80208da016a24d9330466f945037885f12b579b8e51` |
| `SHA256SUMS` | 90 | `fc7f7da2b1985dfec480e85c873f36f5e10a9877c85cc3ebff1f07549e42212a` |

The manifest lists `usr/bin/bittern` and `usr/bin/bitternd`, mode 0755, with their per-file digests.
The kit verifies with `sha256sum -c SHA256SUMS`, and its internal checksums verify every member. The
kit contains no manifest, no signature, and no release anchor.

## CI registration and check run

The implementation was committed as `b4a2411` and the documentation follow-ups as `c4d0212`,
`c9ca429`, and `1184a26`. `main` was fast-forwarded to `1184a26` and pushed. Check workflow run
[45051739634](https://github.com/jagoda/bittern/actions/runs/45051739634) for `1184a26` is green.
`gh workflow list` shows `publish` active, and `gh workflow view publish.yml --ref main --yaml`
returns the workflow instead of a 404, so GitHub accepted the workflow file. No tag was pushed, so
the tag job, the environment approval, the signature step, the draft upload, and the
draft-to-published flip did not run.

## Tests

| Area | Cases |
| --- | --- |
| Tag parser | `TestParseBetaTagReadsTheGenerationAsHexadecimal`, `TestParseBetaTagRejectsMalformedTags` |
| Tag command | `TestTagCommandPrintsTheGeneration`, `TestTagCommandRejectsAMalformedTag` |
| Packer | `TestPackBuildsDeterministicPayloadAndManifest`, `TestPackRejectsAnUnsafeTree`, `TestPackDryRunWritesNothing`, `TestPackRemovesAStaleSignature` |
| Launcher | `TestLauncherPicksTheActiveBundle`, `TestLauncherFallsBackToTheBaselineWithoutAPointer`, `TestLauncherFallsBackToTheBaselineForGenerationZero`, `TestLauncherFallsBackToTheBaselineWhenThePointerIsUnreadable`, `TestLauncherRejectsARelativePath`, `TestLauncherRejectsAnEscapingPointerPath`, `TestLauncherRejectsAnEscapingBundlePath`, `TestLauncherRejectsAMalformedPointer`, `TestLauncherRejectsASymlinkedBundleBinary`, `TestLauncherRejectsABundleBinaryWithTheWrongOwner`, `TestLauncherRejectsAMissingBundleBinary`, `TestAgentUnitStartsTheFixedLauncher`, `TestPreparationKitInstallsTheLauncherAndPlatformMetadata` |
| Update restart | `TestUpdateRestartRunsTheNewBundleVersion`, `TestUpdateRollbackRestartsThePreviousBundleVersion` |
| Platform metadata | `TestPlatformProviderReadsThePreparationKitMetadata`, `TestKitPreparedPlatformFixturePassesPreflightAndMissingMetadataFails` |

The packer test proves determinism, that the file list matches the archive exactly with no directory
or special entries, that `payload.sha256` and `payload.size` match the archive bytes, that the packer
writes no signature, and that the manifest signs and verifies through the existing `sign` and
`verify` commands with a test key.

The tag parser test accepts `app-beta-00001f` as 31 and `app-beta-000010` as 16, and rejects
uppercase, five characters, seven characters, an empty suffix, a `g` prefix, a non-hex character,
generation zero, the stable namespace, and a trailing separator.

## What remains unproven

- No tag was created and no release was published. The publish workflow has not run end to end.
  `gh workflow view` accepts the workflow syntax; that is not evidence that the signature, draft,
  asset check, or environment approval path works on a real run.
- The CI test key was not used locally. The local manifest is unsigned, and the packer tests sign a
  temporary manifest with a generated test key. The `beta` secret's decoded key id and the anchor
  equality are checked by the workflow, not by this local run.
- The QEMU update scenario from BIT-009 slice three is still the release gate. A valid update, an
  invalid signature, a digest mismatch, a downgrade, and a rollback have not been exercised against
  this payload.
- The update transaction installs a bundle under `/var/lib/bittern/app/bundles/<generation>/files`
  and `bitternd.service` now starts the fixed launcher at `/usr/libexec/bittern/bitternd`. The
  launcher selection and refusal cases and the engine's restart step are covered by unit tests. The
  live systemd restart into a new bundle, and a real rollback restart, still need the QEMU update
  scenario in BIT-009 slice three. No real systemd run is part of this change.
- The beta kit was not installed on hardware. The installer trust-anchor step and the unit
  enablement are covered by existing checks and script inspection, not by a fresh Debian run.
- The production release signature remains offline and airgapped under ADR 0029. ADR 0029 is still
  Proposed, and no release key or release trust anchor exists.

## Verification

`make check`, `go vet ./...`, and `gofmt` pass in this container. The main check run above is
green. `gh workflow view publish.yml --ref main --yaml` accepts the workflow. The local assets above
were produced by `make beta-payload`, and the kit was extracted and checked with
`sha256sum -c SHA256SUMS`. A manual pack, sign, and verify with a generated test key also passed; the
real CI test key was not used locally. No tag was created on the real repository and no release was
published.
