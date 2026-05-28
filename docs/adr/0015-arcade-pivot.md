# The repo becomes an Arcade of isolated Games; Scribble is one Game, mounted at its own URL slug

> **Scope: Arcade-wide.** Supersedes [ADR 0014](0014-homepage-join-by-code.md) in
> full and [ADR 0002](0002-code-as-primary-url-identifier.md) in part — its
> "URL doesn't encode the game type / code is the global primary identifier"
> stance is reversed here; its Crockford Base32 code-format decision still governs.

The repo stops being a single Scribble app and becomes an **Arcade**: one Go
binary, one VPS, one deployment, serving multiple **Games**. A Game is one
playable product in the Arcade (Scribble is the first; a second is coming). The
Arcade root `/` becomes a home / nav / game-picker; each Game is mounted under
its own URL slug (`/scribble/...`). The Go module path moves
`github.com/quidge/scribble` → `github.com/quidge/arcade` (the remote is already
`Quidge/arcade`).

**Games are isolated — no shared platform layer.** There is deliberately no
shared Player / Host / session / lobby abstraction. The rationale is
rule-of-three, not rule-of-two: with only a second Game on the horizon, a
platform layer extracted now would almost certainly be the wrong abstraction.
The next Game is built fresh alongside Scribble; a shared layer (if any) is
extracted later, from three real examples, not predicted from one. The one piece
that *is* shared is the `joincode` leaf package — a pure value/format utility
(parse, canonicalize, format, alphabet). Sharing a value type is not the
premature abstraction being avoided; sharing the *stateful* session registry
would be, so each Game keeps its own.

**`main.go` owns the slugs.** Each Game receives its base path via constructor
(e.g. `scribbleweb.New(registry, gitSHA, "/scribble")`) and uses that base path
for both route registration *and* absolute-URL generation (the redirect after
"new game", lobby links). This keeps `main.go` as the single authority over the
Arcade's namespace. We rejected having each Game hardcode its own slug (buries
the slug inside the Game and strips `main.go` of namespace authority), and we
rejected `http.StripPrefix` mounting (the Game still needs its prefix to emit
absolute redirects, so the prefix would have to be plumbed in anyway —
`StripPrefix` would hide it from routing while we re-supplied it for URLs).

**Join codes are namespaced per Game.** The same code can exist in two Games and
mean different things; there is no Arcade-wide code lookup. A code only resolves
under its Game's slug — the slug, not the code, is what tells the server which
Game (and therefore which session registry) to consult. This is the direct
reversal of ADR 0002's "code is the primary identifier; the server discovers the
game type by looking up the code": that stance was YAGNI-correct when there was
one game type, and the YAGNI condition has now flipped. (ADR 0002's code
*format* — 6-character Crockford Base32 with a readability dash — is untouched
and still governs every Game's codes.)

**No backwards-compatibility for old URLs.** The cutover is clean — no legacy
`/g/...` redirect shim. Session URLs were always ephemeral: sessions are
in-memory and die within minutes, so no durable bookmark to a session ever
existed. Only `/` was durable, and it is intentionally becoming the picker.

**Per-Game layouts — no shared chrome.** Each Game owns its full HTML. A Game's
interface and aesthetic *is* the product, and a Game dominates the screen while
you play it; inheriting Arcade chrome would dilute that. The current `base.tmpl`
moves into Scribble. The `internal/arcade` shell has its own minimal layout.
Getting back to the Arcade is a plain link each Game places itself, not
inherited chrome.

The target layout (described here for intent; the package move is separate
later work):

```
internal/
  joincode/                 shared leaf (the only shared package)
  arcade/                   shell: home, nav, game-picker, 404, own minimal layout
  games/
    scribble/  (chain draft gamesession ghost hostpromote presence round
                roundcomplete seatconn strokes web)   + base.tmpl moves here
    <newgame>/              fresh, built later
```

A `games/` parent directory groups the Games, chosen for legibility. The
`SCRIBBLE_` env-var prefix convention is preserved: infrastructure knobs stay
unprefixed (`ADDR`, `DATA_DIR`); a Game's product knobs keep its prefix
(`SCRIBBLE_...`).

**Documentation structure — a deliberate asymmetry.** Glossaries are
**distributed per Game**: a root `CONTEXT-MAP.md` index, a slim shared root
`CONTEXT.md` (only the Arcade-wide terms — Arcade, Game, Join code), and
`internal/games/scribble/CONTEXT.md` for Scribble's own glossary (moved from
root). ADRs, by contrast, stay **centralized**: one `docs/adr/` with a single
global number sequence for the whole repo. The asymmetry is intentional —
hunting for the next free ADR number across scattered per-context directories is
error-prone, so one location keeps numbering unambiguous, and Arcade-vs-Game
scope becomes semantic (each ADR states its scope, as this one does) rather than
structural. ADRs remain immutable to intent and are not flattened: supersession
chains are kept as the historical record, and current state is always read from
code plus `CONTEXT.md`, never reconstructed from ADRs alone.

