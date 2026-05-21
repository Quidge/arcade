# Testing strategy: deep-module units, whole-app integration, prose scenarios

Three tiers of automated and quasi-automated verification, each with a distinct purpose, location, and audience:

1. **Unit tests — co-located, exercise a single deep module.** Each `*.go` file in the project's internal packages has a sibling `*_test.go` testing its deep module in isolation, with collaborators stubbed or trivial. Tests are written alongside the module, not as a separate phase — they're a property of "the module exists and works," not a checklist item. The unit/integration line is drawn at *scope*, not at *I/O presence*: a test of one handler using `httptest.NewRecorder` is still a unit test if it's testing one module.

2. **Integration tests — `tests/integration/`, exercise the whole wired-up application.** Files under `tests/integration/` are gated by the build constraint `//go:build integration`, so `go test ./...` skips them by default and `go test -tags=integration ./...` runs both tiers. Integration tests spin up `httptest.NewServer(mux)` with the real router, real handlers, and real internal modules composed end-to-end. Real WebSocket clients connect through the real upgrade path. Anything touching SQLite on disk is always integration, regardless of scope.

3. **End-to-end UI tests — `tests/e2e/`, Playwright specs exercising the rendered DOM through real browsers.** TypeScript `.spec.ts` files driving Chromium, Firefox, and WebKit via `@playwright/test`. Each spec spins up the real binary via Playwright's `webServer` config, opens one or more browser contexts (mapping to distinct Players), and asserts on visible DOM and WebSocket-driven state transitions. This tier replaces the earlier `tests/scenario/` prose contracts — see ADR 0012, which supersedes this ADR's prior rejection of Playwright.

E2E tests are not run by `go test`. They live behind their own `pnpm test:e2e` invocation (or `just test-e2e`). The integration tier still covers the protocol layer; the e2e tier covers what only a real browser can — visible DOM, WebSocket-driven roster updates across tabs, drawing canvas interactions, and the like.

## Considered Options

- **Unit tests under `tests/unit/` instead of co-located** (rejected) — fights Go convention. Loses access to package-private identifiers; loses co-location ergonomics with the code under test; tooling (coverage, IDE integrations, `go test -run`) all assumes co-location. The visual-hierarchy gain is not worth the friction.
- **`//go:build integration` co-located instead of `tests/integration/` directory** (rejected at marginal cost) — also idiomatic, also widely used. Loses the top-level visual signal that "this directory contains a tier of tests with a distinct purpose." Cost of the directory is small; signal is real.
- **Playwright (or other E2E test runner) for browser-driven coverage** (rejected at the time of writing; later adopted via ADR 0012) — adds a Node toolchain to dev and CI for what was then a small visual surface. The cost-benefit didn't pay off until the UI grew past easy human eyeballing. That day came; see ADR 0012 for the reversal and its reasoning. (The original speculation here — that an MCP-driven browser skill would be the right successor instead of a JS test framework — turned out to be incomplete: MCP-driven exploration is useful but not deterministic enough to gate CI, which is what Playwright supplies.)
- **Gherkin format for scenarios** (rejected) — the Given/When/Then rigidity adds ceremony without payoff at this scope. The format chosen (Setup + numbered actor-prefixed steps with `Expected:`) reads naturally for humans and translates straightforwardly to mechanical browser actions.
- **A Go runner that approximates the scenario via httptest + simulated clients** (rejected) — duplicates the integration test suite without adding signal. The two artifacts would drift; the scenario is the wrong place to put a protocol-level assertion.

## Consequences

- The `justfile` exposes `test-unit` (default, untagged `go test ./...`), `test-integration` (`go test -tags=integration ./tests/integration/...`), `test-e2e` (`pnpm test:e2e`, runs Playwright), and `test-all` (the Go tiers in sequence). E2E is not folded into `test-all` because it is slower and depends on the Node toolchain; it is invoked separately as a local pre-merge step.
- "Deep module" is the organizing principle for what gets a focused unit test. Working definition: a module whose interface is small relative to its implementation. Examples in the current design: Crockford Base32 codec, GameSession registry, Host promotion engine, Round controller, Draft store, Ghost provider. When in doubt, ask whether the test covers a single small API with rich behavior behind it — if yes, unit; if it composes multiple such APIs, integration.
- Visual conformance verification is supplied by the Playwright tier under `tests/e2e/`. Per ADR 0012, this replaces the earlier manual-PR-review-with-prose-contracts stopgap. New visual surface should land with at least one Playwright spec covering its golden path.

## Amended by ADR 0013

The minimum-bar phrasing above ("at least one Playwright spec covering its golden path") sets a floor for e2e coverage but is silent on the ceiling. ADR 0013 adds the admission discipline that governs the ceiling: a test belongs in the e2e tier only if no lower tier can give equal confidence, and each e2e spec maps to one user journey. The three-tier declaration in this ADR is unchanged; ADR 0013 is additive scope discipline for tier 3 specifically.
