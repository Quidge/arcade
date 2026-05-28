# Arcade

The Arcade is a single web app — one Go binary, one VPS, one deployment — that
hosts a small set of party **Games**. Scribble is the first Game. This file is
the **Arcade-wide** glossary: only the terms that span Games live here. Each
Game keeps its own glossary next to its code (Scribble's is at
`internal/games/scribble/CONTEXT.md`). See `CONTEXT-MAP.md` for the index, and
ADR 0015 for the pivot that established this structure.

## Language

**Arcade**:
The container app and the product as a whole. One binary serving multiple Games,
each mounted under its own URL slug (`/scribble/...`). The Arcade root `/` is the
home / nav / game-picker — it does not itself run any gameplay. The Arcade has no
shared platform layer: it does not own Players, Hosts, sessions, or lobbies.
Those concepts live inside individual Games and are not shared between them (see
ADR 0015).
_Avoid_: platform, site, hub, portal, framework

**Game**:
One playable product in the Arcade, mounted at its own URL slug. Scribble is one
Game; more are added alongside it, built fresh rather than on a shared
abstraction (rule-of-three, not rule-of-two — see ADR 0015). A Game owns its
full HTML and aesthetic, its own in-memory session registry, and its own
glossary; the only thing Games share is the `joincode` value/format package. A
Game's slug is owned by `main.go`, which passes the base path to the Game so it
can both register routes and emit absolute URLs.
_Avoid_: app (alone — the Arcade is also an app), module, plugin, mini-game.
A specific play instance of a Game is **not** a Game — in Scribble that is a
**GameSession** (see `internal/games/scribble/CONTEXT.md`).

**Join code**:
The short, human-readable code that identifies one play instance within a Game.
Format (per ADR 0002, the part still in force): 6 characters of Crockford Base32
with a dash in the middle for readability (e.g. `A4B-K9P`) — no I/L/O/U,
case-insensitive, dashes ignored on input. Codes are **namespaced per Game**:
the same code can exist in two Games and mean different things, and a code only
resolves under its Game's slug (`/scribble/g/<code>`). There is no Arcade-wide
code lookup and no global resolver — the slug, not the code, selects the Game
(see ADR 0015, which reversed ADR 0002's "code is the global primary identifier"
stance). The shared `joincode` package supplies parsing, canonicalization,
formatting, and the alphabet; each Game owns the registry the code resolves
against.
_Avoid_: room code, game id, session id, PIN

## Scope note

Beyond these three terms, domain vocabulary is **per Game** — defined in that
Game's own `CONTEXT.md`, not here. Don't lift Scribble terms (GameSession,
Player, Host, Chain, Entry, Round, Reveal, …) up to this file; they are
Scribble's, and a future Game may use the same English words for different
concepts.
