// Package draft is the per-Player-per-Round Draft store. It owns
// the mutable in-progress input each Player streams over WebSocket
// during a Round and exposes a small surface the Round controller
// can drive at Round-end.
//
// Keys are (Round, Player). A Player who Disconnects mid-Round and
// then Reconnects finds the same buffer under the same key; the
// store is therefore an in-memory mirror of "what the server has
// most recently heard from this seat about this Round." Empty
// drafts (zero characters) flow through to Ghost fill at Round-end
// per ADR 0003; any input at all ships as-is.
//
// Submission is one-way per CONTEXT.md: once a (Round, Player)
// entry is submitted, Apply is rejected for that key.
package draft

import "sync"

// Key uniquely identifies a Draft buffer within the store.
type Key struct {
	Round  int
	Player string
}

// Snapshot is the immutable view returned by Get. Text is the most
// recent Draft text the server has accepted; Submitted is true if
// the Player has explicitly submitted.
type Snapshot struct {
	Text      string
	Submitted bool
}

// Store holds in-progress Drafts. The zero value is not usable;
// construct one via New.
type Store struct {
	mu      sync.Mutex
	entries map[Key]Snapshot
}

// New constructs an empty Store.
func New() *Store {
	return &Store{entries: map[Key]Snapshot{}}
}

// Apply accepts a Draft update for (round, player). text is the
// full Draft snapshot per ADR 0003 (no diff). If the entry has
// already been Submitted, the update is rejected and Apply returns
// false; submission is one-way.
func (s *Store) Apply(round int, player, text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := Key{Round: round, Player: player}
	cur := s.entries[k]
	if cur.Submitted {
		return false
	}
	cur.Text = text
	s.entries[k] = cur
	return true
}

// Submit marks (round, player) as submitted and returns the
// snapshot taken under the lock. Repeated Submit calls are
// idempotent: the snapshot returned on a second Submit is the
// snapshot that was sealed by the first call. ok is true if the
// key existed (or was created) by the time the call returned.
func (s *Store) Submit(round int, player string) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := Key{Round: round, Player: player}
	cur := s.entries[k]
	cur.Submitted = true
	s.entries[k] = cur
	return cur, true
}

// Get returns the current snapshot for (round, player), or the
// zero Snapshot if no entry exists.
func (s *Store) Get(round int, player string) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[Key{Round: round, Player: player}]
}

// IsEmpty reports whether the Draft for (round, player) has zero
// characters. A submitted-but-empty Draft still counts as empty —
// Ghost fill is driven by content, not by submission state.
func (s *Store) IsEmpty(round int, player string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[Key{Round: round, Player: player}].Text == ""
}