## Considered Options

- **A shared platform layer now (rejected)** — extracting a common
  Player/Host/session/lobby abstraction across Scribble and the second Game.
  Rejected on rule-of-three: two examples don't reveal which parts are genuinely
  common versus incidentally similar, so the abstraction would likely be wrong
  and then expensive to unwind. Revisit once a third Game makes the shared shape
  observable rather than guessed.

- **Game hardcodes its own slug (rejected)** — each Game knows it lives at
  `/scribble`. Buries the slug inside the Game and removes `main.go`'s authority
  over the Arcade namespace, so slug collisions and remounts become a
  Game-by-Game concern instead of one place.

- **`http.StripPrefix` mounting (rejected)** — strip the slug before the Game's
  router sees the request. The Game still has to emit *absolute* redirects (after
  "new game") and lobby links, which need the prefix, so the prefix would be
  plumbed into the constructor regardless. `StripPrefix` would only hide the
  prefix from routing while we re-supplied it for URL generation — more moving
  parts for no gain over passing the base path once.

- **Arcade-wide join-code namespace / a global resolver (rejected)** — one code
  space across all Games, with a root code box that discovers the Game by
  looking up the code (the ADR 0002 model, extended). Rejected because it
  re-introduces the "what if the code's Game doesn't match the URL" ambiguity
  the slug cleanly removes, and forces a global registry — exactly the kind of
  shared stateful layer this pivot is avoiding. Per-Game namespacing means a code
  is only meaningful under its slug, and a root code box has nothing global to
  resolve against (which is why this supersedes ADR 0014).

- **A legacy `/g/<code>` redirect shim (rejected)** — preserve old session URLs.
  YAGNI: session URLs were never durable (in-memory sessions expire within
  minutes), so there is nothing to preserve. Only `/` was durable and it is
  becoming the picker by design.

- **Shared chrome / a common Arcade layout wrapping every Game (rejected)** — a
  single base template the Games inherit. Rejected because a Game's look *is* the
  product and a Game owns the whole screen during play; shared chrome would both
  constrain Game aesthetics and imply a platform coupling we're explicitly not
  building. A back-to-Arcade link is cheap for each Game to place itself.

- **Splitting the pivot into several ADRs (rejected)** — one ADR per concern
  (container, routing, join codes, layout, docs). Rejected because the decisions
  are mutually reinforcing — per-Game code namespacing follows from slug-owned
  routing, which follows from Game isolation — and reading them apart loses the
  through-line. This is recorded as one coherent pivot ADR instead.

- **Per-context ADR directories (rejected)** — `internal/games/scribble/adr/`,
  `internal/arcade/adr/`, etc., mirroring the distributed glossaries. Rejected
  because it makes "what's the next ADR number?" a cross-directory scan and
  invites collisions; centralized numbering with stated per-ADR scope is the
  cheaper, less error-prone trade.
