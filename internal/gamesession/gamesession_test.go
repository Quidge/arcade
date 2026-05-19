package gamesession

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
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

// expectNoEvent asserts the channel receives nothing within a short
// window. Used to verify no-op transitions emit no event.
func expectNoEvent(t *testing.T, g *GameSession) {
	t.Helper()
	select {
	case e := <-g.Events():
		t.Fatalf("unexpected event: %+v", e)
	case <-time.After(50 * time.Millisecond):
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

func reconnectAsync(g *GameSession, name string) <-chan struct {
	P   *Player
	Err error
} {
	ch := make(chan struct {
		P   *Player
		Err error
	}, 1)
	go func() {
		time.Sleep(5 * time.Millisecond)
		p, err := g.Reconnect(name)
		ch <- struct {
			P   *Player
			Err error
		}{p, err}
	}()
	return ch
}

func disconnectAsync(g *GameSession, name string) {
	go func() {
		time.Sleep(5 * time.Millisecond)
		g.Disconnect(name)
	}()
}

// startBackgroundDrain pulls events into a slice via a goroutine
// that is always parked on receive. It returns a snapshot of what
// it has captured so far and a stop function.
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

func TestFirstJoinIsHostAndConnected(t *testing.T) {
	r := NewRegistry()
	g := r.Create()

	doneA := joinAsync(g, "Alice")
	e1 := expectEvent(t, g)
	resA := <-doneA
	if resA.Err != nil {
		t.Fatalf("Join Alice: %v", resA.Err)
	}
	if !resA.P.Host {
		t.Errorf("first joiner should be Host, got Host=false")
	}
	if !resA.P.Connected {
		t.Errorf("fresh join should be Connected=true, got false")
	}
	if e1.Kind != PlayerJoined || e1.Player.Name != "Alice" || !e1.Player.Host || !e1.Player.Connected {
		t.Errorf("event = %+v want PlayerJoined Alice host connected", e1)
	}
	if len(e1.Roster) != 1 || !e1.Roster[0].Host || !e1.Roster[0].Connected {
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
	if !resB.P.Connected {
		t.Errorf("fresh join should be Connected=true, got false")
	}
	if e2.Player.Host {
		t.Errorf("event reports Bob as host: %+v", e2)
	}
}

func TestJoinExistingSeatReturnsErrSeatExists(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	_, _ = g.Join("Alice")
	_, err := g.Join("Alice")
	if !errors.Is(err, ErrSeatExists) {
		t.Errorf("second Join with same name: err=%v want ErrSeatExists", err)
	}
}

func TestJoinCapCountsSeatsIncludingDisconnected(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	_, stop := startBackgroundDrain(g)
	defer stop()

	for i := 0; i < MaxPlayers; i++ {
		name := fmt.Sprintf("p%d", i)
		if _, err := g.Join(name); err != nil {
			t.Fatalf("Join(%q): %v", name, err)
		}
	}
	// Disconnect one — seat is held but not live.
	g.Disconnect("p0")
	// 9th Join still rejected: cap counts seats, not connections.
	if _, err := g.Join("over"); !errors.Is(err, ErrCapExceeded) {
		t.Errorf("9th Join with one disconnected: err=%v want ErrCapExceeded", err)
	}
}

func TestDisconnectPreservesSeatAndEmits(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	_, stop := startBackgroundDrain(g)
	if _, err := g.Join("Alice"); err != nil {
		t.Fatalf("Join Alice: %v", err)
	}
	if _, err := g.Join("Bob"); err != nil {
		t.Fatalf("Join Bob: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	stop()

	disconnectAsync(g, "Alice")
	e := expectEvent(t, g)
	if e.Kind != PlayerDisconnected || e.Player.Name != "Alice" {
		t.Errorf("event = %+v want PlayerDisconnected Alice", e)
	}
	if e.Player.Connected {
		t.Errorf("event Player.Connected = true, want false on disconnect")
	}
	roster := g.Roster()
	if len(roster) != 2 {
		t.Fatalf("roster after Disconnect should still have 2 seats: %+v", roster)
	}
	var aliceSeen bool
	for _, p := range roster {
		if p.Name == "Alice" {
			aliceSeen = true
			if p.Connected {
				t.Errorf("Alice should be Connected=false after Disconnect, got %+v", p)
			}
			if !p.Host {
				t.Errorf("Alice should retain Host across Disconnect, got %+v", p)
			}
		}
	}
	if !aliceSeen {
		t.Errorf("Alice seat removed by Disconnect, roster: %+v", roster)
	}
}

func TestDisconnectUnknownIsNoop(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	g.Disconnect("ghost") // should not panic, no event
	expectNoEvent(t, g)
}

func TestDisconnectAlreadyDisconnectedIsNoop(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	_, stop := startBackgroundDrain(g)
	if _, err := g.Join("Alice"); err != nil {
		t.Fatalf("Join Alice: %v", err)
	}
	g.Disconnect("Alice")
	time.Sleep(20 * time.Millisecond)
	stop()

	// Second disconnect: no event.
	go func() {
		time.Sleep(5 * time.Millisecond)
		g.Disconnect("Alice")
	}()
	expectNoEvent(t, g)
}

func TestReconnectOnDisconnectedSeatFlipsAndEmits(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	_, stop := startBackgroundDrain(g)
	if _, err := g.Join("Alice"); err != nil {
		t.Fatalf("Join Alice: %v", err)
	}
	g.Disconnect("Alice")
	time.Sleep(20 * time.Millisecond)
	stop()

	doneA := reconnectAsync(g, "Alice")
	e := expectEvent(t, g)
	res := <-doneA
	if res.Err != nil {
		t.Fatalf("Reconnect Alice: %v", res.Err)
	}
	if e.Kind != PlayerReconnected || e.Player.Name != "Alice" {
		t.Errorf("event = %+v want PlayerReconnected Alice", e)
	}
	if !e.Player.Connected || !e.Player.Host {
		t.Errorf("reconnected Alice should be Connected and still Host: %+v", e.Player)
	}
}

func TestReconnectOnConnectedSeatIsNoop(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	_, stop := startBackgroundDrain(g)
	if _, err := g.Join("Alice"); err != nil {
		t.Fatalf("Join Alice: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	stop()

	// Reconnect while already connected: no event, returns seat.
	go func() {
		time.Sleep(5 * time.Millisecond)
		_, _ = g.Reconnect("Alice")
	}()
	expectNoEvent(t, g)
	roster := g.Roster()
	if len(roster) != 1 || !roster[0].Connected {
		t.Errorf("roster after no-op reconnect: %+v", roster)
	}
}

func TestReconnectUnseatedReturnsErrNotSeated(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	if _, err := g.Reconnect("Nobody"); !errors.Is(err, ErrNotSeated) {
		t.Errorf("Reconnect unseated: err=%v want ErrNotSeated", err)
	}
}

func TestJoinOrderStableAcrossDisconnectReconnect(t *testing.T) {
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
	// Disconnect and reconnect Bob.
	g.Disconnect("Bob")
	if _, err := g.Reconnect("Bob"); err != nil {
		t.Fatalf("Reconnect Bob: %v", err)
	}

	roster := g.Roster()
	if len(roster) != len(names) {
		t.Fatalf("roster len = %d want %d", len(roster), len(names))
	}
	for i, p := range roster {
		if p.Name != names[i] {
			t.Errorf("roster[%d] = %q want %q", i, p.Name, names[i])
		}
	}
}

func TestHostStaysOnSeatAcrossDisconnect(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	_, stop := startBackgroundDrain(g)
	defer stop()

	if _, err := g.Join("Alice"); err != nil {
		t.Fatalf("Join Alice: %v", err)
	}
	if _, err := g.Join("Bob"); err != nil {
		t.Fatalf("Join Bob: %v", err)
	}
	g.Disconnect("Alice")
	roster := g.Roster()
	for _, p := range roster {
		if p.Name == "Alice" && !p.Host {
			t.Errorf("Alice lost Host across Disconnect: %+v", p)
		}
		if p.Name == "Bob" && p.Host {
			t.Errorf("Bob promoted to Host (auto-migrate not in this slice): %+v", p)
		}
	}
	if _, err := g.Reconnect("Alice"); err != nil {
		t.Fatalf("Reconnect Alice: %v", err)
	}
	roster = g.Roster()
	for _, p := range roster {
		if p.Name == "Alice" && !p.Host {
			t.Errorf("Alice lost Host across Reconnect: %+v", p)
		}
	}
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

func TestHasSeatTrueAfterJoinFalseOtherwise(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	if g.HasSeat("Alice") {
		t.Errorf("HasSeat(Alice) before Join: true, want false")
	}
	_, stop := startBackgroundDrain(g)
	defer stop()
	if _, err := g.Join("Alice"); err != nil {
		t.Fatalf("Join Alice: %v", err)
	}
	if !g.HasSeat("Alice") {
		t.Errorf("HasSeat(Alice) after Join: false, want true")
	}
	g.Disconnect("Alice")
	if !g.HasSeat("Alice") {
		t.Errorf("HasSeat(Alice) after Disconnect: false, want true (seat persists)")
	}
}

// hostName returns the name of the seat that currently holds the
// Host badge, or "" if none. Used by Host-promotion tests.
func hostName(g *GameSession) string {
	for _, p := range g.Roster() {
		if p.Host {
			return p.Name
		}
	}
	return ""
}

// seedPlayers Joins the named seats in order. The event channel is
// drained by a background goroutine to avoid dropped emits; the
// drain is stopped before the test starts asserting on events.
func seedPlayers(t *testing.T, g *GameSession, names ...string) {
	t.Helper()
	_, stop := startBackgroundDrain(g)
	for _, n := range names {
		if _, err := g.Join(n); err != nil {
			stop()
			t.Fatalf("Join %s: %v", n, err)
		}
	}
	time.Sleep(20 * time.Millisecond)
	stop()
}

func TestLeaveRemovesSeatAndEmitsPlayerLeft(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	seedPlayers(t, g, "Alice", "Bob")

	go func() {
		time.Sleep(5 * time.Millisecond)
		_ = g.Leave("Bob")
	}()
	e := expectEvent(t, g)
	if e.Kind != PlayerLeft || e.Player.Name != "Bob" {
		t.Errorf("event = %+v want PlayerLeft Bob", e)
	}
	roster := g.Roster()
	if len(roster) != 1 || roster[0].Name != "Alice" {
		t.Errorf("roster after Leave: %+v want [Alice]", roster)
	}
	// Capacity freed: re-Joining Bob succeeds (and emits an event
	// we drain so the channel doesn't accumulate).
	doneB := joinAsync(g, "Bob")
	<-g.Events()
	if resB := <-doneB; resB.Err != nil {
		t.Errorf("re-Join after Leave: %v", resB.Err)
	}
}

func TestLeaveUnseatedReturnsErrNotSeated(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	if err := g.Leave("Nobody"); !errors.Is(err, ErrNotSeated) {
		t.Errorf("Leave unseated: err=%v want ErrNotSeated", err)
	}
}

func TestLeaveByHostMigratesHost(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	seedPlayers(t, g, "Alice", "Bob", "Carol")
	// Alice (Host) leaves.
	go func() {
		time.Sleep(5 * time.Millisecond)
		_ = g.Leave("Alice")
	}()
	left := expectEvent(t, g)
	if left.Kind != PlayerLeft || left.Player.Name != "Alice" {
		t.Errorf("first event = %+v want PlayerLeft Alice", left)
	}
	if left.Player.Host {
		t.Errorf("leaving Host should have Host=false in PlayerLeft event: %+v", left.Player)
	}
	hostEv := expectEvent(t, g)
	if hostEv.Kind != HostChanged || hostEv.Player.Name != "Bob" {
		t.Errorf("second event = %+v want HostChanged Bob", hostEv)
	}
	if hostEv.Notice == "" {
		t.Errorf("HostChanged event should carry Notice text")
	}
	if got := hostName(g); got != "Bob" {
		t.Errorf("current Host after Leave = %q want Bob", got)
	}
}

func TestLeaveByNonHostDoesNotEmitHostChanged(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	seedPlayers(t, g, "Alice", "Bob")
	go func() {
		time.Sleep(5 * time.Millisecond)
		_ = g.Leave("Bob")
	}()
	e := expectEvent(t, g)
	if e.Kind != PlayerLeft || e.Player.Name != "Bob" {
		t.Errorf("event = %+v want PlayerLeft Bob", e)
	}
	expectNoEvent(t, g) // no HostChanged should follow
	if got := hostName(g); got != "Alice" {
		t.Errorf("Host after non-Host Leave: %q want Alice", got)
	}
}

func TestTransferHostMovesBadge(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	seedPlayers(t, g, "Alice", "Bob", "Carol")
	go func() {
		time.Sleep(5 * time.Millisecond)
		if err := g.TransferHost("Alice", "Carol"); err != nil {
			t.Errorf("TransferHost: %v", err)
		}
	}()
	e := expectEvent(t, g)
	if e.Kind != HostChanged || e.Player.Name != "Carol" {
		t.Errorf("event = %+v want HostChanged Carol", e)
	}
	if got := hostName(g); got != "Carol" {
		t.Errorf("Host after transfer = %q want Carol", got)
	}
}

func TestTransferHostByNonHostReturnsErrNotHost(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	if _, err := g.Join("Alice"); err != nil {
		t.Fatalf("Join Alice: %v", err)
	}
	if _, err := g.Join("Bob"); err != nil {
		t.Fatalf("Join Bob: %v", err)
	}
	_, stop := startBackgroundDrain(g)
	defer stop()
	if err := g.TransferHost("Bob", "Alice"); !errors.Is(err, ErrNotHost) {
		t.Errorf("non-Host transfer: err=%v want ErrNotHost", err)
	}
}

func TestTransferHostToSelfReturnsErrSelfTransfer(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	if _, err := g.Join("Alice"); err != nil {
		t.Fatalf("Join Alice: %v", err)
	}
	_, stop := startBackgroundDrain(g)
	defer stop()
	if err := g.TransferHost("Alice", "Alice"); !errors.Is(err, ErrSelfTransfer) {
		t.Errorf("self transfer: err=%v want ErrSelfTransfer", err)
	}
}

func TestTransferHostToUnknownReturnsErrNotSeated(t *testing.T) {
	r := NewRegistry()
	g := r.Create()
	if _, err := g.Join("Alice"); err != nil {
		t.Fatalf("Join Alice: %v", err)
	}
	_, stop := startBackgroundDrain(g)
	defer stop()
	if err := g.TransferHost("Alice", "Ghost"); !errors.Is(err, ErrNotSeated) {
		t.Errorf("unknown target: err=%v want ErrNotSeated", err)
	}
}

// The grace-timer tests run inside a synctest bubble so time
// advances deterministically when all bubble goroutines are
// durably blocked. This lets each test exercise the production
// 15-second grace window (DefaultHostGraceDuration) directly
// without WithHostGraceDuration shims or real-time sleeps —
// `time.Sleep(15 * time.Second)` completes in microseconds of
// wall-clock and proves the test is asserting against the same
// constant production uses. synctest.Wait() drains the timer
// callback goroutine after fire so assertions race-freely.

func TestHostDisconnectGraceExpiryMigrates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRegistry()
		g := r.Create()
		seedPlayers(t, g, "Alice", "Bob")

		snapshot, stop := startBackgroundDrain(g)
		defer stop()
		g.Disconnect("Alice")
		// Fake-time past the grace deadline; synctest.Wait then
		// settles the AfterFunc callback goroutine.
		time.Sleep(DefaultHostGraceDuration + time.Second)
		synctest.Wait()

		if got := hostName(g); got != "Bob" {
			t.Errorf("Host after grace expiry = %q want Bob", got)
		}
		// Alice's seat persists, still disconnected.
		for _, p := range g.Roster() {
			if p.Name == "Alice" {
				if p.Host {
					t.Errorf("Alice should have lost Host badge: %+v", p)
				}
				if p.Connected {
					t.Errorf("Alice should still be disconnected: %+v", p)
				}
			}
		}
		// Event-stream contract: a HostChanged event should have
		// been emitted with Bob as the new Host and a non-empty
		// notice.
		var sawHostChanged bool
		for _, e := range snapshot() {
			if e.Kind == HostChanged && e.Player.Name == "Bob" {
				sawHostChanged = true
				if e.Notice == "" {
					t.Errorf("HostChanged event should carry Notice text")
				}
			}
		}
		if !sawHostChanged {
			t.Errorf("no HostChanged event in stream: %+v", snapshot())
		}
	})
}

func TestHostReconnectBeforeGraceCancels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRegistry()
		g := r.Create()
		seedPlayers(t, g, "Alice", "Bob")

		_, stop := startBackgroundDrain(g)
		defer stop()
		g.Disconnect("Alice")
		// Reconnect well before grace expires.
		time.Sleep(5 * time.Second)
		if _, err := g.Reconnect("Alice"); err != nil {
			t.Fatalf("Reconnect Alice: %v", err)
		}
		// Advance past the original grace deadline; the cancelled
		// timer must not fire.
		time.Sleep(DefaultHostGraceDuration + time.Second)
		synctest.Wait()
		if got := hostName(g); got != "Alice" {
			t.Errorf("Host after Reconnect within grace = %q want Alice", got)
		}
	})
}

func TestHostReconnectAfterGraceDoesNotReclaim(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRegistry()
		g := r.Create()
		seedPlayers(t, g, "Alice", "Bob")

		_, stop := startBackgroundDrain(g)
		defer stop()
		g.Disconnect("Alice")
		time.Sleep(DefaultHostGraceDuration + time.Second)
		synctest.Wait()
		if got := hostName(g); got != "Bob" {
			t.Fatalf("Host after grace expiry = %q want Bob", got)
		}
		// Alice rejoins after migration: she should NOT auto-reclaim.
		if _, err := g.Reconnect("Alice"); err != nil {
			t.Fatalf("Reconnect Alice: %v", err)
		}
		synctest.Wait()
		if got := hostName(g); got != "Bob" {
			t.Errorf("Host after late Reconnect = %q want Bob (no auto-reclaim)", got)
		}
	})
}

func TestGraceSkipsDisconnectedNextInOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRegistry()
		g := r.Create()
		seedPlayers(t, g, "Alice", "Bob", "Carol")

		_, stop := startBackgroundDrain(g)
		defer stop()
		// Bob is disconnected before Alice's grace fires; Carol
		// should inherit Host.
		g.Disconnect("Bob")
		g.Disconnect("Alice")
		time.Sleep(DefaultHostGraceDuration + time.Second)
		synctest.Wait()
		if got := hostName(g); got != "Carol" {
			t.Errorf("Host after grace with Bob disconnected = %q want Carol", got)
		}
	})
}

func TestRecursiveAutoMigrateWhenNewHostDisconnects(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRegistry()
		g := r.Create()
		seedPlayers(t, g, "Alice", "Bob", "Carol")

		_, stop := startBackgroundDrain(g)
		defer stop()
		g.Disconnect("Alice")
		time.Sleep(DefaultHostGraceDuration + time.Second) // Alice → Bob.
		synctest.Wait()
		if got := hostName(g); got != "Bob" {
			t.Fatalf("after first grace: Host = %q want Bob", got)
		}
		g.Disconnect("Bob")
		time.Sleep(DefaultHostGraceDuration + time.Second) // Bob → Carol.
		synctest.Wait()
		if got := hostName(g); got != "Carol" {
			t.Errorf("after second grace: Host = %q want Carol", got)
		}
	})
}

