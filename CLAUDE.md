# Scribble

## Common dev commands
- `just web` to start the dev server; this command starts on a port dedicated to your branch or worktree, which will avoid collisions with other users developing on the same system

Run `just` (no arguments) to see other commands if needed.

## Agent skills

### Issue tracker

Issues live on GitHub at `Quidge/scribble`, managed via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Canonical defaults (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context — `CONTEXT.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.

### Dependency policy

stdlib-first posture; agents must ask before introducing new Go dependencies. See `docs/agents/dependencies.md`.
