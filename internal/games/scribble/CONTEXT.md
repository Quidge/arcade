# Scribble

Scribble is one **Game** in the Arcade (see the root `CONTEXT.md` for the
Arcade-wide terms — Arcade, Game, Join code): a digital reimagining of
Telestrations — a co-located, mobile-first, server-rendered party game. This
glossary covers Scribble's own vocabulary; Scribble is isolated from every other
Game in the Arcade (no shared platform layer — see ADR 0015).

## Language

**GameSession**:
A single play instance of one game in the Scribble suite, capped at 10 Players. Has a Host-configured Round timer (15–120s in 15s increments, or off). Starts when the Host clicks "Start," walks through N Rounds and a per-Chain Reveal, and ends when the Host explicitly ends the GameSession (typically after the Reveal has completed but available at any time).
_Avoid_: game (alone — overloaded with the game-type concept), session (alone — overloaded with HTTP/auth-session), room, match, instance

**Player**:
A participant in a GameSession, identified by a display name they choose when joining. A Player is a persistent seat in the GameSession: their record survives WebSocket disconnects and is only removed when they Leave (kicked, voluntarily quit, or GameSession ends). A Player's connection status — currently Connected via WebSocket, or not — is a separate concern from membership. Display names are unique within a GameSession; rejoining after a disconnect requires the same display name on the same join code.
_Avoid_: user, guest

**Host**:
The Player currently holding host powers in a GameSession. Exactly one per GameSession at any time. Initially the Player who created the GameSession by clicking "New game," but Host can change hands: the current Host may voluntarily transfer Host to any other Player (no recipient consent needed), and on Host disconnect lasting longer than 15 seconds, Host auto-migrates to the next Player in join order. A Player rejoining after losing Host does *not* automatically reclaim it. Host abilities: setting the Round timer in the lobby, starting the GameSession, kicking other Players (removes the target's seat in the lobby; force-disconnects them post-Start per ADR 0009), force-advancing the current Round, driving Reveal pacing for a Chain whose starter is absent, transferring Host to another Player, and ending the GameSession explicitly.
_Avoid_: leader, moderator, admin, owner

**Chain**:
The sequence of Entries belonging to one starting Player. Travels around the table once — each other Player contributes one Entry to it — and returns to its starter for the Reveal. A GameSession with N Players has N Chains, one per Player.
_Avoid_: pad, thread, story

**Entry**:
A single Player's contribution to a Chain at one Round. Either a Caption or a Drawing. A Chain in a GameSession with N Players has N Entries.
_Avoid_: turn, move, link

**Round**:
One tick of GameSession progress, in which every Player completes one Entry on the Chain currently in front of them. A Round ends when all Entries are in, the GameSession's Round timer expires, or the Host force-advances — whichever comes first. At Round-end, any Player whose draft is non-empty has their draft shipped as their Entry; any Player whose draft is empty has their slot filled by their Ghost. The GameSession has N Rounds with N Players.
_Avoid_: phase, step

**Reveal**:
The post-final-Round phase of a GameSession, during which Chains are walked one at a time in join order. Each Chain's starter drives their own Chain's pacing — tapping to advance through Entries one at a time, then once more after the whole-Chain view, then on to the next Chain. If a starter is absent at Reveal time (any reason — voluntary Leave, Kick, or Disconnect — collapse to one operational state per ADR 0009), the Host drives that Chain on their behalf per ADR 0004. The driver role is re-evaluated on every advance, so a starter who reconnects mid-Reveal-of-their-own-Chain regains control immediately. The Reveal phase ends only when the Host explicitly ends the GameSession; clients linger on the final Chain until then.
_Avoid_: results, summary, recap, ending

**Caption**:
A text Entry on a Chain. The Chain's first Entry is its **starter caption** — invented from nothing by the Chain's starter, encouraged-but-not-enforced to be one sentence or less. All subsequent Captions are **guess captions** — written in response to the Drawing immediately preceding them. The central reveal moment is the diff between the starter caption and the final guess caption.
_Avoid_: prompt, label, guess (as a noun standing alone)

**Drawing**:
A visual Entry, made in response to the Caption immediately preceding it on the Chain. Composed of one or more **strokes** — each stroke is a polyline of points in normalized 0..1 coordinates, captured between one pointer-down and the next pointer-up. Rendered black on white at a single fixed brush width (MVP). A Drawing's wire shape is the full ordered list of strokes; see ADR 0010 for the streaming model. A Ghost Drawing carries canned strokes from the in-repo library, identical in rendering to a Player-authored Drawing but flagged so the UI shows the "X's Ghost" label.
_Avoid_: scribble (overloaded with the product name), sketch, doodle

**Ghost**:
A bot stand-in for a Player who submits nothing during a Round. At Round-end, if the server holds no draft input for the Player (zero typed characters, zero strokes), the Ghost contributes an Entry on the Player's behalf using canned content from a small in-repo library (MVP) — later possibly LLM-generated. Ghost Entries are visibly labeled as such in the UI (e.g., "Becky's Ghost"). Replaces the older "placeholder" framing — every slot ends up with real-looking content, attributed either to a Player or their Ghost.
_Avoid_: bot, AI, placeholder, fallback

**Draft**:
A Player's in-progress, not-yet-submitted input for the current Round, held server-side. Typed characters and drawn strokes stream from the Player's client over WebSocket and accumulate as the Draft. On submit (or when the Round ends with non-empty Draft), the Draft is finalized as that Round's Entry for the Player. Submission is one-way: once a Player has explicitly submitted in a Round, the Draft is locked and cannot be edited; a Player who Disconnects after submitting stays submitted.
_Avoid_: working copy, sketch, scratch

## Verbs

**Join**:
A new Player takes a seat in the GameSession. Happens when a WebSocket upgrade arrives with a display name no existing seat holds and capacity allows. Creates the seat, establishes the first connection, and (if the GameSession was empty) confers Host. Lobby-only: once the GameSession has Started, a fresh display name is rejected — the roster is sealed at Start. (Reconnect with an *existing* display name still works post-Start; see ADR 0003.) Fails with a capacity error if the 10-Player cap is already reached.

**Reconnect**:
An existing seat is re-bound to a new live connection. Happens when a WebSocket upgrade arrives with a display name an existing seat already holds. If the seat already had a live connection, that prior connection is superseded — the new connection wins. No authentication step; any WebSocket upgrade typing a held name takes the seat (trust model per ADR 0003).

**Disconnect**:
A Player's live WebSocket connection ends. The seat persists with its Host status, join-order position, and any server-held state intact. The Player remains a member of the GameSession.

**Leave**:
A Player's seat is removed from the GameSession. In the lobby, triggered by a Host kick, a voluntary "leave game" action, or the GameSession ending — frees the display name and the slot toward the 10-Player cap, distinct from Disconnect. Once the GameSession has started, Leave and Kick no longer remove the seat: they collapse into the same Disconnected state as a connection drop (seat persists, Connected flips to false, Ghost fills any missing Entries, Host drives the absent starter's reveal). The only seat-removal path post-Start is the GameSession ending. See ADR 0009.

