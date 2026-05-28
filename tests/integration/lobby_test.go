//go:build integration

// Package integration_test exercises the whole-app HTTP+WebSocket
// stack against the real wired-up mux. Runs only under
// `go test -tags=integration`.
package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/quidge/scribble/internal/arcade"
	"github.com/quidge/scribble/internal/games/scribble/gamesession"
	"github.com/quidge/scribble/internal/games/scribble/web"
	"github.com/quidge/scribble/internal/joincode"
)

// scribbleBase is the URL slug Scribble is mounted under in the
// integration harness — the real prefix main.go uses (ADR 0015), so
// the prefix behavior is actually exercised. Routes and request URLs
// are built from this constant throughout the tier.
const scribbleBase = "/scribble"

// rosterMsg mirrors the wire-format envelope so this test can
// assert on the JSON without depending on internal/web's private
// types. The Connected field exercises the wire-format contract
// added by the persistent-seat slice.
type rosterMsg struct {
	Type    string `json:"type"`
	Players []struct {
		Name      string `json:"name"`
		Host      bool   `json:"host"`
		Connected bool   `json:"connected"`
	} `json:"players"`
}

// newApp wires up the full stack and returns an httptest.Server.
func newApp(t *testing.T) (*httptest.Server, *gamesession.Registry) {
	t.Helper()
	reg := gamesession.NewRegistry()
	srvWeb := web.New(reg, "test", scribbleBase)
	mux := http.NewServeMux()
	arcade.New().Routes(mux)
	srvWeb.Routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, reg
}

// httpClient returns a no-redirect client so we can inspect 303s.
func httpClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// createSession posts to /g and returns the canonical code from
// the Location header.
func createSession(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	resp, err := httpClient().Post(srv.URL+scribbleBase+"/g", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatalf("POST %s/g: %v", scribbleBase, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST %s/g status = %d want 303", scribbleBase, resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	const prefix = scribbleBase + "/g/"
	if !strings.HasPrefix(loc, prefix) {
		t.Fatalf("Location = %q", loc)
	}
	canonicalJoinCode, ok := joincode.Parse(strings.TrimPrefix(loc, prefix))
	if !ok {
		t.Fatalf("Location code %q does not parse", loc)
	}
	return canonicalJoinCode
}

func wsURL(srv *httptest.Server, canonicalJoinCode, name string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") +
		scribbleBase + "/g/" + joincode.Format(canonicalJoinCode) + "/ws?name=" + name
}

func dialAs(t *testing.T, srv *httptest.Server, canonicalJoinCode, name string) (*websocket.Conn, *http.Response) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, wsURL(srv, canonicalJoinCode, name), nil)
	if err != nil {
		return nil, resp
	}
	return c, resp
}

func readRoster(t *testing.T, c *websocket.Conn) rosterMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, p, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m rosterMsg
	if err := json.Unmarshal(p, &m); err != nil {
		t.Fatalf("unmarshal %q: %v", p, err)
	}
	if m.Type != "roster" {
		t.Fatalf("got message type %q want roster", m.Type)
	}
	return m
}

// readUntil reads roster messages until one matches pred, or the
// deadline expires.
func readUntil(t *testing.T, c *websocket.Conn, pred func(rosterMsg) bool) rosterMsg {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		_, p, err := c.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m rosterMsg
		if err := json.Unmarshal(p, &m); err != nil {
			continue
		}
		if pred(m) {
			return m
		}
	}
	t.Fatalf("predicate never matched within deadline")
	return rosterMsg{}
}

func TestTwoPlayersSeeEachOtherInRoster(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	if alice == nil {
		t.Fatalf("Alice failed to connect")
	}
	defer alice.CloseNow()

	m := readRoster(t, alice)
	if len(m.Players) != 1 || m.Players[0].Name != "Alice" || !m.Players[0].Host || !m.Players[0].Connected {
		t.Fatalf("initial roster for Alice: %+v", m)
	}

	bob, _ := dialAs(t, srv, code, "Bob")
	if bob == nil {
		t.Fatalf("Bob failed to connect")
	}
	defer bob.CloseNow()

	m = readUntil(t, alice, func(m rosterMsg) bool { return len(m.Players) == 2 })
	if !rosterContains(m, "Alice", true, true) {
		t.Errorf("Alice's broadcast roster missing Alice/host/connected: %+v", m)
	}
	if !rosterContains(m, "Bob", false, true) {
		t.Errorf("Alice's broadcast roster missing Bob/non-host/connected: %+v", m)
	}

	m = readUntil(t, bob, func(m rosterMsg) bool { return len(m.Players) == 2 })
	if !rosterContains(m, "Alice", true, true) {
		t.Errorf("Bob's broadcast roster missing Alice/host/connected: %+v", m)
	}
	if !rosterContains(m, "Bob", false, true) {
		t.Errorf("Bob's broadcast roster missing Bob: %+v", m)
	}
}

