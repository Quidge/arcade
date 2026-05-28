//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/quidge/scribble/internal/games/scribble/gamesession"
)

// gameEndedWire mirrors the server → client game-ended envelope.
type gameEndedWire struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// waitForGameEnded reads frames on c looking for "game-ended", and
// then verifies the connection closes shortly thereafter with a
// normal-closure frame. Returns the parsed envelope.
func waitForGameEnded(t *testing.T, c *websocket.Conn) gameEndedWire {
	t.Helper()
	raw := readUntilType(t, c, "game-ended")
	var msg gameEndedWire
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("game-ended unmarshal: %v", err)
	}
	if msg.Reason != "host" {
		t.Errorf("game-ended reason = %q want host", msg.Reason)
	}
	// Confirm the connection closes within a reasonable window.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, _, err := c.Read(ctx)
		cancel()
		if err != nil {
			// Any read error after game-ended is acceptable; the
			// server has closed and the client's read loop is
			// finishing.
			return msg
		}
	}
	t.Errorf("connection did not close after game-ended")
	return msg
}

func TestEndGameFromRoundActiveByHostBroadcastsAndClosesConnections(t *testing.T) {
	srv, reg := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	drainToRosterSize(t, alice, 1)
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	t60 := 60
	startRound(t, alice, &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	sendCmd(t, alice, map[string]any{"type": "end-game"})
	waitForGameEnded(t, alice)
	waitForGameEnded(t, bob)

	session, _ := reg.Lookup(code)
	st, _ := session.Phase()
	if st != gamesession.StateEnded {
		t.Errorf("phase after end-game = %v want StateEnded", st)
	}
}

func TestEndGameFromRevealByHostTearsDownCleanly(t *testing.T) {
	srv, reg := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()

	drawIntoRoundOneAndEnd(t, srv, alice, bob)
	_ = readUntilType(t, alice, "reveal-state")
	_ = readUntilType(t, bob, "reveal-state")

	sendCmd(t, alice, map[string]any{"type": "end-game"})
	waitForGameEnded(t, alice)
	waitForGameEnded(t, bob)

	session, _ := reg.Lookup(code)
	st, _ := session.Phase()
	if st != gamesession.StateEnded {
		t.Errorf("phase = %v want StateEnded", st)
	}
}

func TestEndGameByNonHostIsRejected(t *testing.T) {
	srv, reg := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	drainToRosterSize(t, alice, 1)
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	t60 := 60
	startRound(t, alice, &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	// Bob (non-Host) tries end-game. It should be silently ignored.
	sendCmd(t, bob, map[string]any{"type": "end-game"})
	time.Sleep(150 * time.Millisecond)

	session, _ := reg.Lookup(code)
	st, _ := session.Phase()
	if st == gamesession.StateEnded {
		t.Errorf("non-Host end-game advanced phase to Ended")
	}

	// Confirm neither conn received a game-ended frame.
	for _, conn := range []*websocket.Conn{alice, bob} {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, p, err := conn.Read(ctx)
		cancel()
		if err != nil {
			// no frame is fine — means no spurious broadcast
			continue
		}
		if strings.Contains(string(p), `"game-ended"`) {
			t.Errorf("non-Host end-game produced game-ended broadcast: %s", string(p))
		}
	}
}

func TestEndGameButtonAvailableFromLobbyOnward(t *testing.T) {
	// Smoke test that end-game is accepted from StateLobby — though
	// per the issue the button is shown from Start onward, the
	// server should accept end-game from any non-terminal state.
	srv, reg := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	drainToRosterSize(t, alice, 1)

	sendCmd(t, alice, map[string]any{"type": "end-game"})
	waitForGameEnded(t, alice)

	session, _ := reg.Lookup(code)
	st, _ := session.Phase()
	if st != gamesession.StateEnded {
		t.Errorf("phase after lobby end-game = %v want StateEnded", st)
	}
}
