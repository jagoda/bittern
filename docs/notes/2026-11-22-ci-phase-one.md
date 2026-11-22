# CI phase one

- Date: 2026-11-22
- Status: implementation note. CI phase one is running. Phase two landed on 2026-11-22 and is
  recorded in [the phase two note](2026-11-22-beta-payload-and-publish.md). No release was
  published, no signing key was created in the repository, and the QEMU update scenario is still
  open.
- Related: [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline),
  [workflow](../../.github/workflows/check.yml),
  [channel, CI, and publication note](2026-11-22-channel-ci-and-publication.md)

## Phase two update

The build step recorded below is historical. Phase two replaces `make poc-payload` with
`make beta-payload` and adds [the publish workflow](../../.github/workflows/publish.yml). The phase
one check job and its environment setup remain the single definition of green; the publish workflow
calls this workflow as a reusable check. See
[the phase two note](2026-11-22-beta-payload-and-publish.md) for what landed and what is unproven.

## What landed

[`.github/workflows/check.yml`](../../.github/workflows/check.yml) was the phase one workflow and,
at phase one, the only workflow in the repository.

| Property | Value |
| --- | --- |
| Triggers | `push` to `main` and every `pull_request` |
| Concurrency | One run per ref, cancel the in-progress run for the same ref |
| Permissions | `contents: read` at workflow level, nothing else |
| Runner | `ubuntu-24.04`, named instead of the floating `ubuntu-latest` alias |
| Check step | `make check`, the same target a developer runs |
| Go | `actions/setup-go` with `go-version-file: agent/go.mod`, which is Go 1.27.4 |
| Python | `python3-yaml` 6.0.1-2build2 and `python3-jsonschema` 4.10.3-2ubuntu1 from apt |
| Build step | `make poc-payload`, after the check job passes |
| Artifact | `bittern-poc-unsigned-test-build`, five-day retention |
| Secrets | None |
| Publishing | None |

The two Python packages are the only prerequisites beyond the Python standard library that
`make check` needs, and they match the Debian package list in
[schema/README.md](../../schema/README.md). apt is used rather than pip because the runner's Python
is externally managed (PEP 668) and pip would need `--break-system-packages`.

The build job runs the existing `make poc-payload` target, because the bundle packer and
`make beta-payload` do not exist yet. The artifact is the POC payload tarball: the `bittern` and
`bitternd` binaries, the three units, `install.sh`, `cleanup.sh`, `storage-setup.sh`, the payload
README, and `SHA256SUMS`. It is unsigned, unpublished, and not the app bundle. The artifact name and
the job summary both say so. The app bundle artifact arrives with the bundle packer in phase two.

At the time of this note the run held one artifact of 9.5 MB, expiring on 2026-11-27.

## Action pins

Every action is GitHub-owned, because the repository allows GitHub-owned and verified actions only,
and every action is pinned by full commit SHA, because the repository requires SHA pinning.

| Action | Version | Commit SHA |
| --- | --- | --- |
| `actions/checkout` | v7.0.1 | `3d3c42e5aac5ba805825da76410c181273ba90b1` |
| `actions/setup-go` | v7.1.0 | `d8cbb8045d7f43706b9177cceb36c1f91a6644d5` |
| `actions/upload-artifact` | v7.0.2 | `cf430e030ddbb5b0abf93d22962f4752f3646cd9` |

The SHAs were resolved from the GitHub API on 2026-11-22. Each release tag is a lightweight ref that
points at the release commit, and each of those commits is the head of the current major tag. A pin
changes only in a reviewed commit.

## First run and the chrony check defect

The first run failed in `make check`, in two places:

- `TestGeneratedConfigurationHooks`, in the chrony subtest of every golden case: `chronyd -p`
  reported `Could not open <temp>/chrony/chrony.conf : Permission denied`.
- `TestFirstBootConsoleBlockGoesOnlyToTheConsole`: apply reported `create chrony check file: open
  /etc/chrony/.bittern-check-<n>.conf: permission denied`.

The cause was the runner image, not the code path the developer sees. The image installs chronyd and
loads the distribution AppArmor profile in enforce mode; the profile allows chronyd to read only
`/etc/chrony`, `/var/lib/chrony`, `/var/log/chrony` and `/run/chrony`. The first check hands chronyd a
file in the Go test temp tree, which the profile denies. The second check copies the config into
`/etc/chrony` on purpose, so the profile accepts it, but the test process runs as `runner` and
cannot write that directory. The development container has no chronyd, so both validators were
skipped there and neither defect was visible.

The fix keeps the checks running:

- The workflow appends `/tmp/Test*/** r,` to `/etc/apparmor.d/local/usr.sbin.chronyd` and reloads the
  profile. The profile stays in enforce mode and gains only the read path the validator needs.
- `chronyCheckFile` in [system.go](../../agent/internal/apply/system.go) takes the directory it
  should use as an input. The appliance passes `/etc/chrony`; the unit tests pass a temporary
  directory they control.
- There is no fallback. If the directory cannot accept the copy, the check fails with the directory
  and the permission error instead of validating the staged file. A read-only root or wrong
  permissions must make the check fail, not turn an unavailable check into a passing one.
- Three unit tests cover the `/etc/chrony` copy, an unwritable directory, and a missing directory.

No check was skipped, removed, or loosened. chronyd validates the same rendered bytes; only the
readable path changed.

## Verification

| Run | Event | Commit | Conclusion | Duration |
| --- | --- | --- | --- | --- |
| [branch run](https://github.com/jagoda/bittern/actions/runs/45035805336) | `pull_request` | `d2f0ce7` | success | 1m38s |
| [main run](https://github.com/jagoda/bittern/actions/runs/45035992353) | `push` | `d2f0ce7` | success | 2m19s |

The [first failing run](https://github.com/jagoda/bittern/actions/runs/45034908292) is kept for the
record. The branch run is the pull request run on the commit that introduced the workflow, and the
main run started when `main` was fast-forwarded to the same commit. The check job took 1m1s on the
branch run, and the build job 31s.

A follow-up commit, `80e4803`, pointed the build job's Go cache key at `agent/go.sum` so the second
`setup-go` step stops logging a cache annotation. Its
[main run](https://github.com/jagoda/bittern/actions/runs/45036651733) is green with no annotations.

## What phase one does not cover

- No secrets, no write permission, and no publishing step.
- No release signing, no CI test key, and no protected environment. Those are phase two.
- No app bundle and no beta preparation kit. The packer and `make beta-payload` are phase two.
- No OS image build. [ADR 0007](../adr/0007-mkosi-os-image-and-releases.md) defers it.
- No QEMU or hardware tests. The hosted-runner KVM question is unresolved and not assumed, per
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md) point 5.
- The artifact is not a release. No device should trust it.

## Triage and next steps

- The phase one acceptance in [BIT-042](../plan/implementation-plan.md#bit-042-release-channel-and-ci-pipeline)
  is met, except the app bundle artifact, which moves to phase two with the packer.
- Phase two is step 4 of the [release work order](../plan/implementation-plan.md#release-work-order-2026-11-22).
- The CI runner environment differs from the development container: it has chronyd, nft, and Docker,
  and no dnsmasq. The first run showed that the host validators are the part of `make check` most
  likely to surprise on a new environment. A future environment change should assume they run.
- No new open question was needed. The KVM question stays unresolved.
