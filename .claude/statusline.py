#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///

"""
Display: `<Model name> | <tokens used (in thousands, rounded to 1 decimal place)> | <CC session UUID>`
"""

import json
import sys

CYAN = "\033[36m"
GREEN = "\033[32m"
YELLOW = "\033[33m"
RED = "\033[31m"
DIM = "\033[90m"
RESET = "\033[0m"


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


def generate_status_line(data):
    model_name = data.get("model", {}).get("display_name", "Claude")
    session_id = data.get("session_id", "") or "--------"

    ctx = data.get("context_window", {})
    used_pct = ctx.get("used_percentage", 0) or 0
    window_size = ctx.get("context_window_size", 200000) or 200000
    used_tokens = int(window_size * (used_pct / 100))

    parts = [
        f"{CYAN}{model_name}{RESET}",
        f"{usage_color(used_pct)}{format_tokens(used_tokens)} used{RESET}",
        f"{DIM}{session_id}{RESET}",
    ]
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
