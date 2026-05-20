# Round-0 waiting-room UI prototype — Variant A (Fireside semicircle)

**Question:** What should the round-0 waiting room look like for both Host and other Players? The previous lobby UI worked but felt unfinished, particularly on mobile.

**Verdict (initial):** Variant A — a radial "campfire" layout where players sit around a central join-code card — is the direction worth committing to. Variants B and C were explored as deliberate alternatives but aren't being carried forward.

**Shape:** Sub-shape A from the prototype skill. The variant is rendered on the existing `/g/{code}` route, gated by `?variant=A`. Real WebSocket, real roster data, real host actions.

## How to flip variants

Open `/g/<code>`, join with a name, then use the floating bar at the bottom-centre (arrow keys also work). The variant is in the URL, so each one is shareable. The switcher is dev-only — it is hidden when the page is served from a non-local hostname.

## Variant A — Fireside semicircle (campfire)

Players sit around a central card. The middle holds the join code and a copy-link button; for the Host, the timer selector and Start button sit in a row immediately below the stage. Empty seats are rendered as dotted slots so the room *feels* like it's filling up. Long names truncate with an ellipsis to keep each seat the same footprint.

The design handles: roster (with host badge + connection state), self vs others, host-only kick/transfer affordances, leave, copy link, transient notices.

## Mobile-fit iteration

The first cut didn't fit a mobile viewport cleanly. Two passes on 390×844 (iPhone 12-class) and 375×667 (iPhone SE) tightened the design:

- Centre hub shrunk from 56% → 38% of the stage so it stops bleeding into the side seats.
- Host controls (timer + Start) moved **out** of the hub into a row below the stage — there isn't room for them inside the hub on mobile.
- Seat radius pulled in from 44% → 36%; stage gets 1.75rem padding so cardinal-point seats don't clip.
- Inline `Make Host` / `Kick` buttons removed from each seat — at 4+ players they overlap adjacent seats. Replaced with **tap a seat → bottom-sheet action menu** (host-only, on others). Seats now stay the same size whether actions exist or not.
- Body gets a 5rem bottom padding while the dev switcher is visible so it doesn't cover the Leave button at the bottom of the page.

Verified on chrome-devtools at 390×844 and 375×667 with 0, 4, and 8 players, host view and guest view, real WebSocket data. No clipping, no overlap, no console errors. Screenshots embedded in the linked GitHub issue.

## Folding A into the real lobby

When A is promoted into the real lobby:

- Keep the variant-A markup, styles, and JS; delete the Variant B and C panels, their styles, and the renderers.
- Delete the floating variant switcher and the `?variant=` plumbing.
- Delete this NOTES.md.
- Rewrite the result properly — the variant code was written under prototype constraints (no tests, minimal error handling).
