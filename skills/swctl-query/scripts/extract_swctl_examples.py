#!/usr/bin/env python3
"""Generate representative swctl examples from SkyWalking docs and e2e cases."""

from __future__ import annotations

import argparse
import contextlib
import datetime as dt
import re
import shlex
import shutil
import tempfile
import urllib.request
import zipfile
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path

from extract_swctl_commands import CommandInfo, collect_tree


VALUE_GLOBAL_FLAGS = {
    "--authorization",
    "--base-url",
    "--config",
    "--display",
    "--grpc-addr",
    "--password",
    "--timezone",
    "--username",
    "--admin-url",
}
BOOL_GLOBAL_FLAGS = {"--debug", "--help", "--insecure", "--version", "-h", "-v"}
STOP_TOKENS = {"|", "||", "&&", ";", ">", ">>", "2>", "2>>"}
QUERY_RE = re.compile(r"^(?P<indent>\s*)(?:-\s*)?query:\s*(?P<value>.*)$")
DEFAULT_GITHUB_REPO = "apache/skywalking"
DEFAULT_GITHUB_REF = "master"


@dataclass(frozen=True)
class Example:
    path: Path
    line_number: int
    command: str


@dataclass(frozen=True)
class SourceRoot:
    path: Path
    label: str


def download_github_archive(repo: str, ref: str, destination: Path) -> None:
    url = f"https://github.com/{repo}/archive/{ref}.zip"
    request = urllib.request.Request(url, headers={"User-Agent": "skywalking-banyandb-skill-generator"})
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=destination.parent, prefix=f"{destination.name}.", suffix=".tmp", delete=False) as temp_file:
        temp_path = Path(temp_file.name)
        with urllib.request.urlopen(request, timeout=120) as response:
            shutil.copyfileobj(response, temp_file)
    temp_path.replace(destination)


def valid_zip(path: Path) -> bool:
    try:
        with zipfile.ZipFile(path) as archive:
            return archive.testzip() is None
    except zipfile.BadZipFile:
        return False


def archive_cache_path(cache_dir: Path, repo: str, ref: str) -> Path:
    safe_repo = repo.replace("/", "-")
    safe_ref = ref.replace("/", "-")
    return cache_dir / f"{safe_repo}-{safe_ref}.zip"


@contextlib.contextmanager
def open_skywalking_source(local_root: Path | None, github_repo: str, github_ref: str, github_cache: Path | None) -> SourceRoot:
    if local_root is not None:
        yield SourceRoot(path=local_root.resolve(), label=str(local_root.resolve()))
        return
    label = f"https://github.com/{github_repo}/tree/{github_ref}"
    with tempfile.TemporaryDirectory(prefix="skywalking-github-") as temp_dir:
        temp_path = Path(temp_dir)
        archive_path = archive_cache_path(github_cache, github_repo, github_ref) if github_cache else temp_path / "skywalking.zip"
        if archive_path.exists() and not valid_zip(archive_path):
            archive_path.unlink()
        if not archive_path.exists():
            download_github_archive(github_repo, github_ref, archive_path)
        extract_dir = temp_path / "source"
        with zipfile.ZipFile(archive_path) as archive:
            archive.extractall(extract_dir)
        roots = [path for path in extract_dir.iterdir() if path.is_dir()]
        if not roots:
            raise RuntimeError(f"downloaded GitHub archive for {github_repo}@{github_ref} did not contain a source directory")
        yield SourceRoot(path=roots[0], label=label)


def build_child_index(node: CommandInfo) -> dict[tuple[str, ...], dict[str, CommandInfo]]:
    indexes: dict[tuple[str, ...], dict[str, CommandInfo]] = {}

    def visit(current: CommandInfo) -> None:
        index: dict[str, CommandInfo] = {}
        for child in current.commands:
            index[child.path[-1]] = child
            for alias in child.aliases:
                index[alias] = child
        indexes[current.path] = index
        for child in current.commands:
            visit(child)

    visit(node)
    return indexes


def trim_shell_tail(command: str) -> str:
    quote: str | None = None
    escaped = False
    for idx, char in enumerate(command):
        if escaped:
            escaped = False
            continue
        if char == "\\":
            escaped = True
            continue
        if quote:
            if char == quote:
                quote = None
            continue
        if char in {"'", '"'}:
            quote = char
            continue
        if char in {"|", ";", ">", "&"}:
            return command[:idx].strip().rstrip("\\").strip().rstrip("`'\"")
    return command.strip().rstrip("\\").strip().rstrip("`'\"")


