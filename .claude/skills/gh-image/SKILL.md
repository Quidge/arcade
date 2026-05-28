---
name: gh-image
description: Upload screenshots, images, or GIFs to this repository's GitHub release asset host and produce embeddable GitHub-flavored markdown. Use when the user wants visual proof for PRs, issues, or comments without committing binary files to git history, regardless of which client or workflow is used to publish the markdown.
---

# GitHub Image Asset and Markdown Workflow

Use this repo-specific flow to publish an image and generate reusable GitHub-flavored markdown.

## Preconditions

- Ensure the image file exists before upload.
- Run from this repository so `uv run` can execute local scripts.

## Upload Image Asset

Run:

```sh
uv run .claude/skills/gh-image/upload-asset.py /absolute/or/relative/path/to/image.png
```

Example:
```sh
$ uv run .claude/skills/gh-image/upload-asset.py /absolute/or/relative/path/to/the_image_name.png
https://github.com/quidge/arcade/releases/download/asset-dump/the_image_name+<uuid>.png
```

The generated UUID suffix is required because two release assets cannot share the same name.

## Build Markdown Snippet

If you're embedding this in GH flavored markdown, you can use a snippet like this with the uploaded asset url:

```md
![alt-text-for-screenshot](https://github.com/quidge/arcade/releases/download/asset-dump/<filename>+<uuid>.png)
```

Use an alt text that describes what the image proves.

## Cleanup

**NO CLEANUP IS NECESSARY FOR THE ASSET**. Leaving many assets in the release is fine.
