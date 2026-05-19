# Disconnect handling via Ghost and server-held Drafts

When a Player drops mid-Round, their work is preserved and their absence is filled — but not via an inert placeholder. Two coupled commitments make this work:

1. **Drafts are server-held.** Every keystroke (for Captions) and every stroke (for Drawings) streams from the client to the server over WebSocket as it happens. The server holds the in-progress state per Player per Round. Partial-input capture is therefore a side-effect of the architecture — at Round-end, whatever the server last saw is shipped as the Entry.
2. **Empty slots are filled by the Player's Ghost,** a bot stand-in that contributes canned content (MVP) from a small in-repo library, attributed visibly in the UI as "[Player]'s Ghost." "Empty" means the server has zero recorded input for the Player when the Round ends — any input at all (even a single character or stroke) ships as-is.

Rejoin is by typing the same join code with the same display name; display names are therefore unique within a GameSession. No auth in MVP — spoofing risk is acceptable in a co-located, trusted-friends context.

## Considered Options

- **Client-held drafts with submit-only upload** (rejected) — simpler in the small but doesn't fit the WebSocket-first architecture, loses work on disconnect, and forces partial-input to be a separate complex feature instead of falling out of the protocol naturally.
- **Inert placeholder content like `(no caption)`** (rejected in favor of Ghost) — flat, ungenerous, kills comedy. Ghost-produced content preserves Chain integrity and turns absences into part of the joke.
- **No rejoin at all** (rejected) — too socially harsh for a co-located dinner-table use case where the dropped Player is standing right there.
- **Account-based identity for rejoin** (deferred) — would harden the rejoin mechanism against spoofing, but auth is out of scope for MVP.
- **Client-bearer token issued on Join, persisted in localStorage and presented on Reconnect** (deferred) — a lighter-weight hardening than account-based auth. The server issues an opaque token on first Join; the client stores it; subsequent WebSocket upgrades to the same seat present it as evidence of "yes, I'm the original holder." Closes the impersonation hole without account-creation friction. Worth revisiting if spoofing in the trusted-friends context produces real friction.

## Consequences

- The WebSocket protocol must carry draft-update messages (keystrokes for captions, strokes for drawings) at runtime velocity, not just notification-level events. This is the right shape for the realtime layer but adds wire chatter.
- Ghost content generation is a v1 concern — at MVP it's canned (an in-repo library); a later iteration may swap in LLM-generated content. The interface (`server picks an Entry on behalf of an absent Player`) is the same either way.
- A Player who AFKs Round 0 (their own starter Caption) is an edge case — their Ghost would start their Chain. Behavior deferred until it actually shows up in testing.
