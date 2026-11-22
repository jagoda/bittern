# Beta activation launcher, platform metadata, and draft before approval

- Date: 2026-11-22
- Status: implementation note. The fixed agent launcher, the install-time platform metadata, and the
  two-job publish workflow landed. No tag was created and no release was published.
- Related: [ADR 0030](../adr/0030-ab-from-the-first-image.md),
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [ADR 0029](../adr/0029-release-signing-and-key-custody.md),
  [update transaction specification](2026-11-22-update-transaction-spec.md),
  [update engine slice two](2026-11-22-update-engine-slice-two.md),
  [beta payload and publish](2026-11-22-beta-payload-and-publish.md),
  [beta kit README](../../packaging/README-beta-kit.md),
  [publish workflow](../../.github/workflows/publish.yml)

## Why

An installed beta bundle verified, promoted, committed, and changed `app/current`, but the agent
unit still started `/usr/bin/bitternd`. The update changed files and not the running program. The
preparation kit also omitted `/usr/lib/bittern/platform.json`, so a kit-prepared bench box failed
preflight with `platform.missing` and could not install any bundle. The publish workflow put the
`beta` environment on the only write job, so the draft was created after approval and an unapproved
run left nothing to inspect, which contradicted ADR 0032 point 7.

## The fixed launcher

| Item | Value |
| --- | --- |
| Launcher path | `/usr/libexec/bittern/bitternd`, root-owned, mode 0755, outside `/var/lib/bittern` |
| Source | [`agent/cmd/bittern-launch`](../../agent/cmd/bittern-launch/main.go) and [`agent/internal/launcher`](../../agent/internal/launcher/launcher.go) |
| Unit | `bitternd.service` `ExecStart=/usr/libexec/bittern/bitternd --ui ...` |
| Active pointer | `/var/lib/bittern/app/current`, decimal generation |
| Active binary | `<state-root>/app/bundles/<generation>/files/usr/bin/bitternd` |
| Baseline | `/usr/bin/bitternd` |

The launcher reads the pointer and execs the bundle binary. A missing pointer, a generation of `0`,
or a pointer it cannot read falls back to the baseline. A present but malformed pointer, a relative
or escaping path, a missing bundle binary, a symlink, a non-regular file, a non-executable file, or
a file whose owner is not root is refused. It never writes anything. The bundle path must stay under
the generation's `files` tree and the pointer must stay under the state root.

The update engine's start step now restarts the unit instead of starting it, on install and on
rollback. A start on an already active unit would leave the old process running. `ExecSystem`
delegates to `apply.ExecSystem.RestartUnit`.

## Platform metadata

| Item | Value |
| --- | --- |
| Path | `/usr/lib/bittern/platform.json` |
| Contents | `{"schema_version":1,"generation":1}` plus a newline |
| Owner and mode | `root:root`, mode 0644 |
| Kit file | [`packaging/platform.json`](../../packaging/platform.json) |
| Installer | Creates `/usr/lib/bittern` and installs the file in `install_files` |

The provider is unchanged and still fails closed when the file is missing, is a symlink, is not
root-owned, or is group- or other-writable. Generation 1 is provisional until the image ADR fixes
the image layout and the final metadata writer; changing it is one constant, one kit file, one
installer line, and the provider test.

## The two publish jobs

| Job | Environment | Permissions | Actions |
| --- | --- | --- | --- |
| `tag` | none | `contents: read` | Validates the exact `app-beta-<6 hex>` rule with the Go parser |
| `checks` | none | `contents: read` | Reusable phase one workflow, unsigned build |
| `draft` | `beta-draft`, no required reviewer | `contents: write` | Checks generation and source commit, signs with the CI test key, verifies, creates the draft, uploads five assets, records their SHA-256 |
| `publish` | `beta`, required reviewer | `contents: write` | Takes no build action, verifies the draft is still a draft with exactly five assets and the recorded digests, then flips it to published |

An unapproved, rejected, or cancelled run leaves the draft and publishes nothing. The publish job
downloads the draft assets and verifies them against the digest record from the draft job, so a
replaced or incomplete asset cannot be published under the approved run. Every action stays pinned
by full commit SHA.

The CI test key is decoded to a 0600 file in the runner's temporary directory, removed by a trap,
and removed again by an `always()` step. `BITTERN_CI_SIGNING_KEY` is stored in `beta-draft`, which
has no required reviewer and the same `app-beta-*` tag policy as `beta`. The approval gate is the
separate `beta` environment, so a job gated by an approving environment never has to create the
draft the approver needs to inspect. This is recorded in the ADR 0032 follow-up line and in the
[beta draft environment note](2026-11-22-beta-draft-environment.md).

## Tests

| Area | Cases |
| --- | --- |
| Launcher | `TestLauncherPicksTheActiveBundle`, `TestLauncherFallsBackToTheBaselineWithoutAPointer`, `TestLauncherFallsBackToTheBaselineForGenerationZero`, `TestLauncherFallsBackToTheBaselineWhenThePointerIsUnreadable`, `TestLauncherRejectsARelativePath`, `TestLauncherRejectsAnEscapingPointerPath`, `TestLauncherRejectsAnEscapingBundlePath`, `TestLauncherRejectsAMalformedPointer`, `TestLauncherRejectsASymlinkedBundleBinary`, `TestLauncherRejectsABundleBinaryWithTheWrongOwner`, `TestLauncherRejectsAMissingBundleBinary` |
| Unit and kit | `TestAgentUnitStartsTheFixedLauncher`, `TestPreparationKitInstallsTheLauncherAndPlatformMetadata` |
| Restart | `TestUpdateRestartRunsTheNewBundleVersion`, `TestUpdateRollbackRestartsThePreviousBundleVersion` |
| Platform | `TestPlatformProviderReadsThePreparationKitMetadata`, `TestKitPreparedPlatformFixturePassesPreflightAndMissingMetadataFails` |

The restart cases use a fake system whose restart reads `app/current` and the bundle manifest
version, which is the launcher's selection rule. They prove an update changes the running version
and a rollback restores the previous one in the fake. They are not live systemd evidence.

## Limits

- The launcher and restart were not run under systemd in this container. A live restart into a new
  bundle and a live rollback restart remain BIT-009 slice three evidence in QEMU.
- The installer's platform and launcher steps are checked by script inspection and the shipped kit
  file content, not by a full root install on fresh Debian. The QEMU clean-install scenario is the
  live check when it runs.
- No tag was created and no release was published. The publish workflow was not run end to end.
  `gh workflow view` is syntax acceptance, not evidence that the approval, digest check, or visible
  flip works on a real run.
- The CI test key was not used locally. The `beta-draft` environment secret is an operational
  change on GitHub that this change cannot populate because secret values are write-only.
