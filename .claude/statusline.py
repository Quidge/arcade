#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///

"""
Display: `<Model name> | <tokens used> | [<branch> [🌳 if worktree]] | <CC session UUID>`

The git segment is omitted when the working directory is not inside a git repo.
"""

import json
import subprocess
import sys

CYAN = "\033[36m"
GREEN = "\033[32m"
YELLOW = "\033[33m"
RED = "\033[31m"
MAGENTA = "\033[35m"
DIM = "\033[90m"
RESET = "\033[0m"

WORKTREE_EMOJI = "🌳"


def usage_color(percentage):
    if percentage < 50:
        return GREEN
    if percentage < 75:
        return YELLOW
    if percentage < 90:
        return RED
    return "\033[91m"


def format_tokens(tokens):
    if tokens < 1000:
        return str(int(tokens))
    if tokens < 1_000_000:
        return f"{tokens / 1000:.1f}k"
    return f"{tokens / 1_000_000:.2f}M"


def _git(args, cwd):
    """Run a git command; return stripped stdout, or None if it failed."""
    try:
        result = subprocess.run(
            ["git", *args],
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=1,
        )
    except (FileNotFoundError, subprocess.TimeoutExpired):
        return None
    if result.returncode != 0:
        return None
    return result.stdout.strip()


def git_segment(cwd):
    """Return a formatted git segment, or None if cwd is not inside a git repo."""
    git_dir = _git(["rev-parse", "--git-dir"], cwd)
    if git_dir is None:
        return None

    branch = _git(["symbolic-ref", "--short", "HEAD"], cwd)
    if not branch:
        sha = _git(["rev-parse", "--short", "HEAD"], cwd)
        branch = f"detached@{sha}" if sha else "detached"

    common_dir = _git(["rev-parse", "--git-common-dir"], cwd)
    in_worktree = common_dir is not None and common_dir != git_dir
    suffix = f" {WORKTREE_EMOJI}" if in_worktree else ""

    return f"{MAGENTA}{branch}{RESET}{suffix}"


def generate_status_line(data):
    model_name = data.get("model", {}).get("display_name", "Claude")
    session_id = data.get("session_id", "") or "--------"

    ctx = data.get("context_window", {})
    used_pct = ctx.get("used_percentage", 0) or 0
    window_size = ctx.get("context_window_size", 200000) or 200000
    used_tokens = int(window_size * (used_pct / 100))

    cwd = data.get("workspace", {}).get("current_dir") or data.get("cwd")
    git = git_segment(cwd) if cwd else None

    parts = [
        f"{CYAN}{model_name}{RESET}",
        f"{usage_color(used_pct)}{format_tokens(used_tokens)} used{RESET}",
    ]
    if git:
        parts.append(git)
    parts.append(f"{DIM}{session_id}{RESET}")
    return " | ".join(parts)


def main():
    try:
        data = json.loads(sys.stdin.read())
        print(generate_status_line(data))
    except Exception as e:
        print(f"{RED}Claude | error: {e}{RESET}")
    sys.exit(0)


if __name__ == "__main__":
    main()
