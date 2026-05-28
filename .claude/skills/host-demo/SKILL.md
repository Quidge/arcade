---
name: host-demo
description: >-
  Host a live, human-in-the-loop playtest of the scribble party game. Start the
  LAN dev server, show a QR code for the human to join from their phone,
  simulate the other players via the chrome-devtools MCP server, and act out
  their personas through a full game including the reveal. Use when the user
  wants to "host a demo", run a scribble playtest, "get a feel for the app",
  simulate players joining a game, or play the app alongside simulated opponents.
---

# Host Demo

Run a real game of scribble where the user plays from their phone and every
other player is simulated through a browser controlled by the chrome-devtools
MCP server. The goal is to let the user *feel* the app as it currently is —
warts included — so stay faithful to the live build and surface anything odd.

## The cast

Six players. One is the human; five are simulated. Act each simulated player
out consistently — their persona should show in their captions, drawings, and
timing.

| Player | Who | Device | Persona / behaviour |
|---|---|---|---|
| `jd` | **the human user** | their phone | The real player. Do not simulate. |
| `thomas` | simulated, **host** | iPhone | Creates the game, sets the timer, starts it. Plays it straight. |
| `Phillip The Great` | simulated | iPad | Strong sense of humour — captions are witty and absurd. |
| `charles` | simulated | iPhone | Plain, reasonable captions and drawings. |
| `janice` | simulated | iPhone | Spotty connection — **drop her offline and reconnect her periodically** across the game (see step 7). |
| `david` | simulated | old iPhone SE | Laconic. Captions are single nouns ("Pineapple."). Small screen. |

The roster is the default. If the user names different players or personas,
follow theirs instead.

## Device profiles

Apply with the chrome-devtools `emulate` tool on each player's page, so the
demo exercises real viewport sizes.

| Device | `emulate` viewport | userAgent |
|---|---|---|
| iPhone | `390x844x3,mobile,touch` | `Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1` |
| iPad | `820x1180x2,touch` | `Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1` |
| old iPhone SE | `320x568x2,mobile,touch` | `Mozilla/5.0 (iPhone; CPU iPhone OS 15_8 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.6 Mobile/15E148 Safari/604.1` |

## Workflow

### 1. Preflight

- Confirm the working directory is the scribble repo (it has a `justfile` and
  `CLAUDE.md` describing scribble).
- Read `internal/games/scribble/CONTEXT.md` — the canonical glossary of how
  scribble works today (GameSession, Chain, Round, Reveal, Caption, Ghost,
  Draft). It is the single source of truth for game behaviour; this skill never
  restates it. (The root `CONTEXT.md` holds only Arcade-wide terms.)
- Skim open bugs so known issues can be told apart from fresh findings:
  `gh issue list --label bug`.
- Confirm `qrencode` is installed: `which qrencode`. If missing, install it
  (`brew install qrencode`) — it is small and quick.
- The chrome-devtools MCP tools are needed; they are loaded on demand.

### 2. Start the dev server on the LAN

```bash
just web        # run in the background — it stays up for the whole session
```

Find the port it bound (the scribble process, not other listeners):

```bash
lsof -nP -iTCP -sTCP:LISTEN | grep -i scribble
```

Find the machine's LAN IP (try `en0` then `en1`):

```bash
ipconfig getifaddr en0 || ipconfig getifaddr en1
```

The join base URL is `http://<LAN-IP>:<PORT>`. The chrome-devtools browser
runs on this machine, so simulated players can use `http://localhost:<PORT>`;
the human needs the LAN IP form.

### 3. Host the game, then build the QR code

Open the host's page first so a game code exists before generating the QR:

1. `new_page` → `http://localhost:<PORT>/`
2. `emulate` with the iPhone profile (thomas is on an iPhone).
3. Click **Host a new game**. The URL becomes `/g/XXX-XXX` — that is the code.
4. Enter display name `thomas`, Join. thomas is now in the lobby as host.

Generate and display the QR for the human's LAN URL:

```bash
qrencode -t UTF8 -m 2 "http://<LAN-IP>:<PORT>/g/XXX-XXX"
```

Print the QR in a fenced code block, and also give the URL and code as text.
Tell the user to join as `jd`.

### 4. Bring in the simulated players

For each of `Phillip The Great`, `charles`, `janice`, `david`:

1. `new_page` → `http://localhost:<PORT>/g/XXX-XXX` with a **distinct
   `isolatedContext`** name (e.g. `phillip`, `charles`, `janice`, `david`).
   Isolated contexts keep each player's session/cookies separate — without
   this they share one identity.
2. `emulate` with that player's device profile from the table above.
3. Fill the display name, click Join.

Set a comfortable round timer on the host page before starting — **90 s** so
the human is not rushed drawing on a phone.

### 5. Wait for the human — poll, don't ask

Do **not** ask the user "have you joined yet?". Instead poll: `take_snapshot`
of the host page every ~15–20 s and check the Players list for `jd`. Once `jd`
appears, confirm in chat and have thomas click **Start**.

Throughout the game, the same rule applies: to learn game state, read the
relevant page with `take_snapshot` or `take_screenshot` — never ask the human
to report what their screen says.

### 6. Play the rounds

`internal/games/scribble/CONTEXT.md` (read in step 1) defines the Round
structure, Ghost fills, and the Reveal driver model — work from it, not from
memory.

Each round, for every simulated player: select their page, read the prompt,
submit something **in character**, then move on. The human plays their own
turns; you only watch for them.

- **Captions** — type into the textbox, click Submit. Keep personas distinct:
  Phillip witty, david a single noun, charles plain, thomas straight, janice
  ordinary (when she is online).
- **Drawings** — the canvas only accepts pointer events. Use
  `scripts/draw_on_canvas.js`: edit its `STROKES` constant to suit the prompt,
  pass the whole function to `evaluate_script`, then click Submit. A rough
  recognisable scribble is plenty.
- Speed: rounds may advance faster than you can choreograph everyone. Missed
  turns become ghost fills — acceptable. The host can also **Force advance**.
- If the user says to speed up, submit simulated players quickly and lean on
  Force advance; still let the human take their turns.

### 7. Janice's flaky connection

Make janice live up to her persona. A few times during the game, on her page:

- **Drop offline**: `emulate` with `networkConditions: "Offline"`.
- **Reconnect**: `emulate` again with her viewport and no `networkConditions`.

Time at least one drop so she misses a round (observe the ghost fill). Drop and
reconnect her at unpredictable moments — that is her character, not a setup for
any later step.

### 8. Drive the reveal

Drive the reveal per the Reveal model in `internal/games/scribble/CONTEXT.md`: each chain is paced by
its starter, and the host paces a chain whose starter is absent. To advance a
chain, switch to the driving player's page and click `Next` there; other pages
show "Watching". Walk each chain from the correct page, screenshot the chains,
and narrate the funny results to the user.

The **human's chain is theirs to drive** — hand control back and let `jd` click
through it.

### 9. Wrap up

Tell the user the demo is done. Collect anything that looked broken, confusing,
or rough during play and offer to file issues — the repo's issue-tracker skill
and `docs/agents/` cover conventions. Faithful observations are the point of
the exercise; report them plainly.

## Bundled resources

- `scripts/draw_on_canvas.js` — pointer-event drawing helper for drawing rounds.

For game mechanics, read `internal/games/scribble/CONTEXT.md` (see step 1) —
this skill deliberately keeps no copy of it.
