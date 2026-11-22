#!/usr/bin/env python3
"""Tests for packaging/release_preflight.py.

The generation arithmetic and every precondition message are tested without a
network call, a GitHub token, a release, or the Go tool. The live preflight
uses the same functions with the real gh and bittern-release commands.
"""

from __future__ import annotations

import importlib.util
import subprocess
import sys
import unittest
from pathlib import Path
from typing import Callable
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "packaging" / "release_preflight.py"

spec = importlib.util.spec_from_file_location("release_preflight", SCRIPT)
assert spec is not None and spec.loader is not None
release_preflight = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release_preflight)

PreflightError = release_preflight.PreflightError
TAG = {"type": "tag", "name": "app-beta-*"}
MAIN = {"type": "branch", "name": "main"}


def release(tag: str, draft: bool = False) -> dict:
    return {"tagName": tag, "isDraft": draft}


def fake_parser(tag: str) -> int:
    if tag.startswith("app-stable-"):
        raise AssertionError("a stable tag reached the beta parser")
    if not tag.startswith("app-beta-"):
        raise PreflightError("tag-parser-rejected", f"rejected {tag}")
    suffix = tag.removeprefix("app-beta-")
    if len(suffix) != 6 or any(character not in "0123456789abcdef" for character in suffix):
        raise PreflightError("tag-parser-rejected", f"rejected {tag}")
    return int(suffix, 16)


def check_run(
    head: str,
    status: str = "completed",
    conclusion: str | None = "success",
) -> dict:
    return {
        "databaseId": 1,
        "headSha": head,
        "status": status,
        "conclusion": conclusion,
        "url": "https://example.invalid/run/1",
    }


def environment(reviewer: str = "jagoda", bypass: bool = False) -> dict:
    return {
        "can_admins_bypass": bypass,
        "protection_rules": [
            {
                "type": "required_reviewers",
                "reviewers": [{"type": "User", "reviewer": {"login": reviewer}}],
            },
            {"type": "branch_policy"},
        ],
    }


def fake_command(
    code: int = 0, stdout: str = "", stderr: str = ""
) -> Callable[[list[str]], subprocess.CompletedProcess[str]]:
    def command(_arguments: list[str]) -> subprocess.CompletedProcess[str]:
        return subprocess.CompletedProcess([], code, stdout, stderr)

    return command


