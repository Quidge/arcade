# Testing strategy: deep-module units, whole-app integration, prose scenarios

Three tiers of automated and quasi-automated verification, each with a distinct purpose, location, and audience:

1. **Unit tests — co-located, exercise a single deep module.** Each `*.go` file in the project's internal packages has a sibling `*_test.go` testing its deep module in isolation, with collaborators stubbed or trivial. Tests are written alongside the module, not as a separate phase — they're a property of "the module exists and works," not a checklist item. The unit/integration line is drawn at *scope*, not at *I/O presence*: a test of one handler using `httptest.NewRecorder` is still a unit test if it's testing one module.

2. **Integration tests — `tests/integration/`, exercise the whole wired-up application.** Files under `tests/integration/` are gated by the build constraint `//go:build integration`, so `go test ./...` skips them by default and `go test -tags=integration ./...` runs both tiers. Integration tests spin up `httptest.NewServer(mux)` with the real router, real handlers, and real internal modules composed end-to-end. Real WebSocket clients connect through the real upgrade path. Anything touching SQLite on disk is always integration, regardless of scope.

3. **Scenarios — `tests/scenario/`, prose contracts describing end-to-end user flows.** Numbered Markdown files (`0001-...md`, `0002-...md`) modeled on the ADR numbering scheme. Each scenario has a `## Setup` section establishing prerequisite state and a `## Steps` section of numbered, actor-prefixed actions with inline `Expected:` annotations describing what a human reader (or browser-driving agent) should observe at each step. Scenarios extend rather than proliferate — one scenario covers multiple slices when those slices compose into a single user flow.

Scenarios are not executed by `go test`. Until a future scenario-testing skill exists (likely to use the chrome-devtools-mcp server under the hood), scenarios are pure prose contracts: the integration test suite covers the protocol layer of what they describe, and visual conformance is verified manually at PR review. When the skill ships, the same scenario files become directly executable by browser-driving agents, with no format migration required.

## Considered Options

- **Unit tests under `tests/unit/` instead of co-located** (rejected) — fights Go convention. Loses access to package-private identifiers; loses co-location ergonomics with the code under test; tooling (coverage, IDE integrations, `go test -run`) all assumes co-location. The visual-hierarchy gain is not worth the friction.
- **`//go:build integration` co-located instead of `tests/integration/` directory** (rejected at marginal cost) — also idiomatic, also widely used. Loses the top-level visual signal that "this directory contains a tier of tests with a distinct purpose." Cost of the directory is small; signal is real.
- **Playwright (or other E2E test runner) for browser-driven coverage** (rejected) — adds a Node toolchain to dev and CI for what is currently a small visual surface. The cost-benefit doesn't pay off until the UI grows past easy human eyeballing. When that day comes, a scenario-testing skill using browser MCP is the path forward, not a JS test framework.
- **Gherkin format for scenarios** (rejected) — the Given/When/Then rigidity adds ceremony without payoff at this scope. The format chosen (Setup + numbered actor-prefixed steps with `Expected:`) reads naturally for humans and translates straightforwardly to mechanical browser actions.
- **A Go runner that approximates the scenario via httptest + simulated clients** (rejected) — duplicates the integration test suite without adding signal. The two artifacts would drift; the scenario is the wrong place to put a protocol-level assertion.

## Consequences

- The `justfile` exposes `test-unit` (default, untagged `go test ./...`), `test-integration` (`go test -tags=integration ./tests/integration/...`), and `test-all` (both, in sequence). CI's `check` recipe runs `test-all`, so both tiers gate every PR.
- "Deep module" is the organizing principle for what gets a focused unit test. Working definition: a module whose interface is small relative to its implementation. Examples in the current design: Crockford Base32 codec, GameSession registry, Host promotion engine, Round controller, Draft store, Ghost provider. When in doubt, ask whether the test covers a single small API with rich behavior behind it — if yes, unit; if it composes multiple such APIs, integration.
- Visual conformance verification is manual at PR review until the scenario-testing skill exists. The scope this covers is acknowledged-small (a few screens and interactions through the early slices); the gap closes when the skill ships. New scenarios should not be added merely to expand visual coverage during this gap.
