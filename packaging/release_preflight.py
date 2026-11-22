#!/usr/bin/env python3
"""Release preflight for the dispatch-driven beta release.

ADR 0033 makes a manual dispatch of release.yml the normal publication path.
This script is the executable precondition. It verifies, in order:

- the current main head has a completed, successful check.yml run on that exact
  commit;
- the maintainer's local run verifies BITTERN_CI_SIGNING_KEY exists in the
  beta-draft environment, by name only; a workflow run without an environment
  does not read the key, and the draft job's beta-draft gate is where it is
  present and used;
- both environments admit the app-beta-* tag and the main branch; beta needs
  the maintainer as reviewer with admin bypass off; --tag-path does not require
  main;
- the next generation is the highest published app-beta-* generation plus one,
  ignoring drafts, other channels, and tags without a published release;
- the computed tag does not exist;
- the computed tag round-trips through bittern-release tag, the same Go parser
  the publish workflow uses.

It prints the tag, the commit, the generation, and the exact next action. It
never creates a tag, draft, or release in any mode. --plan is the explicit plan
mode; --tag-path checks the tag-push fallback, which does not need main.

The check is read-only. The only writes are the GitHub Actions output and
summary files when the runner provides them.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Callable, Iterable

ROOT = Path(__file__).resolve().parents[1]
TOOL = ROOT / "agent" / "bin" / "bittern-release"
DEFAULT_REPO = "jagoda/bittern"
DEFAULT_REVIEWER = "jagoda"
TAG_PREFIX = "app-beta-"
TAG_POLICY = "app-beta-*"
DISPATCH_REF = "main"
DISPATCH_WORKFLOW = "release.yml"
CHECK_WORKFLOW = "check.yml"
SECRET_NAME = "BITTERN_CI_SIGNING_KEY"
SECRET_ENVIRONMENT = "beta-draft"
PUBLISH_ENVIRONMENT = "beta"
MAX_GENERATION = 0xFFFFFF


class PreflightError(RuntimeError):
    """A precondition failed with a stable code and a human-readable reason."""

    def __init__(self, code: str, message: str):
        super().__init__(message)
        self.code = code
        self.message = message


Runner = Callable[[list[str]], subprocess.CompletedProcess[str]]


def run(command: list[str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(command, cwd=ROOT, capture_output=True, text=True, check=False)


def gh_json(arguments: list[str], run_command: Runner = run) -> object:
    result = run_command(["gh", *arguments])
    if result.returncode != 0:
        raise PreflightError(
            "gh-failed",
            f"gh {' '.join(arguments)} failed: {result.stderr.strip() or result.stdout.strip()}",
        )
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise PreflightError(
            "gh-output",
            f"gh {' '.join(arguments)} did not return JSON: {error}",
        ) from error


def parse_tag_go(tag: str, run_command: Runner = run) -> int:
    """Parse a tag with the Go parser the publish workflow uses."""
    if not TOOL.exists():
        try:
            display = TOOL.relative_to(ROOT)
        except ValueError:
            display = TOOL
        raise PreflightError(
            "release-tool-missing",
            f"{display} is missing; run make bittern-release first",
        )
    result = run_command([str(TOOL), "tag", tag])
    if result.returncode != 0:
        raise PreflightError(
            "tag-parser-rejected",
            f"bittern-release tag {tag} rejected the tag: "
            f"{result.stderr.strip() or result.stdout.strip()}",
        )
    try:
        return int(result.stdout.strip())
    except ValueError as error:
        raise PreflightError(
            "tag-parser-output",
            f"bittern-release tag {tag} printed {result.stdout.strip()!r}, not a generation",
        ) from error


def compute_tag(generation: int) -> str:
    if generation < 1:
        raise PreflightError(
            "generation-out-of-range",
            f"generation {generation} is below 1; application bundle generations start at 1",
        )
    if generation > MAX_GENERATION:
        raise PreflightError(
            "generation-out-of-range",
            f"generation {generation} does not fit the six hexadecimal characters in the tag",
        )
    return f"{TAG_PREFIX}{generation:06x}"


def next_generation(
    releases: Iterable[dict], parse_tag: Callable[[str], int] = parse_tag_go
) -> int:
    """Return the highest published beta generation plus one.

    Drafts are not published. Tags outside the beta namespace are another
    channel. A published release whose tag the Go parser rejects is not a
    valid beta generation, so it is ignored rather than counted.
    """
    highest = 0
    for release in releases:
        if release.get("isDraft"):
            continue
        tag = release.get("tagName", "")
        if not tag.startswith(TAG_PREFIX):
            continue
        try:
            generation = parse_tag(tag)
        except PreflightError as error:
            if error.code != "tag-parser-rejected":
                raise
            continue
        if generation > highest:
            highest = generation
    return highest + 1


def check_main_head(
    head: str,
    runs: list[dict],
    dispatch_sha: str | None = None,
) -> dict:
    if not runs:
        raise PreflightError(
            "check-run-missing",
            f"there is no {CHECK_WORKFLOW} run on main; push main and wait for a green run",
        )
    latest = runs[0]
    if dispatch_sha is not None and dispatch_sha != head:
        raise PreflightError(
            "main-head-moved-during-release",
            f"the main head {head} is not the dispatch commit {dispatch_sha}; "
            "main moved after the release was dispatched, so dispatch again from the new head",
        )
    if latest.get("headSha") != head:
        raise PreflightError(
            "main-head-moved-after-run",
            f"the main head {head} has no check run; the latest {CHECK_WORKFLOW} run is on "
            f"{latest.get('headSha')}. Wait for a green run on the head, then run the preflight again",
        )
    if latest.get("status") != "completed":
        raise PreflightError(
            "check-run-incomplete",
            f"the latest {CHECK_WORKFLOW} run on main is {latest.get('status')}; "
            "wait for it to complete and run the preflight again",
        )
    if latest.get("conclusion") != "success":
        raise PreflightError(
            "check-run-failed",
            f"the latest {CHECK_WORKFLOW} run on main concluded {latest.get('conclusion')}; "
            "fix main before releasing",
        )
    return latest


def check_secret(
    names: list[str] | None = None, ci_presence: str | None = None
) -> None:
    """Verify the secret name without reading a value.

    The maintainer's local preflight reads the name from gh secret list. A job
    that declares the beta-draft environment may pass ci_presence from the
    secret comparison instead. The release workflow's preflight job does not
    declare the environment and does not check the key; the draft job's
    beta-draft gate is where the key is present and used.
    """
    if ci_presence is not None:
        if ci_presence.lower() != "true":
            raise PreflightError(
                "secret-missing",
                f"{SECRET_NAME} is empty in the {SECRET_ENVIRONMENT} environment; "
                "add the CI test key before releasing",
            )
        return
    if names is None:
        raise PreflightError(
            "secret-list-failed",
            f"cannot list the {SECRET_ENVIRONMENT} secret names; "
            "run the preflight with an admin token that can read environment secrets",
        )
    if SECRET_NAME not in names:
        raise PreflightError(
            "secret-missing",
            f"{SECRET_NAME} is not a secret in the {SECRET_ENVIRONMENT} environment",
        )


def reviewers(environment: dict) -> set[str]:
    found: set[str] = set()
    for rule in environment.get("protection_rules", []):
        if rule.get("type") != "required_reviewers":
            continue
        for entry in rule.get("reviewers", []):
            login = entry.get("reviewer", {}).get("login")
            if login:
                found.add(login)
    return found


def check_environment(
    name: str,
    policies: list[dict],
    environment: dict | None,
    dispatch: bool,
    reviewer: str = DEFAULT_REVIEWER,
) -> None:
    if environment is None:
        raise PreflightError(
            "environment-missing",
            f"the {name} environment does not exist",
        )
    tags = {policy["name"] for policy in policies if policy.get("type") == "tag"}
    branches = {policy["name"] for policy in policies if policy.get("type") == "branch"}
    other = {
        (policy.get("type"), policy.get("name"))
        for policy in policies
        if policy.get("type") not in ("tag", "branch")
    }
    if tags != {TAG_POLICY}:
        raise PreflightError(
            "environment-tag-scope",
            f"the {name} environment allows tags {sorted(tags)}, not exactly {TAG_POLICY}",
        )
    if other:
        raise PreflightError(
            "environment-extra-ref",
            f"the {name} environment allows unexpected ref policies {sorted(other)}",
        )
    extra_branches = branches - {DISPATCH_REF}
    if extra_branches:
        raise PreflightError(
            "environment-extra-ref",
            f"the {name} environment allows branches {sorted(extra_branches)}; "
            f"only {DISPATCH_REF} is part of the release path",
        )
    if dispatch and DISPATCH_REF not in branches:
        raise PreflightError(
            "environment-dispatch-ref",
            f"the {name} environment does not admit the {DISPATCH_REF} branch; "
            f"a workflow_dispatch at refs/heads/{DISPATCH_REF} is refused before the job starts. "
            f"Add a {DISPATCH_REF} branch policy to {name}",
        )
    if name != PUBLISH_ENVIRONMENT:
        return
    found = reviewers(environment)
    if found != {reviewer}:
        raise PreflightError(
            "environment-reviewer",
            f"the {name} environment requires reviewers {sorted(found)}, not {reviewer}",
        )
    if environment.get("can_admins_bypass") is not False:
        raise PreflightError(
            "environment-admin-bypass",
            f"the {name} environment allows administrators to bypass the approval gate",
        )


def check_tag_free(tag: str, exists: bool) -> None:
    if exists:
        raise PreflightError(
            "tag-exists",
            f"the computed tag {tag} already exists; a release tag is never moved or reused. "
            "Discard the stale draft or choose the next generation after investigating",
        )


def check_parser(tag: str, expected: int, parse_tag: Callable[[str], int]) -> int:
    generation = parse_tag(tag)
    if generation != expected:
        raise PreflightError(
            "tag-parser-mismatch",
            f"the Go parser read generation {generation} from {tag}, but the arithmetic "
            f"computed generation {expected}",
        )
    return generation


def check_confirmation(confirm: str, tag: str) -> None:
    if confirm and confirm != tag:
        raise PreflightError(
            "confirmation-mismatch",
            f"the confirmation {confirm} does not equal the computed tag {tag}; "
            "run make release-preflight and dispatch with the tag it prints",
        )


def tag_exists(tag: str, run_command: Runner = run) -> bool:
    result = run_command(["git", "ls-remote", "--tags", "origin", f"refs/tags/{tag}"])
    if result.returncode != 0:
        raise PreflightError(
            "tag-check-failed",
            f"git ls-remote failed while checking {tag}: "
            f"{result.stderr.strip() or result.stdout.strip()}",
        )
    return bool(result.stdout.strip())


def read_main_head(repo: str, run_command: Runner = run) -> str:
    data = gh_json(["api", f"repos/{repo}/commits/main"], run_command)
    if not isinstance(data, dict) or not data.get("sha"):
        raise PreflightError("main-head-read", f"gh api returned no main head for {repo}")
    return data["sha"]


def read_check_runs(repo: str, run_command: Runner = run) -> list[dict]:
    data = gh_json(
        [
            "run",
            "list",
            "--workflow",
            CHECK_WORKFLOW,
            "--branch",
            "main",
            "--limit",
            "1",
            "--json",
            "databaseId,headSha,status,conclusion,url",
            "--repo",
            repo,
        ],
        run_command,
    )
    if not isinstance(data, list):
        raise PreflightError("check-run-read", "gh run list did not return a list")
    return data


def read_secret_names(repo: str, run_command: Runner = run) -> list[str]:
    data = gh_json(
        ["secret", "list", "--env", SECRET_ENVIRONMENT, "--repo", repo, "--json", "name"],
        run_command,
    )
    if not isinstance(data, list):
        raise PreflightError("secret-list-failed", "gh secret list did not return a list")
    return [entry.get("name", "") for entry in data if isinstance(entry, dict)]


def read_environment(repo: str, name: str, run_command: Runner = run) -> dict:
    try:
        data = gh_json(["api", f"repos/{repo}/environments/{name}"], run_command)
    except PreflightError as error:
        if "Not Found" in error.message or "404" in error.message:
            raise PreflightError(
                "environment-missing",
                f"the {name} environment does not exist in {repo}",
            ) from error
        raise
    if not isinstance(data, dict):
        raise PreflightError("environment-read", f"gh api returned no environment for {name}")
    return data


def read_branch_policies(repo: str, name: str, run_command: Runner = run) -> list[dict]:
    data = gh_json(
        ["api", f"repos/{repo}/environments/{name}/deployment-branch-policies"],
        run_command,
    )
    if not isinstance(data, dict) or not isinstance(data.get("branch_policies"), list):
        raise PreflightError(
            "environment-read",
            f"gh api returned no branch policies for the {name} environment",
        )
    return data["branch_policies"]


def read_releases(repo: str, run_command: Runner = run) -> list[dict]:
    data = gh_json(
        [
            "release",
            "list",
            "--limit",
            "1000",
            "--json",
            "tagName,isDraft",
            "--repo",
            repo,
        ],
        run_command,
    )
    if not isinstance(data, list):
        raise PreflightError("release-list-failed", "gh release list did not return a list")
    return data


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Verify the beta release preconditions and print the next generation.",
    )
    parser.add_argument(
        "--plan",
        action="store_true",
        help="print the plan banner; every check still runs and nothing changes",
    )
    parser.add_argument(
        "--tag-path",
        action="store_true",
        help="check the tag-push fallback; do not require the main branch policy",
    )
    parser.add_argument("--repo", default=os.environ.get("GITHUB_REPOSITORY", DEFAULT_REPO))
    parser.add_argument("--reviewer", default=DEFAULT_REVIEWER)
    args = parser.parse_args(argv)

    failures: list[PreflightError] = []
    facts: dict[str, str] = {}

    def attempt(action: Callable[[], object]) -> object | None:
        try:
            return action()
        except PreflightError as error:
            failures.append(error)
            return None

    head = attempt(lambda: read_main_head(args.repo))
    runs = attempt(lambda: read_check_runs(args.repo))
    ci = os.environ.get("GITHUB_ACTIONS") == "true"
    dispatch_sha = os.environ.get("GITHUB_SHA") if ci else None
    latest = None
    if head is not None and runs is not None:
        latest = attempt(lambda: check_main_head(head, runs, dispatch_sha))
        if latest is not None:
            facts["main head"] = head
            facts["check run"] = (
                f"{latest.get('databaseId')} {latest.get('url')} {latest.get('conclusion')}"
            )

    ci_presence = os.environ.get("BITTERN_PREFLIGHT_SECRET_PRESENT") if ci else None
    if ci_presence is not None:
        try:
            check_secret(None, ci_presence)
            facts["secret"] = f"{SECRET_NAME} present in {SECRET_ENVIRONMENT} (name only)"
        except PreflightError as error:
            failures.append(error)
    elif ci:
        facts["secret"] = (
            "not checked by this job; the beta-draft draft job holds the key"
        )
    else:
        names = attempt(lambda: read_secret_names(args.repo))
        try:
            check_secret(names)
            facts["secret"] = f"{SECRET_NAME} present in {SECRET_ENVIRONMENT} (name only)"
        except PreflightError as error:
            failures.append(error)

    for name in (SECRET_ENVIRONMENT, PUBLISH_ENVIRONMENT):
        environment = attempt(lambda name=name: read_environment(args.repo, name))
        policies = attempt(lambda name=name: read_branch_policies(args.repo, name))
        if environment is not None and policies is not None:
            attempt(
                lambda name=name, policies=policies, environment=environment: check_environment(
                    name,
                    policies,
                    environment,
                    dispatch=not args.tag_path,
                    reviewer=args.reviewer,
                )
            )
            tag_policy = sorted(p["name"] for p in policies if p.get("type") == "tag")
            branch_policy = sorted(p["name"] for p in policies if p.get("type") == "branch")
            facts[f"environment {name}"] = (
                f"tags {', '.join(tag_policy) or 'none'}; "
                f"branches {', '.join(branch_policy) or 'none'}"
            )

    releases = attempt(lambda: read_releases(args.repo))
    generation = None
    tag = None
    if releases is not None:
        generation = attempt(lambda: next_generation(releases, parse_tag_go))
    if generation is not None:
        tag = attempt(lambda: compute_tag(generation))
    if tag is not None and generation is not None:
        exists = attempt(lambda: tag_exists(tag))
        if exists is not None:
            attempt(lambda: check_tag_free(tag, exists))
            attempt(lambda: check_parser(tag, generation, parse_tag_go))
            facts["published beta"] = (
                f"highest {generation - 1}" if generation > 1 else "none"
            )
            facts["tag"] = tag
            facts["commit"] = head or ""
            facts["generation"] = str(generation)

    if tag is not None:
        confirm = os.environ.get("INPUT_CONFIRM", "")
        attempt(lambda: check_confirmation(confirm, tag))

    if failures:
        print("preflight: fail" + (" (plan mode)" if args.plan else ""))
        for error in failures:
            print(f"fail: [{error.code}] {error.message}")
        return 1

    if head is None or tag is None or generation is None or latest is None:
        print("preflight: fail (plan mode)" if args.plan else "preflight: fail")
        print("fail: [preflight-incomplete] one or more checks returned no result")
        return 1

    next_action = (
        f"git tag {tag} {head} && git push origin {tag}"
        if args.tag_path
        else f"gh workflow run {DISPATCH_WORKFLOW} --ref {DISPATCH_REF} -f confirm={tag}"
    )

    if args.plan:
        print("preflight: plan (no tag, draft, or release will be created)")
    else:
        print("preflight: ok")
    print(f"repository: {args.repo}")
    for key in (
        "main head",
        "check run",
        "secret",
        f"environment {SECRET_ENVIRONMENT}",
        f"environment {PUBLISH_ENVIRONMENT}",
        "published beta",
    ):
        if key in facts:
            print(f"{key}: {facts[key]}")
    print(f"tag: {tag}")
    print(f"commit: {head}")
    print(f"generation: {generation}")
    print(f"next: {next_action}")

    output_path = os.environ.get("GITHUB_OUTPUT")
    if output_path:
        try:
            with open(output_path, "a", encoding="utf-8") as output:
                output.write(f"tag={tag}\n")
                output.write(f"generation={generation}\n")
                output.write(f"commit={head}\n")
                output.write(f"check_run={latest.get('databaseId', '')}\n")
        except OSError as error:
            print(
                f"fail: [output-write-failed] cannot write {output_path}: {error}",
                file=sys.stderr,
            )
            return 1

    summary_path = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary_path:
        try:
            with open(summary_path, "a", encoding="utf-8") as summary:
                summary.write("### Release preflight\n\n")
                summary.write(f"- Tag: `{tag}`\n")
                summary.write(f"- Commit: `{head}`\n")
                summary.write(f"- Generation: {generation}\n")
                summary.write(f"- Next: `{next_action}`\n")
        except OSError:
            pass

    return 0


if __name__ == "__main__":
    sys.exit(main())