def replace_swctl_subcommands(line: str) -> str:
    result: list[str] = []
    idx = 0
    while idx < len(line):
        start = line.find("$(", idx)
        if start < 0:
            result.append(line[idx:])
            break
        after_start = start + 2
        command_start = after_start
        while command_start < len(line) and line[command_start].isspace():
            command_start += 1
        if not line.startswith("swctl ", command_start):
            result.append(line[idx : after_start])
            idx = after_start
            continue
        result.append(line[idx:start])
        depth = 1
        quote: str | None = None
        escaped = False
        cursor = after_start
        while cursor < len(line):
            char = line[cursor]
            if escaped:
                escaped = False
                cursor += 1
                continue
            if char == "\\":
                escaped = True
                cursor += 1
                continue
            if quote:
                if char == quote:
                    quote = None
                cursor += 1
                continue
            if char in {"'", '"'}:
                quote = char
                cursor += 1
                continue
            if line.startswith("$(", cursor):
                depth += 1
                cursor += 2
                continue
            if char == ")":
                depth -= 1
                cursor += 1
                if depth == 0:
                    break
                continue
            cursor += 1
        result.append("${from_swctl}")
        idx = cursor
    return "".join(result)


def extract_command_fragments(line: str) -> list[str]:
    fragments: list[str] = []
    search_from = 0
    while True:
        idx = line.find("swctl ", search_from)
        if idx < 0:
            break
        fragment = line[idx:].strip().removeprefix("$ ").strip().strip("\"'")
        fragment = replace_swctl_subcommands(fragment)
        fragment = trim_shell_tail(fragment)
        if fragment:
            fragments.append(fragment)
        search_from = idx + len("swctl ")
    return fragments


def logical_query_lines(query: str) -> list[tuple[int, str]]:
    logical_lines: list[tuple[int, str]] = []
    pending = ""
    pending_line_number = 0
    for offset, raw_line in enumerate(query.splitlines()):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if pending:
            pending = f"{pending} {line}"
        else:
            pending = line
            pending_line_number = offset
        if pending.endswith("\\"):
            pending = pending[:-1].rstrip()
            continue
        logical_lines.append((pending_line_number, pending))
        pending = ""
    if pending:
        logical_lines.append((pending_line_number, pending))
    return logical_lines


def iter_query_blocks(path: Path) -> list[tuple[int, str]]:
    lines = path.read_text(encoding="utf-8", errors="ignore").splitlines()
    blocks: list[tuple[int, str]] = []
    line_idx = 0
    while line_idx < len(lines):
        match = QUERY_RE.match(lines[line_idx])
        if not match:
            line_idx += 1
            continue
        value = match.group("value").strip()
        line_number = line_idx + 1
        if value in {"|", "|-", "|+", ">", ">-", ">+"}:
            content_indent: int | None = None
            block_lines: list[str] = []
            block_start = line_number + 1
            line_idx += 1
            while line_idx < len(lines):
                next_line = lines[line_idx]
                if next_line.strip():
                    next_indent = len(next_line) - len(next_line.lstrip(" "))
                    if content_indent is None:
                        content_indent = next_indent
                    if next_indent < content_indent:
                        break
                block_lines.append(next_line)
                line_idx += 1
            blocks.append((block_start, "\n".join(block_lines)))
            continue
        if "swctl " in value:
            blocks.append((line_number, value))
        line_idx += 1
    return blocks


def iter_markdown_code_blocks(path: Path) -> list[tuple[int, str]]:
    lines = path.read_text(encoding="utf-8", errors="ignore").splitlines()
    blocks: list[tuple[int, str]] = []
    in_fence = False
    fence_marker = ""
    block_start = 0
    block_lines: list[str] = []
    for line_idx, line in enumerate(lines):
        stripped = line.strip()
        if not in_fence and (stripped.startswith("```") or stripped.startswith("~~~")):
            in_fence = True
            fence_marker = stripped[:3]
            block_start = line_idx + 2
            block_lines = []
            continue
        if in_fence and stripped.startswith(fence_marker):
            if any("swctl " in block_line for block_line in block_lines):
                blocks.append((block_start, "\n".join(block_lines)))
            in_fence = False
            fence_marker = ""
            block_lines = []
            continue
        if in_fence:
            block_lines.append(line)
        elif re.match(r"^\s*(?:\$\s*)?swctl\s+", line):
            blocks.append((line_idx + 1, line))
    return blocks


def tokenize(command: str) -> list[str]:
    try:
        tokens = shlex.split(command)
    except ValueError:
        return []
    clean_tokens: list[str] = []
    for token in tokens:
        if token in STOP_TOKENS:
            break
        clean_tokens.append(token)
    return clean_tokens


def skip_global_options(tokens: list[str]) -> int:
    idx = 1
    while idx < len(tokens):
        token = tokens[idx]
        if not token.startswith("-"):
            break
        flag = token.split("=", 1)[0]
        if "=" in token:
            idx += 1
        elif flag in VALUE_GLOBAL_FLAGS:
            idx += 2
        elif flag in BOOL_GLOBAL_FLAGS:
            idx += 1
        else:
            break
    return idx


