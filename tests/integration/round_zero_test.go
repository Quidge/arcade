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

// roundStateMsg mirrors the server → client envelope for the
// active-Round state push. The envelope is the polymorphic shape
// introduced by issue #28: a content_kind discriminator, a nullable
// prompt, and a nested draft payload whose `kind` selects between
// {text} (Round 0) and {strokes} (future Round types).
type roundStateMsg struct {
	Type        string `json:"type"`
	Round       int    `json:"round"`
	DeadlineMS  *int64 `json:"deadline_ms"`
	ContentKind string `json:"content_kind"`
	Prompt      *struct {
		Kind string `json:"kind"`
		Text string `json:"text,omitempty"`
	} `json:"prompt"`
	Draft struct {
		Kind string `json:"kind"`
		Text string `json:"text,omitempty"`
	} `json:"draft"`
	Submitted bool `json:"submitted"`
}

// roundEndedEntry mirrors the on-wire entry in round-ended. Round
// 0 only ever emits Caption entries (Kind="caption").
type roundEndedEntry struct {
	Player     string `json:"player"`
	Kind       string `json:"kind"`
	Text       string `json:"text,omitempty"`
	Ghost      bool   `json:"ghost"`
	GhostLabel string `json:"ghost_label,omitempty"`
}

type roundEndedMsg struct {
	Type    string            `json:"type"`
	Entries []roundEndedEntry `json:"entries"`
}

// readUntilType reads frames on c, JSON-decoding each, returning
// the first whose "type" field matches want. The outer deadline
// bounds the total wait; individual Read calls use shorter
// per-call timeouts so a slow message doesn't block the loop
// from re-checking the outer deadline.
func readUntilType(t *testing.T, c *websocket.Conn, want string) []byte {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), deadline.Sub(time.Now()))
		_, p, err := c.Read(ctx)
		cancel()
		if err != nil {
			// Likely deadline-exceeded; loop will re-check the
			// outer deadline and either retry or bail.
			break
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(p, &probe); err != nil {
			continue
		}
		if probe.Type == want {
			return p
		}
	}
	t.Fatalf("never saw a %q frame", want)
	return nil
}

// drainOpenRoster reads frames until the connection's initial
// roster + any in-flight rosters settle to the expected size.
func drainToRosterSize(t *testing.T, c *websocket.Conn, size int) {
	t.Helper()
	_ = readUntil(t, c, func(m rosterMsg) bool { return len(m.Players) == size })
}

// startRound has Alice (Host) set a timer and start the Round.
func startRound(t *testing.T, c *websocket.Conn, seconds *int) {
	t.Helper()
	sendCmd(t, c, map[string]any{"type": "timer", "seconds": seconds})
	sendCmd(t, c, map[string]any{"type": "start"})
}

func TestRoundZeroAllSubmitted(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	drainToRosterSize(t, alice, 1)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	// Long timer so the round must end via all-submitted, not expiry.
	t60 := 60
	startRound(t, alice, &t60)

	// Both Players observe the round-state broadcast.
	aliceRS := readUntilType(t, alice, "round-state")
	bobRS := readUntilType(t, bob, "round-state")
	var rs roundStateMsg
	if err := json.Unmarshal(aliceRS, &rs); err != nil {
		t.Fatalf("alice round-state: %v", err)
	}
	if rs.DeadlineMS == nil || *rs.DeadlineMS == 0 {
		t.Errorf("alice round-state deadline_ms = %+v", rs.DeadlineMS)
	}
	if rs.Draft.Kind != "text" || rs.Draft.Text != "" || rs.Submitted {
		t.Errorf("alice round-state should be empty text + unsubmitted: %+v", rs)
	}
	if rs.ContentKind != "caption" {
		t.Errorf("alice round-state content_kind = %q want \"caption\"", rs.ContentKind)
	}
	if rs.Prompt != nil {
		t.Errorf("alice round-state prompt should be null for Round 0, got %+v", rs.Prompt)
	}
	if rs.Round != 0 {
		t.Errorf("alice round-state round = %d want 0", rs.Round)
	}
	_ = bobRS // shape already validated by Unmarshal in readUntilType path

	// Both type and submit.
	sendCmd(t, alice, map[string]any{"type": "draft", "text": "alice's caption"})
	sendCmd(t, bob, map[string]any{"type": "draft", "text": "bob's caption"})
	sendCmd(t, alice, map[string]any{"type": "submit"})
	sendCmd(t, bob, map[string]any{"type": "submit"})

	// Round-ended broadcast: every entry has its typed text, no Ghost.
	end := readUntilType(t, alice, "round-ended")
	var re roundEndedMsg
	if err := json.Unmarshal(end, &re); err != nil {
		t.Fatalf("round-ended: %v", err)
	}
	if len(re.Entries) != 2 {
		t.Fatalf("entries = %+v", re.Entries)
	}
	for _, e := range re.Entries {
		if e.Ghost {
			t.Errorf("entry marked Ghost despite explicit submit: %+v", e)
		}
	}
}

