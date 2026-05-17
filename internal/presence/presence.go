// Package presence is a generic WebSocket fan-out broadcaster. It
// knows nothing about Players, GameSessions, or wire formats — it
// just relays opaque byte payloads to a set of subscribed
// connections.
//
// One Broadcaster is intended per fan-out group (e.g. one per
// GameSession in the lobby layer). The web package owns the wiring.
package presence

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// writeTimeout is the per-write deadline applied when broadcasting
// to each subscriber. A slow client gets dropped rather than
// stalling the whole fan-out.
const writeTimeout = 5 * time.Second

// Broadcaster is the fan-out hub. The zero value is not usable;
// obtain one via New.
type Broadcaster struct {
	mu   sync.RWMutex
	subs map[*subscription]struct{}
}

type subscription struct {
	conn *websocket.Conn
}

// New constructs a Broadcaster with no subscribers.
func New() *Broadcaster {
	return &Broadcaster{subs: map[*subscription]struct{}{}}
}

// Subscribe registers conn for fan-out and blocks until either ctx
// is canceled or the connection closes (e.g. the client disconnects
// or a Broadcast write fails). It always returns nil — the caller's
// loop ends when this function returns.
//
// The caller retains ownership of the *websocket.Conn lifecycle:
// it must call Close(...) / CloseNow() before its handler exits.
func (b *Broadcaster) Subscribe(ctx context.Context, conn *websocket.Conn) error {
	s := &subscription{conn: conn}
	b.add(s)
	defer b.remove(s)

	// CloseRead starts a goroutine that reads (and discards) all
	// incoming frames. The returned context is canceled when the
	// connection closes for any reason, so we can park here.
	readCtx := conn.CloseRead(ctx)
	<-readCtx.Done()
	return nil
}

// Broadcast writes payload as a text message to every current
// subscriber. Writes happen serially with a short per-write
// deadline; any write that errors causes that subscriber's
// connection to be force-closed, which will in turn cause its
// Subscribe call to return through the normal lifecycle.
func (b *Broadcaster) Broadcast(payload []byte) {
	b.mu.RLock()
	subs := make([]*subscription, 0, len(b.subs))
	for s := range b.subs {
		subs = append(subs, s)
	}
	b.mu.RUnlock()

	for _, s := range subs {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		err := s.conn.Write(ctx, websocket.MessageText, payload)
		cancel()
		if err != nil {
			// Force-close so the subscribed handler unblocks and the
			// per-conn cleanup path runs in its own goroutine.
			_ = s.conn.CloseNow()
		}
	}
}

func (b *Broadcaster) add(s *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[s] = struct{}{}
}

func (b *Broadcaster) remove(s *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subs, s)
}