def canonical_path(tokens: list[str], indexes: dict[tuple[str, ...], dict[str, CommandInfo]]) -> tuple[str, ...] | None:
    if not tokens or tokens[0] != "swctl":
        return None
    idx = skip_global_options(tokens)
    path: tuple[str, ...] = ()
    while idx < len(tokens):
        token = tokens[idx]
        child = indexes.get(path, {}).get(token)
        if child is None:
            break
        path = child.path
        idx += 1
    return path or None


def add_example(
    examples: dict[tuple[str, ...], list[Example]],
    seen: set[tuple[tuple[str, ...], str]],
    indexes: dict[tuple[str, ...], dict[str, CommandInfo]],
    path: Path,
    line_number: int,
    line: str,
) -> None:
    for fragment in extract_command_fragments(line):
        tokens = tokenize(fragment)
        path_key = canonical_path(tokens, indexes)
        if not path_key:
            continue
        dedupe_key = (path_key, re.sub(r"\s+", " ", fragment))
        if dedupe_key in seen:
            continue
        seen.add(dedupe_key)
        examples[path_key].append(Example(path=path, line_number=line_number, command=fragment))


def collect_examples(skywalking_root: Path, indexes: dict[tuple[str, ...], dict[str, CommandInfo]]) -> dict[tuple[str, ...], list[Example]]:
    examples: dict[tuple[str, ...], list[Example]] = defaultdict(list)
    seen: set[tuple[tuple[str, ...], str]] = set()
    e2e_root = skywalking_root / "test" / "e2e-v2" / "cases"
    if e2e_root.exists():
        for path in sorted(path for path in e2e_root.rglob("*") if path.suffix in {".yaml", ".yml"}):
            for block_line_number, query in iter_query_blocks(path):
                for offset, line in logical_query_lines(query):
                    add_example(examples, seen, indexes, path, block_line_number + offset, line)
    docs_root = skywalking_root / "docs"
    if docs_root.exists():
        for path in sorted(path for path in docs_root.rglob("*.md")):
            for block_line_number, block in iter_markdown_code_blocks(path):
                for offset, line in logical_query_lines(block):
                    add_example(examples, seen, indexes, path, block_line_number + offset, line)
    return dict(sorted(examples.items(), key=lambda item: (" ".join(item[0]), item[0])))


def render_examples(examples: dict[tuple[str, ...], list[Example]], source: SourceRoot, max_per_command: int) -> str:
    generated_at = dt.datetime.now(dt.UTC).replace(microsecond=0).isoformat()
    lines = [
        "# swctl Docs and e2e Examples",
        "",
        f"Generated by `extract_swctl_examples.py` from `{source.label}` at `{generated_at}`.",
        "",
        "Examples are grouped by canonical command path from local `swctl --help`. They are representative SkyWalking docs and e2e patterns, not an exhaustive API contract.",
        "",
    ]
    for path_key, items in examples.items():
        command_path = " ".join(path_key)
        lines.append(f"## `{command_path}`")
        lines.append("")
        lines.append(f"- Matched examples: {len(items)}")
        lines.append("")
        for example in items[:max_per_command]:
            try:
                rel_path = example.path.relative_to(source.path)
            except ValueError:
                rel_path = example.path
            lines.append(f"- Source: `{rel_path}:{example.line_number}`")
            lines.append("")
            lines.append("```bash")
            lines.append(example.command)
            lines.append("```")
            lines.append("")
    return "\n".join(lines).rstrip() + "\n"


def default_output_path() -> Path:
    return Path(__file__).resolve().parents[1] / "references" / "examples.md"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--swctl", default="/usr/bin/swctl", help="Path to the swctl binary.")
    parser.add_argument("--skywalking-root", type=Path, default=None, help="Optional local SkyWalking source checkout. If omitted, download GitHub archive.")
    parser.add_argument("--github-repo", default=DEFAULT_GITHUB_REPO, help="GitHub repository to download when --skywalking-root is omitted.")
    parser.add_argument("--github-ref", default=DEFAULT_GITHUB_REF, help="GitHub branch, tag, or SHA to download when --skywalking-root is omitted.")
    parser.add_argument("--github-cache", type=Path, default=None, help="Optional directory for caching downloaded GitHub archives.")
    parser.add_argument("--output", type=Path, default=default_output_path(), help="Markdown output path.")
    parser.add_argument("--max-per-command", type=int, default=5, help="Maximum examples to emit per command path.")
    args = parser.parse_args()

    root = collect_tree(args.swctl)
    indexes = build_child_index(root)
    with open_skywalking_source(args.skywalking_root, args.github_repo, args.github_ref, args.github_cache) as source:
        examples = collect_examples(source.path, indexes)
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(render_examples(examples, source, args.max_per_command), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
