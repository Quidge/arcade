# Code-as-primary URL identifier; Crockford Base32 join codes

GameSession URLs are `/g/<code>` where `<code>` is a 6-character Crockford Base32 code with a dash in the middle for readability (e.g. `/g/A4B-K9P`). The URL deliberately does *not* encode the game type — when Scribble grows from one game (the Telestrations clone) to a small suite, the code remains the primary identifier and the server discovers the game type by looking up the code. Game-type-specific non-session paths (e.g. "start a new X") will live under separate hierarchies where the type is genuinely primary, not under `/g/`.

Crockford Base32 is chosen for human readability — no I/L/O/U in the alphabet, case-insensitive input, dashes ignored on input — which makes codes easy to read aloud across a dinner table and type on a phone.

## Considered Options

- **`/g/<game-type>/<code>`** (rejected) — declares the multi-game-suite ambition in the routing layer, but conflates "what game is this" with "what session am I in," introduces a "what if URL type doesn't match the code's type" edge case, and pays a YAGNI cost while there's only one game type.
- **4-char codes** (rejected) — too small a code space if multiple GameSessions are concurrent.
- **8-char codes** (rejected as overkill) — 32⁸ ≈ 1T combos is far more than needed at friends-only scale. 6 chars (32⁶ ≈ 1B) is plenty.
- **UUIDs in URL** (rejected) — unfriendly to read aloud or type, defeats the purpose of a code players manually enter.