**Start**:
The Host transitions the GameSession from the lobby into its first Round. Requires at least 2 seated Players. One-way: once Started, a GameSession cannot return to the lobby, and the roster is sealed — no new Joins after Start (existing seats can still Reconnect). The Host's selected Round timer (15–120s in 15s increments, or off) is sealed at Start time and applies to every Round.

**Submit**:
A Player explicitly finalizes their Draft as their Entry for the current Round. One-way — a submitted Draft cannot be edited or un-submitted. A Player who submits and then Disconnects stays submitted; their Entry is the snapshot taken at submit time. Submitting does not, on its own, end the Round: the Round ends only when every seat has submitted, the timer expires, or the Host force-advances.

**Force advance**:
The Host ends the current Round early, before all seats have submitted and before the timer (if any) expires. Available only while a Round is active and only to the Host. Round-end finalization follows the same rules as the other triggers (non-empty Drafts ship as Entries; empty Drafts are filled by Ghosts) — Force advance is a low-friction tool, not a separate state.

**Reveal advance**:
The driver of the current Chain (its starter, or the Host as fallback when the starter is absent) advances the Reveal one step — revealing the next Entry, or transitioning from step-mode to whole-Chain view, or moving on to the next Chain. The driver is re-evaluated per advance against current connection state, so reconnect-mid-Reveal-of-one's-own-Chain restores the starter as driver. Only meaningful during the Reveal phase.

**End game**:
The Host explicitly ends the GameSession, terminating the room. Available to the Host at any time, regardless of phase. Closes all WebSockets cleanly and removes the GameSession from the registry; clients receive a "game ended" signal and render a thanks-for-playing landing. The only way out of a GameSession that has Started.

## Relationships

- A **GameSession** has 2 to 10 **Players** (the floor of 2 is preserved for testing and small-group hobby use; the board game is typically more fun with 4+)
- A **GameSession** with N **Players** has N **Chains**
- A **Chain** belongs to one starting **Player** (its starter) — the one who wrote its starter Caption and who sees its reveal
- A **Chain** with N Players has N **Entries**, alternating **Caption** and **Drawing** — index 0 is always the starter Caption, index 1 a Drawing, index 2 a guess Caption, index 3 a Drawing, etc.
- A **GameSession** advances through N **Rounds**; in each Round, every Player completes one Entry, each on a different Chain
- A **Player** contributes exactly one Entry to each Chain over the course of a GameSession

## Example dialogue

> **Dev:** "If a **Player** disconnects mid-Round, what happens to the **Entry** they were working on?"
> **Domain expert:** "The Player's **Draft** is held server-side, so if they typed or drew anything at all before disconnecting, that gets shipped as their **Entry** at Round-end. If they contributed nothing, their **Ghost** fills the slot — visibly labeled, so the room knows it wasn't them."

> **Dev:** "When the **GameSession** ends, do we show all the **Chains** at once?"
> **Domain expert:** "No — each **Chain** belongs to its starter, and the delight is each starter walking the group through what happened to *their* Chain. The **Reveal** is per-Chain, in join order."

## Flagged ambiguities

- **"Scribble"** is both the product name and the natural English word for the visual Entry type. To keep prose unambiguous, we use **Drawing** as the canonical term in code and docs. User-facing UI copy may still call them "scribbles" for tone — that's a copy choice, not a domain term.
- **"Round"** in colloquial talk about Telestrations sometimes refers to a single Player's turn at the pad. Here a Round is the global tick where *every* Player advances one Entry simultaneously. A single Player's contribution is an **Entry**, not a Round.
- **"Game"** is an **Arcade-wide** term (defined in the root `CONTEXT.md`), not a Scribble term: it names a whole product in the Arcade — Scribble itself is one Game. Within Scribble it must never stand for a play instance; the play instance is a **GameSession**. So "Game" = Scribble-the-product (Arcade scope); "GameSession" = one play of it (Scribble scope).
- **"Session"** alone is also avoided — too close to HTTP-session / auth-session in any web developer's head. Always use the compound **GameSession** when referring to a play instance.
