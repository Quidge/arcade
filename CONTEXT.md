# Scribble

Scribble is a small web app for playing a digital reimagining of Telestrations — a co-located, mobile-first, server-rendered party game. Eventually a small suite of party games; for now, just the one.

## Language

**GameSession**:
A single play instance of one game in the Scribble suite, capped at 8 Players. Has a Host-configured Round timer (15–120s in 15s increments, or off). Starts when the Host clicks "Start" and ends after the reveal.
_Avoid_: game (alone — overloaded with the game-type concept), session (alone — overloaded with HTTP/auth-session), room, match, instance

**Player**:
A participant in a GameSession, identified by a display name they choose when joining. Display names are unique within a GameSession; rejoining after a disconnect requires the same display name on the same join code.
_Avoid_: user, guest

**Host**:
The Player who created the GameSession by clicking "New game." Exactly one per GameSession. Has unique abilities: setting the Round timer in the lobby, starting the GameSession, kicking other Players, and force-advancing the current Round.
_Avoid_: leader, moderator, admin, owner

**Chain**:
The sequence of Entries belonging to one starting Player. Travels around the table once — each other Player contributes one Entry to it — and returns to its starter for the reveal. A GameSession with N Players has N Chains, one per Player.
_Avoid_: pad, thread, story

**Entry**:
A single Player's contribution to a Chain at one Round. Either a Caption or a Drawing. A Chain in a GameSession with N Players has N Entries.
_Avoid_: turn, move, link

**Round**:
One tick of GameSession progress, in which every Player completes one Entry on the Chain currently in front of them. A Round ends when all Entries are in, the GameSession's Round timer expires, or the Host force-advances — whichever comes first. At Round-end, any Player whose draft is non-empty has their draft shipped as their Entry; any Player whose draft is empty has their slot filled by their Ghost. The GameSession has N Rounds with N Players.
_Avoid_: phase, step

**Caption**:
A text Entry on a Chain. The Chain's first Entry is its **starter caption** — invented from nothing by the Chain's starter, encouraged-but-not-enforced to be one sentence or less. All subsequent Captions are **guess captions** — written in response to the Drawing immediately preceding them. The central reveal moment is the diff between the starter caption and the final guess caption.
_Avoid_: prompt, label, guess (as a noun standing alone)

**Drawing**:
A visual Entry, made in response to the Caption immediately preceding it on the Chain.
_Avoid_: scribble (overloaded with the product name), sketch, doodle

**Ghost**:
A bot stand-in for a Player who submits nothing during a Round. At Round-end, if the server holds no draft input for the Player (zero typed characters, zero strokes), the Ghost contributes an Entry on the Player's behalf using canned content from a small in-repo library (MVP) — later possibly LLM-generated. Ghost Entries are visibly labeled as such in the UI (e.g., "Becky's Ghost"). Replaces the older "placeholder" framing — every slot ends up with real-looking content, attributed either to a Player or their Ghost.
_Avoid_: bot, AI, placeholder, fallback

**Draft**:
A Player's in-progress, not-yet-submitted input for the current Round, held server-side. Typed characters and drawn strokes stream from the Player's client over WebSocket and accumulate as the Draft. On submit (or when the Round ends with non-empty Draft), the Draft is finalized as that Round's Entry for the Player.
_Avoid_: working copy, sketch, scratch

## Relationships

- A **GameSession** has 2 to 8 **Players** (the floor of 2 is preserved for testing and small-group hobby use; the board game is typically more fun with 4+)
- A **GameSession** with N **Players** has N **Chains**
- A **Chain** belongs to one starting **Player** (its starter) — the one who wrote its starter Caption and who sees its reveal
- A **Chain** with N Players has N **Entries**, alternating **Caption** and **Drawing** — index 0 is always the starter Caption, index 1 a Drawing, index 2 a guess Caption, index 3 a Drawing, etc.
- A **GameSession** advances through N **Rounds**; in each Round, every Player completes one Entry, each on a different Chain
- A **Player** contributes exactly one Entry to each Chain over the course of a GameSession

## Example dialogue

> **Dev:** "If a **Player** disconnects mid-Round, what happens to the **Entry** they were working on?"
> **Domain expert:** "The Player's **Draft** is held server-side, so if they typed or drew anything at all before disconnecting, that gets shipped as their **Entry** at Round-end. If they contributed nothing, their **Ghost** fills the slot — visibly labeled, so the room knows it wasn't them."

> **Dev:** "When the **GameSession** ends, do we show all the **Chains** at once?"
> **Domain expert:** "No — each **Chain** belongs to its starter, and the delight is each starter walking the group through what happened to *their* Chain. The reveal is per-Chain, in some order."

## Flagged ambiguities

- **"Scribble"** is both the product name and the natural English word for the visual Entry type. To keep prose unambiguous, we use **Drawing** as the canonical term in code and docs. User-facing UI copy may still call them "scribbles" for tone — that's a copy choice, not a domain term.
- **"Round"** in colloquial talk about Telestrations sometimes refers to a single Player's turn at the pad. Here a Round is the global tick where *every* Player advances one Entry simultaneously. A single Player's contribution is an **Entry**, not a Round.
- **"Game"** is *not* a domain term in Scribble. The word is too overloaded — it could mean either the type of game in the suite (Telestrations clone, future additions) or one play instance. The play instance is a **GameSession**. When the suite has more than one game type, we'll introduce a separate domain term for the type concept; for now, talk about "the Telestrations clone" or "the game we're playing" colloquially.
- **"Session"** alone is also avoided — too close to HTTP-session / auth-session in any web developer's head. Always use the compound **GameSession** when referring to a play instance.
