package roundcomplete

import (
	"testing"

	"github.com/quidge/arcade/internal/games/scribble/chain"
	"github.com/quidge/arcade/internal/games/scribble/draft"
	"github.com/quidge/arcade/internal/games/scribble/ghost"
	"github.com/quidge/arcade/internal/games/scribble/strokes"
)

func TestContentKindForRound(t *testing.T) {
	cases := []struct {
		round int
		want  string
	}{
		{0, "caption"},
		{1, "drawing"},
		{2, "caption"},
		{3, "drawing"},
		{4, "caption"},
	}
	for _, tc := range cases {
		if got := ContentKindForRound(tc.round); got != tc.want {
			t.Errorf("ContentKindForRound(%d) = %q, want %q", tc.round, got, tc.want)
		}
	}
}

func TestMaterializeCaptions_Round0_EmptyDrafts_GhostFill(t *testing.T) {
	drafts := draft.New()
	picker := ghost.New(0).Picker()
	seats := []string{"alice", "bob"}

	entries := materializeCaptions(0, seats, drafts, picker)

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	for i, e := range entries {
		if e.Player != seats[i] {
			t.Errorf("entry %d Player = %q, want %q", i, e.Player, seats[i])
		}
		if e.Kind != chain.EntryCaption {
			t.Errorf("entry %d Kind = %v, want EntryCaption", i, e.Kind)
		}
		if !e.Ghost {
			t.Errorf("entry %d Ghost = false, want true", i)
		}
		if e.Text == "" {
			t.Errorf("entry %d Ghost Text is empty; Pick should supply text", i)
		}
	}
}

func TestMaterializeCaptions_Round0_NonEmptyDrafts_ShipDraft(t *testing.T) {
	drafts := draft.New()
	drafts.Apply(0, "alice", "alice's caption")
	drafts.Apply(0, "bob", "bob's caption")
	picker := ghost.New(0).Picker()
	seats := []string{"alice", "bob"}

	entries := materializeCaptions(0, seats, drafts, picker)

	wantText := map[string]string{"alice": "alice's caption", "bob": "bob's caption"}
	for i, e := range entries {
		if e.Player != seats[i] || e.Kind != chain.EntryCaption || e.Ghost || e.Text != wantText[seats[i]] {
			t.Errorf("entry %d = %+v, want non-ghost Caption with text %q", i, e, wantText[seats[i]])
		}
	}
}

func TestMaterializeCaptions_Round0_Mixed(t *testing.T) {
	drafts := draft.New()
	drafts.Apply(0, "alice", "alice wrote something")
	// bob's draft stays empty
	picker := ghost.New(0).Picker()
	seats := []string{"alice", "bob"}

	entries := materializeCaptions(0, seats, drafts, picker)

	if entries[0].Ghost || entries[0].Text != "alice wrote something" {
		t.Errorf("alice entry = %+v, want non-ghost with her text", entries[0])
	}
	if !entries[1].Ghost || entries[1].Text == "" {
		t.Errorf("bob entry = %+v, want ghost with non-empty text", entries[1])
	}
}

// TestMaterializeCaptions_Round2_GuessCaptionKindFires verifies
// the roundNum > 0 branch in materializeCaptions: at Round 2 the
// Ghost slot kind passed to Pick is GuessCaption, not
// StarterCaption. We assert this indirectly by reusing one Picker
// across both Rounds and checking the returned Ghost texts differ
// — at R0 Pick returns from the StarterCaption library; at R2 it
// returns from the (currently empty) GuessCaption library and
// falls back to the package's generic fallback string. Either way
// the texts must diverge if the kind branch fires correctly.
func TestMaterializeCaptions_Round2_GuessCaptionKindFires(t *testing.T) {
	drafts := draft.New()
	picker := ghost.New(0).Picker()

	r0 := materializeCaptions(0, []string{"alice"}, drafts, picker)
	r2 := materializeCaptions(2, []string{"bob"}, drafts, picker)

	if !r0[0].Ghost || !r2[0].Ghost {
		t.Fatalf("expected ghost entries; got r0=%+v r2=%+v", r0[0], r2[0])
	}
	if r0[0].Text == r2[0].Text {
		t.Errorf("expected R0 (StarterCaption) and R2 (GuessCaption) Ghost text to differ; both = %q", r0[0].Text)
	}
}

