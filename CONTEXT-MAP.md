# Context Map

The Arcade's domain documentation is **distributed**: Arcade-wide terms live at
the root, and each Game keeps its own glossary next to its code. This file is the
index. ADRs, by contrast, are **centralized** in `docs/adr/` under one global
number sequence (see ADR 0015 for why the structure is asymmetric).

## Glossaries

- **[`CONTEXT.md`](CONTEXT.md)** — Arcade-wide glossary. Only the terms that span
  Games: Arcade, Game, Join code.
- **[`internal/games/scribble/CONTEXT.md`](internal/games/scribble/CONTEXT.md)** —
  Scribble's glossary: GameSession, Player, Host, Chain, Entry, Round, Reveal,
  Caption, Drawing, Ghost, Draft, and Scribble's verbs.

## Decisions

- **[`docs/adr/`](docs/adr/)** — all Architecture Decision Records, one global
  number sequence for the whole repo. Each ADR states its own scope
  (Arcade-wide vs. a named Game), since the shared directory no longer signals
  scope.

## Conventions

- A term that spans Games goes in the root `CONTEXT.md`. A term that belongs to
  one Game goes in that Game's `CONTEXT.md`. The same English word may name
  different concepts in different Games — that's expected; keep them separate.
- Read the relevant Game's glossary before working inside that Game; read the
  root glossary for cross-cutting work or the Arcade shell (`internal/arcade`).
