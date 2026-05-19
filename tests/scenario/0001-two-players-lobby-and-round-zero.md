# Scenario 0001 — Two players lobby (and Round 0 to follow)

Two friends, Alice and Bob, get into a GameSession lobby together
from two browser tabs. Alice creates the GameSession; Bob joins via
the shared link. Both see the live roster update, the Host badge on
Alice, and a working share affordance. This file will grow as
subsequent slices add Round 0, Rounds, and reveal — keep extending
this single scenario.

## Setup

- A fresh `scribble` server is running on the local machine. Alice
  has the homepage open; Bob has a blank tab.
- Neither player has any prior GameSession state.

## Steps

1. **Alice:** Loads the homepage at `/`.
   Expected: a heading "Start a game" and a single "New game"
   button. No GameSession code visible yet.

2. **Alice:** Clicks "New game".
   Expected: the URL changes to `/g/<code>` with a freshly
   generated 6-character Crockford Base32 code, dash in the middle
   (e.g. `A4B-K9P`). The lobby page renders, showing the code, a
   "Pick a display name" input, and a Join button. No roster or
   share affordance is visible yet.

3. **Alice:** Types `Alice` into the input and clicks Join.
   Expected: the name panel disappears. A "Players" panel appears
   with one entry, `Alice`, marked with a "Host" badge. A "Share
   this lobby" panel appears below it with a pre-filled text input
   showing the lobby URL and a "Copy link" button.

4. **Bob:** Copies the URL from Alice's address bar (or clicks
   "Copy link" on Alice's screen and pastes), opens it in his own
   tab.
   Expected: the lobby page renders with the same code as Alice's,
   the "Pick a display name" input visible, and no roster yet.

5. **Bob:** Types `Bob` into the input and clicks Join.
   Expected: Bob's name panel disappears; his Players panel
   appears with two entries: `Alice` (with the Host badge) at the
   top, and `Bob` (no badge) below. Bob's own name is rendered
   with the "you" highlight color.

6. **Alice:** Looks at her own tab without reloading.
   Expected: her Players panel now shows two entries — `Alice`
   (Host) and `Bob` — added live, with no refresh. Alice retains
   the Host badge.

7. **Alice:** Clicks "Copy link" on her share panel.
   Expected: the URL is on the clipboard and the helper text
   briefly shows "Copied!" before fading back.

8. **Bob:** Closes his tab.
   Expected: on Alice's tab, Bob's entry in the Players list is
   marked as disconnected (greyed-out name with a "(disconnected)"
   tag after it) within a second or two. Bob's seat is *not*
   removed — his name is still visible. Alice remains, still
   marked Host and connected.

9. **Alice:** Closes her tab.
   Expected: there is no other connected client to observe, but
   the GameSession still holds Alice's seat with the Host badge
   and `connected=false`. The seat is preserved across the
   disconnect.

10. **Alice:** Reopens `/g/<code>` and types `Alice` into the input
    again.
    Expected: her Players panel reappears with both seats —
    `Alice` (Host, connected) and `Bob` (greyed out as
    disconnected). Alice's HOST badge is unchanged; her position
    in the join order is unchanged. No "duplicate name" error;
    the server dispatched a **Reconnect**, not a Join.

11. **Charlie:** With Alice disconnected (between steps 9 and
    10), opens `/g/<code>` and types `Alice` into the input.
    Expected (impostor case, ADR 0003): Charlie's browser
    successfully connects and lands on Alice's seat — there is no
    auth, so the server can't tell impostor from owner, and
    Reconnect replaces the prior live connection (there isn't
    one). The lobby looks normal from Charlie's side.

12. **Alice (after 11):** Reopens `/g/<code>` and types `Alice`.
    Expected: under replace-always, Alice's new connection
    supersedes Charlie's. Charlie's tab receives a close frame
    with the "superseded" reason and shows "This seat was taken
    over by another connection." Alice's lobby is live and
    normal.

## Failure modes to verify by hand

- **Unknown code:** visiting `/g/Z9Z-Z9Z` (well-formed but never
  created) returns a 404 page that links back to home.
- **Lobby cap (8 players):** with 8 seats taken (connected or
  disconnected), a 9th tab attempting to join shows "This game
  session is full (8 players maximum)." and the form stays
  editable. Disconnected seats are *not* silently reclaimed.
- **Visually confusable code:** typing `/g/A4B-K9I` (contains `I`)
  in the address bar returns 404 — the alphabet rejects I, L, O, U.
- **Multi-tab on the same device:** opening a second tab to the
  same GameSession with the same display name supersedes the
  first; the older tab shows the "superseded" status line.

## Notes for future slices

This file is the prose contract for the lobby join flow. When
Round 0 (starter Caption entry) ships, extend this file with
post-lobby Steps rather than starting a new scenario. Same for
Rounds, reveal, and Host migration.
