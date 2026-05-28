package web

import "testing"

// state captures the cursor's observable fields for compact
// assertion.
type cursorState struct {
	ChainIdx int
	EntryIdx int
	Mode     string
}

func step(c *revealCursor) cursorState {
	ci, ei, m := c.Step()
	return cursorState{ChainIdx: ci, EntryIdx: ei, Mode: m}
}

func TestRevealCursorN2TwoEntriesPerChain(t *testing.T) {
	// Two chains of length 2: the documented walk is
	//   step → step → full → step → step → full → complete
	// observed across six advances starting from the initial state.
	c := newRevealCursor([]int{2, 2})

	want := []cursorState{
		{ChainIdx: 0, EntryIdx: 0, Mode: revealModeStep},       // initial
		{ChainIdx: 0, EntryIdx: 1, Mode: revealModeStep},       // after advance 1
		{ChainIdx: 0, EntryIdx: 1, Mode: revealModeFull},       // after advance 2
		{ChainIdx: 1, EntryIdx: 0, Mode: revealModeStep},       // after advance 3
		{ChainIdx: 1, EntryIdx: 1, Mode: revealModeStep},       // after advance 4
		{ChainIdx: 1, EntryIdx: 1, Mode: revealModeFull},       // after advance 5
		{ChainIdx: -1, EntryIdx: -1, Mode: revealModeComplete}, // after advance 6
	}

	if got := step(c); got != want[0] {
		t.Errorf("initial = %+v want %+v", got, want[0])
	}
	for i := 1; i < len(want); i++ {
		c.Advance()
		if got := step(c); got != want[i] {
			t.Errorf("after advance %d = %+v want %+v", i, got, want[i])
		}
	}
	if !c.Done() {
		t.Errorf("Done() = false after full walk")
	}
}

func TestRevealCursorAdvancePastCompleteIsNoop(t *testing.T) {
	c := newRevealCursor([]int{1})
	c.Advance() // step → full
	c.Advance() // full → complete
	c.Advance() // complete → complete (no-op)
	c.Advance()
	if got := step(c); got.Mode != revealModeComplete {
		t.Errorf("advance past complete = %+v", got)
	}
}

func TestRevealCursorEmptyChainsIsComplete(t *testing.T) {
	c := newRevealCursor(nil)
	if !c.Done() {
		t.Errorf("empty cursor not Done")
	}
}

func TestRevealCursorAsymmetricChainLengths(t *testing.T) {
	// Chain 0 has 3 entries; Chain 1 has 1 entry. The walk is
	//   (0,0,step) → (0,1,step) → (0,2,step) → (0,2,full)
	//   → (1,0,step) → (1,0,full) → complete
	c := newRevealCursor([]int{3, 1})
	want := []cursorState{
		{0, 0, revealModeStep},
		{0, 1, revealModeStep},
		{0, 2, revealModeStep},
		{0, 2, revealModeFull},
		{1, 0, revealModeStep},
		{1, 0, revealModeFull},
		{-1, -1, revealModeComplete},
	}
	if got := step(c); got != want[0] {
		t.Errorf("initial = %+v want %+v", got, want[0])
	}
	for i := 1; i < len(want); i++ {
		c.Advance()
		if got := step(c); got != want[i] {
			t.Errorf("after advance %d = %+v want %+v", i, got, want[i])
		}
	}
}
