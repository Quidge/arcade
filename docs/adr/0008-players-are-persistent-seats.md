# Players are persistent seats; WebSocket connection state is a separate concern

A Player is a slot in a GameSession that persists across WebSocket disconnects, not an ephemeral pairing of "display name + live connection." When a Player Disconnects — their WebSocket dies, intentionally or otherwise — their seat remains in the GameSession: Host status, position in the join order, and any server-held state attached to them are preserved. The seat is removed only by Leave (Host kick, voluntary "leave game," or GameSession ending).

A separate per-Player flag tracks whether the Player currently has a live WebSocket. The verb pair **Reconnect** / **Disconnect** operates on this connection state. The verb pair **Join** / **Leave** operates on seat membership. These two pairs are not interchangeable, and conflating them was the bug PR #14 inadvertently introduced.

This ADR restores what issue #6's implementation elided. #6 chose the simpler single-state model — "the Players map *is* the roster; only currently-connected Players are present" — because none of #6's user-facing requirements (the lobby happy path) needed the distinction. Subsequent slices do:

- ADR 0005's Host auto-migration on disconnect requires a stable join-order list that survives disconnects (round-robin must *skip* disconnected Players, not forget them ever existed).
- ADR 0003's server-held Drafts must outlive a mid-Round WebSocket drop — they are keyed by Player, and the Player must still exist when the Round ends.
- Ghost slot-filling (issue #8) likewise requires an absent Player's seat to still be in the roster when their Chain reaches them.

The single-state model could not support these without smearing the state pair into other domain objects: Draft keyed by display name independently of any Player record, join-order list maintained separately from membership, Host badge tracked outside the Player struct. The seat-as-persistent abstraction is the unifying concept. We accept the modest extra complexity of two-state Players in exchange for not needing parallel bookkeeping in three downstream slices.

## Considered Options

- **Single-state Player, where Disconnect = full membership removal** (rejected — the path PR #14 took). Simpler in the local sense but pushes the persistence requirement onto Draft, Chain, Ghost, and Host-migration code as orphan-bookkeeping. The cumulative complexity exceeds maintaining two-state Players in one place.
- **Two-state, but only post-Start** (rejected). Disconnect = Leave in the lobby; Disconnect = stay-seated in the active game. Tempting because lobby seats abandoned mid-pre-game feel less load-bearing. Rejected because (a) the dead-Host lobby case still needs auto-migrate, which works the same whether or not the game has started, and (b) two phase-dependent disconnect semantics is harder to teach than one rule.
- **One-state Player plus a separate per-display-name "presence record"** (rejected). Same information, awkwardly split across two structures; the join-order list lives nowhere natural.
- **Connection multiplicity (multiple live WebSockets per seat)** (rejected for MVP). Would let "phone + tablet at the same table" both observe the GameSession under one Player identity. Real Draft-input-merge problem; not worth solving in MVP. Replace-always (the new WebSocket supersedes the old) handles the common case (refresh, network reconnect) without it.

## Consequences

- `internal/gamesession.Player` carries a `Connected bool` field. Roster serialization carries the field on the wire so clients can render disconnected Players (e.g., greyed out with a "(disconnected)" tag).
- The domain API exposes three methods — `Join`, `Reconnect`, `Disconnect` — each doing one thing. `Leave` is reserved as the seat-removal verb but is not implemented in the slice that introduces seat persistence; it lands alongside Host kick in the slice covering ADR 0005's Host-management work.
- Display-name uniqueness is enforced at the seat level: a WebSocket upgrade with a name that has an existing seat is dispatched to `Reconnect`, never to `Join`. The `ErrDuplicateName` error code is deleted; the only Join rejection is `ErrCapExceeded`.
- Connection conflict (a new WebSocket upgrade arrives for a seat that already has a live connection) is resolved by **replace-always**: the new connection supersedes the old, which is closed with a "superseded" reason. This handles refresh, multi-tab, and impostor scenarios uniformly under one rule. The trust model (ADR 0003) accepts the impersonation risk in exchange for not requiring auth.
- The 8-Player cap counts seats, not live connections. A disconnected Player whose seat is held does not free a slot for a 9th joiner.
- The lobby may enter a transient "Host is disconnected" state until ADR 0005's auto-migrate ships. The lobby visibly shows the disconnected-Host situation; no action is taken to free the lobby in the interim. This is an explicit cost between slices, mitigated by the fact that Host-management work (ADR 0005, issue #7) is the natural next slice and removes the deadlock.
- Event surface: `PlayerJoined`, `PlayerDisconnected`, `PlayerReconnected`. `PlayerLeft` is removed until kick lands in the Host-management slice.
