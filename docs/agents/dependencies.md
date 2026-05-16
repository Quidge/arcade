# Dependency policy

Scribble's Go server takes a stdlib-first posture: prefer the standard library when it covers a need adequately; reach for a third-party library only when the standard library genuinely doesn't reach.

This is a **posture, not a hard rule**. Real needs do exist where stdlib falls short — Go's standard library has no WebSocket implementation, no SQLite driver, no Litestream client. When such a need arises, a dependency is the right answer.

## Rule for agents

**Before introducing a new Go dependency (anything added to `go.mod` that isn't already there), open the question with the maintainer.** Do not silently `go get` a library because it would be convenient. Surface the choice — what need it serves, what alternatives exist, why this one — and wait for an explicit decision.

This applies to:

- Direct dependencies added to `go.mod`
- Replacing an existing dependency with a fork or alternative
- Any non-stdlib package referenced in implementation code

This does not apply to:

- Test helper packages that don't get linked into the production binary (still mention them, but the bar is lower)
- Tooling installed via the `justfile` or CI scripts that lives outside `go.mod` (e.g., `golangci-lint`)
- Transitive dependencies pulled in as a consequence of an already-approved direct dependency

## Where to capture decisions

When a new dependency is approved, the maintainer's reasoning lands in an ADR if the choice is hard-to-reverse, surprising-without-context, and a real trade-off. Otherwise, a short note in the PR description is sufficient.

## Why this matters

Dependencies are easy to add and hard to remove. They become part of the project's vocabulary, its API surface (when their types leak), its security boundary, and its onboarding cost. The friction of asking first is small; the friction of unwinding a dependency the team didn't think hard about is large.