func TestRoundZeroTimerExpiryWithAFKGhost(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	drainToRosterSize(t, alice, 1)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	// 1-second timer — short enough to keep the test snappy, long
	// enough for Alice to type. Bob is the AFK player.
	t1 := 1
	startRound(t, alice, &t1)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	sendCmd(t, alice, map[string]any{"type": "draft", "text": "alice typed"})
	// Bob never types — he's the AFK player.

	end := readUntilType(t, alice, "round-ended")
	var re roundEndedMsg
	if err := json.Unmarshal(end, &re); err != nil {
		t.Fatalf("round-ended: %v", err)
	}
	if len(re.Entries) != 2 {
		t.Fatalf("entries = %+v", re.Entries)
	}
	for _, e := range re.Entries {
		switch e.Player {
		case "Alice":
			if e.Ghost {
				t.Errorf("Alice ghost-filled despite typing: %+v", e)
			}
			if e.Text != "alice typed" {
				t.Errorf("Alice entry text = %q", e.Text)
			}
		case "Bob":
			if !e.Ghost {
				t.Errorf("Bob AFK entry not Ghost: %+v", e)
			}
			if e.GhostLabel != "Bob's Ghost" {
				t.Errorf("Bob Ghost label = %q want %q", e.GhostLabel, "Bob's Ghost")
			}
			if e.Text == "" {
				t.Errorf("Bob Ghost text empty")
			}
		}
	}
}

