# Publish recovery and verification

- Date: 2026-11-23
- Status: fixed. The publish job now verifies the downloaded draft against the shipped
  `packaging/trust/beta-ci.pub` anchor, and `release.yml` has a recovery input that completes an
  existing draft without a preflight, build, signature, or tag creation. The real
  `app-beta-000001` draft verified locally; no release was published, and the tag and draft were
  left in place.
- Type: bug fix, recovery path, and verification note.
- Related: [release workflow](../../.github/workflows/release.yml),
  [publish workflow](../../.github/workflows/publish.yml),
  [draft verifier](../../packaging/verify_release_draft.py),
  [verifier tests](../../tests/release/test_verify_release_draft.py),
  [beta bench rollout](../runbooks/beta-bench-rollout.md),
  [principles](../principles.md),
  [ADR 0031](../adr/0031-update-channel-on-github-releases.md),
  [ADR 0032](../adr/0032-continuous-integration-and-build-pipeline.md),
  [ADR 0033](../adr/0033-dispatch-driven-release-trigger.md),
  [release call path note](2026-11-22-release-call-path.md),
  [beta payload and publish note](2026-11-22-beta-payload-and-publish.md)

## Diagnosis

The maintainer's real dispatch, run
[45089237654](https://github.com/jagoda/bittern/actions/runs/45089237654), passed the preflight,
the checks, the build, and the signing job. It failed in `publish the verified draft` at its first
`gh release view` call:

```text
failed to run git: fatal: not a git repository (or any of the parent directories): .git
##[error]Process completed with exit code 1.
```

The publish job had no `actions/checkout` step, so `gh` tried to infer the repository from the
working directory's git metadata. There was none. The draft and the tag were untouched; the failure
was before any verification or publication.

The old publish job also trusted a base64 digest string produced by the signing job. That string is
not available to a recovery run that starts after the signing job, so the old job could only publish
within the same run that created the draft. A recovery path needed a trust decision that survives
the run and verifies the durable draft.

## Repository context

Every `gh` call in the publish job now names the repository explicitly. The chosen form is
`-R "$GITHUB_REPOSITORY"` for `gh release` commands and an explicit
`repos/$GITHUB_REPOSITORY/...` path for `gh api`. The publish job also checks out the repository, but
only because the verifier and `packaging/trust/beta-ci.pub` are repository files. The checkout is not
the repository context for `gh`; the explicit flag is. That keeps the recovery and normal paths
independent of whether a checkout exists and avoids making a git inference failure possible again.

| Call | Before | After |
| --- | --- | --- |
| Release lookup | `gh release view "$TAG"` | `gh release view "$TAG" -R "$GITHUB_REPOSITORY"` |
| Asset download | `gh release download "$TAG"` | `gh release download "$TAG" -R "$GITHUB_REPOSITORY"` |
| Tag lookup | None in the publish job | `gh api "repos/$GITHUB_REPOSITORY/git/ref/tags/$TAG"` |
| Publish flip | `gh release edit "$TAG" --draft=false` | `gh release edit "$TAG" -R "$GITHUB_REPOSITORY" --draft=false` |

## Verification model

`packaging/verify_release_draft.py` replaces the digest string. It checks the draft through `gh`,
downloads the five assets into a temporary directory, and verifies the downloaded bytes. Each
problem has one message and the script stops at the first failure:

| Check | Failure message |
| --- | --- |
| Tag form, using the shared tag rule | `tag ... is not the expected app-beta-* form: ...` |
| Tag exists | `tag ... does not exist in ...` |
| Draft exists | `draft release ... does not exist in ...` |
| Release is a draft | `release ... is not a draft; only a draft is verified and published` |
| Exactly five names | `the draft is missing asset(s): ...` or `the draft has unexpected asset(s): ...` |
| No duplicate names | `the draft lists asset(s) more than once: ...` |
| Manifest verifies with the shipped anchor | `manifest.json does not verify with the shipped anchor ...` |
| Signing key matches the anchor | `manifest signing key ... does not match the shipped anchor ...` |
| Channel is beta | `manifest channel is ..., not 'beta'` |
| Generation equals the tag | `manifest generation ... does not equal the tag generation ...` |
| Payload filename is the app archive | `manifest payload filename is ..., not 'bittern-app.tar.gz'` |
| Kit and checksums are not payloads | `... the kit and SHA256SUMS are not payloads` |
| Payload digest matches the bytes | `manifest payload.sha256 ... does not match bittern-app.tar.gz ...` |
| `SHA256SUMS` lines are well formed | `SHA256SUMS line ... is malformed; expected '<sha256>  <asset>'` |
| `SHA256SUMS` names are expected | `SHA256SUMS lists ..., which is not one of the five release assets` |
| `SHA256SUMS` names are unique | `SHA256SUMS lists ... more than once` |
| `SHA256SUMS` digests match | `SHA256SUMS digest for ... is ..., but the downloaded bytes are ...` |
| Kit has a published digest | `SHA256SUMS does not list bittern-beta-kit.tar.gz; the kit has no published digest` |

After verification the script prints the SHA-256 of all five downloaded assets and adds the same
table to the run summary. The manifest signature anchors `manifest.json`, `manifest.json.sig`, and
the payload digest; `SHA256SUMS` anchors the kit. ADR 0031 defines `SHA256SUMS` as the kit checksum
file, not a checksum for all five, and a checksum file cannot contain its own digest. The verifier
therefore requires every one of the five assets to have an anchored digest and requires every
`SHA256SUMS` entry to match the downloaded bytes, instead of asking the file to list itself.

## Recovery input

`release.yml` now has `publish_draft`, an optional tag string. When it is set, the preflight is
skipped and the release job calls the existing publish workflow with `publish_only=true`,
`dry_run=false`, and `create_tag=false`. The `validate tag`, `checks and unsigned test build`, and
`sign and stage draft` jobs are skipped. The single publish job is the same implementation as the
normal path. It runs in the `beta` environment, so the approval gate is unchanged.

The recovery path refuses a draft that does not exist, a tag that does not exist, a tag outside
`app-beta-` plus exactly six lowercase hexadecimal characters, a tag whose generation is zero, and
every verification failure above. A refusal exits nonzero before the publish flip.

```sh
gh workflow run release.yml --ref main -f publish_draft=app-beta-000001
```

## Proof against the real draft

The verifier ran against the real `app-beta-000001` draft with the shipped anchor and the release
tool built from this commit:

```sh
make bittern-release
python3 packaging/verify_release_draft.py \
  --repo jagoda/bittern \
  --tag app-beta-000001 \
  --anchor packaging/trust/beta-ci.pub \
  --release-tool agent/bin/bittern-release \
  --work-dir /tmp/bittern-proof
```

Output:

```text
verify-draft: ok: app-beta-000001 in jagoda/bittern
tag: app-beta-000001 (generation 1)
anchor: packaging/trust/beta-ci.pub (key id e19d358aa7caaca5e5ddb26560227a285a88bc6f136621f3972e0ac054b5c9f3)
payload: bittern-app.tar.gz; the kit and SHA256SUMS are not payloads
SHA256SUMS: every listed digest matches; all five assets have an anchored digest
asset digests:
  manifest.json  ff80569ee2a7dde68078b70ee87c5a03fc72ed3834d35e31475c22476edfbfff
  manifest.json.sig  1912b3e3eafc134c4c89d1b410c8e8dd52a89b75ea6ba1fd37e576985e3151a3
  bittern-app.tar.gz  9b48c56ce3be5319566aaeba367692b07bd58aba2d36bef6df8c4737789e8598
  bittern-beta-kit.tar.gz  050d30362af89d6466fae87c0fb3afa838359300a395b6fb54bff9874f1a98c1
  SHA256SUMS  48a4e433502ed6a266170cc9e3935592c72dfdcbbf77bc0e7ee8791f187b0e3e
```

A malformed tag is refused before any GitHub call:

```text
$ python3 packaging/verify_release_draft.py --tag app-beta-00001 --repo jagoda/bittern
verify-draft: fail: [tag-form] tag 'app-beta-00001' is not the expected app-beta-* form: tag "app-beta-00001" has a 5-character suffix; "app-beta-" needs exactly 6 lowercase hexadecimal characters
```

## State confirmation

The verification only downloads. It does not edit or publish the release:

```text
$ gh release view app-beta-000001 --json isDraft,assets --jq '{isDraft, assets: [.assets[].name]}'
{"isDraft":true,"assets":["manifest.json","manifest.json.sig","bittern-app.tar.gz","bittern-beta-kit.tar.gz","SHA256SUMS"]}
$ gh api repos/jagoda/bittern/git/ref/tags/app-beta-000001 --jq '.object.sha'
b0e7eebf79cf7ff04a80e73a4a8de00d009e0d95
```

## Finding

An irreversible step needs a verifiable, repeatable completion path. The tag and draft are the
durable state; a failure at the publish job must complete that state, not delete it and not force a
new release number. The rule is recorded as
[OP-2](../principles.md#op-2-an-irreversible-step-has-a-verifiable-completion-path-that-does-not-need-a-new-generation).
The recovery runbook procedure is in
[the beta bench rollout](../runbooks/beta-bench-rollout.md#complete-an-existing-draft-after-a-publish-failure).

## Next step

Approve the `beta` environment for the existing `app-beta-000001` recovery run when the maintainer
is ready to publish generation 1. Do not delete the tag or the draft, and do not create a new tag.
