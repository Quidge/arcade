# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

This repo is **multi-context**: it is an Arcade hosting multiple Games (see ADR 0015). Glossaries are **distributed** — an Arcade-wide one at the root plus one per Game — while ADRs are **centralized** in a single `docs/adr/` with one global number sequence.

## Before exploring, read these

- **`CONTEXT-MAP.md`** at the repo root — the index of glossaries; start here to find the right one.
- **`CONTEXT.md`** at the repo root — Arcade-wide terms only (Arcade, Game, Join code).
- The **Game's own `CONTEXT.md`** for whichever Game you're working in (e.g. `internal/games/scribble/CONTEXT.md`).
- **`docs/adr/`** — read ADRs that touch the area you're about to work in. Each ADR states its own scope (Arcade-wide vs. a named Game).

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The producer skill (`/grill-with-docs`) creates them lazily when terms or decisions actually get resolved.

## File structure

```
/
├── CONTEXT-MAP.md          index of glossaries
├── CONTEXT.md              Arcade-wide glossary
├── internal/games/
│   └── scribble/
│       └── CONTEXT.md      Scribble's glossary
├── docs/adr/               centralized, one global number sequence
│   ├── 0001-some-decision.md
│   └── 0002-another-decision.md
└── ...
```

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in the relevant glossary — the Game's own `CONTEXT.md` for Game-specific concepts, the root `CONTEXT.md` for Arcade-wide ones. Don't drift to synonyms the glossary explicitly avoids. The same English word can mean different things in different Games, so use the glossary that matches the context you're working in.

If the concept you need isn't in the glossary yet, that's a signal — either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/grill-with-docs`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (some decision) — but worth reopening because…_
