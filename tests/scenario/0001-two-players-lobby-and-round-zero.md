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

## Host mobility and Leave

These steps continue from a normal two-player lobby (Alice as
Host, Bob connected) and exercise the Host-management slice
(issue #7 / ADR 0005).

13. **Alice:** In her roster panel, next to `Bob` she sees two
    small action buttons: "Make Host" and "Kick." Next to her
    own row she sees no such buttons. At the bottom of the
    Players panel she sees a "Leave game" button. Bob's view
    shows neither "Make Host" nor "Kick" — only his own "Leave
    game" button.

14. **Alice:** Clicks "Make Host" next to Bob.
    Expected: the Host badge moves to Bob's row in both Alice's
    and Bob's panels. A transient notice appears in both panels
    reading "Alice transferred Host to Bob." Alice's row now
    has neither "Make Host" nor "Kick" affordances (she is no
    longer Host); Bob's row in Alice's view shows neither
    affordance for the same reason (Bob is now Host, and a Host
    sees the affordances on _other_ Players). Bob's view of
    Alice's row now shows "Make Host" and "Kick" — Bob is the
    Host now.

15. **Bob:** Clicks "Make Host" next to Alice to give Host back.
    Expected: badge moves back to Alice; notice reads "Bob
    transferred Host to Alice."

16. **Alice:** Clicks "Kick" next to Bob.
    Expected: Bob's tab is closed by the server and shows the
    message "You were removed from this game session by the
    host." Bob's seat disappears from Alice's roster. A notice
    appears in Alice's panel reading "Bob was kicked from the
    game."

17. **Bob (after 16):** Reopens `/g/<code>` and types `Bob`.
    Expected: Bob joins as a fresh seat (a Join, not a
    Reconnect) — the kick removed his prior seat. Alice's
    roster shows Bob again; Bob has no Host badge.

18. **Bob:** Clicks his own "Leave game" button and confirms
    the prompt.
    Expected: Bob's tab returns to the name-entry view. Bob's
    seat is removed from Alice's roster. Alice sees a notice
    reading "Bob left the game."

19. **Bob:** Re-joins as `Bob` from the name-entry view to set
    up the next step.

20. **Alice (Host, with Bob connected):** Closes her tab.
    Expected: Bob's roster marks Alice as disconnected (greyed
    out, "(disconnected)" tag) but still showing the Host
    badge. A grace timer is running server-side, invisible to
    the UI. If Alice reopens her tab and re-enters `Alice`
    within ~15 seconds, the grace timer is cancelled and her
    Host badge persists unchanged.

21. **Alice (continued, Host has gone for >15s):** After the
    grace window elapses, Bob's panel shows the Host badge move
    to Bob, and a notice appears reading "Alice was
    disconnected — Bob is now the Host." Alice's row remains
    in the roster, greyed out, with no Host badge.

22. **Alice (after 21):** Reopens `/g/<code>` and rejoins as
    `Alice`.
    Expected: Alice rejoins as an ordinary connected Player.
    She does NOT auto-reclaim the Host badge — Bob remains
    Host. The badge can be handed back via step 15 if Bob
    chooses, but there is no automatic restoration.

23. **Bob (now Host, with Alice connected as ordinary Player):**
    Clicks "Leave game" and confirms.
    Expected: Bob's seat is removed immediately. Alice's
    roster shows the Host badge move to her right away — there
    is no grace wait for a voluntary Leave. The notice reads
    "Bob left the game — Alice is now the Host."

## Round 0 — starter Captions

These steps continue from a normal two-player lobby with Alice as
Host and Bob connected. They exercise the Round-0 slice (issue
#8 / ADR 0003 / ADR 0009).

24. **Alice (Host):** In her lobby Players panel sees, alongside
    the per-row Make-Host / Kick affordances, a Host-only block at
    the bottom with a "Round timer" dropdown (defaulted to 60 s)
    and a "Start" button. Bob does not see these controls.

25. **Alice:** Changes the Round timer dropdown to 30 s. No
    visible change yet — the choice is held server-side until
    Start.

26. **Alice:** Clicks "Start."
    Expected: both Alice's and Bob's screens transition from the
    lobby Players + Host-controls layout to a Round-0 panel
    titled "Round 0 — Starter Caption" with the prompt "Invent
    the first Caption for your Chain" and the hint "One sentence.
    Anything goes." A textarea is focused on each player's screen
    and a countdown reading "Time left: 0:30" begins ticking
    down, synchronized between the two browsers (both show the
    same remaining seconds within one tick). Alice additionally
    sees a small Host-only "Force advance" button next to the
    Submit button; Bob does not.

27. **Alice and Bob:** Each types their starter Caption a few
    characters at a time, watching the textarea on their own
    screen. Each keystroke is silently streamed to the server.
    Neither sees the other's text on their own screen — Drafts
    are per-Player.

28. **Alice:** Clicks "Submit."
    Expected: Alice's textarea grays out and locks; the "Submit"
    button disables; a "Submitted — waiting for others…" banner
    appears above the countdown. Bob sees no change — submission
    status is not broadcast in this MVP.

29. **Bob:** Lets the countdown reach 0 without submitting.
    Expected: at deadline, both browsers swap from the Round-0
    panel to a "Round 0 complete" panel reading "More game flow
    coming soon." Bob's typed Draft was shipped as his Entry
    automatically (any input ships per ADR 0003) — there is no
    visible Entry list in this slice, but his Caption is held
    server-side for the reveal slice.

## Round 0 — Disconnect and Ghost edge cases

These steps reset to a fresh two-player lobby (Alice as Host, Bob
connected) and exercise the protocol's resilience.

30. **Alice (Host):** Sets the timer to 60 s and clicks Start.
    Both screens transition to Round 0 with a 1:00 countdown.

31. **Bob:** Types a partial Caption ("the cat is on") and
    closes his tab.
    Expected: Alice's screen continues showing the countdown.
    Bob's seat in the underlying domain still has his partial
    Draft preserved server-side; the lobby roster (if shown)
    would mark Bob disconnected.

32. **Bob:** Reopens `/g/<code>` and types his name again to
    Reconnect.
    Expected: the Round-0 panel appears immediately with his
    countdown matching Alice's (the same `deadline_ms`), and his
    textarea is pre-filled with "the cat is on" — the partial
    Draft restored from the server. He can continue typing where
    he left off.

33. **Bob:** Finishes the sentence ("the cat is on the mat") and
    clicks Submit. Alice waits.

34. **Alice:** Lets the timer run out (Alice has typed nothing).
    Expected: at deadline, both screens swap to "Round 0
    complete." Alice's slot is filled by her Ghost — visibly
    labeled "Alice's Ghost" in any subsequent UI exposing the
    finalized Entries.

35. **Reset, then Host force-advance scenario:** Set up a fresh
    Round 0 with timer 60 s. Bob types a couple of words; Alice
    has typed nothing. Alice clicks "Force advance."
    Expected: both screens transition immediately to "Round 0
    complete." Bob's typed words ship as his Entry (per ADR
    0003); Alice's slot is Ghost-filled.

36. **Reset, then Leave mid-Round:** Set up a fresh Round 0
    with timer 60 s. Bob clicks "Leave game" mid-typing. Per ADR
    0009, his seat is *not* removed; instead his connection
    drops (his tab returns to name entry) and his row in Alice's
    roster — if shown — would be marked disconnected. Bob's
    Draft up to that moment is preserved server-side, and at
    Round-end (Alice force-advances or the timer expires) Bob's
    Entry is either his partial text or a Ghost depending on
    whether he had typed anything before pressing Leave.

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
