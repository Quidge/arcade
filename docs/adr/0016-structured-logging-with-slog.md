# Structured logging with `slog`: an injected logger, severity-based levels, and a session correlator

> **Scope: Arcade-wide.** Establishes how every Game and the Arcade shell emit
> logs. Builds on [ADR 0015](0015-arcade-pivot.md) (no shared platform layer) —
> a logger is injected infrastructure, not a shared domain layer.

The Arcade adopts the standard library's `log/slog` for structured logging,
replacing ad-hoc `log.Printf`. The motivation is operational: diagnosing a
multi-player play-test today means watching several browser sessions at once,
with no server-side view of state transitions or connectivity (see issues #49,
#50). This ADR fixes *how* logging is wired and *what level means what*, so the
event set can be filled in without re-litigating structure.

**`slog`, not a third-party logger.** stdlib-first posture (see
`docs/agents/dependencies.md`): `slog` is structured, in the standard library,
and needs no new dependency. The handler is swappable later if a real need
appears.

**The logger is injected from `main.go`, never `slog.Default()`.** A logger is
infrastructure — the same shape as `gitSHA` and `basePath`, which `main.go`
already constructs and passes into each Game's `New()`. `main.go` is the one
place that knows the deployment context, so it owns handler, level, and
destination. This does **not** violate ADR 0015's "no shared platform layer":
that prohibition is about shared *domain* (Players, Hosts, sessions), and a
logger is not domain. Each Game receives a `*slog.Logger` and derives children
from it. We reject `slog.Default()` / package-level logging: it scatters
configuration and makes consistent format/level impossible to set from one
place.

**Three tiers of bound context.** Attributes are bound where they first become
knowable, via `logger.With(...)`, so every line downstream inherits them:

1. **Process (global).** The base logger carries `git_sha` and nothing else.
   `built_at` is redundant with the sha; `hostname`/`pid` are pointless on one
   binary / one VPS (ADR 0015); an `env` attribute is omitted until dev and prod
   logs actually share a destination (they don't — dev is the `just web`
   terminal, prod is the container stream).
2. **Per-Game.** `main.go` derives `base.With("game", "scribble")` and injects
   *that* into the Game. The value is a stable lowercase token, **not** the URL
   slug (`/scribble`) — the log key must not couple to the mount path. A second
   Game gets a consistent identity for free.
3. **Per-GameSession / per-connection.** The Game binds
   `.With("join_code", code)` on its per-GameSession runtime-state object and a
   further `.With("player", name)` per connection. `join_code` is the
   **correlator**: every line for one play instance carries it, so a single
   session's whole story greps out by code.

**Log-attribute keys are per-Game; no shared keys package.** `join_code` is the
one genuinely shared concept (the `joincode` package), so it is the soft
convention for the correlator key. But `player`, `round`, `chain` are Scribble's
alone — a future Game need not have them. A shared keys vocabulary would be the
platform layer ADR 0015 avoids, for no payoff at one Game. Extract one from
three real examples, not one (rule-of-three).

**Levels are assigned by operational severity — who, if anyone, must act — not
by gameplay drama.**

- **ERROR** — server faults / broken invariants that should never happen in a
  healthy process: the `marshal *` family, template render failures. A non-zero
  ERROR rate means there is a bug.
- **WARN** — a degraded or stuck state that is *not* by-design **and is
  detectable at a decision point the code already passes through**. No
  background watchdogs or timers may be introduced solely to populate this
  bucket. The one call site today is the #49 condition: during Reveal, the
  resolved driver (the Chain's starter if connected, else the Host per ADR 0004)
  is itself disconnected, so no actor can advance. Detectable with no new
  machinery at `broadcastRevealState` and on the Disconnect path during Reveal.
  Heartbeat/ping-timeout WARN is **deferred** — there is no heartbeat in the
  code; one is not built just to fill the bucket. If a heartbeat is added later
  for its own reasons, its timeout slots in here.
- **INFO** — the lifecycle spine, defined by a test: *an operator can
  reconstruct the full story of one GameSession from INFO alone* — who joined,
  who dropped, what phase it was in, why it advanced — without enabling DEBUG.
  One low-cardinality line per state transition. This **includes connection
  transitions** (Connect / Disconnect / Reconnect / Supersede): capped at 10
  Players, these are single-digit-per-session volume, not churn — and they are
  the exact signal the motivating bugs (#49, #50) needed. By-design events like
  Ghost fill, Force advance, and the Host driving for an absent starter are INFO,
  not WARN — they are normal, not faults.
- **DEBUG** — useful only when reproducing a bug: rejected client actions (a
  non-Host attempting Start, a non-driver attempting reveal-advance), per-message
  parse garbage, and per-connection *write* failures to a single socket (the
  client is already gone). `LOG_LEVEL=debug` turns these on for a play-test
  reproduction; default INFO keeps them out.
- **Never logged at any level — Draft / stroke streaming.** Per ADR 0010 a
  Drawing ships in full on every stroke update; per-keystroke and per-stroke
  traffic is high-frequency wire data, not lifecycle. Logging it would drown the
  stream and drag Entry content (and its PII surface) into the logs.

**This re-buckets the existing logging; it is not purely additive.** The ~35
current `log.Printf` calls are an undifferentiated stream, and several are
mislabeled — a non-Host's rejected Start is not a server fault, a `marshal`
failure is. Adopting this ADR means moving each existing call to its correct
level (rejected actions → DEBUG, `marshal*`/render → ERROR), alongside adding
the new INFO lifecycle spine.

**Display names are logged as `player`; there is no synthetic id, and no IPs.**
A `Player` is `{Name, Host, Connected}` — the display name *is* the only
identifier (there is no opaque id to log instead), so per-Player correlation is
impossible without it. Display names are self-chosen, ephemeral party nicknames
(the GameSession is in-memory and dies when the Host ends it), not real-world
PII; a synthetic id would be machinery with no payoff at one hobby Game. Client
IPs are deliberately not logged (more sensitive than nicknames, and nothing
needs them). **The migration hardens a real log-injection vector for free:** the
current `log.Printf("... from %s", name)` interpolates a raw, unescaped display
name into a text line, so a name containing a newline can forge fake log lines;
`slog`'s handlers escape attribute *values* (`player="Bob\nlevel=ERROR ..."`
stays one quoted field on one line), neutralizing it.

**Output: stderr, `TextHandler`, default INFO, one env knob.** The binary writes
one stream to stderr (Go's `log` default; the distroless container captures it);
**retention, rotation, and aggregation are the platform's job** — `docker logs`
/ journald in prod, terminal scrollback under `just web` — never in-app files.
The handler is `TextHandler` (logfmt) in *both* environments: the primary
debugging workflow is a human reading the `just web` terminal during a
play-test, and it is equally readable via `docker logs`. **`JSONHandler` is
deferred** until a log-aggregation consumer actually exists — switching is a
one-line handler swap with no call-site changes, so deferring costs nothing. The
level threshold defaults to INFO with a **`LOG_LEVEL`** env var for DEBUG opt-in.
`LOG_LEVEL` is **unprefixed**, following the convention in `main.go` (infra
knobs `ADDR`/`DATA_DIR` are unprefixed; `SCRIBBLE_*` is for Scribble product
knobs only) — logging is Arcade-wide infrastructure, so `SCRIBBLE_` would be
wrong.

## Considered Options

- **A third-party logger (zap, zerolog) (rejected)** — faster and feature-rich,
  but pulls a dependency against the stdlib-first posture for a hobby app whose
  log volume is trivial. `slog` covers the need; revisit only if a concrete
  performance or feature gap appears.

- **Package-level / `slog.Default()` logging (rejected)** — no constructor
  plumbing, but configuration scatters and `main.go` loses the ability to set
  format/level/destination from one place. Injection mirrors how `gitSHA` and
  `basePath` are already threaded.

- **A shared log-keys vocabulary package (rejected)** — consistent keys across
  Games for cross-Game grep-ability, but it is precisely the shared platform
  layer ADR 0015 avoids, for almost no payoff at one Game. Per-Game keys, with
  `join_code` as the one shared correlator convention. Rule-of-three.

- **A synthetic per-seat id for logs (rejected)** — would avoid logging display
  names, but display names are the only Player identifier and are low-sensitivity
  nicknames; a synthetic id is machinery without a driving requirement. Add one
  if a real PII/compliance need ever appears.

- **`JSONHandler` from day one (rejected)** — machine-ingestible, but there is no
  aggregation pipeline today, and JSON is harder to read in the terminal and in
  `docker logs`, which is where debugging actually happens now. Text now, JSON
  the day something consumes it (a one-line swap).

- **Purely additive logging — leave the existing 35 calls alone (rejected)** —
  smaller diff, but leaves `marshal` faults and rejected client actions sharing
  one undifferentiated stream, so `LOG_LEVEL` filtering would be meaningless for
  the existing lines. Re-bucketing is the point.

- **WARN populated by watchdogs / timeout sweeps (rejected)** — would catch more
  stuck states (e.g. a Reveal idle too long), but invents background machinery
  the app does not have and couples a logging decision to a scheduler. WARN is
  limited to states observable at decision points the code already reaches;
  active stuck-detection is a separate feature if it is ever wanted.
