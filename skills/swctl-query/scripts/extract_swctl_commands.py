#!/usr/bin/env python3
"""Generate a Markdown reference from the local swctl help tree."""

from __future__ import annotations

import argparse
import datetime as dt
import re
import subprocess
from dataclasses import dataclass, field
from pathlib import Path


ANSI_RE = re.compile(r"\x1b\[[0-9;]*m")
SECTION_ENDS = {"OPTIONS:", "GLOBAL OPTIONS:"}


@dataclass
class CommandInfo:
    path: tuple[str, ...]
    aliases: tuple[str, ...] = ()
    description: str = ""
    usage: str = ""
    options: list[tuple[str, str]] = field(default_factory=list)
    commands: list["CommandInfo"] = field(default_factory=list)

    @property
    def name(self) -> str:
        return " ".join(self.path)


def strip_ansi(text: str) -> str:
    return ANSI_RE.sub("", text)


def run_help(swctl: str, path: tuple[str, ...]) -> str:
    command = [swctl, *path, "--help"]
    result = subprocess.run(command, check=False, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    return strip_ansi(result.stdout)


def parse_usage(help_text: str) -> str:
    lines = help_text.splitlines()
    for idx, line in enumerate(lines):
        if line.strip() == "USAGE:" and idx + 1 < len(lines):
            usage_lines: list[str] = []
            for usage_line in lines[idx + 1 :]:
                stripped = usage_line.strip()
                if stripped in {"COMMANDS:", "OPTIONS:", "GLOBAL OPTIONS:"}:
                    break
                if stripped:
                    usage_lines.append(stripped)
            return " ".join(usage_lines)
    return ""


def parse_description(help_text: str) -> str:
    lines = help_text.splitlines()
    for idx, line in enumerate(lines):
        if line.strip() == "NAME:" and idx + 1 < len(lines):
            name_line = lines[idx + 1].strip()
            if " - " in name_line:
                return name_line.split(" - ", 1)[1].strip()
    return ""


def parse_commands(help_text: str) -> list[tuple[str, tuple[str, ...], str]]:
    commands: list[tuple[str, tuple[str, ...], str]] = []
    in_commands = False
    for line in help_text.splitlines():
        stripped = line.strip()
        if stripped == "COMMANDS:":
            in_commands = True
            continue
        if in_commands and stripped in SECTION_ENDS:
            break
        if not in_commands or not stripped:
            continue
        match = re.match(r"\s*([A-Za-z0-9_-]+(?:,\s*[A-Za-z0-9_-]+)*)\s{2,}(.*)", line)
        if not match:
            continue
        names = tuple(part.strip() for part in match.group(1).split(","))
        if not names or names[0] == "help":
            continue
        commands.append((names[0], names[1:], match.group(2).strip()))
    return commands


def parse_options(help_text: str, section: str = "OPTIONS:") -> list[tuple[str, str]]:
    options: list[tuple[str, str]] = []
    in_options = False
    for line in help_text.splitlines():
        stripped = line.strip()
        if stripped == section:
            in_options = True
            continue
        if in_options and stripped.endswith(":") and stripped != section:
            break
        if not in_options or not stripped:
            continue
        match = re.match(r"\s*(--[^ ]+(?:\s+[^ ]+)?)\s{2,}(.*)", line)
        if match:
            options.append((match.group(1).strip(), match.group(2).strip()))
        elif options:
            option, description = options[-1]
            options[-1] = (option, (description + " " + stripped).strip())
    return options


def collect_tree(swctl: str, path: tuple[str, ...] = (), max_depth: int = 5) -> CommandInfo:
    help_text = run_help(swctl, path)
    node = CommandInfo(
        path=path,
        description=parse_description(help_text),
        usage=parse_usage(help_text),
        options=parse_options(help_text),
    )
    if len(path) >= max_depth:
        return node
    for command, aliases, description in parse_commands(help_text):
        child = collect_tree(swctl, (*path, command), max_depth=max_depth)
        child.aliases = aliases
        if description:
            child.description = description
        node.commands.append(child)
    return node


def render_options(options: list[tuple[str, str]]) -> list[str]:
    if not options:
        return ["- None."]
    return [f"- `{option}`: {description}" if description else f"- `{option}`" for option, description in options]


def render_node(node: CommandInfo, depth: int = 2) -> list[str]:
    lines: list[str] = []
    if node.path:
        heading = "#" * min(depth, 6)
        lines.append(f"{heading} `{node.name}`")
        if node.aliases:
            lines.append(f"- Aliases: {', '.join(f'`{alias}`' for alias in node.aliases)}")
        if node.description:
            lines.append(f"- Description: {node.description}")
        if node.usage:
            lines.append(f"- Usage: `{node.usage}`")
        if node.commands:
            lines.append("- Subcommands: " + ", ".join(f"`{child.path[-1]}`" for child in node.commands))
        lines.append("")
        lines.append("Options:")
        lines.extend(render_options(node.options))
        lines.append("")
    for child in node.commands:
        lines.extend(render_node(child, depth + 1 if node.path else depth))
    return lines


def render_markdown(root: CommandInfo, swctl: str) -> str:
    generated_at = dt.datetime.now(dt.UTC).replace(microsecond=0).isoformat()
    root_help = run_help(swctl, ())
    global_options = parse_options(root_help, "GLOBAL OPTIONS:")
    top_level = ", ".join(f"`{child.path[-1]}`" for child in root.commands)
    lines = [
        "# swctl Command Reference",
        "",
        f"Generated by `extract_swctl_commands.py` from `{swctl} --help` at `{generated_at}`.",
        "",
        "Use this as the authoritative local command and option reference for this workstation. Prefer `--display json` when command output needs parsing.",
        "",
        "## Top-Level Commands",
        "",
        top_level,
        "",
        "## Global Options",
        "",
    ]
    lines.extend(render_options(global_options))
    lines.extend(["", "## Command Details", ""])
    lines.extend(render_node(root))
    return "\n".join(lines).rstrip() + "\n"


def default_output_path() -> Path:
    return Path(__file__).resolve().parents[1] / "references" / "commands.md"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--swctl", default="/usr/bin/swctl", help="Path to the swctl binary.")
    parser.add_argument("--output", type=Path, default=default_output_path(), help="Markdown output path.")
    parser.add_argument("--max-depth", type=int, default=5, help="Maximum subcommand depth to inspect.")
    args = parser.parse_args()

    root = collect_tree(args.swctl, max_depth=args.max_depth)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(render_markdown(root, args.swctl), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
