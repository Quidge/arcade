package chain

import (
	"errors"
	"reflect"
	"testing"

	"github.com/quidge/scribble/internal/strokes"
)

func TestInitCreatesOneChainPerSeat(t *testing.T) {
	s := New()
	s.Init([]string{"Alice", "Bob"})
	if got := s.N(); got != 2 {
		t.Errorf("N = %d want 2", got)
	}
	if got := s.Starter(0); got != "Alice" {
		t.Errorf("Starter(0) = %q want Alice", got)
	}
	if got := s.Starter(1); got != "Bob" {
		t.Errorf("Starter(1) = %q want Bob", got)
	}
	if got := s.Len(0); got != 0 {
		t.Errorf("Len(0) = %d want 0", got)
	}
}

func TestAssignmentsForRoundN2(t *testing.T) {
	s := New()
	s.Init([]string{"Alice", "Bob"})
	// Round 0: each player on their own chain.
	if got := s.AssignmentsForRound(0); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("Round 0 = %v want [0 1]", got)
	}
	// Round 1: each player on the other player's chain.
	if got := s.AssignmentsForRound(1); !reflect.DeepEqual(got, []int{1, 0}) {
		t.Errorf("Round 1 = %v want [1 0]", got)
	}
}

func TestAssignmentsForRoundN3(t *testing.T) {
	// N=3 not shipped at this slice, but the rotation rule must be
	// correct for the next slice that unclamps MaxPlayers.
	s := New()
	s.Init([]string{"A", "B", "C"})
	// Each row is round r; entry [p] is chainIdx for seat at pos p.
	cases := []struct {
		round int
		want  []int
	}{
		{0, []int{0, 1, 2}},
		{1, []int{1, 2, 0}},
		{2, []int{2, 0, 1}},
		{3, []int{0, 1, 2}}, // wraps after N rounds.
	}
	for _, c := range cases {
		if got := s.AssignmentsForRound(c.round); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Round %d = %v want %v", c.round, got, c.want)
		}
	}
}

func TestAppendLandsInRotatedChainN2(t *testing.T) {
	s := New()
	s.Init([]string{"Alice", "Bob"})
	// Round 0: Alice and Bob each append to their own Chain.
	if err := s.Append(0, "Alice", Entry{Kind: EntryCaption, Text: "alice starter"}); err != nil {
		t.Fatalf("Append Alice/0: %v", err)
	}
	if err := s.Append(0, "Bob", Entry{Kind: EntryCaption, Text: "bob starter"}); err != nil {
		t.Fatalf("Append Bob/0: %v", err)
	}
	if got := s.Entries(0); len(got) != 1 || got[0].Text != "alice starter" || got[0].Player != "Alice" {
		t.Errorf("Chain 0 = %+v want [alice starter]", got)
	}
	if got := s.Entries(1); len(got) != 1 || got[0].Text != "bob starter" || got[0].Player != "Bob" {
		t.Errorf("Chain 1 = %+v want [bob starter]", got)
	}
	// Round 1: Alice → chain 1 (Bob's), Bob → chain 0 (Alice's).
	if err := s.Append(1, "Alice", Entry{Kind: EntryDrawing, Strokes: []strokes.Stroke{{{X: 0.1, Y: 0.1}}}}); err != nil {
		t.Fatalf("Append Alice/1: %v", err)
	}
	if err := s.Append(1, "Bob", Entry{Kind: EntryDrawing, Strokes: []strokes.Stroke{{{X: 0.2, Y: 0.2}}}}); err != nil {
		t.Fatalf("Append Bob/1: %v", err)
	}
	chain0 := s.Entries(0)
	if len(chain0) != 2 || chain0[1].Player != "Bob" {
		t.Errorf("Chain 0 after Round 1 = %+v", chain0)
	}
	chain1 := s.Entries(1)
	if len(chain1) != 2 || chain1[1].Player != "Alice" {
		t.Errorf("Chain 1 after Round 1 = %+v", chain1)
	}
}

func TestAppendDuplicateRejected(t *testing.T) {
	s := New()
	s.Init([]string{"Alice", "Bob"})
	if err := s.Append(0, "Alice", Entry{Kind: EntryCaption, Text: "first"}); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if err := s.Append(0, "Alice", Entry{Kind: EntryCaption, Text: "second"}); !errors.Is(err, ErrAlreadyAppended) {
		t.Errorf("second Append err = %v want ErrAlreadyAppended", err)
	}
	if got := s.Entries(0); len(got) != 1 || got[0].Text != "first" {
		t.Errorf("Chain 0 after rejected duplicate = %+v", got)
	}
}

func TestAppendUnknownSeatRejected(t *testing.T) {
	s := New()
	s.Init([]string{"Alice", "Bob"})
	if err := s.Append(0, "Charlie", Entry{Kind: EntryCaption, Text: "x"}); !errors.Is(err, ErrUnknownSeat) {
		t.Errorf("Append unknown seat: err=%v want ErrUnknownSeat", err)
	}
}

func TestPromptForRoundZeroNone(t *testing.T) {
	s := New()
	s.Init([]string{"Alice", "Bob"})
	if _, ok := s.PromptFor(0, "Alice"); ok {
		t.Errorf("PromptFor round 0 should return ok=false")
	}
}

func TestPromptForReadsPreviousEntry(t *testing.T) {
	s := New()
	s.Init([]string{"Alice", "Bob"})
	_ = s.Append(0, "Alice", Entry{Kind: EntryCaption, Text: "alice starter"})
	_ = s.Append(0, "Bob", Entry{Kind: EntryCaption, Text: "bob starter"})
	// In round 1, Alice works on chain 1 (Bob's); prompt is chain 1
	// entry 0 = "bob starter".
	got, ok := s.PromptFor(1, "Alice")
	if !ok {
		t.Fatalf("PromptFor round 1 Alice: ok=false")
	}
	if got.Text != "bob starter" {
		t.Errorf("PromptFor round 1 Alice = %q want %q", got.Text, "bob starter")
	}
	// Bob works on chain 0 (Alice's); prompt is "alice starter".
	got, ok = s.PromptFor(1, "Bob")
	if !ok || got.Text != "alice starter" {
		t.Errorf("PromptFor round 1 Bob = %+v ok=%v", got, ok)
	}
}

func TestEntriesIsDefensiveCopy(t *testing.T) {
	s := New()
	s.Init([]string{"Alice", "Bob"})
	_ = s.Append(0, "Alice", Entry{Kind: EntryDrawing, Strokes: []strokes.Stroke{{{X: 0.1, Y: 0.1}}}})
	got := s.Entries(0)
	got[0].Strokes[0][0].X = 0.99
	got2 := s.Entries(0)
	if got2[0].Strokes[0][0].X == 0.99 {
		t.Errorf("Entries returned aliased storage")
	}
}

func TestInitIsIdempotent(t *testing.T) {
	s := New()
	s.Init([]string{"Alice", "Bob"})
	s.Init([]string{"Charlie", "Dave"}) // ignored
	if got := s.Starter(0); got != "Alice" {
		t.Errorf("second Init reseeded seats: Starter(0) = %q want Alice", got)
	}
}
