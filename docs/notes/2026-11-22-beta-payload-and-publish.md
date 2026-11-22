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
| Publish workflow | `.github/workflows/publish.yml` runs on `app-beta-*`, validates the exact tag rule first, calls the phase one check workflow as a reusable check, downloads the unsigned assets, checks the tag generation and source commit against the manifest, signs with the `beta` environment secret, verifies the signature with the CI test anchor, creates a draft with exactly the five release assets, and publishes the draft. |
| Permissions and pins | The workflow has `contents: read` at workflow level and `contents: write` only on the publish job. Every action is GitHub-owned and pinned by full commit SHA. |
| Checks | `check.yml` is now reusable, and its build job produces the unsigned beta payload, manifest, kit, and checksums as `bittern-beta-unsigned-test-build`. |

The five release assets are `manifest.json`, `manifest.json.sig`, `bittern-app.tar.gz`,
`bittern-beta-kit.tar.gz`, and `SHA256SUMS`. The kit's internal checksum file covers its own files;
the published `SHA256SUMS` covers the kit tarball.

## Compatibility rule for beta test bundles

`make beta-payload` sets `os_generation_min` and `os_generation_max` to 1, and
[`defaults/release.yaml`](../../defaults/release.yaml) records the same rule with `schema_min` 1,
`schema_max` 2, and `updater_min` 1.

The platform provider is provisional. It reads `/usr/lib/bittern/platform.json`, and no image build
writes that file yet. The rule defines generation 1 as the Debian 13 amd64 base that the beta kit and
the bench runs use. A beta bundle therefore claims exactly one OS generation, not a range from 0 to
the maximum. That is deliberate:

- A range that accepts every generation would silently claim compatibility with an OS that was
  never tested. The verifier would accept the bundle on an unknown platform and the claim would be
  false.
- A missing metadata file fails closed at preflight with `platform.missing`, so a device that cannot
  prove its generation does not update.
- When the image writes generation 1 metadata under ADR 0030 and a later generation is tested, the
  range widens in a reviewed change with dated evidence. Widening it before that is not a default.
- The manifest integer is authoritative. The tag is only the publish selector and the workflow fails
  when the tag generation and the manifest generation disagree.

This is a design delta, not a permanent compatibility matrix. [OQ-35](../open-questions.md) still
owns the supported delivery model and base.

One consequence is explicit: the kit does not write `/usr/lib/bittern/platform.json`. The file is
image-owned, and no decision assigns it to the preparation kit. A device prepared by the kit
therefore fails closed at preflight with `platform.missing` until the image or the QEMU fixture
writes generation 1. The compatibility claim is honest, but the kit alone does not yet make the
update path live end to end.

## Workflow behavior and one honest difference

The task requires `contents: write` only on the publish job, and the `beta` environment gates that
job. Draft creation, asset upload, and the draft-to-published flip therefore all happen after the
approval. An unapproved, rejected, or cancelled run leaves only the tag and no draft. ADR 0032 point
7 describes an unapproved run that leaves a draft. The permission model and that consequence cannot
both hold with a single write job and a job-level environment. This change follows the permission
model and records the narrower consequence. It publishes nothing without approval. Restoring a draft
before approval would need a separate write job or a new ADR decision.

The environment's tag policy is the documented `app-beta-*` glob, which is broader than the exact
six-hex rule. The workflow's first job applies the exact rule with the Go parser before any check,
build, or release step, so the policy cannot admit a malformed tag into the release path.

## Local result

`make beta-payload` was run twice with `PAYLOAD_CREATED=2026-11-22T00:00:00Z`. The payload and kit
bytes were identical across the two runs. The manifest digest below is for that fixed creation time;
a CI run uses its own `created` value and therefore its own manifest digest.

| Asset | Size | SHA-256 |
| --- | --- | --- |
| `bittern-app.tar.gz` | 9883538 | `4d8e468730ccd96bb23a253cbfd968d9ce98eb2459b455bdc925bfc9c545e90c` |
| `manifest.json` | 834 | `58acb8b619a3a1095f7bf28780e8288ecaa1d9d6a0805bc5841535e01d28765c` |
| `bittern-beta-kit.tar.gz` | 9769149 | `bdc8e00c35c0c636cb78a8203bdbc1d4aeed70f1beaf74062f0c61e98f044373` |
| `SHA256SUMS` | 90 | `3c5a74198edd64360accca3aaab4d756db6e9e5bb7a58f6de920198f55b9eea5` |

The manifest lists `usr/bin/bittern` and `usr/bin/bitternd`, mode 0755, with their per-file digests.
The kit verifies with `sha256sum -c SHA256SUMS`, and its internal checksums verify every member. The
kit contains no manifest, no signature, and no release anchor.

## CI registration and check run

The change was committed as `b4a2411`, fast-forwarded onto `main`, and pushed. The check workflow
run [45050966145](https://github.com/jagoda/bittern/actions/runs/45050966145) is green: `make check`
passed in 1m19s and the unsigned test build completed in 34s. `gh workflow list` shows `publish`
active, and `gh workflow view publish.yml --ref main --yaml` returns the workflow instead of a 404,
so GitHub accepted the workflow file. No tag was pushed, so the tag job, the environment approval,
the signature step, the draft upload, and the draft-to-published flip did not run.

## Tests

| Area | Cases |
| --- | --- |
| Tag parser | `TestParseBetaTagReadsTheGenerationAsHexadecimal`, `TestParseBetaTagRejectsMalformedTags` |
| Tag command | `TestTagCommandPrintsTheGeneration`, `TestTagCommandRejectsAMalformedTag` |
| Packer | `TestPackBuildsDeterministicPayloadAndManifest`, `TestPackRejectsAnUnsafeTree`, `TestPackDryRunWritesNothing`, `TestPackRemovesAStaleSignature` |

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
- The update transaction installs a bundle under `/var/lib/bittern/app/bundles/<generation>/files`,
  but the fixed launcher that starts the agent from `app/current` is not part of this change. The
  kit installs the binaries in `/usr/bin`; an update would verify, promote, and commit without the
  running agent switching to the bundle tree. That gap is part of the unproven update path, not a
  claim that the packer makes the update live.
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
