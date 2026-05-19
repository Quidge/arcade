#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.13"
# dependencies = ["click"]
# ///
"""Upload a file to the quidge/scribble asset-dump release (to embed in Issues or PRs)."""

import shutil
import subprocess
import tempfile
import uuid
from dataclasses import dataclass
from pathlib import Path

import click


@dataclass(frozen=True, slots=True)
class GitHubRepo:
    """owner/name pair for github.com (matches `gh --repo`)."""

    owner: str
    name: str

    def __str__(self) -> str:
        return f"{self.owner}/{self.name}"

    def release_asset_download_url(self, tag: str, asset_filename: str) -> str:
        return (
            f"https://github.com/{self.owner}/{self.name}/releases/download/"
            f"{tag}/{asset_filename}"
        )


# Fixed target: this script only supports the dedicated release on quidge/scribble.
REPO = GitHubRepo(owner="quidge", name="scribble")
RELEASE_TAG = "asset-dump"


@click.command(context_settings={"help_option_names": ["-h", "--help"]})
@click.argument(
    "path",
    type=click.Path(
        exists=True,
        file_okay=True,
        dir_okay=False,
        readable=True,
        resolve_path=True,
        path_type=Path,
    ),
)
def main(path: Path) -> None:
    """Upload a file to GH and return the hosted URL. Useful for embedding in Issues or PRs.

    \b
    Example:
    \b
      $ .claude/skills/gh-image/upload-asset.py /tmp/a-screenshot.png
      https://github.com/quidge/scribble/releases/download/asset-dump/a-screenshot+<uuid>.png

    Paste the printed URL into an Issue or PR comment.
    """

    suffix = path.suffix
    dest_name = f"{path.stem}+{uuid.uuid4()}{suffix}"
    dest = Path(tempfile.gettempdir()) / dest_name

    shutil.copy2(path, dest)
    cmd = [
        "gh",
        "release",
        "upload",
        RELEASE_TAG,
        str(dest),
        "--repo",
        str(REPO),
    ]
    try:
        completed = subprocess.run(
            cmd,
            check=False,
            capture_output=True,
            text=True,
        )
    finally:
        dest.unlink(missing_ok=True)

    if completed.stderr:
        click.echo(completed.stderr, nl=False, err=True)
        if not completed.stderr.endswith("\n"):
            click.echo(err=True)

    if completed.returncode != 0:
        raise click.exceptions.Exit(completed.returncode)

    # `gh release upload` usually prints nothing on success; emit the canonical asset URL.
    if completed.stdout:
        click.echo(completed.stdout, nl=False)
        if not completed.stdout.endswith("\n"):
            click.echo()
    else:
        url = REPO.release_asset_download_url(RELEASE_TAG, dest_name)
        click.echo(url)


if __name__ == "__main__":
    main()