// rosterContains asserts the roster snapshot carries a player with
// the named host and connected flags. The Connected check exercises
// the wire-format contract on every assertion path.
func rosterContains(m rosterMsg, name string, host bool, connected bool) bool {
	for _, p := range m.Players {
		if p.Name == name && p.Host == host && p.Connected == connected {
			return true
		}
	}
	return false
}

func TestCapExceededRejected(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	conns := make([]*websocket.Conn, 0, gamesession.MaxPlayers)
	for i := 0; i < gamesession.MaxPlayers; i++ {
		name := "p" + string(rune('0'+i))
		c, _ := dialAs(t, srv, code, name)
		if c == nil {
			t.Fatalf("Player %d failed to connect", i)
		}
		defer c.CloseNow()
		_ = readRoster(t, c)
		conns = append(conns, c)
	}

	dupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	over, _, err := websocket.Dial(dupCtx, wsURL(srv, code, "overflow"), nil)
	if err != nil {
		return
	}
	defer over.CloseNow()

	ctx, cancelR := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelR()
	_, _, err = over.Read(ctx)
	if err == nil {
		t.Fatalf("expected close on 9th player conn, got no error")
	}
	if status := websocket.CloseStatus(err); status != websocket.StatusPolicyViolation {
		t.Errorf("close status = %v want %v (err=%v)", status, websocket.StatusPolicyViolation, err)
	}
	if !strings.Contains(err.Error(), "session full") {
		t.Errorf("close reason did not mention session full: %v", err)
	}
}

