// Package strokes is the per-Player-per-Round Draft store for
// visual Entries. It is a sibling of internal/draft: identical
// lifecycle (apply-full-replace per update, seal on Submit,
// snapshot on Get), but stores a Drawing (ordered list of strokes)
// instead of a string.
//
// Full-replace on the wire is ADR 0010. Each Apply carries the
// entire current stroke list; the server replaces wholesale. A
// Player who Disconnects mid-Round and then Reconnects finds the
// same Drawing under the same key.
package strokes

import "sync"

// Point is a single sample on a stroke in normalized 0..1 canvas
// coordinates. The renderer (MVP) draws polylines through these
// points at a single fixed brush width.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Stroke is a polyline captured between one pointer-down and the
// next pointer-up. The empty stroke (zero points) is permitted on
// the wire — the store does not reject it — but renderers may
// ignore it.
type Stroke []Point

// Drawing is the ordered list of strokes that make up one Entry's
// visual content.
type Drawing []Stroke

// Key uniquely identifies a Drawing buffer within the store.
type Key struct {
	Round  int
	Player string
}

// Snapshot is the immutable view returned by Get.
type Snapshot struct {
	Strokes   Drawing
	Submitted bool
}

// Store holds in-progress Drawings. The zero value is not usable;
// construct one via New.
type Store struct {
	mu      sync.Mutex
	entries map[Key]Snapshot
}

// New constructs an empty Store.
func New() *Store {
	return &Store{entries: map[Key]Snapshot{}}
}

// Apply accepts a Drawing update for (round, player). strokes is
// the full Drawing snapshot per ADR 0010. If the entry has
// already been Submitted, the update is rejected and Apply returns
// false; submission is one-way.
func (s *Store) Apply(round int, player string, drawing Drawing) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := Key{Round: round, Player: player}
	cur := s.entries[k]
	if cur.Submitted {
		return false
	}
	// Defensive copy so callers cannot mutate the stored value
	// after handing it to Apply.
	cur.Strokes = cloneDrawing(drawing)
	s.entries[k] = cur
	return true
}

// Submit marks (round, player) as submitted and returns the
// snapshot taken under the lock. Repeated Submit calls are
// idempotent.
func (s *Store) Submit(round int, player string) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := Key{Round: round, Player: player}
	cur := s.entries[k]
	cur.Submitted = true
	s.entries[k] = cur
	return snapshotCopy(cur), true
}

// Get returns the current snapshot for (round, player), or the
// zero Snapshot if no entry exists. The returned snapshot is a
// defensive copy.
func (s *Store) Get(round int, player string) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return snapshotCopy(s.entries[Key{Round: round, Player: player}])
}

// IsEmpty reports whether the Drawing for (round, player) has zero
// strokes. A submitted-but-empty Drawing still counts as empty —
// Ghost fill is driven by content, not by submission state.
func (s *Store) IsEmpty(round int, player string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries[Key{Round: round, Player: player}].Strokes) == 0
}

func cloneDrawing(d Drawing) Drawing {
	if len(d) == 0 {
		return nil
	}
	out := make(Drawing, len(d))
	for i, s := range d {
		out[i] = append(Stroke(nil), s...)
	}
	return out
}

func snapshotCopy(s Snapshot) Snapshot {
	return Snapshot{Strokes: cloneDrawing(s.Strokes), Submitted: s.Submitted}
}
