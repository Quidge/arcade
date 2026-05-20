// Package round is the Round controller: it owns the state of one
// in-progress Round (which seats are active, which have submitted,
// when the timer expires) and fires a callback at Round-end. The
// controller is a pure domain module — no I/O, no WebSocket, no
// JSON, no knowledge of Drafts or Ghosts. The caller (typically
// roundcomplete.Materialize) assembles Entries from the appropriate
// Draft store and the Ghost provider inside its OnEnd callback.
//
// Splitting the Drafts/Ghosts responsibilities out of Config makes
// the controller reusable across Round types (text-Caption,
// strokes-Drawing) without code changes: a Round 1 instance points
// at a strokes Draft store from outside, exactly as Round 0 points
// at a text Draft store.
package round

import (
	"errors"
	"sync"
	"time"
)

// ErrNoActiveRound is returned by Submit, ForceAdvance when no
// Round is currently running.
var ErrNoActiveRound = errors.New("round: no active round")

// ErrAlreadyActive is returned by Start when a Round is already
// in progress and has not yet been finalized.
var ErrAlreadyActive = errors.New("round: a round is already active")

// ErrUnknownSeat is returned by Submit when the named seat is not
// part of the active Round.
var ErrUnknownSeat = errors.New("round: unknown seat for active round")

// EndReason discriminates the three Round-end triggers.
type EndReason int

const (
	// EndAllSubmitted: every seat has explicitly submitted.
	EndAllSubmitted EndReason = iota
	// EndTimerExpired: the deadline elapsed before all seats
	// submitted.
	EndTimerExpired
	// EndForceAdvanced: the Host invoked ForceAdvance.
	EndForceAdvanced
)

// EndCallback is invoked once per Round, after the controller has
// released its lock and reset its in-flight state. The caller is
// expected to assemble Entries from the appropriate Draft store
// and Ghost provider for `seats` in the order given (which is the
// same order as Start), then commit them to whatever Chain or
// reveal accumulator it owns. The callback runs synchronously on
// the goroutine that triggered the end — implementations should
// be quick or hand work off to a goroutine.
type EndCallback func(round int, seats []string, reason EndReason)

// Config wires the controller to its environment.
type Config struct {
	// OnEnd is invoked once at Round-end. Required.
	OnEnd EndCallback
	// Now optionally overrides the controller's wall-clock source.
	// Tests inject a fake clock; production passes nil to use
	// time.Now.
	Now func() time.Time
}

// Controller owns the in-progress Round. Construct via New.
type Controller struct {
	cfg Config

	mu        sync.Mutex
	active    bool
	round     int
	seats     []string
	seatSet   map[string]bool
	submitted map[string]bool
	deadline  time.Time
	timerSet  bool
	timer     *time.Timer
	timerGen  uint64
}

// New constructs a Controller. cfg.OnEnd must be non-nil.
func New(cfg Config) *Controller {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Controller{cfg: cfg}
}

// Start begins a new Round. seats is the ordered list of Player
// names eligible to submit; timerSeconds is the Round timer (0
// means "off" — only all-submitted or ForceAdvance can end the
// Round). Returns the absolute deadline (zero if no timer) and
// ErrAlreadyActive if a Round is already running.
func (c *Controller) Start(round int, seats []string, timerSeconds int) (time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		return time.Time{}, ErrAlreadyActive
	}
	c.active = true
	c.round = round
	c.seats = append(c.seats[:0:0], seats...)
	c.seatSet = make(map[string]bool, len(seats))
	for _, n := range seats {
		c.seatSet[n] = true
	}
	c.submitted = make(map[string]bool, len(seats))
	if timerSeconds > 0 {
		c.timerSet = true
		c.deadline = c.cfg.Now().Add(time.Duration(timerSeconds) * time.Second)
		c.timerGen++
		gen := c.timerGen
		c.timer = time.AfterFunc(time.Duration(timerSeconds)*time.Second, func() {
			c.handleTimerExpire(gen)
		})
	} else {
		c.timerSet = false
		c.deadline = time.Time{}
	}
	return c.deadline, nil
}

// CurrentRound returns the active Round number and whether a
// Round is currently in progress.
func (c *Controller) CurrentRound() (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.round, c.active
}

// Deadline returns the active Round's deadline. ok is true if a
// timer is set; false either because no Round is active or because
// the Host chose "off."
func (c *Controller) Deadline() (deadline time.Time, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active || !c.timerSet {
		return time.Time{}, false
	}
	return c.deadline, true
}

// Active reports whether a Round is currently in progress.
func (c *Controller) Active() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

// HasSeat reports whether player is part of the active Round's
// seat list. False if no Round is active.
func (c *Controller) HasSeat(player string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active && c.seatSet[player]
}

// Submitted reports whether player has already submitted in the
// active Round.
func (c *Controller) Submitted(player string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.submitted[player]
}

// Submit records player's submission. If this submission causes
// every seat to be submitted, the Round ends with EndAllSubmitted.
// A second Submit for the same player is a no-op (idempotent).
// Returns ErrNoActiveRound or ErrUnknownSeat for invalid calls.
//
// Note: the controller no longer touches any Draft store. Callers
// (the web layer) are expected to seal the appropriate Draft for
// (round, player) *before* calling Submit so the snapshot taken
// inside OnEnd matches what the seat last sent.
func (c *Controller) Submit(player string) error {
	c.mu.Lock()
	if !c.active {
		c.mu.Unlock()
		return ErrNoActiveRound
	}
	if !c.seatSet[player] {
		c.mu.Unlock()
		return ErrUnknownSeat
	}
	if c.submitted[player] {
		c.mu.Unlock()
		return nil
	}
	c.submitted[player] = true
	allDone := len(c.submitted) == len(c.seats)
	if !allDone {
		c.mu.Unlock()
		return nil
	}
	c.endAndUnlock(EndAllSubmitted)
	return nil
}

// ForceAdvance ends the current Round immediately. The caller is
// expected to have already verified the actor is the Host; the
// controller does not re-check.
func (c *Controller) ForceAdvance() error {
	c.mu.Lock()
	if !c.active {
		c.mu.Unlock()
		return ErrNoActiveRound
	}
	c.endAndUnlock(EndForceAdvanced)
	return nil
}

// handleTimerExpire is invoked by time.AfterFunc when the deadline
// elapses. The generation token guards against a stale callback
// firing after an earlier all-submitted or force-advance already
// finalized the Round.
func (c *Controller) handleTimerExpire(gen uint64) {
	c.mu.Lock()
	if !c.active || gen != c.timerGen {
		c.mu.Unlock()
		return
	}
	c.endAndUnlock(EndTimerExpired)
}

// endAndUnlock finalizes the active Round, releases c.mu, and
// invokes OnEnd. The caller holds c.mu on entry and must not
// touch c after this call returns — endAndUnlock owns the unlock.
func (c *Controller) endAndUnlock(reason EndReason) {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.timerGen++ // invalidate any in-flight timer closure
	roundNum := c.round
	seats := c.seats
	c.active = false
	c.seats = nil
	c.seatSet = nil
	c.submitted = nil
	c.deadline = time.Time{}
	c.timerSet = false
	c.mu.Unlock()
	c.cfg.OnEnd(roundNum, seats, reason)
}
