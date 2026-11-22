#!/usr/bin/env python3
"""Link check for Bittern Markdown.

Rules:
- Walk every Markdown file under the repository, not only files reachable from README.md.
- Check relative inline links, images, and reference-definition targets.
- Ignore http(s), mailto, other URI schemes, protocol-relative URLs, pure fragments, and absolute
  paths. Anchors and query strings are removed before checking the target path.
- Report a broken target as: link: file:line -> target
- Report every README that is not linked from another Markdown file. A top-level component README
  must be linked from the root README.md.

Uses only the Python standard library and never accesses the network.
"""

from __future__ import annotations

import os
import re
import sys
from pathlib import Path
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[2]
IGNORED_DIRS = {".git", ".hg", ".svn", ".worktrees", "node_modules", "vendor"}
COMPONENT_DIRS = ("agent", "ui", "schema", "templates", "defaults", "image", "tests", "fleet")

FENCE_RE = re.compile(r"^\s*(`{3,}|~{3,})")
LINK_OPEN_RE = re.compile(r"(?<!\\)!?\[[^\]]*\]\(")
REF_DEF_RE = re.compile(r"^ {0,3}\[[^\]]+\]:[ \t]*(<[^>]*>|\S+)")
URI_SCHEME_RE = re.compile(r"^[A-Za-z][A-Za-z0-9+.-]*:")


def markdown_files() -> list[Path]:
    files: list[Path] = []
    for dirpath, dirnames, filenames in os.walk(ROOT):
        dirnames[:] = sorted(name for name in dirnames if name not in IGNORED_DIRS)
        for name in sorted(filenames):
            if name.endswith(".md"):
                files.append(Path(dirpath) / name)
    return files


def iter_content_lines(text: str):
    """Yield (line number, line) outside fenced code blocks."""
    fence = None
    for lineno, line in enumerate(text.splitlines(), 1):
        stripped = line.lstrip()
        if fence is not None:
            if stripped.startswith(fence):
                fence = None
            continue
        match = FENCE_RE.match(line)
        if match:
            fence = match.group(1)
            continue
        yield lineno, line


def mask_inline_code(line: str) -> str:
    """Replace inline code spans with spaces so link-like text in them is ignored."""
    result: list[str] = []
    i = 0
    while i < len(line):
        if line[i] != "`":
            result.append(line[i])
            i += 1
            continue
        end_ticks = i
        while end_ticks < len(line) and line[end_ticks] == "`":
            end_ticks += 1
        ticks = line[i:end_ticks]
        close = line.find(ticks, end_ticks)
        if close == -1:
            result.append(line[i])
            i += 1
            continue
        span_end = close + len(ticks)
        result.append(" " * (span_end - i))
        i = span_end
    return "".join(result)


def parse_inline_destination(line: str, start: int):
    """Return (destination, end index) for a destination beginning after an opening parenthesis."""
    i = start
    while i < len(line) and line[i] in " \t":
        i += 1
    if i >= len(line):
        return None, None

    if line[i] == "<":
        close_angle = line.find(">", i + 1)
        if close_angle == -1:
            return None, None
        close_paren = line.find(")", close_angle + 1)
        if close_paren == -1:
            return None, None
        return line[i + 1 : close_angle], close_paren + 1

    j = i
    depth = 0
    escaped = False
    while j < len(line):
        char = line[j]
        if escaped:
            escaped = False
        elif char == "\\":
            escaped = True
        elif char == "(":
            depth += 1
        elif char == ")":
            if depth == 0:
                return line[i:j], j + 1
            depth -= 1
        elif char in " \t" and depth == 0:
            close_paren = line.find(")", j)
            if close_paren == -1:
                return None, None
            return line[i:j], close_paren + 1
        j += 1
    return None, None


def external_or_non_relative(target: str) -> bool:
    if not target or target.startswith("#"):
        return True
    if target.startswith("//"):
        return True
    if target.lower().startswith(("http://", "https://", "mailto:")):
        return True
    if URI_SCHEME_RE.match(target):
        return True
    if target.startswith(("/", "\\")):
        return True
    return False


def target_path(source: Path, target: str):
    path_part = target.split("#", 1)[0].split("?", 1)[0]
    if not path_part:
        return None
    path_part = unquote(path_part)
    return Path(os.path.normpath(os.path.join(str(source.parent), path_part)))


def scan_file(source: Path, inbound: dict[Path, set[Path]], failures: list[str]) -> None:
    rel_source = source.relative_to(ROOT).as_posix()
    try:
        text = source.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError) as exc:
        failures.append(f"link: {rel_source}:1 -> unreadable Markdown ({exc})")
        return

    def check_target(target: str, lineno: int) -> None:
        target = target.strip()
        if external_or_non_relative(target):
            return
        resolved = target_path(source, target)
        if resolved is None:
            return
        inbound.setdefault(resolved, set()).add(source)
        if not resolved.exists():
            failures.append(f"link: {rel_source}:{lineno} -> {target}")

    for lineno, line in iter_content_lines(text):
        masked = mask_inline_code(line)
        for match in LINK_OPEN_RE.finditer(masked):
            target, _ = parse_inline_destination(masked, match.end())
            if target is not None:
                check_target(target, lineno)

        definition = REF_DEF_RE.match(masked)
        if definition:
            target = definition.group(1)
            if target.startswith("<") and target.endswith(">"):
                target = target[1:-1]
            check_target(target, lineno)


def main() -> int:
    files = markdown_files()
    failures: list[str] = []
    inbound: dict[Path, set[Path]] = {}

    for source in files:
        scan_file(source, inbound, failures)

    root_readme = ROOT / "README.md"
    readmes = sorted(path for path in files if path.name == "README.md")

    for component in COMPONENT_DIRS:
        readme = ROOT / component / "README.md"
        if not readme.exists():
            failures.append(f"link: {component}/README.md:1 -> missing README")

    for readme in readmes:
        if readme == root_readme:
            continue
        rel_readme = readme.relative_to(ROOT).as_posix()
        sources = inbound.get(readme, set())
        is_component = readme.parent.parent == ROOT and readme.parent.name in COMPONENT_DIRS
        if is_component:
            if root_readme not in sources:
                failures.append(
                    f"link: {rel_readme}:1 -> unlinked README "
                    "(root README.md does not link to it)"
                )
        elif not any(source != readme for source in sources):
            failures.append(
                f"link: {rel_readme}:1 -> unlinked README (no Markdown file links to it)"
            )

    for failure in failures:
        print(failure)
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
