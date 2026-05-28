//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/quidge/arcade/internal/games/scribble/gamesession"
)

// roundStateDrawingMsg mirrors the server → client envelope for a
// Round whose content kind is "drawing". The Draft and Prompt fields
// carry the wire-format strokes payload (a Drawing is an ordered
// list of Strokes, each Stroke a list of {x,y} points).
type roundStateDrawingMsg struct {
	Type        string `json:"type"`
	Round       int    `json:"round"`
	DeadlineMS  *int64 `json:"deadline_ms"`
	ContentKind string `json:"content_kind"`
	Prompt      *struct {
		Kind    string                 `json:"kind"`
		Text    string                 `json:"text,omitempty"`
		Strokes [][]map[string]float64 `json:"strokes,omitempty"`
	} `json:"prompt"`
	Draft struct {
		Kind    string                 `json:"kind"`
		Text    string                 `json:"text,omitempty"`
		Strokes [][]map[string]float64 `json:"strokes,omitempty"`
	} `json:"draft"`
	Submitted bool `json:"submitted"`
}

// drainRosterAndFinishRound0 walks two seats from join through the
// end of Round 0 by force-advance, with both seats submitting their
// caption first. The Round-0 round-ended frame is consumed off each
// seat's read buffer before returning, leaving the seat ready to
// read the immediately-following Round-1 round-state.
func drainRosterAndFinishRound0(t *testing.T, alice, bob *websocket.Conn, captionA, captionB string, timerSecs *int) {
	t.Helper()
	drainToRosterSize(t, alice, 1)
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	startRound(t, alice, timerSecs)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	if captionA != "" {
		sendCmd(t, alice, map[string]any{"type": "draft", "text": captionA})
		sendCmd(t, alice, map[string]any{"type": "submit"})
	}
	if captionB != "" {
		sendCmd(t, bob, map[string]any{"type": "draft", "text": captionB})
		sendCmd(t, bob, map[string]any{"type": "submit"})
	}
	if captionA == "" || captionB == "" {
		// One seat (or both) didn't submit — force-advance to end.
		sendCmd(t, alice, map[string]any{"type": "advance"})
	}
	_ = readUntilType(t, alice, "round-ended")
	_ = readUntilType(t, bob, "round-ended")
}

func TestRoundOneStartsImmediatelyAfterRoundZeroFinalizes(t *testing.T) {
	srv, reg := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()

	t60 := 60
	drainRosterAndFinishRound0(t, alice, bob, "alice's caption", "bob's caption", &t60)

	// Both seats should receive a Round-1 round-state unicast.
	rsA := readUntilType(t, alice, "round-state")
	rsB := readUntilType(t, bob, "round-state")

	var msgA roundStateDrawingMsg
	if err := json.Unmarshal(rsA, &msgA); err != nil {
		t.Fatalf("alice round-state: %v", err)
	}
	if msgA.Round != 1 {
		t.Errorf("alice round = %d want 1", msgA.Round)
	}
	if msgA.ContentKind != "drawing" {
		t.Errorf("alice content_kind = %q want drawing", msgA.ContentKind)
	}
	if msgA.Draft.Kind != "strokes" {
		t.Errorf("alice draft.kind = %q want strokes", msgA.Draft.Kind)
	}
	if len(msgA.Draft.Strokes) != 0 {
		t.Errorf("alice draft.strokes should start empty: %+v", msgA.Draft.Strokes)
	}
	if msgA.Prompt == nil || msgA.Prompt.Kind != "caption" {
		t.Fatalf("alice prompt missing or wrong kind: %+v", msgA.Prompt)
	}
	// Alice's chain idx for round 1 = (0+1)%2 = 1 → Bob's chain →
	// Bob's caption as prompt.
	if msgA.Prompt.Text != "bob's caption" {
		t.Errorf("alice prompt text = %q want bob's caption", msgA.Prompt.Text)
	}

	var msgB roundStateDrawingMsg
	if err := json.Unmarshal(rsB, &msgB); err != nil {
		t.Fatalf("bob round-state: %v", err)
	}
	if msgB.Prompt == nil || msgB.Prompt.Text != "alice's caption" {
		t.Errorf("bob prompt = %+v want alice's caption", msgB.Prompt)
	}

	// Verify session phase advanced.
	session, _ := reg.Lookup(code)
	st, roundNum := session.Phase()
	if st != gamesession.StateRoundActive {
		t.Errorf("phase = %v want StateRoundActive", st)
	}
	if roundNum != 1 {
		t.Errorf("round = %d want 1", roundNum)
	}
}

