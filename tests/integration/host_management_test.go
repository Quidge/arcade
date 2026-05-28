//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/quidge/arcade/internal/arcade"
	"github.com/quidge/arcade/internal/games/scribble/gamesession"
	"github.com/quidge/arcade/internal/games/scribble/web"
)

// newAppWithGrace is like newApp but lets the test inject a short
// Host-auto-migrate grace duration so disconnect-grace cases run
// in milliseconds rather than the production 15-second default.
func newAppWithGrace(t *testing.T, grace time.Duration) (*httptest.Server, *gamesession.Registry) {
	t.Helper()
	reg := gamesession.NewRegistry(gamesession.WithHostGraceDuration(grace))
	srvWeb := web.New(reg, "test", scribbleBase)
	mux := http.NewServeMux()
	arcade.New().Routes(mux)
	srvWeb.Routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, reg
}

// noticeMsg mirrors the wire-format envelope for notice broadcasts.
type noticeMsg struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// sendCmd writes a JSON client command on c. Used to exercise the
// transfer-host / kick / leave verbs.
func sendCmd(t *testing.T, c *websocket.Conn, cmd map[string]any) {
	t.Helper()
	payload, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal cmd: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatalf("write cmd: %v", err)
	}
}

// waitForHost polls the domain registry until the named code's
// session has the given player marked Host. Bypasses WebSocket
// read buffers so the test isn't sensitive to broadcast ordering.
func waitForHost(t *testing.T, reg *gamesession.Registry, canonicalJoinCode, name string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		session, ok := reg.Lookup(canonicalJoinCode)
		if ok {
			for _, p := range session.Roster() {
				if p.Host && p.Name == name {
					return
				}
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("Host never became %q within deadline", name)
}

// waitForRosterSize polls until the named session's roster has
// exactly want seats.
func waitForRosterSize(t *testing.T, reg *gamesession.Registry, canonicalJoinCode string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		session, ok := reg.Lookup(canonicalJoinCode)
		if ok && len(session.Roster()) == want {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("roster never reached size %d within deadline", want)
}

func TestVoluntaryHostTransferMovesBadgeAndBroadcastsNotice(t *testing.T) {
	srv, reg := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	if alice == nil {
		t.Fatalf("Alice dial failed")
	}
	defer alice.CloseNow()
	_ = readRoster(t, alice)

	bob, _ := dialAs(t, srv, code, "Bob")
	if bob == nil {
		t.Fatalf("Bob dial failed")
	}
	defer bob.CloseNow()
	_ = readUntil(t, alice, func(m rosterMsg) bool { return len(m.Players) == 2 })

	// Alice transfers Host to Bob.
	sendCmd(t, alice, map[string]any{"type": "transfer-host", "target": "Bob"})

	waitForHost(t, reg, code, "Bob")
	// Alice should now show Bob as Host on the wire.
	m := readUntil(t, alice, func(m rosterMsg) bool {
		return rosterContains(m, "Bob", true, true) && rosterContains(m, "Alice", false, true)
	})
	if len(m.Players) != 2 {
		t.Errorf("post-transfer roster: %+v", m)
	}
}

func TestVoluntaryTransferIgnoredFromNonHost(t *testing.T) {
	srv, reg := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	_ = readRoster(t, alice)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	_ = readUntil(t, bob, func(m rosterMsg) bool { return len(m.Players) == 2 })

	// Bob (not Host) tries to give Host to himself.
	sendCmd(t, bob, map[string]any{"type": "transfer-host", "target": "Alice"})

	// Give the server a beat; Host should remain Alice.
	time.Sleep(50 * time.Millisecond)
	session, _ := reg.Lookup(code)
	host := ""
	for _, p := range session.Roster() {
		if p.Host {
			host = p.Name
		}
	}
	if host != "Alice" {
		t.Errorf("non-Host transfer changed Host to %q, want Alice", host)
	}
}

func TestHostKickRemovesSeatAndClosesTargetConn(t *testing.T) {
	srv, reg := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	_ = readRoster(t, alice)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	_ = readUntil(t, bob, func(m rosterMsg) bool { return len(m.Players) == 2 })

	// Alice kicks Bob.
	sendCmd(t, alice, map[string]any{"type": "kick", "target": "Bob"})

	// Bob's WS should receive a close with "kicked" reason.
	deadline := time.Now().Add(2 * time.Second)
	var closeErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		_, _, err := bob.Read(ctx)
		cancel()
		if err == nil {
			continue
		}
		closeErr = err
		break
	}
	if closeErr == nil {
		t.Fatalf("Bob's WS never closed after kick")
	}
	if !strings.Contains(closeErr.Error(), "kicked") {
		t.Errorf("close reason did not mention kicked: %v", closeErr)
	}

	// Domain state: Bob's seat is gone.
	waitForRosterSize(t, reg, code, 1)
}

func TestKickedPlayerComesBackAsFreshJoin(t *testing.T) {
	srv, reg := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	_ = readRoster(t, alice)
	bob, _ := dialAs(t, srv, code, "Bob")
	_ = readUntil(t, bob, func(m rosterMsg) bool { return len(m.Players) == 2 })

	sendCmd(t, alice, map[string]any{"type": "kick", "target": "Bob"})
	waitForRosterSize(t, reg, code, 1)
	_ = bob.CloseNow()

	// Bob comes back. He hits Join (no seat exists), so the new
	// Bob is Connected=true and (since Alice still holds the
	// badge) non-Host — a fresh seat, not a Reconnect.
	bob2, _ := dialAs(t, srv, code, "Bob")
	if bob2 == nil {
		t.Fatalf("Bob re-join failed")
	}
	defer bob2.CloseNow()
	m := readUntil(t, bob2, func(m rosterMsg) bool { return len(m.Players) == 2 })
	if !rosterContains(m, "Bob", false, true) {
		t.Errorf("re-joined Bob not present as fresh seat: %+v", m)
	}
}

func TestKickByNonHostIsIgnored(t *testing.T) {
	srv, reg := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	_ = readRoster(t, alice)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	_ = readUntil(t, bob, func(m rosterMsg) bool { return len(m.Players) == 2 })

	// Bob (non-Host) tries to kick Alice.
	sendCmd(t, bob, map[string]any{"type": "kick", "target": "Alice"})

	time.Sleep(50 * time.Millisecond)
	waitForRosterSize(t, reg, code, 2) // no removal
}

func TestVoluntaryLeaveRemovesSelfAndBroadcasts(t *testing.T) {
	srv, reg := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	_ = readRoster(t, alice)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	_ = readUntil(t, alice, func(m rosterMsg) bool { return len(m.Players) == 2 })

	sendCmd(t, bob, map[string]any{"type": "leave"})

	// Bob's WS will be closed by the server; Alice should see his
	// seat removed (roster shrinks to 1).
	_ = readUntil(t, alice, func(m rosterMsg) bool { return len(m.Players) == 1 })
	waitForRosterSize(t, reg, code, 1)
}

func TestVoluntaryLeaveByHostMigratesImmediately(t *testing.T) {
	// The grace timer is set high enough that if voluntary leave
	// were mistakenly routed through the grace path the test would
	// fail by timeout — i.e., migration without waiting proves the
	// path is not gated on the timer.
	srv, reg := newAppWithGrace(t, 10*time.Second)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	_ = readRoster(t, alice)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	_ = readUntil(t, bob, func(m rosterMsg) bool { return len(m.Players) == 2 })

	sendCmd(t, alice, map[string]any{"type": "leave"})

	// Bob should be Host within a normal RTT, well under the
	// 10s grace window.
	waitForHost(t, reg, code, "Bob")
	waitForRosterSize(t, reg, code, 1)
}

func TestHostDisconnectAutoMigratesAfterGrace(t *testing.T) {
	srv, reg := newAppWithGrace(t, 80*time.Millisecond)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	_ = readRoster(t, alice)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	_ = readUntil(t, bob, func(m rosterMsg) bool { return len(m.Players) == 2 })

	// Alice disconnects.
	_ = alice.CloseNow()
	// After ~80ms grace, Host should be Bob.
	waitForHost(t, reg, code, "Bob")
}

func TestHostReconnectWithinGraceCancelsMigration(t *testing.T) {
	srv, reg := newAppWithGrace(t, 200*time.Millisecond)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	_ = readRoster(t, alice)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	_ = readUntil(t, bob, func(m rosterMsg) bool { return len(m.Players) == 2 })

	_ = alice.CloseNow()
	// Reconnect within grace.
	time.Sleep(50 * time.Millisecond)
	alice2, _ := dialAs(t, srv, code, "Alice")
	if alice2 == nil {
		t.Fatalf("Alice reconnect failed")
	}
	defer alice2.CloseNow()
	// Wait past where grace would have fired.
	time.Sleep(300 * time.Millisecond)
	session, _ := reg.Lookup(code)
	host := ""
	for _, p := range session.Roster() {
		if p.Host {
			host = p.Name
		}
	}
	if host != "Alice" {
		t.Errorf("Host after reconnect within grace = %q want Alice", host)
	}
}

func TestHostReconnectAfterGraceDoesNotReclaim(t *testing.T) {
	srv, reg := newAppWithGrace(t, 60*time.Millisecond)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	_ = readRoster(t, alice)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	_ = readUntil(t, bob, func(m rosterMsg) bool { return len(m.Players) == 2 })

	_ = alice.CloseNow()
	waitForHost(t, reg, code, "Bob")

	// Alice comes back after migration: should NOT auto-reclaim.
	alice2, _ := dialAs(t, srv, code, "Alice")
	if alice2 == nil {
		t.Fatalf("Alice reconnect failed")
	}
	defer alice2.CloseNow()
	time.Sleep(50 * time.Millisecond)
	session, _ := reg.Lookup(code)
	host := ""
	for _, p := range session.Roster() {
		if p.Host {
			host = p.Name
		}
	}
	if host != "Bob" {
		t.Errorf("Host after late reconnect = %q want Bob (no reclaim)", host)
	}
}

func TestHostChangeBroadcastsNotice(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	defer alice.CloseNow()
	_ = readRoster(t, alice)
	bob, _ := dialAs(t, srv, code, "Bob")
	defer bob.CloseNow()
	_ = readUntil(t, bob, func(m rosterMsg) bool { return len(m.Players) == 2 })

	sendCmd(t, alice, map[string]any{"type": "transfer-host", "target": "Bob"})

	// Bob should receive a notice mentioning Bob as the new Host.
	deadline := time.Now().Add(2 * time.Second)
	var got noticeMsg
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		_, data, err := bob.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read on bob: %v", err)
		}
		var nm noticeMsg
		if err := json.Unmarshal(data, &nm); err != nil {
			continue
		}
		if nm.Type == "notice" {
			got = nm
			break
		}
	}
	if got.Type != "notice" {
		t.Fatalf("never received a notice message")
	}
	if !strings.Contains(got.Text, "Bob") || !strings.Contains(got.Text, "Alice") {
		t.Errorf("notice text = %q should name both Alice and Bob", got.Text)
	}
}
