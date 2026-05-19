# Strokes ship full-replace on the wire, symmetric with text Drafts

A Player's in-progress Drawing streams from their client over WebSocket using the same envelope pattern as text Drafts: each update carries the *entire current* stroke list, not just the new stroke since the last update. Wire shape:

```json
{ "type": "draft", "strokes": [[{"x":0.30,"y":0.70}, ...], [...], ...] }
```

A new stroke (on pointer-up) means the client appends locally, then sends the full updated list. Undo means the client pops locally, then sends the full shorter list. The server replaces its stored Drawing wholesale on every Apply, identical to how text Drafts are handled. On Reconnect, the server unicasts the full current stroke list, identical to how text rehydration works.

This is deliberately the inverse of the more obvious "append the new stroke incrementally" design. We accept worse asymptotic bandwidth — a Drawing with K strokes sent over K updates carries O(K²) total bytes — in exchange for:

- **Wire symmetry with text Drafts.** Text Drafts already ship full-snapshot per keystroke (`{"type":"draft","text":"<entire current text>"}`). A drawing slice that introduced an append-style envelope would mean two different mental models for "what does a draft update look like?" — one full-replace, one incremental. One mental model is easier to teach, easier to debug, and easier to extend if a third Draft type ever appears.
- **Trivial reconnect handling.** Server stores the latest snapshot; on Reconnect it unicasts that snapshot. No replay log, no "here's everything since you disconnected" reconstruction, no client-side accumulation. The same code path that rehydrates a text Reconnect rehydrates a strokes Reconnect.
- **Idempotent updates.** A duplicate or retransmitted full-replace message produces the same state. An out-of-order append wouldn't.
- **No upfront optimization for a problem we haven't measured.** A co-located party game with 2–8 Players, normalized 0..1 coordinates serialized as JSON, and Drawings that realistically max out at a few dozen strokes per Round produces wire traffic in the low tens of KB even in pathological cases. The bandwidth concern is theoretical at MVP scale; we revisit if it ever becomes empirical.

If the asymptotic cost ever becomes a real problem, the migration path is well-understood: introduce a second envelope (`stroke-append`) alongside the existing `draft`, run both for a transition, then deprecate the snapshot path. The change is mechanical because the data shape doesn't change — only the *delta-encoding* of updates does. Locking in the simpler shape now does not foreclose the harder shape later.

## Considered Options

- **Incremental append (`{"type":"stroke-append","points":[...]}`)** (rejected). Smallest wire footprint. Cost: an append-style envelope creates a wire-format asymmetry with text Drafts; reconnect rehydration becomes "server replays the full list anyway"; undo requires either a separate `stroke-remove` envelope or a client-side "I removed N strokes, please drop the last N" message. The bandwidth saving is real but the complexity cost is paid by every reader of the wire format, on every slice that touches Drafts. Not worth it pre-measurement.
- **Hybrid: snapshot every N strokes, append in between** (rejected). Attempts to bound the quadratic cost while preserving most of the append simplicity. Introduces a third concept ("when does a snapshot fire?") that doesn't exist for text and produces edge cases around partial-snapshot rehydration. More complex than either pure option.
- **Binary wire format for strokes** (rejected for MVP). A packed binary encoding (e.g., interleaved Int16 coordinates) would shrink stroke payloads by ~5×. Cost: a second wire-format dialect (JSON for everything else, binary for strokes), client-side encoder/decoder code, harder debuggability ("what does this frame mean?"). Wrong slice to optimize on.
- **Defer the strokes implementation entirely until we have measurement data on user-typical Drawing complexity** (rejected). The slice's entire value is the user-facing Drawing flow; deferring the implementation defeats the slice. We instead defer the optimization, not the feature.

## Consequences

- The new `internal/strokes` package's `Apply(round, player, drawing)` method takes the full `Drawing` value and replaces wholesale, mirroring `internal/draft.Apply(round, player, text)`. Same lifecycle, same shape, different value type.
- The `round-state` envelope's `draft` field carries the full strokes list on every Reconnect or unicast snapshot. No incremental rehydration path exists.
- The `draft` client → server envelope carries the full strokes list on every pointer-up and every Undo. Client-side stroke management owns the in-flight list; the server is a downstream replica.
- A Round's worst-case strokes payload size is `O(strokes_in_round × points_per_stroke)` per *update*, and `O(strokes_in_round² × points_per_stroke)` over the Round. For the MVP's expected complexity (tens of strokes per Round, tens of points per stroke), the total wire traffic per Round is bounded in the low hundreds of KB even pathologically. Measurement before optimization.
- The migration path to incremental encoding, if ever needed, is documented and is a single-slice change. Adding `stroke-append` alongside `draft` and switching clients over is a wire-format extension, not a data-model change.
- A future polish slice adding stroke metadata (color, width, tool) extends the per-stroke shape but does not change the full-replace semantics. The asymmetry-with-text trade-off is paid once and stays paid.
