# Arcade

The repo is an Arcade hosting multiple Games; Scribble is the first, mounted at `/scribble`. See ADR 0015 and `CONTEXT-MAP.md`.

## Common dev commands
- `just web` to start the dev server; this command starts on a port dedicated to your branch or worktree, which will avoid collisions with other users developing on the same system.
- `just test-unit`, `just test-integration`, `just test-all` — the Go test tiers. `test-unit` is the default (untagged `go test ./...`); `test-integration` runs `tests/integration/` behind the `//go:build integration` tag; `test-all` runs both in sequence.
- `just test-e2e` — the Playwright UI tier under `tests/e2e/` (TypeScript specs across chromium/firefox/webkit). Requires Node + pnpm; the recipe handles port assignment per branch via `wt step eval`. Not folded into `test-all`. See ADR 0012.
- `just check` — fmt + lint + all Go test tiers + tidy. Does not run e2e.

Run `just` (no arguments) to see other commands if needed.

## Agent skills

### Issue tracker

Issues live on GitHub at `Quidge/arcade`, managed via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Canonical defaults (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Multi-context — distributed glossaries (root `CONTEXT.md` for Arcade-wide terms, one per Game e.g. `internal/games/scribble/CONTEXT.md`), indexed by `CONTEXT-MAP.md`; centralized `docs/adr/` with one global number sequence. See `docs/agents/domain.md`.

### Dependency policy

stdlib-first posture; agents must ask before introducing new Go dependencies. See `docs/agents/dependencies.md`.
