# Scenario 0002 — N=2 playable: Round 1 Drawing, per-Chain reveal, end-game

Two friends, Alice and Bob, play a complete two-Player game from
the lobby to the reveal payoff and a Host-initiated tear-down. This
file extends scenario 0001: it picks up at a fresh two-player lobby
(Alice as Host, Bob connected) and walks through Round 0, Round 1
(the drawing round introduced in issue #29), and the per-Chain
Reveal. End-game tear-down is exercised at multiple points.

## Setup

- A fresh `scribble` server running on the local machine.
- Two browser tabs: Alice's homepage open; Bob ready to follow the
  lobby link Alice shares.

## Steps — happy-path end-to-end

1. **Alice** creates a GameSession (clicks "New game" on the home
   page) and joins the lobby as `Alice`. The Host badge sits on her
   row.

2. **Bob** opens the lobby link Alice shares and joins as `Bob`.
   Both rosters now show `Alice (Host, connected)` and `Bob (no
   badge, connected)`.

3. **Alice (Host):** Sets the Round timer to 30s and clicks
   "Start".
   Expected: both browsers transition to the Round 0 caption view
   with prompt "Invent the first Caption for your Chain", a
   focused textarea, and a synchronised "Time left: 0:30"
   countdown. A small "End game" button now appears on Alice's
   screen at the bottom-right of the layout; Bob does not see this
   button.

4. **Alice and Bob** each type a starter Caption ("a wizard
   losing an argument with a goose" and "two squirrels reviewing a
   contract") and both click "Submit". Each sees a "Submitted —
   waiting for others…" banner. Once the second submit lands, the
   server transitions through Round 0 finalization.

5. **Both browsers** transition to the **Round 1 — Drawing** view
   within a fraction of a second of the second submit.
   Expected:
   - A prompt panel at the top showing the *other* Player's
     Round 0 caption (Alice's screen reads "two squirrels
     reviewing a contract"; Bob's reads "a wizard losing an
     argument with a goose").
   - A roughly square, white drawing canvas filling the rest of
     the viewport width.
   - "Undo last stroke" and "Submit" buttons below the canvas.
   - A countdown ticking down from 0:30, in sync between both
     browsers.
   - Alice sees a small "Force advance" affordance next to
     Submit; Bob does not.
   - Alice's "End game" button is still visible at the bottom.

6. **Alice** drags a few quick strokes on her canvas with the
   mouse (or finger on touch). Each pointer-up shows a fresh
   black stroke; the canvas accumulates strokes over time. She
   makes a deliberate mistake on stroke 3, clicks **"Undo last
   stroke"**, and confirms stroke 3 disappears while strokes 1
   and 2 remain. None of Alice's strokes appear on Bob's screen
   — drawing state is per-Player while in progress (privacy is
   tested at the wire level in `tests/integration/round_one_test.go`).

7. **Bob** draws his response on his own canvas, leaves it
   visibly different from Alice's. Both Players click **Submit**.
   Each canvas locks, the "Submitted — waiting for others…"
   banner appears under the canvas, and once both submits land
   the room transitions to the **Reveal**.

8. **Both browsers** transition to the **Reveal** view at the
   same time.
   Expected:
   - A persistent header reading **"Alice's Chain"** (Chain
     index 0's starter).
   - One Entry card visible: Alice's starter Caption from
     Round 0, with the author name (Alice).
   - A "Next" button on Alice's screen (she is the driver —
     her own Chain). Bob sees "Watching — Alice is driving."
     with no Next button.

9. **Alice** clicks "Next".
   Expected: a second Entry card appears showing Bob's Drawing
   on Alice's Chain (rendered black on white in the same
   normalized-coordinate style as the live canvas). Bob's
   screen mirrors the update; the header still reads "Alice's
   Chain".

10. **Alice** clicks "Next" again.
    Expected: the cards stay in place but the header (or a meta
    line) flips to "Whole chain" — the cursor entered `full`
    mode. Both Players see the same view.

11. **Alice** clicks "Next" once more.
    Expected: the room transitions to **Bob's Chain**. The
    header reads "Bob's Chain" and one Entry card is visible:
    Bob's starter Caption. The driver flips to **Bob** —
    Alice's "Next" button disappears and is replaced by
    "Watching — Bob is driving.", while Bob's screen now shows
    the "Next" button.

12. **Bob** clicks "Next" through his Chain to walk Alice
    through it: a second Entry (Alice's Drawing) appears, then
    the whole-Chain view, and finally the cursor reaches
    `complete`.
    Expected at `complete`: the Reveal panel reads "Reveal
    complete — host can end the game." with no Next button
    visible on either screen. The GameSession remains in
    `StateReveal` on the server (no automatic transition to
    `StateEnded`).

13. **Alice (Host)** clicks **"End game"** at the bottom of
    her screen and confirms the prompt.
    Expected: both browsers swap to a centred **"Thanks for
    playing!"** view with a link back to `/`. The WebSocket on
    each tab is closed cleanly by the server immediately after
    the `game-ended` broadcast.

## Steps — Reveal driver fallback and reconnect

These steps reset to a fresh two-Player lobby and re-run through
the round phases so the reveal begins. They then exercise the
driver-eligibility re-evaluation rule (ADR 0011): when a Chain's
starter is absent the Host drives that Chain; when the starter
returns, control snaps back.

14. Alice and Bob complete Round 0 (both submit captions) and
    Round 1 (both submit drawings) as in steps 3–7. The Reveal
    begins on **Alice's Chain** with Alice driving.

15. **Alice** advances her Chain through `step → step → full`
    (two clicks). Cursor now sits at the final Entry of Chain 0
    in `full` mode; the next click will transition to Bob's
    Chain.

16. **Bob** closes his browser tab (simulating Disconnect).
    Alice's roster — if shown — now marks Bob disconnected.

17. **Alice** clicks "Next" once more.
    Expected: the Reveal transitions to **Bob's Chain**. The
    header reads "Bob's Chain" and the driver line on Alice's
    screen reads "You're driving." — because Bob is absent, the
    Host (Alice) drives Bob's Chain by fallback per ADR 0004.

18. **Bob** reopens `/g/<code>` and rejoins as `Bob`.
    Expected: Bob's screen immediately renders the current
    Reveal-state — same Chain, same entries visible. The driver
    line on his screen reads "You're driving." because the
    server's per-command driver check re-evaluates against live
    connection state on every advance. On Alice's screen the
    driver line flips to "Watching — Bob is driving." the next
    time a `reveal-state` is broadcast (which happens on Bob's
    next click).

19. **Bob** clicks "Next" to walk the rest of his Chain.
    Expected: Bob's clicks succeed — the server's driver check
    accepts him now that he is Connected. The Chain walks
    through to `complete` and the Reveal-complete view appears
    on both screens.

20. **Alice (Host)** clicks "End game" to tear down. Both
    screens swap to "Thanks for playing!" and disconnect
    cleanly.

## Steps — End-game mid-Round

21. Reset to a fresh lobby. Alice starts a Round with a 60s
    timer. Both browsers transition to the Round 0 view; Alice
    has typed nothing yet.

22. **Alice (Host)** clicks "End game" and confirms.
    Expected: both screens swap to "Thanks for playing!" and
    the connections close cleanly. End-game is available in
    every phase from Start onward — the host does not have to
    wait for the Round or Reveal to finish.

## Steps — Ghost responses end-to-end

23. Reset to a fresh lobby. Alice starts Round 0 with a 30s
    timer. Both Players type a Caption; both submit. Round 1
    begins.

24. **Alice** draws on her canvas; **Bob** types or clicks
    nothing — his canvas stays blank. The timer reaches 0.
    Expected: the room transitions to the Reveal as before.
    Walking through **Alice's Chain**, the Drawing card shows
    a labelled "Bob's Ghost" tag because Bob produced no
    strokes — the server filled his slot with the in-repo
    canned Drawing (a triangle, MVP). The drawing renders
    normally; the label distinguishes Ghost-supplied content.

25. **Alice (Host)** ends the game.

## Failure modes / edge cases to verify by hand

- **Stroke privacy mid-Round:** while drawing, only your own
  canvas updates. The wire-level guard is asserted by
  `TestRoundOneInProgressStrokesArePrivate`.
- **End-game by non-Host:** Bob (non-Host) sending an
  `end-game` command via developer tools (or any client
  emitting that envelope) is silently rejected by the server.
  The phase does not advance to `StateEnded`.
- **Reconnect mid-Reveal of one's own Chain:** the driver role
  re-arrives on the next advance, not via a separate event.
  No "Next" button appears on the reconnected client until the
  next `reveal-state` broadcast arrives (or until the unicast
  on reconnect, whichever is first).
- **`reveal-state` envelope on reconnect:** opening a fresh
  tab to `/g/<code>` and rejoining as a known seat name lands
  the user directly into the Reveal panel with the same
  Chain, the same entries_visible, and the right driver. No
  re-walk of prior Chains.

## Notes for future slices

Extend this file when N>=3 ships (more Chains to walk), when a
"Save the room's chains as an album" or similar export ships,
and when authentication / persistence land. The single-scenario
pattern of file 0001 + this file 0002 is the working contract
until something forces a split.
