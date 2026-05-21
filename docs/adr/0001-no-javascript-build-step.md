# No JavaScript build step

Scribble's frontend is server-rendered HTML (Go `html/template`) augmented by hand-rolled vanilla JS where needed (canvas drawing, WebSocket client). No bundler, no transpiler, no `package.json`. Trades modern frontend ergonomics (TypeScript, JSX, components, hot-reload) for operational simplicity — one Go binary, no Node toolchain in CI or dev loop — and matches the project's stdlib-only Go posture.

## Consequences

- The drawing canvas and the realtime client must be written by hand. This is bounded — those are the only two pieces of nontrivial frontend code — but it commits us to vanilla DOM APIs and `WebSocket`/`fetch` directly rather than any abstraction over them.
- Adding a build step later is a meaningful threshold to cross, not a free pivot. Revisit only if the frontend grows substantially or if a specific frontend feature genuinely requires a transpiled language.

## Scope: production only

This ADR's "no `package.json`, no Node toolchain" rule applies to the **production path** — the shipped binary, the production container image, and what runs in front of users. Dev and test tooling are a separate concern. ADR 0012 later introduced a Node toolchain (Playwright, pnpm) under `tests/e2e/` for end-to-end UI tests; the repo now contains a root-level `package.json`, `pnpm-lock.yaml`, `.nvmrc`, and `playwright.config.ts`, and `node_modules/` is gitignored. None of that enters the production binary or container image — the shipped artifact is still a static Go binary with server-rendered HTML and hand-rolled vanilla JS. See ADR 0012 for the reasoning behind the dev/test exception.
