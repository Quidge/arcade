package gamesession

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// expectEvent waits up to 100ms for the next event on g and returns
// it. The test goroutine must be parked on receive when the emit
// happens — Events() is unbuffered with non-blocking send.
func expectEvent(t *testing.T, g *GameSession) Event {
	t.Helper()
	select {
	case e := <-g.Events():
		return e
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out waiting for event")
		return Event{}
	}
}

// joinAsync triggers Join from a goroutine, returning a channel
// that receives the (Player, error) tuple once the goroutine
// completes. The test goroutine should be parked on <-g.Events()
// before calling this so the synchronous emit lands.
func joinAsync(g *GameSession, name string) <-chan struct {
	P   *Player
	Err error
} {
	ch := make(chan struct {
		P   *Player
		Err error
	}, 1)
	// Tiny delay to ensure the test goroutine reaches the receive
	// before Join runs. The non-blocking-send design makes the
	// test inherently coupled to the consumer being parked.
	go func() {
		time.Sleep(5 * time.Millisecond)
		p, err := g.Join(name)
		ch <- struct {
			P   *Player
			Err error
		}{p, err}
	}()
	return ch
}

func leaveAsync(g *GameSession, name string) {
	go func() {
		time.Sleep(5 * time.Millisecond)
		g.Leave(name)
	}()
}

// startBackgroundDrain pulls events into a slice via a goroutine
// that is always parked on receive. It returns a snapshot of what
// it has captured so far and a stop function. Tests using this
// should pause briefly before reading the snapshot to let the
// goroutine drain.
func startBackgroundDrain(g *GameSession) (snapshot func() []Event, stop func()) {
	var mu sync.Mutex
	var events []Event
	done := make(chan struct{})
	go func() {
		for {
			select {
			case e := <-g.Events():
				mu.Lock()
				events = append(events, e)
				mu.Unlock()
			case <-done:
				return
			}
		}
	}()
	return func() []Event {
			mu.Lock()
			defer mu.Unlock()
			out := make([]Event, len(events))
			copy(out, events)
			return out
		}, func() {
			close(done)
		}
}

func TestRegistryCreateProducesUniqueCodes(t *testing.T) {
	r := NewRegistry()
	seen := map[string]bool{}
	const N = 200
	for i := 0; i < N; i++ {
		g := r.Create()
		if seen[g.Code()] {
			t.Fatalf("Create returned duplicate code %q", g.Code())
		}
		seen[g.Code()] = true
	}
}

func TestRegistryLookupUnknownReturnsFalse(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Lookup("NOSUCH"); ok {
		t.Errorf("Lookup of unknown code returned ok=true")
	}
}

func TestRegistryLookupAfterCreate(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	got, ok := r.Lookup(g.Code())
	if !ok {
		t.Fatalf("Lookup after Create: ok=false")
	}
	if got != g {
		t.Errorf("Lookup returned different *GameSession")
	}
}

func TestFirstJoinIsHost(t *testing.T) {
	r := NewRegistry()
	g := r.Create()

	// Park on receive, then trigger Join from a goroutine.
	doneA := joinAsync(g, "Alice")
	e1 := expectEvent(t, g)
	resA := <-doneA
	if resA.Err != nil {
		t.Fatalf("Join Alice: %v", resA.Err)
	}
	if !resA.P.Host {
		t.Errorf("first joiner should be Host, got Host=false")
	}
	if e1.Kind != PlayerJoined || e1.Player.Name != "Alice" || !e1.Player.Host {
		t.Errorf("event = %+v want PlayerJoined Alice host", e1)
	}
	if len(e1.Roster) != 1 || !e1.Roster[0].Host {
		t.Errorf("event roster = %+v", e1.Roster)
	}

	doneB := joinAsync(g, "Bob")
	e2 := expectEvent(t, g)
	resB := <-doneB
	if resB.Err != nil {
		t.Fatalf("Join Bob: %v", resB.Err)
	}
	if resB.P.Host {
		t.Errorf("second joiner should not be Host, got Host=true")
	}
	if e2.Player.Host {
		t.Errorf("event roster reports Bob as host: %+v", e2)
	}
}

func TestJoinDuplicateNameReturnsError(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	_, _ = g.Join("Alice")
	_, err := g.Join("Alice")
	if !errors.Is(err, ErrDuplicateName) {
		t.Errorf("second Join with same name: err=%v want ErrDuplicateName", err)
	}
}

func TestJoinCapExceeded(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	// Drain so emits don't accumulate in tests; events are
	// non-blocking-send so they're dropped if nothing is parked,
	// but we keep a hot consumer anyway.
	_, stop := startBackgroundDrain(g)
	defer stop()

	for i := 0; i < MaxPlayers; i++ {
		name := fmt.Sprintf("p%d", i)
		if _, err := g.Join(name); err != nil {
			t.Fatalf("Join(%q): %v", name, err)
		}
	}
	_, err := g.Join("over")
	if !errors.Is(err, ErrCapExceeded) {
		t.Errorf("9th Join: err=%v want ErrCapExceeded", err)
	}
}

func TestLeaveRemovesAndEmits(t *testing.T) {
	r := NewRegistry()
	g := r.Create()

	// Background drain to absorb the Join events while we set up.
	_, stop := startBackgroundDrain(g)
	if _, err := g.Join("Alice"); err != nil {
		t.Fatalf("Join Alice: %v", err)
	}
	if _, err := g.Join("Bob"); err != nil {
		t.Fatalf("Join Bob: %v", err)
	}
	time.Sleep(20 * time.Millisecond) // let drainer pick up
	stop()

	// Park on receive, then trigger Leave from a goroutine.
	leaveAsync(g, "Alice")
	e := expectEvent(t, g)

	if e.Kind != PlayerLeft || e.Player.Name != "Alice" {
		t.Errorf("event = %+v want PlayerLeft Alice", e)
	}
	roster := g.Roster()
	if len(roster) != 1 || roster[0].Name != "Bob" {
		t.Errorf("roster after Leave: %+v", roster)
	}
}

func TestLeaveUnknownIsNoop(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	g.Leave("ghost") // should not panic
}

func TestRosterReturnsJoinOrder(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	_, stop := startBackgroundDrain(g)
	defer stop()

	names := []string{"Alice", "Bob", "Carol", "Dave"}
	for _, n := range names {
		if _, err := g.Join(n); err != nil {
			t.Fatalf("Join(%q): %v", n, err)
		}
	}
	roster := g.Roster()
	if len(roster) != len(names) {
		t.Fatalf("roster len = %d, want %d", len(roster), len(names))
	}
	for i, p := range roster {
		if p.Name != names[i] {
			t.Errorf("roster[%d] = %q want %q", i, p.Name, names[i])
		}
	}
}
