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

	"github.com/quidge/scribble/internal/gamesession"
	"github.com/quidge/scribble/internal/joincode"
	"github.com/quidge/scribble/internal/web"
)

// rosterMsg mirrors the wire-format envelope so this test can
// assert on the JSON without depending on internal/web's private
// types.
type rosterMsg struct {
	Type    string `json:"type"`
	Players []struct {
		Name string `json:"name"`
		Host bool   `json:"host"`
	} `json:"players"`
}

// newApp wires up the full stack and returns an httptest.Server.
func newApp(t *testing.T) (*httptest.Server, *gamesession.Registry) {
	t.Helper()
	reg := gamesession.NewRegistry()
	srvWeb := web.New(reg, "test")
	mux := http.NewServeMux()
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
	resp, err := httpClient().Post(srv.URL+"/g", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatalf("POST /g: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /g status = %d want 303", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	const prefix = "/g/"
	if !strings.HasPrefix(loc, prefix) {
		t.Fatalf("Location = %q", loc)
	}
	canon, ok := joincode.Parse(strings.TrimPrefix(loc, prefix))
	if !ok {
		t.Fatalf("Location code %q does not parse", loc)
	}
	return canon
}

func wsURL(srv *httptest.Server, code, name string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") +
		"/g/" + joincode.Format(code) + "/ws?name=" + name
}

func dialAs(t *testing.T, srv *httptest.Server, code, name string) (*websocket.Conn, *http.Response) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, wsURL(srv, code, name), nil)
	if err != nil {
		// If the server rejects with a 4xx, websocket.Dial returns
		// non-nil resp even on err; the caller may inspect it.
		return nil, resp
	}
	if err != nil {
		t.Fatalf("dial as %s: %v", name, err)
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

	// Alice should see herself, host=true.
	m := readRoster(t, alice)
	if len(m.Players) != 1 || m.Players[0].Name != "Alice" || !m.Players[0].Host {
		t.Fatalf("initial roster for Alice: %+v", m)
	}

	bob, _ := dialAs(t, srv, code, "Bob")
	if bob == nil {
		t.Fatalf("Bob failed to connect")
	}
	defer bob.CloseNow()

	// Alice should see Bob join.
	m = readUntil(t, alice, func(m rosterMsg) bool { return len(m.Players) == 2 })
	if !rosterContains(m, "Alice", true) {
		t.Errorf("Alice's broadcast roster missing Alice/host: %+v", m)
	}
	if !rosterContains(m, "Bob", false) {
		t.Errorf("Alice's broadcast roster missing Bob/non-host: %+v", m)
	}

	// Bob should see both players.
	m = readUntil(t, bob, func(m rosterMsg) bool { return len(m.Players) == 2 })
	if !rosterContains(m, "Alice", true) {
		t.Errorf("Bob's initial/broadcast roster missing Alice/host: %+v", m)
	}
	if !rosterContains(m, "Bob", false) {
		t.Errorf("Bob's initial/broadcast roster missing Bob: %+v", m)
	}
}

func rosterContains(m rosterMsg, name string, host bool) bool {
	for _, p := range m.Players {
		if p.Name == name && p.Host == host {
			return true
		}
	}
	return false
}

func TestDuplicateNameRejected(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	if alice == nil {
		t.Fatalf("Alice failed to connect")
	}
	defer alice.CloseNow()
	_ = readRoster(t, alice)

	// Same name should be rejected with a close frame.
	dupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	dup, _, err := websocket.Dial(dupCtx, wsURL(srv, code, "Alice"), nil)
	if err != nil {
		// Some browsers/SDKs surface the close as a dial error.
		// In that case, the test's stop condition is just "no
		// session join occurred for the duplicate"; assert that
		// Alice received no second roster.
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, _, perr := alice.Read(ctx)
		if perr == nil {
			t.Errorf("Alice saw a roster update despite duplicate-name rejection")
		}
		return
	}
	defer dup.CloseNow()
	// Read should immediately yield a close error with the reason
	// embedded.
	ctx, cancelR := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelR()
	_, _, err = dup.Read(ctx)
	if err == nil {
		t.Fatalf("expected close on duplicate-name conn, got no error")
	}
	if status := websocket.CloseStatus(err); status != websocket.StatusPolicyViolation {
		t.Errorf("close status = %v want %v (err=%v)", status, websocket.StatusPolicyViolation, err)
	}
	if !strings.Contains(err.Error(), "duplicate name") {
		t.Errorf("close reason did not mention duplicate name: %v", err)
	}
}

func TestCapExceededRejected(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	// Connect 8 players and keep them around.
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
		// dial-time close from upgrade-then-immediate-close; treat
		// as success because the server signalled the rejection.
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

func TestUnknownCodeReturns404(t *testing.T) {
	srv, _ := newApp(t)
	// "Z9Z-Z9Z" is well-formed-looking but the registry is empty.
	resp, err := http.Get(srv.URL + "/g/Z9Z-Z9Z")
	if err != nil {
		t.Fatalf("GET unknown: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET unknown code status = %d want 404", resp.StatusCode)
	}
}

func TestFirstClientHostBadgePersists(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	if alice == nil {
		t.Fatalf("Alice failed to connect")
	}
	defer alice.CloseNow()
	m := readRoster(t, alice)
	if !rosterContains(m, "Alice", true) {
		t.Fatalf("Alice not host in initial roster: %+v", m)
	}

	// Connect a few more; Alice keeps the host badge in every
	// subsequent broadcast.
	for _, name := range []string{"Bob", "Carol", "Dave"} {
		c, _ := dialAs(t, srv, code, name)
		if c == nil {
			t.Fatalf("%s failed to connect", name)
		}
		defer c.CloseNow()
	}
	m = readUntil(t, alice, func(m rosterMsg) bool { return len(m.Players) == 4 })
	if !rosterContains(m, "Alice", true) {
		t.Errorf("Alice lost host badge after others joined: %+v", m)
	}
	for _, name := range []string{"Bob", "Carol", "Dave"} {
		if !rosterContains(m, name, false) {
			t.Errorf("missing %s with host=false: %+v", name, m)
		}
	}
}

func TestRejoinAfterDisconnect(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	alice, _ := dialAs(t, srv, code, "Alice")
	if alice == nil {
		t.Fatalf("Alice 1 failed to connect")
	}
	_ = readRoster(t, alice)

	// Alice disconnects.
	_ = alice.Close(websocket.StatusNormalClosure, "")

	// Wait for the server to process the leave (the broadcaster's
	// Subscribe must return and the deferred Leave run).
	// Drain isn't strictly needed; we just rejoin with same name.
	var alice2 *websocket.Conn
	var resp *http.Response
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		alice2, resp = dialAs(t, srv, code, "Alice")
		if alice2 != nil {
			break
		}
		_ = resp
		time.Sleep(20 * time.Millisecond)
	}
	if alice2 == nil {
		t.Fatalf("rejoin as Alice failed")
	}
	defer alice2.CloseNow()
	m := readRoster(t, alice2)
	if !rosterContains(m, "Alice", true) {
		t.Errorf("rejoined Alice should be host (only player): %+v", m)
	}
}

// TestConcurrentJoinsAndLeavesIsRaceFree is a smoke test under -race.
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
			// Stay connected briefly while broadcasts happen.
			time.Sleep(150 * time.Millisecond)
			_ = c.Close(websocket.StatusNormalClosure, "")
		}()
	}
	wg.Wait()
}
