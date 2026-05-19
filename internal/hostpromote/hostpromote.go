// Package hostpromote implements the Host promotion engine: a pure
// function over (current Host name, join-ordered Player list with
// per-Player Connected state, transition event) that returns the
// new Host's name and a human-readable broadcast notice.
//
// The engine performs no I/O, holds no timers, and has no
// WebSocket awareness. The 15-second grace timer that triggers
// DisconnectGraceExpired lives adjacent (in the gamesession
// package) — the engine is invoked *after* the grace decision is
// made. See ADR 0005 and issue #7.
package hostpromote

import "fmt"

// Player is the minimal seat shape the engine needs: a name and a
// live-connection flag. The caller passes them in join order.
type Player struct {
	Name      string
	Connected bool
}

// Event discriminates the three transitions that move the Host
// badge. Each carries different inputs:
//
//   - VoluntaryTransfer: current Host hands Host to Target.
//   - DisconnectGraceExpired: current Host was Disconnected and
//     the grace window elapsed without a Reconnect.
//   - HostLeftVoluntarily: current Host invoked Leave; their seat
//     is being removed.
type Event int

const (
	VoluntaryTransfer Event = iota
	DisconnectGraceExpired
	HostLeftVoluntarily
)

// Decision is the engine's output. NewHost is the name of the seat
// that should now hold the Host badge — empty if no eligible
// target exists (e.g. the leaving Host was the only seat). Notice
// is the broadcast text for the room ("Alice transferred Host to
// Bob", etc.).
type Decision struct {
	NewHost string
	Notice  string
}

// Decide returns the promotion Decision for the given transition.
// players is the join-ordered seat list at the moment of decision.
// For HostLeftVoluntarily the leaving Host is still present in
// players — the engine skips them by name. target is read only
// for VoluntaryTransfer; ignored for the other events.
//
// Rules:
//   - VoluntaryTransfer: NewHost = target. Notice names both. If
//     target is the current Host, equals empty string, or is not
//     present in players, returns an empty Decision (NewHost="").
//   - DisconnectGraceExpired / HostLeftVoluntarily: walk join order
//     starting from the position *after* currentHost, wrapping,
//     skipping currentHost and any Connected=false seat. The first
//     match becomes NewHost. If no connected seat remains, falls
//     back to the next seat in join order regardless of Connected
//     (a Host badge held by a disconnected seat is still better
//     than a session with no Host at all). Returns an empty
//     Decision only if no other seat exists.
func Decide(currentHost string, players []Player, event Event, target string) Decision {
	switch event {
	case VoluntaryTransfer:
		if target == "" || target == currentHost {
			return Decision{}
		}
		for _, p := range players {
			if p.Name == target {
				return Decision{
					NewHost: target,
					Notice:  fmt.Sprintf("%s transferred Host to %s", currentHost, target),
				}
			}
		}
		return Decision{}

	case DisconnectGraceExpired:
		next := nextInOrder(currentHost, players)
		if next == "" {
			return Decision{}
		}
		return Decision{
			NewHost: next,
			Notice:  fmt.Sprintf("%s was disconnected — %s is now the Host", currentHost, next),
		}

	case HostLeftVoluntarily:
		next := nextInOrder(currentHost, players)
		if next == "" {
			return Decision{}
		}
		return Decision{
			NewHost: next,
			Notice:  fmt.Sprintf("%s left the game — %s is now the Host", currentHost, next),
		}
	}
	return Decision{}
}

// nextInOrder walks players starting from the slot after
// currentHost, wrapping around. Returns the first Connected seat
// whose name is not currentHost. If no Connected seat exists,
// falls back to the first non-current seat in join order. Returns
// "" if no non-current seat exists at all.
func nextInOrder(currentHost string, players []Player) string {
	if len(players) == 0 {
		return ""
	}
	start := -1
	for i, p := range players {
		if p.Name == currentHost {
			start = i
			break
		}
	}
	// If currentHost is absent from players (e.g. their seat was
	// already removed), pretend they sat just before index 0 so we
	// scan the whole list once.
	if start < 0 {
		start = len(players) - 1
	}

	n := len(players)
	for offset := 1; offset <= n; offset++ {
		p := players[(start+offset)%n]
		if p.Name == currentHost {
			continue
		}
		if p.Connected {
			return p.Name
		}
	}
	// No Connected seat found; fall back to the first non-current
	// seat encountered in the same join-order scan.
	for offset := 1; offset <= n; offset++ {
		p := players[(start+offset)%n]
		if p.Name != currentHost {
			return p.Name
		}
	}
	return ""
}