class GenerationTests(unittest.TestCase):
    def test_no_tags_starts_at_generation_one(self) -> None:
        self.assertEqual(release_preflight.next_generation([], fake_parser), 1)
        self.assertEqual(release_preflight.compute_tag(1), "app-beta-000001")

    def test_contiguous_history_is_highest_plus_one(self) -> None:
        releases = [release(tag) for tag in ("app-beta-000001", "app-beta-000002", "app-beta-000003")]
        self.assertEqual(release_preflight.next_generation(releases, fake_parser), 4)
        self.assertEqual(release_preflight.compute_tag(4), "app-beta-000004")

    def test_gap_is_highest_plus_one(self) -> None:
        releases = [release(tag) for tag in ("app-beta-000001", "app-beta-000002", "app-beta-000004")]
        self.assertEqual(release_preflight.next_generation(releases, fake_parser), 5)
        self.assertEqual(release_preflight.compute_tag(5), "app-beta-000005")

    def test_draft_is_ignored(self) -> None:
        releases = [release("app-beta-000001"), release("app-beta-000003", draft=True)]
        self.assertEqual(release_preflight.next_generation(releases, fake_parser), 2)
        self.assertEqual(release_preflight.compute_tag(2), "app-beta-000002")

    def test_other_channel_tags_are_ignored(self) -> None:
        releases = [
            release("app-stable-000005"),
            release("os-000006"),
            release("app-beta-000001"),
        ]
        self.assertEqual(release_preflight.next_generation(releases, fake_parser), 2)

    def test_malformed_published_beta_tag_is_ignored(self) -> None:
        releases = [
            release("app-beta-000001"),
            release("app-beta-zzzzzz"),
            release("app-beta-00001"),
        ]
        self.assertEqual(release_preflight.next_generation(releases, fake_parser), 2)

    def test_hexadecimal_generation_is_not_decimal(self) -> None:
        releases = [release("app-beta-0000ff")]
        self.assertEqual(release_preflight.next_generation(releases, fake_parser), 256)
        self.assertEqual(release_preflight.compute_tag(256), "app-beta-000100")

    def test_existing_tag_at_computed_value_is_refused(self) -> None:
        generation = release_preflight.next_generation(
            [release("app-beta-000001")], fake_parser
        )
        tag = release_preflight.compute_tag(generation)
        self.assertEqual(tag, "app-beta-000002")
        with self.assertRaises(PreflightError) as caught:
            release_preflight.check_tag_free(tag, True)
        self.assertEqual(caught.exception.code, "tag-exists")
        self.assertIn(tag, caught.exception.message)

    def test_draft_at_computed_value_is_refused_by_the_tag_check(self) -> None:
        generation = release_preflight.next_generation(
            [release("app-beta-000001"), release("app-beta-000002", draft=True)],
            fake_parser,
        )
        self.assertEqual(generation, 2)
        with self.assertRaises(PreflightError) as caught:
            release_preflight.check_tag_free(
                release_preflight.compute_tag(generation), True
            )
        self.assertEqual(caught.exception.code, "tag-exists")

    def test_out_of_range_generation_is_refused(self) -> None:
        for generation in (0, 0x1000000):
            with self.subTest(generation=generation):
                with self.assertRaises(PreflightError) as caught:
                    release_preflight.compute_tag(generation)
                self.assertEqual(caught.exception.code, "generation-out-of-range")

    def test_parser_infrastructure_errors_are_not_ignored(self) -> None:
        def broken(_tag: str) -> int:
            raise PreflightError("release-tool-missing", "no tool")

        with self.assertRaises(PreflightError) as caught:
            release_preflight.next_generation([release("app-beta-000001")], broken)
        self.assertEqual(caught.exception.code, "release-tool-missing")


