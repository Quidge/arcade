//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/coder/websocket"

	"github.com/quidge/scribble/internal/games/scribble/gamesession"
	"github.com/quidge/scribble/internal/games/scribble/roundcomplete"
)

// TestFullGameAtMaxPlayers drives a 10-Player GameSession from Start
// through all 10 Rounds, into Reveal, walks each Chain, and Ends.
// Lives in the integration tier because the cheaper unit tier can't
// observe the full HTTP + WebSocket fan-out, but the e2e tier can't
// economically justify a 10-browser-context journey at this scope
// (ADR 0013 — cheapest tier wins).
func TestFullGameAtMaxPlayers(t *testing.T) {
	if gamesession.MaxPlayers != 10 {
		t.Fatalf("test premise broken: MaxPlayers = %d, want 10", gamesession.MaxPlayers)
	}

	srv, reg := newApp(t)
	code := createSession(t, srv)

	// Dial 10 seats in join order. The first is the Host.
	const N = 10
	conns := make([]*websocket.Conn, N)
	names := make([]string, N)
	for i := 0; i < N; i++ {
		names[i] = fmt.Sprintf("p%d", i)
		c, _ := dialAs(t, srv, code, names[i])
		if c == nil {
			t.Fatalf("seat %d failed to connect", i)
		}
		t.Cleanup(func() { _ = c.CloseNow() })
		conns[i] = c
	}
	// Drain initial roster broadcasts: each seat watches until it
	// sees N players in the roster.
	for _, c := range conns {
		drainToRosterSize(t, c, N)
	}

	// Host starts the round with a 60s timer (long enough that the
	// Round always ends via all-submitted, not expiry).
	t60 := 60
	startRound(t, conns[0], &t60)

	// Walk every Round. Even Rounds are captions; odd are drawings.
	// Each seat submits, then we drain the round-ended frame and
	// (for non-terminal Rounds) the next Round's round-state.
	sampleStroke := []any{
		[]any{
			map[string]float64{"x": 0.5, "y": 0.5},
			map[string]float64{"x": 0.6, "y": 0.6},
		},
	}
	for r := 0; r < N; r++ {
		// Drain the round-state for this Round on every seat.
		for _, c := range conns {
			_ = readUntilType(t, c, "round-state")
		}
		// Each seat submits a Round-r draft of the right kind.
		for i, c := range conns {
			if roundcomplete.ContentKindForRound(r) == "caption" {
				sendCmd(t, c, map[string]any{"type": "draft", "text": fmt.Sprintf("R%d from %s", r, names[i])})
			} else {
				sendCmd(t, c, map[string]any{"type": "draft", "strokes": sampleStroke})
			}
			sendCmd(t, c, map[string]any{"type": "submit"})
		}
		// Wait for the round-ended fan-out on every seat.
		for _, c := range conns {
			_ = readUntilType(t, c, "round-ended")
		}
	}

	// After Round N-1 ends, the room transitions to Reveal. Capture
	// the initial reveal-state from conns[0] (so we know who's
	// driving) and drain the same broadcast off the other conns.
	rawInitial := readUntilType(t, conns[0], "reveal-state")
	for _, c := range conns[1:] {
		_ = readUntilType(t, c, "reveal-state")
	}
	var current revealStateWire
	if err := json.Unmarshal(rawInitial, &current); err != nil {
		t.Fatalf("initial reveal-state unmarshal: %v", err)
	}

	// Confirm the session is actually in Reveal.
	session, _ := reg.Lookup(code)
	st, _ := session.Phase()
	if st != gamesession.StateReveal {
		t.Fatalf("phase after N=10 walk = %v want StateReveal", st)
	}

	// Walk every Chain to its full view, then transition to the
	// next, and finally to "complete" on the last Chain. Per Chain
	// k there are N entries (one per Round). Reaching full requires
	// (N-1) step→step advances plus one step→full. Then a
	// full→step(next) advance transitions to Chain k+1; on the
	// final Chain, that last advance lands in mode="complete".
	for chainIdx := 0; chainIdx < N; chainIdx++ {
		if current.ChainIndex != chainIdx {
			t.Fatalf("expected chain %d, reveal-state says %d", chainIdx, current.ChainIndex)
		}
		// Walk to full mode: (N-1) step-walks then step→full.
		for step := 0; step < N; step++ {
			current = revealAdvance(t, conns, names, current)
		}
		if current.Mode != "full" && current.Mode != "complete" {
			t.Fatalf("chain %d: expected mode full or complete after %d advances, got %+v", chainIdx, N, current)
		}
		if chainIdx < N-1 {
			// Transition to next Chain.
			current = revealAdvance(t, conns, names, current)
		}
	}
	// One last advance from full → complete on the final Chain.
	if current.Mode != "complete" {
		current = revealAdvance(t, conns, names, current)
	}
	if current.Mode != "complete" {
		t.Errorf("final reveal mode = %q want complete", current.Mode)
	}

	// Host ends the game. All seats receive game-ended and the
	// connection is closed.
	sendCmd(t, conns[0], map[string]any{"type": "end-game"})
	for _, c := range conns {
		waitForGameEnded(t, c)
	}
	st, _ = session.Phase()
	if st != gamesession.StateEnded {
		t.Errorf("phase after end-game = %v want StateEnded", st)
	}
}

// revealAdvance sends a reveal-advance from current.Driver and reads
// the resulting broadcast on every seat. Returns the new state. The
// caller is responsible for keeping `current` fresh: the initial
// state is the in-flight reveal-state captured at the round-→-reveal
// transition, and every subsequent advance returns the next.
func revealAdvance(t *testing.T, conns []*websocket.Conn, names []string, current revealStateWire) revealStateWire {
	t.Helper()
	if current.Driver == "" {
		t.Fatalf("revealAdvance called with empty Driver on state %+v", current)
	}
	var driverConn *websocket.Conn
	for i, n := range names {
		if n == current.Driver {
			driverConn = conns[i]
			break
		}
	}
	if driverConn == nil {
		t.Fatalf("no conn for driver %q (names=%v)", current.Driver, names)
	}
	sendCmd(t, driverConn, map[string]any{"type": "reveal-advance"})
	raw := readUntilType(t, conns[0], "reveal-state")
	for _, c := range conns[1:] {
		_ = readUntilType(t, c, "reveal-state")
	}
	var s revealStateWire
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("reveal-state unmarshal: %v", err)
	}
	return s
}
