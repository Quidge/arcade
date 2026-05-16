# Host promotion: voluntary transfer and on-disconnect auto-migrate

The Host role is mobile, not fixed at GameSession creation. Two mechanisms move it:

1. **Voluntary transfer.** The current Host may pick any other Player from the roster and transfer Host to them. The recipient does not need to accept — they just become Host. The previous Host becomes an ordinary Player. The room is visibly notified ("Alice transferred Host to Carol"); the recipient sees a banner naming the powers they now hold.
2. **Auto-migrate on disconnect.** If the Host disconnects, a 15-second grace timer starts. If the Host rejoins within the window, nothing changes — they remain Host. If the window elapses with the Host still disconnected, Host transfers to the **next Player in join order, skipping disconnected Players, wrapping if necessary**. The migration is announced to the room ("Alice was disconnected — Carol is now the Host"). If the original Host rejoins later, they rejoin as an ordinary Player; they do *not* automatically reclaim Host. The current Host may voluntarily transfer it back, or to anyone else, if they choose.

These two mechanisms compose: if the auto-migrated Host themselves disconnects, the same rule applies again, walking further down the join order.

## Considered Options

- **Auto-reclaim by original Host on rejoin** (rejected) — feels intuitive ("you were Host, you're back, you're Host again") but ping-pongs the role every time the original Host's connection blips, and makes the migrated Host's experience unstable. The interim Host accepted responsibility; respect that until they choose to hand it back.
- **Longest-connected as the deterministic migration target** (rejected in favor of join-order round-robin) — also deterministic, but a Player's "connection age" resets on rejoin, so a brief tunnel-blip penalizes them. Join order doesn't have that quirk and is trivially reproducible in tests (the setup *is* the migration plan).
- **Random migration target** (rejected) — non-deterministic, harder to test, no social benefit over join order.
- **Vote-to-elect a new Host** (rejected) — over-engineered for ≤8 friends in a co-located room; introduces a "we're voting" state that kills momentum mid-game.
- **Game-over on Host disconnect** (rejected) — too socially harsh. The Host is just "whoever clicked New game first," not someone with an in-fiction role. Ending the GameSession because one phone died punishes everyone.
- **No voluntary transfer in MVP** (rejected) — auto-migrate alone covers the disconnect case, but the "I want to hand this off because I'm going to the bathroom" case is real and the UI cost is small: a button next to each other Player in the Host's roster view.
- **Require recipient consent on voluntary transfer** (rejected for MVP) — adds a modal-acknowledgment step for a trusted-friends context where it isn't needed. The visible notification on the recipient's screen ("You're now the Host") is sufficient social signal.

## Consequences

- Host state is part of the GameSession's authoritative server state, not derived from "who clicked New game." Any code that asks "who is the Host?" must check current state, not creation metadata.
- The disconnect grace timer is a server-side concern (the server holds Player connection state via the WebSocket) and must survive the Host's own disconnect — the timer cannot live on the Host's client.
- A migrated Host inherits *all* Host powers, including driving reveal for Chains whose starter is absent (per ADR 0004). The fallback-reveal-driver responsibility is not split from the rest of the role.
- Voluntary transfer adds a UI surface: in the Host's roster view, each other Player has a "Make Host" affordance. Non-Host Players' roster views do not show this affordance.
- The "what if every Player disconnects" case is not addressed here — GameSession-with-zero-connected-Players is a separate question (does it pause, end, or persist for rejoin?). Deferred.
