package web

// revealCursor is the pure FSM that walks the reveal phase per
// ADR 0011. It owns the (chainIdx, entryIdx, mode) tuple and the
// per-Chain lengths it walks against. It knows nothing about
// WebSockets, GameSessions, Players, or driver authorization —
// those are the caller's job. The cursor is reset via Start, paced
// via Advance, observed via Step, and queried for terminal state
// via Done.
//
// Transition rule for N chains with lengths chainLens:
//
//	(chainIdx=0, entryIdx=0,           mode="step")
//	  Advance → (0, 1, "step")
//	  Advance → ...
//	  Advance → (0, chainLens[0]-1, "step")
//	  Advance → (0, chainLens[0]-1, "full")     // whole-Chain view
//	  Advance → (1, 0, "step")
//	  ...
//	  Advance → (N-1, chainLens[N-1]-1, "full")
//	  Advance → (-1, -1, "complete")
//
// At "complete" Step returns the terminal sentinel and Advance is
// a no-op.
type revealCursor struct {
	chainIdx  int
	entryIdx  int
	mode      string
	chainLens []int
}

const (
	revealModeStep     = "step"
	revealModeFull     = "full"
	revealModeComplete = "complete"
)

// newRevealCursor constructs a cursor positioned at the first
// Entry of the first Chain in mode "step". chainLens must have at
// least one entry; an empty chainLens transitions immediately to
// "complete". Each chainLens[i] must be > 0 — empty chains are not
// expected at this slice's scale (every seat contributes exactly
// one Entry per Round; chain lengths equal the number of finalized
// Rounds at reveal time).
func newRevealCursor(chainLens []int) *revealCursor {
	c := &revealCursor{chainLens: append([]int(nil), chainLens...)}
	c.reset()
	return c
}

func (c *revealCursor) reset() {
	if len(c.chainLens) == 0 || c.chainLens[0] <= 0 {
		c.chainIdx = -1
		c.entryIdx = -1
		c.mode = revealModeComplete
		return
	}
	c.chainIdx = 0
	c.entryIdx = 0
	c.mode = revealModeStep
}

// Step returns the cursor's current (chainIdx, entryIdx, mode).
// In mode "complete", chainIdx and entryIdx are -1.
func (c *revealCursor) Step() (chainIdx, entryIdx int, mode string) {
	return c.chainIdx, c.entryIdx, c.mode
}

// Done reports whether the cursor has walked off the last Chain.
func (c *revealCursor) Done() bool {
	return c.mode == revealModeComplete
}

// Advance moves the cursor one tick. The behavior at each state:
//
//   - "step" with more Entries on this Chain: move to next Entry,
//     stay in "step".
//   - "step" at the last Entry of this Chain: stay on the same
//     entry, switch to "full".
//   - "full": move to the next Chain at entryIdx=0 in "step", or
//     to "complete" if this was the last Chain.
//   - "complete": no-op.
func (c *revealCursor) Advance() {
	switch c.mode {
	case revealModeStep:
		if c.entryIdx+1 < c.chainLens[c.chainIdx] {
			c.entryIdx++
			return
		}
		c.mode = revealModeFull
	case revealModeFull:
		if c.chainIdx+1 < len(c.chainLens) {
			c.chainIdx++
			c.entryIdx = 0
			c.mode = revealModeStep
			return
		}
		c.chainIdx = -1
		c.entryIdx = -1
		c.mode = revealModeComplete
	case revealModeComplete:
		// no-op
	}
}