func TestCapExceededWithDisconnectedSeat(t *testing.T) {
	srv, reg := newApp(t)
	code := createSession(t, srv)

	conns := make([]*websocket.Conn, 0, gamesession.MaxPlayers)
	for i := 0; i < gamesession.MaxPlayers; i++ {
		name := "p" + string(rune('0'+i))
		c, _ := dialAs(t, srv, code, name)
		if c == nil {
			t.Fatalf("Player %d failed to connect", i)
		}
		defer c.CloseNow()
		_ = readRoster(t, c)
		conns = append(conns, c)
	}
	// Disconnect one — seat persists. Use CloseNow rather than the
	// graceful Close because we don't care about the close-frame
	// handshake here, only that the server-side handler exits and
	// runs its deferred Disconnect.
	_ = conns[0].CloseNow()

	// Poll the domain registry directly: this avoids racing the
	// broadcast pump and the per-conn read buffers (which would
	// require draining each conn under load).
	session, ok := reg.Lookup(code)
	if !ok {
		t.Fatalf("session %q not found in registry", code)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var p0 gamesession.Player
		for _, p := range session.Roster() {
			if p.Name == "p0" {
				p0 = p
				break
			}
		}
		if p0.Name == "p0" && !p0.Connected {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	stillConnected := false
	for _, p := range session.Roster() {
		if p.Name == "p0" && p.Connected {
			stillConnected = true
		}
	}
	if stillConnected {
		t.Fatalf("server never registered p0 as disconnected within deadline")
	}

	dupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	over, _, err := websocket.Dial(dupCtx, wsURL(srv, code, "overflow"), nil)
	if err != nil {
		// dial-time close treated as success.
		return
	}
	defer over.CloseNow()
	ctx, cancelR := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelR()
	_, _, err = over.Read(ctx)
	if err == nil {
		t.Fatalf("expected close on 9th conn while one seat disconnected, got no error")
	}
	if status := websocket.CloseStatus(err); status != websocket.StatusPolicyViolation {
		t.Errorf("close status = %v want %v (err=%v)", status, websocket.StatusPolicyViolation, err)
	}
	if !strings.Contains(err.Error(), "session full") {
		t.Errorf("close reason did not mention session full: %v", err)
	}
}

func TestUnknownCodeReturns404(t *testing.T) {
	srv, _ := newApp(t)
	resp, err := http.Get(srv.URL + scribbleBase + "/g/Z9Z-Z9Z")
	if err != nil {
		t.Fatalf("GET unknown: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET unknown code status = %d want 404", resp.StatusCode)
	}
}

func TestFirstClientHostBadgePersists(t *testing.T) {
	// With MaxPlayers temporarily clamped to 2 the original "Alice
	// stays Host as Bob, Carol, Dave join" scenario collapses to
	// "Alice stays Host as Bob joins." The intent — Host badge does
	// not migrate on non-Host joins — is unchanged.
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	if alice == nil {
		t.Fatalf("Alice failed to connect")
	}
	defer alice.CloseNow()
	m := readRoster(t, alice)
	if !rosterContains(m, "Alice", true, true) {
		t.Fatalf("Alice not host/connected in initial roster: %+v", m)
	}

	bob, _ := dialAs(t, srv, code, "Bob")
	if bob == nil {
		t.Fatalf("Bob failed to connect")
	}
	defer bob.CloseNow()
	m = readUntil(t, alice, func(m rosterMsg) bool { return len(m.Players) == 2 })
	if !rosterContains(m, "Alice", true, true) {
		t.Errorf("Alice lost host badge after Bob joined: %+v", m)
	}
	if !rosterContains(m, "Bob", false, true) {
		t.Errorf("missing Bob with host=false/connected=true: %+v", m)
	}
}

func TestHostBadgePersistsAcrossDisconnectReconnect(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	if alice == nil {
		t.Fatalf("Alice failed to connect")
	}
	_ = readRoster(t, alice)

	bob, _ := dialAs(t, srv, code, "Bob")
	if bob == nil {
		t.Fatalf("Bob failed to connect")
	}
	defer bob.CloseNow()
	// Bob sees the two-player roster with Alice as host.
	m := readUntil(t, bob, func(m rosterMsg) bool { return len(m.Players) == 2 })
	if !rosterContains(m, "Alice", true, true) {
		t.Fatalf("Bob's roster missing Alice/host/connected: %+v", m)
	}

	// Alice disconnects.
	_ = alice.Close(websocket.StatusNormalClosure, "")
	// Bob should see Alice marked disconnected, seat preserved.
	m = readUntil(t, bob, func(m rosterMsg) bool {
		return rosterContains(m, "Alice", true, false)
	})
	if len(m.Players) != 2 {
		t.Errorf("after Alice disconnect, Bob's roster should still have 2 seats: %+v", m)
	}

	// Alice rejoins under the same name.
	alice2, _ := dialAs(t, srv, code, "Alice")
	if alice2 == nil {
		t.Fatalf("Alice rejoin failed")
	}
	defer alice2.CloseNow()
	m = readUntil(t, alice2, func(m rosterMsg) bool { return len(m.Players) == 2 })
	if !rosterContains(m, "Alice", true, true) {
		t.Errorf("rejoined Alice should be host/connected: %+v", m)
	}

	// Bob also sees Alice's return.
	m = readUntil(t, bob, func(m rosterMsg) bool {
		return rosterContains(m, "Alice", true, true)
	})
	if !rosterContains(m, "Bob", false, true) {
		t.Errorf("Bob lost from roster after Alice reconnect: %+v", m)
	}
}

func TestSupersedeClosesOldConnection(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice1, _ := dialAs(t, srv, code, "Alice")
	if alice1 == nil {
		t.Fatalf("alice1 failed to connect")
	}
	defer alice1.CloseNow()
	_ = readRoster(t, alice1)

	// A second connection arrives under the same name. The first
	// must be closed with the "superseded" reason.
	alice2, _ := dialAs(t, srv, code, "Alice")
	if alice2 == nil {
		t.Fatalf("alice2 failed to connect")
	}
	defer alice2.CloseNow()

	// Drain any in-flight roster broadcasts (the PlayerJoined emit
	// for alice1 races with alice1's broadcaster subscribe; the
	// fan-out may or may not reach this conn). The seat-supersede
	// close frame must arrive eventually.
	deadline := time.Now().Add(2 * time.Second)
	var closeErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		_, _, err := alice1.Read(ctx)
		cancel()
		if err == nil {
			continue
		}
		closeErr = err
		break
	}
	if closeErr == nil {
		t.Fatalf("expected alice1 close frame after supersede, never received")
	}
	if !strings.Contains(closeErr.Error(), "superseded") {
		t.Errorf("close reason did not mention superseded: %v", closeErr)
	}

	// alice2 sees the live roster — one seat, connected.
	m := readRoster(t, alice2)
	if !rosterContains(m, "Alice", true, true) {
		t.Errorf("alice2 initial roster missing Alice/host/connected: %+v", m)
	}
}

func TestConcurrentJoinsAndLeavesIsRaceFree(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	var wg sync.WaitGroup
	for i := 0; i < gamesession.MaxPlayers; i++ {
		name := "p" + string(rune('0'+i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _ := dialAs(t, srv, code, name)
			if c == nil {
				return
			}
			time.Sleep(150 * time.Millisecond)
			_ = c.Close(websocket.StatusNormalClosure, "")
		}()
	}
	wg.Wait()
}
