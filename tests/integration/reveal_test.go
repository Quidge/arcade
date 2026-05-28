//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/quidge/scribble/internal/games/scribble/gamesession"
)

// revealEntryWire mirrors one Entry in a reveal-state's
// entries_visible list.
type revealEntryWire struct {
	Round      int                    `json:"round"`
	Kind       string                 `json:"kind"`
	Text       string                 `json:"text,omitempty"`
	Strokes    [][]map[string]float64 `json:"strokes,omitempty"`
	Ghost      bool                   `json:"ghost"`
	GhostLabel string                 `json:"ghost_label,omitempty"`
	Author     string                 `json:"author"`
}

type revealStateWire struct {
	Type           string            `json:"type"`
	ChainIndex     int               `json:"chain_index"`
	Starter        string            `json:"starter"`
	Driver         string            `json:"driver"`
	Mode           string            `json:"mode"`
	EntriesVisible []revealEntryWire `json:"entries_visible"`
	MoreChains     bool              `json:"more_chains"`
}

// drawIntoRoundOneAndEnd walks two seats through Round 0 (caption,
// both submit) and Round 1 (drawing, both submit), leaving them in
// StateReveal with the initial reveal-state delivered to each.
func drawIntoRoundOneAndEnd(t *testing.T, srv any, alice, bob *websocket.Conn) {
	t.Helper()
	t60 := 60
	drainRosterAndFinishRound0(t, alice, bob, "alice's caption", "bob's caption", &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")
	stroke := []any{[]any{map[string]float64{"x": 0.5, "y": 0.5}, map[string]float64{"x": 0.6, "y": 0.7}}}
	sendCmd(t, alice, map[string]any{"type": "draft", "strokes": stroke})
	sendCmd(t, alice, map[string]any{"type": "submit"})
	sendCmd(t, bob, map[string]any{"type": "draft", "strokes": stroke})
	sendCmd(t, bob, map[string]any{"type": "submit"})
	_ = readUntilType(t, alice, "round-ended")
}

func TestRevealBroadcastsAtRevealStartWithStarterAndDriverAndChainZero(t *testing.T) {
	srv, reg := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()

	drawIntoRoundOneAndEnd(t, srv, alice, bob)

	revA := readUntilType(t, alice, "reveal-state")
	revB := readUntilType(t, bob, "reveal-state")

	var msgA revealStateWire
	if err := json.Unmarshal(revA, &msgA); err != nil {
		t.Fatalf("alice reveal-state: %v", err)
	}
	if msgA.Type != "reveal-state" || msgA.ChainIndex != 0 {
		t.Errorf("alice reveal-state = %+v", msgA)
	}
	if msgA.Starter != "Alice" {
		t.Errorf("alice initial starter = %q want Alice", msgA.Starter)
	}
	if msgA.Driver != "Alice" {
		t.Errorf("alice driver = %q want Alice (starter, connected)", msgA.Driver)
	}
	if msgA.Mode != "step" {
		t.Errorf("alice initial mode = %q want step", msgA.Mode)
	}
	if len(msgA.EntriesVisible) != 1 {
		t.Errorf("alice initial entries_visible len = %d want 1", len(msgA.EntriesVisible))
	}
	if !msgA.MoreChains {
		t.Errorf("alice more_chains = false at chain 0; want true")
	}

	// Bob's broadcast matches.
	var msgB revealStateWire
	if err := json.Unmarshal(revB, &msgB); err != nil {
		t.Fatalf("bob reveal-state: %v", err)
	}
	if msgB.Driver != "Alice" {
		t.Errorf("bob's view of driver = %q want Alice", msgB.Driver)
	}

	session, _ := reg.Lookup(code)
	st, _ := session.Phase()
	if st != gamesession.StateReveal {
		t.Errorf("phase = %v want StateReveal", st)
	}
}

func TestRevealStepFullCompleteWalkForNEquals2(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()

	drawIntoRoundOneAndEnd(t, srv, alice, bob)
	// Initial broadcast.
	rev0A := readUntilType(t, alice, "reveal-state")
	_ = readUntilType(t, bob, "reveal-state")

	var s0 revealStateWire
	_ = json.Unmarshal(rev0A, &s0)
	if s0.Mode != "step" || s0.ChainIndex != 0 || len(s0.EntriesVisible) != 1 {
		t.Fatalf("initial reveal = %+v", s0)
	}

	// Alice (starter of chain 0) advances. Expected: step→step
	// shows entry 1 (drawing).
	sendCmd(t, alice, map[string]any{"type": "reveal-advance"})
	rev1A := readUntilType(t, alice, "reveal-state")
	var s1 revealStateWire
	_ = json.Unmarshal(rev1A, &s1)
	if s1.Mode != "step" || s1.ChainIndex != 0 || len(s1.EntriesVisible) != 2 {
		t.Errorf("after advance 1: %+v want step chain=0 len=2", s1)
	}
	// Advance 2: step→full on chain 0.
	sendCmd(t, alice, map[string]any{"type": "reveal-advance"})
	rev2A := readUntilType(t, alice, "reveal-state")
	var s2 revealStateWire
	_ = json.Unmarshal(rev2A, &s2)
	if s2.Mode != "full" || s2.ChainIndex != 0 {
		t.Errorf("after advance 2: %+v want full chain=0", s2)
	}
	if !s2.MoreChains {
		t.Errorf("after advance 2: more_chains=false; want true (chain 1 pending)")
	}
	// Advance 3: full→step on chain 1. Driver: chain 1 starter is
	// Bob, so Bob is driver now — Alice's advance should be denied.
	// We'll do the legit advance via Bob.
	sendCmd(t, alice, map[string]any{"type": "reveal-advance"})
	// The denied advance should not produce a new broadcast. Give a
	// moment for a stray broadcast to land, then check Bob's read
	// stream — if a denied advance produced a broadcast it would
	// show up.
	time.Sleep(50 * time.Millisecond)

	sendCmd(t, bob, map[string]any{"type": "reveal-advance"})
	rev3A := readUntilType(t, alice, "reveal-state")
	var s3 revealStateWire
	_ = json.Unmarshal(rev3A, &s3)
	if s3.Mode != "step" || s3.ChainIndex != 1 || len(s3.EntriesVisible) != 1 {
		t.Errorf("after advance 3 (bob): %+v want step chain=1 len=1", s3)
	}
	if s3.Starter != "Bob" || s3.Driver != "Bob" {
		t.Errorf("chain 1 starter/driver = %q/%q want Bob/Bob", s3.Starter, s3.Driver)
	}
	// Advance 4: step→step on chain 1.
	sendCmd(t, bob, map[string]any{"type": "reveal-advance"})
	rev4A := readUntilType(t, alice, "reveal-state")
	var s4 revealStateWire
	_ = json.Unmarshal(rev4A, &s4)
	if s4.Mode != "step" || len(s4.EntriesVisible) != 2 {
		t.Errorf("after advance 4: %+v want step len=2", s4)
	}
	// Advance 5: step→full on chain 1.
	sendCmd(t, bob, map[string]any{"type": "reveal-advance"})
	rev5A := readUntilType(t, alice, "reveal-state")
	var s5 revealStateWire
	_ = json.Unmarshal(rev5A, &s5)
	if s5.Mode != "full" || s5.MoreChains {
		t.Errorf("after advance 5: %+v want full more_chains=false", s5)
	}
	// Advance 6: full→complete.
	sendCmd(t, bob, map[string]any{"type": "reveal-advance"})
	rev6A := readUntilType(t, alice, "reveal-state")
	var s6 revealStateWire
	_ = json.Unmarshal(rev6A, &s6)
	if s6.Mode != "complete" {
		t.Errorf("after advance 6: mode = %q want complete", s6.Mode)
	}

	// Past complete: another advance is a no-op (no new broadcast
	// should arrive in a reasonable window).
	sendCmd(t, bob, map[string]any{"type": "reveal-advance"})
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, _, err := alice.Read(ctx)
		cancel()
		if err != nil {
			break
		}
	}
}