func TestNonHostDisconnectDoesNotStartGrace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := NewRegistry()
		g := r.Create()
		seedPlayers(t, g, "Alice", "Bob")

		_, stop := startBackgroundDrain(g)
		defer stop()
		g.Disconnect("Bob")
		time.Sleep(DefaultHostGraceDuration + time.Second)
		synctest.Wait()
		if got := hostName(g); got != "Alice" {
			t.Errorf("Host after non-Host disconnect = %q want Alice", got)
		}
	})
}

func TestVoluntaryHostLeaveDoesNotWaitForGrace(t *testing.T) {
	// Voluntary Leave by the Host migrates the badge immediately,
	// not after the 15-second grace wait. Inside a synctest bubble
	// we can assert this directly: synctest.Wait() returns as soon
	// as the Leave path is settled, without any fake-time advance.
	synctest.Test(t, func(t *testing.T) {
		r := NewRegistry()
		g := r.Create()
		seedPlayers(t, g, "Alice", "Bob")

		_, stop := startBackgroundDrain(g)
		defer stop()
		if err := g.Leave("Alice"); err != nil {
			t.Fatalf("Leave Alice: %v", err)
		}
		synctest.Wait()
		if got := hostName(g); got != "Bob" {
			t.Errorf("Host after voluntary Leave = %q want Bob", got)
		}
	})
}