func TestMaterializeCaptions_Round2_NonEmptyDraftsShipUnchanged(t *testing.T) {
	drafts := draft.New()
	drafts.Apply(2, "alice", "a guess at what was drawn")
	picker := ghost.New(0).Picker()

	entries := materializeCaptions(2, []string{"alice"}, drafts, picker)

	e := entries[0]
	if e.Player != "alice" || e.Kind != chain.EntryCaption || e.Ghost || e.Text != "a guess at what was drawn" {
		t.Errorf("entry = %+v, want non-ghost Caption with the supplied text", e)
	}
}

func TestMaterializeDrawings_EmptyDrafts_GhostFill(t *testing.T) {
	drafts := strokes.New()
	picker := ghost.New(0).Picker()
	seats := []string{"alice", "bob"}

	entries := materializeDrawings(1, seats, drafts, picker)

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	for i, e := range entries {
		if e.Player != seats[i] {
			t.Errorf("entry %d Player = %q, want %q", i, e.Player, seats[i])
		}
		if e.Kind != chain.EntryDrawing {
			t.Errorf("entry %d Kind = %v, want EntryDrawing", i, e.Kind)
		}
		if !e.Ghost {
			t.Errorf("entry %d Ghost = false, want true", i)
		}
		if len(e.Strokes) == 0 {
			t.Errorf("entry %d Strokes is empty; PickDrawing should supply", i)
		}
	}
}

func TestMaterializeDrawings_NonEmptyDrafts_ShipDraft(t *testing.T) {
	drafts := strokes.New()
	alice := strokes.Drawing{{{X: 0.1, Y: 0.1}, {X: 0.9, Y: 0.9}}}
	drafts.Apply(1, "alice", alice)
	picker := ghost.New(0).Picker()

	entries := materializeDrawings(1, []string{"alice"}, drafts, picker)

	if entries[0].Ghost {
		t.Errorf("expected non-ghost entry, got %+v", entries[0])
	}
	if entries[0].Kind != chain.EntryDrawing {
		t.Errorf("Kind = %v, want EntryDrawing", entries[0].Kind)
	}
	if len(entries[0].Strokes) != 1 || len(entries[0].Strokes[0]) != 2 {
		t.Errorf("Strokes shape unexpected: %+v", entries[0].Strokes)
	}
}

func TestMaterializeDrawings_Mixed(t *testing.T) {
	drafts := strokes.New()
	drafts.Apply(1, "alice", strokes.Drawing{{{X: 0.0, Y: 0.0}, {X: 1.0, Y: 1.0}}})
	// bob's stroke draft stays empty
	picker := ghost.New(0).Picker()

	entries := materializeDrawings(1, []string{"alice", "bob"}, drafts, picker)

	if entries[0].Ghost {
		t.Errorf("alice entry = %+v, want non-ghost", entries[0])
	}
	if !entries[1].Ghost || len(entries[1].Strokes) == 0 {
		t.Errorf("bob entry = %+v, want ghost with strokes", entries[1])
	}
}

func TestMaterialize_DispatchesByParity(t *testing.T) {
	textDrafts := draft.New()
	strokeDrafts := strokes.New()
	picker := ghost.New(0).Picker()
	seats := []string{"alice"}

	r0 := Materialize(0, seats, textDrafts, strokeDrafts, picker)
	r1 := Materialize(1, seats, textDrafts, strokeDrafts, picker)

	if r0[0].Kind != chain.EntryCaption {
		t.Errorf("R0 Kind = %v, want EntryCaption", r0[0].Kind)
	}
	if r1[0].Kind != chain.EntryDrawing {
		t.Errorf("R1 Kind = %v, want EntryDrawing", r1[0].Kind)
	}
}

func TestMaterialize_EmptySeats(t *testing.T) {
	textDrafts := draft.New()
	strokeDrafts := strokes.New()
	picker := ghost.New(0).Picker()

	if got := Materialize(0, nil, textDrafts, strokeDrafts, picker); len(got) != 0 {
		t.Errorf("R0 empty seats: got %d entries, want 0", len(got))
	}
	if got := Materialize(1, nil, textDrafts, strokeDrafts, picker); len(got) != 0 {
		t.Errorf("R1 empty seats: got %d entries, want 0", len(got))
	}
}