func TestRoundZeroForceAdvance(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	drainToRosterSize(t, alice, 1)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	t60 := 60
	startRound(t, alice, &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	// Bob submits; Alice has not typed anything; Alice then force-advances.
	// The small sleep ensures Bob's draft + submit have been
	// applied server-side before Alice's advance arrives on a
	// different WebSocket goroutine — without it -race occasionally
	// catches advance landing first and finalizing Bob as Ghost.
	sendCmd(t, bob, map[string]any{"type": "draft", "text": "bob's caption"})
	sendCmd(t, bob, map[string]any{"type": "submit"})
	time.Sleep(100 * time.Millisecond)
	sendCmd(t, alice, map[string]any{"type": "advance"})

	end := readUntilType(t, alice, "round-ended")
	var re roundEndedMsg
	if err := json.Unmarshal(end, &re); err != nil {
		t.Fatalf("round-ended: %v", err)
	}
	for _, e := range re.Entries {
		switch e.Player {
		case "Alice":
			if !e.Ghost {
				t.Errorf("Alice non-Ghost despite no draft: %+v", e)
			}
		case "Bob":
			if e.Ghost {
				t.Errorf("Bob Ghost despite submitting: %+v", e)
			}
			if e.Text != "bob's caption" {
				t.Errorf("Bob text = %q", e.Text)
			}
		}
	}
}

func TestRoundZeroDraftSurvivesDisconnectReconnect(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	drainToRosterSize(t, alice, 1)
	bob, _ := dialAs(t, srv, code, "Bob")
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	t60 := 60
	startRound(t, alice, &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	sendCmd(t, bob, map[string]any{"type": "draft", "text": "halfway through a sent"})
	// Give the server a moment to apply.
	time.Sleep(50 * time.Millisecond)
	_ = bob.CloseNow()

	// Bob reconnects under the same name.
	bob2, _ := dialAs(t, srv, code, "Bob")
	defer bob2.CloseNow()
	// First the roster snapshot, then the unicast round-state.
	rsPayload := readUntilType(t, bob2, "round-state")
	var rs roundStateMsg
	if err := json.Unmarshal(rsPayload, &rs); err != nil {
		t.Fatalf("bob2 round-state: %v", err)
	}
	if rs.Draft.Text != "halfway through a sent" {
		t.Errorf("draft after reconnect = %q want %q", rs.Draft.Text, "halfway through a sent")
	}
	if rs.Submitted {
		t.Errorf("draft after reconnect: Submitted=true unexpected")
	}

	// Bob finishes typing and submits.
	sendCmd(t, bob2, map[string]any{"type": "draft", "text": "halfway through a sentence"})
	sendCmd(t, bob2, map[string]any{"type": "submit"})
	// Alice submits too to end the round.
	sendCmd(t, alice, map[string]any{"type": "draft", "text": "alice's caption"})
	sendCmd(t, alice, map[string]any{"type": "submit"})

	end := readUntilType(t, alice, "round-ended")
	var re roundEndedMsg
	if err := json.Unmarshal(end, &re); err != nil {
		t.Fatalf("round-ended: %v", err)
	}
	for _, e := range re.Entries {
		if e.Player == "Bob" && e.Text != "halfway through a sentence" {
			t.Errorf("Bob final entry = %q", e.Text)
		}
	}
}

func TestRoundZeroReconnectAfterRoundEnd(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	drainToRosterSize(t, alice, 1)
	bob, _ := dialAs(t, srv, code, "Bob")
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	// 1s timer; Bob disconnects, doesn't return until after.
	t1 := 1
	startRound(t, alice, &t1)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")
	_ = bob.CloseNow()
	// Alice waits for round-ended.
	_ = readUntilType(t, alice, "round-ended")

	// Bob reconnects after round-end.
	bob2, _ := dialAs(t, srv, code, "Bob")
	defer bob2.CloseNow()
	// He receives a round-ended (post-round placeholder), not a
	// round-state.
	payload := readUntilType(t, bob2, "round-ended")
	var re roundEndedMsg
	if err := json.Unmarshal(payload, &re); err != nil {
		t.Fatalf("bob2 round-ended: %v", err)
	}
	if len(re.Entries) == 0 {
		t.Errorf("post-round reconnect should carry entries: %+v", re)
	}
}

func TestRoundZeroAuthorization(t *testing.T) {
	srv, reg := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	drainToRosterSize(t, alice, 1)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	// Non-Host Bob tries to start.
	sendCmd(t, bob, map[string]any{"type": "start"})
	time.Sleep(50 * time.Millisecond)
	session, _ := reg.Lookup(code)
	st, _ := session.Phase()
	if st != gamesession.StateLobby {
		t.Errorf("phase after non-Host start = %v want StateLobby", st)
	}

	// Non-Host Bob tries to set timer.
	t30 := 30
	sendCmd(t, bob, map[string]any{"type": "timer", "seconds": &t30})
	time.Sleep(50 * time.Millisecond)

	// Host Alice starts properly.
	t60 := 60
	startRound(t, alice, &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	// Non-Host Bob tries to advance.
	sendCmd(t, bob, map[string]any{"type": "advance"})
	time.Sleep(100 * time.Millisecond)
	st, _ = session.Phase()
	if st != gamesession.StateRoundActive {
		t.Errorf("phase after non-Host advance = %v want StateRoundActive", st)
	}

	// Host Alice can force-advance.
	sendCmd(t, alice, map[string]any{"type": "advance"})
	_ = readUntilType(t, alice, "round-ended")
}

func TestRoundZeroLateOrInvalidCommands(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	drainToRosterSize(t, alice, 1)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	// In lobby: draft/submit/advance are no-ops, conn stays open.
	sendCmd(t, alice, map[string]any{"type": "draft", "text": "premature"})
	sendCmd(t, alice, map[string]any{"type": "submit"})
	sendCmd(t, alice, map[string]any{"type": "advance"})
	time.Sleep(50 * time.Millisecond)
	// Sanity: the conn is still usable.
	t60 := 60
	startRound(t, alice, &t60)
	_ = readUntilType(t, alice, "round-state")

	// Force-advance to end the round.
	sendCmd(t, alice, map[string]any{"type": "advance"})
	_ = readUntilType(t, alice, "round-ended")

	// Post-round: draft/submit are no-ops, conn stays open.
	sendCmd(t, alice, map[string]any{"type": "draft", "text": "post-round"})
	sendCmd(t, alice, map[string]any{"type": "submit"})
	time.Sleep(50 * time.Millisecond)
}

func TestRoundZeroSubmitThenDisconnect(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	drainToRosterSize(t, alice, 1)
	bob, _ := dialAs(t, srv, code, "Bob")
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	t60 := 60
	startRound(t, alice, &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	sendCmd(t, bob, map[string]any{"type": "draft", "text": "bob is locked in"})
	sendCmd(t, bob, map[string]any{"type": "submit"})
	time.Sleep(50 * time.Millisecond)
	_ = bob.CloseNow()

	// Alice force-advances.
	sendCmd(t, alice, map[string]any{"type": "advance"})
	end := readUntilType(t, alice, "round-ended")
	var re roundEndedMsg
	if err := json.Unmarshal(end, &re); err != nil {
		t.Fatalf("round-ended: %v", err)
	}
	for _, e := range re.Entries {
		if e.Player == "Bob" {
			if e.Ghost {
				t.Errorf("Bob ghost-filled despite prior submit: %+v", e)
			}
			if e.Text != "bob is locked in" {
				t.Errorf("Bob entry = %q", e.Text)
			}
		}
	}
}

func TestRoundZeroTimerOffNoDeadline(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	drainToRosterSize(t, alice, 1)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	// seconds:null → off.
	startRound(t, alice, nil)
	payload := readUntilType(t, alice, "round-state")
	var rs roundStateMsg
	if err := json.Unmarshal(payload, &rs); err != nil {
		t.Fatalf("round-state: %v", err)
	}
	if rs.DeadlineMS != nil {
		t.Errorf("timer-off round-state should have null deadline_ms, got %v", *rs.DeadlineMS)
	}

	// Round only ends on Force advance (no all-submitted possible
	// without a timer if some seats never submit).
	sendCmd(t, alice, map[string]any{"type": "advance"})
	_ = readUntilType(t, alice, "round-ended")
}
