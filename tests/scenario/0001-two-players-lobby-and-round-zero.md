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
   Expected: on Alice's tab, Bob disappears from the Players list
   within a second or two. Alice remains, still marked Host.

## Failure modes to verify by hand

- **Unknown code:** visiting `/g/Z9Z-Z9Z` (well-formed but never
  created) returns a 404 page that links back to home.
- **Duplicate display name:** with Alice already in the lobby, a
  second tab joining with `Alice` shows the inline error
  "That display name is already taken in this game session.
  Please pick another." and the form stays editable.
- **Lobby cap (8 players):** with 8 players connected, a 9th
  tab attempting to join shows "This game session is full
  (8 players maximum)." and the form stays editable.
- **Visually confusable code:** typing `/g/A4B-K9I` (contains `I`)
  in the address bar returns 404 — the alphabet rejects I, L, O, U.

## Notes for future slices

This file is the prose contract for the lobby join flow. When
Round 0 (starter Caption entry) ships, extend this file with
post-lobby Steps rather than starting a new scenario. Same for
Rounds, reveal, and Host migration.
