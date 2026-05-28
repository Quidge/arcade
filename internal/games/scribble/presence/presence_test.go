package presence

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// newTestServer mounts a handler at "/" that accepts a websocket
// upgrade and calls b.Subscribe with the provided context. Tests
// can dial the returned URL to obtain a real *websocket.Conn pair.
func newTestServer(t *testing.T, b *Broadcaster) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"},
		})
		if err != nil {
			t.Logf("accept: %v", err)
			return
		}
		defer func() { _ = c.CloseNow() }()
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		_ = b.Subscribe(ctx, c)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dial(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

func readNext(t *testing.T, c *websocket.Conn) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, p, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(p)
}

// waitUntilSubscribed polls the broadcaster's subscriber count
// until it reaches want or the deadline expires.
func waitUntilSubscribed(t *testing.T, b *Broadcaster, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		b.mu.RLock()
		n := len(b.subs)
		b.mu.RUnlock()
		if n == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("subscriber count never reached %d", want)
}

func TestBroadcastDeliversToSubscribers(t *testing.T) {
	b := New()
	srv := newTestServer(t, b)

	c1 := dial(t, srv)
	defer func() { _ = c1.CloseNow() }()
	c2 := dial(t, srv)
	defer func() { _ = c2.CloseNow() }()
	waitUntilSubscribed(t, b, 2)

	b.Broadcast([]byte("hello"))

	if got := readNext(t, c1); got != "hello" {
		t.Errorf("c1 got %q want hello", got)
	}
	if got := readNext(t, c2); got != "hello" {
		t.Errorf("c2 got %q want hello", got)
	}
}

func TestSubscriberDisconnectRemovesItFromBroadcast(t *testing.T) {
	b := New()
	srv := newTestServer(t, b)

	c1 := dial(t, srv)
	c2 := dial(t, srv)
	defer func() { _ = c2.CloseNow() }()
	waitUntilSubscribed(t, b, 2)

	// Disconnect c1; wait for the server-side Subscribe to notice.
	_ = c1.Close(websocket.StatusNormalClosure, "")
	waitUntilSubscribed(t, b, 1)

	b.Broadcast([]byte("after-disconnect"))
	if got := readNext(t, c2); got != "after-disconnect" {
		t.Errorf("c2 got %q want after-disconnect", got)
	}
}

func TestBroadcastWithNoSubscribersDoesNotPanic(t *testing.T) {
	b := New()
	b.Broadcast([]byte("into the void"))
}

func TestConcurrentSubscribeAndBroadcastIsRaceFree(t *testing.T) {
	// Exercise the locks: many concurrent dials, broadcasts, and
	// disconnects. Run with `go test -race`.
	b := New()
	srv := newTestServer(t, b)

	var wg sync.WaitGroup
	const N = 20
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := dial(t, srv)
			defer func() { _ = c.CloseNow() }()
			// Briefly read whatever comes in, then leave.
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			for {
				if _, _, err := c.Read(ctx); err != nil {
					return
				}
			}
		}()
	}

	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				b.Broadcast([]byte("ping"))
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	wg.Wait()
	close(stop)
}
