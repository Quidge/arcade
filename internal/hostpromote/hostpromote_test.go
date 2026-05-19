package hostpromote

import (
	"strings"
	"testing"
)

func p(name string, connected bool) Player { return Player{Name: name, Connected: connected} }

func TestVoluntaryTransferPicksTarget(t *testing.T) {
	d := Decide("Alice", []Player{p("Alice", true), p("Bob", true), p("Carol", true)}, VoluntaryTransfer, "Carol")
	if d.NewHost != "Carol" {
		t.Errorf("NewHost = %q want Carol", d.NewHost)
	}
	if !strings.Contains(d.Notice, "Alice") || !strings.Contains(d.Notice, "Carol") {
		t.Errorf("Notice = %q should name both Alice and Carol", d.Notice)
	}
}

func TestVoluntaryTransferToSelfIsRejected(t *testing.T) {
	d := Decide("Alice", []Player{p("Alice", true), p("Bob", true)}, VoluntaryTransfer, "Alice")
	if d.NewHost != "" {
		t.Errorf("self-transfer should return empty NewHost, got %q", d.NewHost)
	}
}

func TestVoluntaryTransferToUnknownIsRejected(t *testing.T) {
	d := Decide("Alice", []Player{p("Alice", true), p("Bob", true)}, VoluntaryTransfer, "Charlie")
	if d.NewHost != "" {
		t.Errorf("transfer to unknown should return empty NewHost, got %q", d.NewHost)
	}
}

func TestVoluntaryTransferToDisconnectedIsAllowed(t *testing.T) {
	// The spec gives no recipient-consent step; transfer to a
	// disconnected seat is permitted (they remain Host until they
	// rejoin or are otherwise migrated).
	d := Decide("Alice", []Player{p("Alice", true), p("Bob", false)}, VoluntaryTransfer, "Bob")
	if d.NewHost != "Bob" {
		t.Errorf("NewHost = %q want Bob (disconnected target permitted)", d.NewHost)
	}
}

func TestDisconnectGraceExpiredPicksNextConnectedInJoinOrder(t *testing.T) {
	// Alice is disconnected (grace expired). Bob is next in join
	// order and connected — Bob wins.
	d := Decide("Alice",
		[]Player{p("Alice", false), p("Bob", true), p("Carol", true)},
		DisconnectGraceExpired, "")
	if d.NewHost != "Bob" {
		t.Errorf("NewHost = %q want Bob", d.NewHost)
	}
	if !strings.Contains(d.Notice, "disconnected") {
		t.Errorf("Notice = %q should mention disconnect", d.Notice)
	}
}

func TestDisconnectGraceSkipsDisconnectedPlayers(t *testing.T) {
	// Alice (disconnected) → Bob (disconnected) → Carol (connected).
	d := Decide("Alice",
		[]Player{p("Alice", false), p("Bob", false), p("Carol", true)},
		DisconnectGraceExpired, "")
	if d.NewHost != "Carol" {
		t.Errorf("NewHost = %q want Carol (Bob is also disconnected)", d.NewHost)
	}
}

func TestDisconnectGraceWrapsAroundJoinOrder(t *testing.T) {
	// Carol is Host (disconnected). Walk: Alice (start of list).
	d := Decide("Carol",
		[]Player{p("Alice", true), p("Bob", true), p("Carol", false)},
		DisconnectGraceExpired, "")
	if d.NewHost != "Alice" {
		t.Errorf("NewHost = %q want Alice (wrap from end of order)", d.NewHost)
	}
}

func TestDisconnectGraceAllOthersDisconnectedFallsBackToFirstNonCurrent(t *testing.T) {
	// Even when no Connected seat exists, the badge has to land
	// somewhere — pick the next in join order so a Reconnect by
	// any other Player makes them Host immediately.
	d := Decide("Alice",
		[]Player{p("Alice", false), p("Bob", false), p("Carol", false)},
		DisconnectGraceExpired, "")
	if d.NewHost != "Bob" {
		t.Errorf("NewHost = %q want Bob (fallback when no one connected)", d.NewHost)
	}
}

func TestDisconnectGraceSoloHostReturnsEmpty(t *testing.T) {
	d := Decide("Alice", []Player{p("Alice", false)}, DisconnectGraceExpired, "")
	if d.NewHost != "" {
		t.Errorf("solo Host disconnect should return empty, got %q", d.NewHost)
	}
}

func TestHostLeftVoluntarilyPicksNextConnected(t *testing.T) {
	// Alice (Host, still in list at time of decision) leaves
	// voluntarily. Bob is next.
	d := Decide("Alice",
		[]Player{p("Alice", true), p("Bob", true), p("Carol", true)},
		HostLeftVoluntarily, "")
	if d.NewHost != "Bob" {
		t.Errorf("NewHost = %q want Bob", d.NewHost)
	}
	if !strings.Contains(d.Notice, "left") {
		t.Errorf("Notice = %q should mention leaving", d.Notice)
	}
}

func TestHostLeftVoluntarilySkipsDisconnected(t *testing.T) {
	d := Decide("Alice",
		[]Player{p("Alice", true), p("Bob", false), p("Carol", true)},
		HostLeftVoluntarily, "")
	if d.NewHost != "Carol" {
		t.Errorf("NewHost = %q want Carol", d.NewHost)
	}
}

func TestHostLeftAfterSeatRemovedStillScansFromStart(t *testing.T) {
	// Caller may pre-remove the seat from players before calling
	// the engine. We scan from start in that case.
	d := Decide("Alice",
		[]Player{p("Bob", true), p("Carol", true)},
		HostLeftVoluntarily, "")
	if d.NewHost != "Bob" {
		t.Errorf("NewHost = %q want Bob (Alice already removed from list)", d.NewHost)
	}
}

func TestRecursiveMigrationWhenMigratedHostAlsoDrops(t *testing.T) {
	// First migration: Alice → Bob.
	d1 := Decide("Alice",
		[]Player{p("Alice", false), p("Bob", true), p("Carol", true)},
		DisconnectGraceExpired, "")
	if d1.NewHost != "Bob" {
		t.Fatalf("first migration NewHost = %q want Bob", d1.NewHost)
	}
	// Now Bob also drops. Second migration: Bob → Carol (Alice
	// still disconnected, so she's skipped).
	d2 := Decide("Bob",
		[]Player{p("Alice", false), p("Bob", false), p("Carol", true)},
		DisconnectGraceExpired, "")
	if d2.NewHost != "Carol" {
		t.Errorf("second migration NewHost = %q want Carol", d2.NewHost)
	}
}