func TestRoundOneStrokesDraftStreamsAndAccumulates(t *testing.T) {
	srv, _, webSrv := newAppWithServer(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()

	t60 := 60
	drainRosterAndFinishRound0(t, alice, bob, "alice's caption", "bob's caption", &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	// Alice draws two strokes: first an L-shape, then a diagonal.
	strokes1 := []any{
		[]any{map[string]float64{"x": 0.1, "y": 0.1}, map[string]float64{"x": 0.1, "y": 0.5}},
	}
	sendCmd(t, alice, map[string]any{"type": "draft", "strokes": strokes1})
	strokes2 := []any{
		[]any{map[string]float64{"x": 0.1, "y": 0.1}, map[string]float64{"x": 0.1, "y": 0.5}},
		[]any{map[string]float64{"x": 0.2, "y": 0.2}, map[string]float64{"x": 0.8, "y": 0.8}},
	}
	sendCmd(t, alice, map[string]any{"type": "draft", "strokes": strokes2})
	// Bob also draws something so all-submitted ends the round
	// cleanly.
	strokes3 := []any{
		[]any{map[string]float64{"x": 0.5, "y": 0.5}, map[string]float64{"x": 0.6, "y": 0.6}},
	}
	sendCmd(t, bob, map[string]any{"type": "draft", "strokes": strokes3})

	sendCmd(t, alice, map[string]any{"type": "submit"})
	sendCmd(t, bob, map[string]any{"type": "submit"})

	// Round-1 round-ended carries drawing entries; chain.Store
	// holds both Drawings appended at index 1 of each Chain.
	end := readUntilType(t, alice, "round-ended")
	var re roundEndedMsg
	if err := json.Unmarshal(end, &re); err != nil {
		t.Fatalf("round-ended: %v", err)
	}
	if len(re.Entries) != 2 {
		t.Fatalf("entries len = %d want 2", len(re.Entries))
	}
	for _, e := range re.Entries {
		if e.Ghost {
			t.Errorf("entry marked Ghost despite submit: %+v", e)
		}
		if e.Kind != "drawing" {
			t.Errorf("entry kind = %q want drawing", e.Kind)
		}
	}
	time.Sleep(20 * time.Millisecond)

	cs := webSrv.ChainStoreForCode(code)
	// Alice's chain (index 0) holds: [alice's caption, bob's drawing]
	chain0 := cs.Entries(0)
	if len(chain0) != 2 {
		t.Fatalf("chain 0 len = %d want 2: %+v", len(chain0), chain0)
	}
	if chain0[1].Player != "Bob" || chain0[1].Ghost {
		t.Errorf("chain 0 round-1 entry = %+v want Bob non-ghost", chain0[1])
	}
	if len(chain0[1].Strokes) != 1 {
		t.Errorf("chain 0 round-1 strokes count = %d want 1: %+v", len(chain0[1].Strokes), chain0[1].Strokes)
	}
	// Bob's chain (index 1) holds: [bob's caption, alice's drawing]
	chain1 := cs.Entries(1)
	if len(chain1[1].Strokes) != 2 {
		t.Errorf("chain 1 round-1 strokes count = %d want 2 (alice's full-replace): %+v",
			len(chain1[1].Strokes), chain1[1].Strokes)
	}
}

func TestRoundOneSubmitSealsAndForceAdvanceAndAllSubmittedAndTimer(t *testing.T) {
	// All-submitted: both seats submit drawing → round 1 ends → reveal.
	t.Run("all-submitted", func(t *testing.T) {
		srv, reg := newApp(t)
		code := createSession(t, srv)
		alice, _ := dialAs(t, srv, code, "Alice")
		defer alice.CloseNow()
		bob, _ := dialAs(t, srv, code, "Bob")
		defer bob.CloseNow()
		t60 := 60
		drainRosterAndFinishRound0(t, alice, bob, "alice's caption", "bob's caption", &t60)
		_ = readUntilType(t, alice, "round-state")
		_ = readUntilType(t, bob, "round-state")
		stroke := []any{[]any{map[string]float64{"x": 0.5, "y": 0.5}, map[string]float64{"x": 0.6, "y": 0.6}}}
		sendCmd(t, alice, map[string]any{"type": "draft", "strokes": stroke})
		sendCmd(t, alice, map[string]any{"type": "submit"})
		sendCmd(t, bob, map[string]any{"type": "draft", "strokes": stroke})
		sendCmd(t, bob, map[string]any{"type": "submit"})
		_ = readUntilType(t, alice, "round-ended")
		time.Sleep(20 * time.Millisecond)
		session, _ := reg.Lookup(code)
		st, _ := session.Phase()
		if st != gamesession.StateReveal {
			t.Errorf("phase after all-submitted Round 1 = %v want StateReveal", st)
		}
	})

	// Force-advance: host ends Round 1 early.
	t.Run("force-advance", func(t *testing.T) {
		srv, reg := newApp(t)
		code := createSession(t, srv)
		alice, _ := dialAs(t, srv, code, "Alice")
		defer alice.CloseNow()
		bob, _ := dialAs(t, srv, code, "Bob")
		defer bob.CloseNow()
		t60 := 60
		drainRosterAndFinishRound0(t, alice, bob, "alice's caption", "bob's caption", &t60)
		_ = readUntilType(t, alice, "round-state")
		_ = readUntilType(t, bob, "round-state")
		sendCmd(t, alice, map[string]any{"type": "advance"})
		_ = readUntilType(t, alice, "round-ended")
		time.Sleep(20 * time.Millisecond)
		session, _ := reg.Lookup(code)
		st, _ := session.Phase()
		if st != gamesession.StateReveal {
			t.Errorf("phase after force-advance Round 1 = %v want StateReveal", st)
		}
	})

	// Timer expiry: short timer; nobody submits; round 1 ends.
	t.Run("timer-expiry", func(t *testing.T) {
		srv, reg := newApp(t)
		code := createSession(t, srv)
		alice, _ := dialAs(t, srv, code, "Alice")
		defer alice.CloseNow()
		bob, _ := dialAs(t, srv, code, "Bob")
		defer bob.CloseNow()
		t1 := 1
		drainRosterAndFinishRound0(t, alice, bob, "alice's caption", "bob's caption", &t1)
		_ = readUntilType(t, alice, "round-state")
		_ = readUntilType(t, bob, "round-state")
		_ = readUntilType(t, alice, "round-ended")
		time.Sleep(50 * time.Millisecond)
		session, _ := reg.Lookup(code)
		st, _ := session.Phase()
		if st != gamesession.StateReveal {
			t.Errorf("phase after timer-expiry Round 1 = %v want StateReveal", st)
		}
	})
}

func TestRoundOneGhostFillProducesDrawing(t *testing.T) {
	srv, _, webSrv := newAppWithServer(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()

	t60 := 60
	drainRosterAndFinishRound0(t, alice, bob, "alice's caption", "bob's caption", &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	// Neither draws. Alice force-advances; Ghosts fill both slots.
	sendCmd(t, alice, map[string]any{"type": "advance"})
	end := readUntilType(t, alice, "round-ended")
	var re roundEndedMsg
	if err := json.Unmarshal(end, &re); err != nil {
		t.Fatalf("round-ended: %v", err)
	}
	for _, e := range re.Entries {
		if !e.Ghost {
			t.Errorf("entry should be Ghost-filled: %+v", e)
		}
		if e.Kind != "drawing" {
			t.Errorf("Ghost entry kind = %q want drawing", e.Kind)
		}
	}
	time.Sleep(20 * time.Millisecond)
	cs := webSrv.ChainStoreForCode(code)
	chain0 := cs.Entries(0)
	if !chain0[1].Ghost {
		t.Errorf("chain 0 round-1 should be Ghost: %+v", chain0[1])
	}
	if len(chain0[1].Strokes) == 0 {
		t.Errorf("Ghost drawing has no strokes: %+v", chain0[1])
	}
}

func TestRoundOneReconnectMidRoundRestoresStrokes(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	bob, _ := dialAs(t, srv, code, "Bob")

	t60 := 60
	drainRosterAndFinishRound0(t, alice, bob, "alice's caption", "bob's caption", &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	stroke := []any{[]any{map[string]float64{"x": 0.3, "y": 0.3}, map[string]float64{"x": 0.7, "y": 0.7}}}
	sendCmd(t, bob, map[string]any{"type": "draft", "strokes": stroke})
	time.Sleep(50 * time.Millisecond)
	_ = bob.CloseNow()

	bob2, _ := dialAs(t, srv, code, "Bob")
	defer bob2.CloseNow()
	rsPayload := readUntilType(t, bob2, "round-state")
	var rs roundStateDrawingMsg
	if err := json.Unmarshal(rsPayload, &rs); err != nil {
		t.Fatalf("bob2 round-state: %v", err)
	}
	if rs.Round != 1 {
		t.Errorf("bob2 round = %d want 1", rs.Round)
	}
	if len(rs.Draft.Strokes) != 1 {
		t.Errorf("bob2 draft.strokes len = %d want 1: %+v", len(rs.Draft.Strokes), rs.Draft.Strokes)
	}
}

func TestRoundOneInProgressStrokesArePrivate(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()

	t60 := 60
	drainRosterAndFinishRound0(t, alice, bob, "alice's caption", "bob's caption", &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	// Bob draws; Alice should NOT receive any frame carrying Bob's
	// in-flight strokes.
	stroke := []any{[]any{map[string]float64{"x": 0.4, "y": 0.4}, map[string]float64{"x": 0.6, "y": 0.6}}}
	sendCmd(t, bob, map[string]any{"type": "draft", "strokes": stroke})

	// Drain Alice's read buffer for a short window — any frame she
	// receives that contains "strokes" or "x" coordinate data would
	// be a privacy leak.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_, p, err := alice.Read(ctx)
		cancel()
		if err != nil {
			break
		}
		var probe map[string]any
		if err := json.Unmarshal(p, &probe); err != nil {
			continue
		}
		if probe["type"] == "round-state" {
			// A round-state should never carry another seat's strokes.
			if draft, ok := probe["draft"].(map[string]any); ok {
				if strokesField, ok := draft["strokes"].([]any); ok {
					if len(strokesField) > 0 {
						t.Errorf("alice's round-state leaked strokes: %+v", probe)
					}
				}
			}
		}
	}
}
