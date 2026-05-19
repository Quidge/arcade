# Reveal mechanics: per-Chain, starter-driven

At GameSession end, Chains are revealed one at a time in join order. The starter of each Chain controls the pace of *their own* Chain's reveal — their phone shows the "Next" button while everyone else's says "Watching." Entries are shown one at a time, building suspense, with the whole Chain visible at the end for re-reading and screenshotting. The whole room sees the same screen state, synchronized — this is one shared experience, not N parallel ones.

If the starter of a Chain is absent at reveal time — for any reason — the Host takes over driving that Chain's reveal. The Chain still wears the starter's name in the header; the Host is just driving the slideshow on their behalf. "Absent" is one operational state: the starter's seat exists but is not currently bound to a live WebSocket. Voluntary Leave, Kick, and Disconnect mid-game all collapse into that single state per ADR 0009.

## Considered Options

- **Host-driven for all reveals** (rejected) — works, but takes the "delight back to the starter" payoff away from the people who deserve it most.
- **Auto-advance on a server tick** (rejected) — loses the social agency that makes the reveal feel like a shared room moment.
- **Anyone-in-the-room-taps-Next** (rejected) — weakens Host authority and risks race conditions where two people tap.
- **Show only the diff between starter caption and final guess caption** (rejected) — the funniest part is the step-by-step drift, not just the endpoints.
- **Per-Player phones at different states (async reveal)** (rejected) — breaks the co-located shared-moment model that Scribble is designed for.
