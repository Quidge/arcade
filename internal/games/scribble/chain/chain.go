// Package chain is the pure-data home for Chains and their
// Entries (ADR 0011). A Chain belongs to one starting Player (the
// seat at chainIdx in join order) and accumulates one Entry per
// Player over the GameSession's N Rounds. The Store knows nothing
// about WebSockets, GameSessions, drivers, or reveal pacing — it
// is a list-of-lists with seat→chainIdx assignment helpers.
//
// Rotation rule: in Round r, the seat at join-order position p
// works on the Chain whose index is `(p + r) mod N`. At Round 0
// every Player starts their own Chain. The rule is encoded once
// in AssignmentsForRound and exercised at N=2 and N=3 by the
// unit tests — N=3 is not exercised at the slice level but the
// formula must be correct for the next slice that unclamps
// MaxPlayers.
package chain

import (
	"errors"
	"sync"

	"github.com/quidge/arcade/internal/games/scribble/strokes"
)

// EntryKind discriminates the tagged union over Entry content.
type EntryKind int

const (
	// EntryCaption: Text carries the caption.
	EntryCaption EntryKind = iota
	// EntryDrawing: Strokes carries the drawing.
	EntryDrawing
)

// Entry is one finalized contribution to a Chain. Player is the
// seat that contributed it; Ghost is true if the slot was filled
// by the Ghost provider rather than by the seat itself. The
// Kind discriminator selects between Text (Caption) and Strokes
// (Drawing) — exactly one of the two is meaningful per Entry.
type Entry struct {
	Player  string
	Kind    EntryKind
	Ghost   bool
	Text    string
	Strokes []strokes.Stroke
}

// ErrAlreadyAppended is returned by Append when an Entry for the
// same (round, seat) has already landed.
var ErrAlreadyAppended = errors.New("chain: entry already appended for this round and seat")

// ErrUnknownSeat is returned when AssignmentsForRound, PromptFor,
// or Append is called with a seat that was not in the Init roster.
var ErrUnknownSeat = errors.New("chain: seat is not part of this Store")

// Store holds one Chain per seat. The zero value is not usable;
// construct one via New and call Init exactly once before any
// other call.
type Store struct {
	mu sync.Mutex

	seats    []string       // join-order roster captured at Init.
	seatPos  map[string]int // seat name → join-order position.
	chains   [][]Entry      // chains[chainIdx] is the Chain belonging to seats[chainIdx].
	appended map[uint64]bool
	initDone bool
}

// New constructs an empty Store. Init must be called before any
// other method.
func New() *Store {
	return &Store{
		seatPos:  map[string]int{},
		appended: map[uint64]bool{},
	}
}

// Init records the join-order roster for this Store. One Chain
// per seat is created (empty). Calling Init a second time is a
// no-op — once the seats are pinned, the rotation rule is fixed.
func (s *Store) Init(seats []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initDone {
		return
	}
	s.seats = append([]string(nil), seats...)
	s.chains = make([][]Entry, len(seats))
	for i, n := range seats {
		s.seatPos[n] = i
	}
	s.initDone = true
}

// N returns the number of seats / Chains in this Store.
func (s *Store) N() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seats)
}

// Starter returns the display name of the seat that starts the
// Chain at chainIdx (i.e., contributed the Entry at index 0). Out-
// of-range chainIdx returns "".
func (s *Store) Starter(chainIdx int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if chainIdx < 0 || chainIdx >= len(s.seats) {
		return ""
	}
	return s.seats[chainIdx]
}

// Len returns the number of Entries already appended to Chain
// chainIdx. Out-of-range chainIdx returns 0.
func (s *Store) Len(chainIdx int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if chainIdx < 0 || chainIdx >= len(s.chains) {
		return 0
	}
	return len(s.chains[chainIdx])
}

// Entries returns a defensive copy of the Entries on Chain
// chainIdx. Out-of-range chainIdx returns nil.
func (s *Store) Entries(chainIdx int) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if chainIdx < 0 || chainIdx >= len(s.chains) {
		return nil
	}
	return cloneEntries(s.chains[chainIdx])
}

// AssignmentsForRound returns, for each seat in join order, the
// chainIdx that seat works on during round. The rotation rule is
// `(p + round) mod N`, where p is the seat's join-order position.
// The returned slice is indexed by seat position (same order as
// the Init roster).
func (s *Store) AssignmentsForRound(round int) []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.seats)
	if n == 0 {
		return nil
	}
	out := make([]int, n)
	for p := 0; p < n; p++ {
		out[p] = ((p+round)%n + n) % n
	}
	return out
}

// PromptFor returns the Entry the named seat looks at as its
// prompt for round. For round 0 there is no prompt and (Entry{},
// false) is returned. For round >= 1 the prompt is the Entry at
// index round-1 of the Chain assigned to seat for this round.
// If that Entry has not yet been appended, ok is false.
func (s *Store) PromptFor(round int, seat string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if round <= 0 {
		return Entry{}, false
	}
	pos, ok := s.seatPos[seat]
	if !ok {
		return Entry{}, false
	}
	n := len(s.seats)
	if n == 0 {
		return Entry{}, false
	}
	chainIdx := ((pos+round)%n + n) % n
	if len(s.chains[chainIdx]) < round {
		return Entry{}, false
	}
	return cloneEntry(s.chains[chainIdx][round-1]), true
}

// Append records seat's Entry on the Chain assigned to seat for
// round. Returns ErrAlreadyAppended if the same (round, seat)
// has already been appended (idempotency rejection — the round
// controller should only fire OnEnd once, so a second Append is
// a programmer error). Returns ErrUnknownSeat if seat was not in
// the Init roster.
func (s *Store) Append(round int, seat string, entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pos, ok := s.seatPos[seat]
	if !ok {
		return ErrUnknownSeat
	}
	key := uint64(round)<<32 | uint64(uint32(pos))
	if s.appended[key] {
		return ErrAlreadyAppended
	}
	n := len(s.seats)
	chainIdx := ((pos+round)%n + n) % n
	entry.Player = seat
	s.chains[chainIdx] = append(s.chains[chainIdx], cloneEntry(entry))
	s.appended[key] = true
	return nil
}

func cloneEntries(src []Entry) []Entry {
	if len(src) == 0 {
		return nil
	}
	out := make([]Entry, len(src))
	for i, e := range src {
		out[i] = cloneEntry(e)
	}
	return out
}

func cloneEntry(e Entry) Entry {
	out := e
	if len(e.Strokes) > 0 {
		out.Strokes = make([]strokes.Stroke, len(e.Strokes))
		for i, s := range e.Strokes {
			out.Strokes[i] = append(strokes.Stroke(nil), s...)
		}
	}
	return out
}