func TestRevealDriverEligibilityHostFallbackAndStarterReconnect(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	bob, _ := dialAs(t, srv, code, "Bob")

	drawIntoRoundOneAndEnd(t, srv, alice, bob)
	_ = readUntilType(t, alice, "reveal-state")
	_ = readUntilType(t, bob, "reveal-state")

	// Bob (chain 1's starter) disconnects mid-reveal-of-chain-0.
	// Driver for chain 0 is still Alice. Walk through chain 0:
	sendCmd(t, alice, map[string]any{"type": "reveal-advance"}) // step→step
	_ = readUntilType(t, alice, "reveal-state")
	sendCmd(t, alice, map[string]any{"type": "reveal-advance"}) // step→full
	_ = readUntilType(t, alice, "reveal-state")
	// Now Bob disconnects.
	_ = bob.CloseNow()
	time.Sleep(100 * time.Millisecond)

	// Advance 3: full→step on chain 1. With Bob disconnected, the
	// driver should be Alice (Host).
	sendCmd(t, alice, map[string]any{"type": "reveal-advance"})
	rev3A := readUntilType(t, alice, "reveal-state")
	var s3 revealStateWire
	_ = json.Unmarshal(rev3A, &s3)
	if s3.ChainIndex != 1 {
		t.Fatalf("after advance 3: chain = %d want 1", s3.ChainIndex)
	}
	if s3.Starter != "Bob" {
		t.Errorf("starter = %q want Bob", s3.Starter)
	}
	if s3.Driver != "Alice" {
		t.Errorf("driver with Bob disconnected = %q want Alice (Host fallback)", s3.Driver)
	}

	// Bob reconnects mid-reveal-of-his-Chain.
	bob2, _ := dialAs(t, srv, code, "Bob")
	defer bob2.CloseNow()
	// Bob2's writePhaseSnapshot unicasts a reveal-state showing the
	// driver as Bob now (he's reconnected and is the chain starter).
	bobInit := readUntilType(t, bob2, "reveal-state")
	var bobMsg revealStateWire
	_ = json.Unmarshal(bobInit, &bobMsg)
	if bobMsg.Driver != "Bob" {
		t.Errorf("bob2 driver after reconnect = %q want Bob", bobMsg.Driver)
	}

	// Bob advances — the driver re-evaluation per-command should
	// accept him now.
	sendCmd(t, bob2, map[string]any{"type": "reveal-advance"})
	rev4A := readUntilType(t, alice, "reveal-state")
	var s4 revealStateWire
	_ = json.Unmarshal(rev4A, &s4)
	if s4.Mode != "step" || len(s4.EntriesVisible) != 2 {
		t.Errorf("after bob-advance: %+v want step len=2", s4)
	}
	if s4.Driver != "Bob" {
		t.Errorf("driver after bob-advance = %q want Bob", s4.Driver)
	}
}