class FailureMessageTests(unittest.TestCase):
    def assertFailure(self, code: str, message: str, function, *args, **kwargs) -> None:
        with self.assertRaises(PreflightError) as caught:
            function(*args, **kwargs)
        self.assertEqual(caught.exception.code, code)
        self.assertIn(message, caught.exception.message)

    def test_main_head_moved_after_run(self) -> None:
        self.assertFailure(
            "main-head-moved-after-run",
            "has no check run",
            release_preflight.check_main_head,
            "b" * 40,
            [check_run("a" * 40)],
        )

    def test_main_head_moved_during_release(self) -> None:
        self.assertFailure(
            "main-head-moved-during-release",
            "is not the dispatch commit",
            release_preflight.check_main_head,
            "b" * 40,
            [check_run("b" * 40)],
            "a" * 40,
        )

    def test_check_run_missing(self) -> None:
        self.assertFailure(
            "check-run-missing",
            "there is no check.yml run on main",
            release_preflight.check_main_head,
            "b" * 40,
            [],
        )

    def test_check_run_incomplete(self) -> None:
        self.assertFailure(
            "check-run-incomplete",
            "is in_progress",
            release_preflight.check_main_head,
            "b" * 40,
            [check_run("b" * 40, status="in_progress", conclusion=None)],
        )

    def test_check_run_failed(self) -> None:
        self.assertFailure(
            "check-run-failed",
            "concluded failure",
            release_preflight.check_main_head,
            "b" * 40,
            [check_run("b" * 40, conclusion="failure")],
        )

    def test_secret_missing_from_names(self) -> None:
        self.assertFailure(
            "secret-missing",
            "is not a secret",
            release_preflight.check_secret,
            ["OTHER_SECRET"],
        )

    def test_secret_missing_from_ci_presence(self) -> None:
        self.assertFailure(
            "secret-missing",
            "is empty in the beta-draft environment",
            release_preflight.check_secret,
            None,
            "false",
        )

    def test_secret_list_failed(self) -> None:
        self.assertFailure(
            "secret-list-failed",
            "cannot list the beta-draft secret names",
            release_preflight.check_secret,
            None,
            None,
        )

    def test_environment_tag_scope(self) -> None:
        self.assertFailure(
            "environment-tag-scope",
            "not exactly app-beta-*",
            release_preflight.check_environment,
            "beta",
            [{"type": "tag", "name": "v*"}],
            environment(),
            True,
        )

    def test_environment_extra_branch(self) -> None:
        self.assertFailure(
            "environment-extra-ref",
            "only main is part of the release path",
            release_preflight.check_environment,
            "beta-draft",
            [TAG, MAIN, {"type": "branch", "name": "release/*"}],
            environment(),
            True,
        )

    def test_environment_dispatch_ref(self) -> None:
        self.assertFailure(
            "environment-dispatch-ref",
            "does not admit the main branch",
            release_preflight.check_environment,
            "beta-draft",
            [TAG],
            environment(),
            True,
        )

    def test_environment_dispatch_ref_is_not_needed_for_the_tag_path(self) -> None:
        release_preflight.check_environment(
            "beta-draft", [TAG], environment(), False
        )

    def test_environment_reviewer(self) -> None:
        self.assertFailure(
            "environment-reviewer",
            "requires reviewers ['someone-else'], not jagoda",
            release_preflight.check_environment,
            "beta",
            [TAG, MAIN],
            environment(reviewer="someone-else"),
            True,
        )

    def test_environment_admin_bypass(self) -> None:
        self.assertFailure(
            "environment-admin-bypass",
            "allows administrators to bypass",
            release_preflight.check_environment,
            "beta",
            [TAG, MAIN],
            environment(bypass=True),
            True,
        )

    def test_environment_missing(self) -> None:
        self.assertFailure(
            "environment-missing",
            "does not exist",
            release_preflight.check_environment,
            "beta",
            [TAG, MAIN],
            None,
            True,
        )

    def test_tag_exists(self) -> None:
        self.assertFailure(
            "tag-exists",
            "already exists",
            release_preflight.check_tag_free,
            "app-beta-000002",
            True,
        )

    def test_tag_parser_mismatch(self) -> None:
        self.assertFailure(
            "tag-parser-mismatch",
            "computed generation 3",
            release_preflight.check_parser,
            "app-beta-000002",
            3,
            fake_parser,
        )

    def test_confirmation_mismatch(self) -> None:
        self.assertFailure(
            "confirmation-mismatch",
            "does not equal the computed tag",
            release_preflight.check_confirmation,
            "app-beta-000001",
            "app-beta-000002",
        )

    def test_tag_parser_rejected(self) -> None:
        with mock.patch.object(release_preflight, "TOOL", Path(sys.executable)):
            self.assertFailure(
                "tag-parser-rejected",
                "rejected the tag",
                release_preflight.parse_tag_go,
                "app-beta-000002",
                fake_command(code=2, stderr="bad tag"),
            )

    def test_tag_parser_output(self) -> None:
        with mock.patch.object(release_preflight, "TOOL", Path(sys.executable)):
            self.assertFailure(
                "tag-parser-output",
                "not a generation",
                release_preflight.parse_tag_go,
                "app-beta-000002",
                fake_command(stdout="not-a-number\n"),
            )

    def test_release_tool_missing(self) -> None:
        with mock.patch.object(
            release_preflight, "TOOL", Path("/does/not/exist")
        ):
            self.assertFailure(
                "release-tool-missing",
                "run make bittern-release",
                release_preflight.parse_tag_go,
                "app-beta-000002",
            )

    def test_tag_check_failed(self) -> None:
        self.assertFailure(
            "tag-check-failed",
            "git ls-remote failed",
            release_preflight.tag_exists,
            "app-beta-000002",
            fake_command(code=128, stderr="network down"),
        )


if __name__ == "__main__":
    unittest.main()
