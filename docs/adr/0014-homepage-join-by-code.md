# Homepage offers join-by-code; HEAD-probes the existing lobby URL

The homepage gains a code input alongside the existing "Host a new game" button.
On submit, JS issues `fetch('/g/<code>', {method: 'HEAD'})`: 200 redirects to the
lobby, 404 surfaces "No game with that code" inline above the input. Display-name
entry stays inside the lobby — the homepage validates only that the code resolves
to a real GameSession, and every other rejection (cap exceeded, sealed roster,
name conflict) continues to surface at the lobby's WebSocket name-entry where it
already lives.

## Considered Options

- **JSON API on `/g/<code>` (rejected)** — adds a public machine-readable surface
  and an implicit JSON-versioning commitment to answer a question (does this
  code resolve?) that an HTTP status code already answers. YAGNI; revisit when
  a second consumer with real shape requirements appears.
- **POST `/join` endpoint that 303s (rejected)** — pure-HTML and refresh-safe,
  but duplicates `handleLobby`'s validation in a second handler and threads
  error state through query params. HEAD on the existing handler reuses
  validation exactly, at the cost of requiring JS on the homepage.
- **HTMX (rejected)** — new client-side dependency, meaningful drift from
  ADR 0001's "hand-rolled vanilla JS, direct `WebSocket`/`fetch`" posture. The
  homepage interaction is ~15 lines of vanilla JS without it.
- **Homepage collects display name too (rejected)** — moves the lobby's existing
  WS-driven name-entry onto the homepage, requiring either a new pre-validation
  endpoint (race condition with another joiner picking the same name in the gap
  before WS opens) or a homepage-opened WebSocket. The lobby already handles
  every name-related rejection.