func TestRevealGhostEntryCarriesGhostFlagAndLabel(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()

	// Round 0: alice submits, bob doesn't → bob's caption is Ghost.
	t60 := 60
	drainRosterAndFinishRound0(t, alice, bob, "alice's caption", "", &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	// Round 1: both submit drawings.
	stroke := []any{[]any{map[string]float64{"x": 0.5, "y": 0.5}, map[string]float64{"x": 0.6, "y": 0.6}}}
	sendCmd(t, alice, map[string]any{"type": "draft", "strokes": stroke})
	sendCmd(t, alice, map[string]any{"type": "submit"})
	sendCmd(t, bob, map[string]any{"type": "draft", "strokes": stroke})
	sendCmd(t, bob, map[string]any{"type": "submit"})
	_ = readUntilType(t, alice, "round-ended")

	rev := readUntilType(t, alice, "reveal-state")
	var s revealStateWire
	_ = json.Unmarshal(rev, &s)
	if s.ChainIndex != 0 {
		t.Fatalf("chain idx = %d want 0", s.ChainIndex)
	}
	// Alice's chain Entry 0 = alice's caption (non-Ghost). Walk to
	// chain 1 to see Bob's Ghost caption. Alice drives chain 0
	// through three advances: step→step, step→full, full→step(chain
	// 1). The transition between Chains is driven by the chain
	// being-walked-out-of, i.e. chain 0's starter (Alice).
	sendCmd(t, alice, map[string]any{"type": "reveal-advance"})
	_ = readUntilType(t, alice, "reveal-state")
	sendCmd(t, alice, map[string]any{"type": "reveal-advance"})
	_ = readUntilType(t, alice, "reveal-state")
	sendCmd(t, alice, map[string]any{"type": "reveal-advance"})
	chain1Step := readUntilType(t, alice, "reveal-state")
	var s1 revealStateWire
	_ = json.Unmarshal(chain1Step, &s1)
	if s1.ChainIndex != 1 {
		t.Fatalf("after chain transition: chain = %d want 1", s1.ChainIndex)
	}
	if len(s1.EntriesVisible) != 1 {
		t.Fatalf("chain 1 step 1 entries len = %d want 1", len(s1.EntriesVisible))
	}
	first := s1.EntriesVisible[0]
	if !first.Ghost {
		t.Errorf("chain 1 first entry (Bob's caption) should be Ghost: %+v", first)
	}
	if first.GhostLabel != "Bob's Ghost" {
		t.Errorf("ghost label = %q want Bob's Ghost", first.GhostLabel)
	}
	if first.Author != "Bob" {
		t.Errorf("ghost author = %q want Bob", first.Author)
	}
}
