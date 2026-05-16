# Scribble

## Common dev commands
- `just web-wt` to start the dev server; this command starts on port dedicated to your branch or worktree, which will avoid collisions with other users developing on the same system

Run `just` (no arguments) to see other commands if needed.

## Agent skills

### Issue tracker

Issues live on GitHub at `Quidge/scribble`, managed via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Canonical defaults (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context — `CONTEXT.md` and `docs/adr/` at the repo root (neither exists yet; created lazily by `/grill-with-docs`). See `docs/agents/domain.md`.
