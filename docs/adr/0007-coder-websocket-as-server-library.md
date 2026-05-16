# Server-side WebSocket library: coder/websocket

The project takes a dependency on `coder/websocket` (previously `nhooyr.io/websocket`) as the server-side WebSocket library. Go's standard library has no WebSocket support, so this is a required dependency, not a discretionary one.

`coder/websocket` is chosen over `gorilla/websocket` for two reasons. First, its API is built around `context.Context` throughout — read deadlines, write deadlines, cancellation, and connection lifecycle are all expressed through standard Go context propagation, with no parallel timeout-management ceremony. This matches the rest of the project's stdlib-idiomatic posture. Second, it has substantially more documentation coverage on context7 (65 indexed snippets vs. gorilla's 28, with a benchmark score of 89.54 vs. 77.85), which means agents working on this codebase can fetch current, well-structured API docs on demand rather than relying on training-data familiarity. Both libraries have "High" source reputation; quality on the documentation side favors `coder/websocket`.

## Considered Options

- **`gorilla/websocket`** (rejected) — long-time standard, battle-tested, mature ecosystem. Was archived by its original maintainer in 2022 and revived under new maintainers; currently active but with a more conservative release cadence. API is functional but predates `context.Context` adoption — manual `SetReadDeadline`, ping/pong management, and byte-oriented read/write feel dated next to modern Go idioms. Stronger choice if the project prioritized "the library the most contributors will have used before"; chosen against here because of the modern-API and context7-coverage advantages.
- **`golang.org/x/net/websocket`** (rejected) — officially in the Go subrepos but effectively abandoned. Predates `context.Context`. Widely considered legacy. Not a real option.
- **Roll our own WebSocket implementation** (rejected) — protocol-level work has subtle gotchas (frame fragmentation, ping/pong, close handshakes) that don't reward implementation effort here. Out of scope for the project's learning goals.

## Consequences

- A non-stdlib dependency is added to `go.mod`. This is the first such direct dependency for this codebase; future agents should treat the bar for adding more dependencies as "ask before adding" — see `docs/agents/dependencies.md`.
- The agent picking up the first WebSocket-touching slice will be the one to actually `go get` this library and write code against it. They should fetch current docs via `mcp__context7__query-docs` for `/coder/websocket` rather than relying on training-data recall.
- If the project ever needs a feature `coder/websocket` doesn't support — uncommon, but possible for niche protocol extensions — migration to `gorilla/websocket` is non-trivial but bounded; the WebSocket-touching code is concentrated in the broadcaster and presence module(s).
