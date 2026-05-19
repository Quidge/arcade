// Package round is the Round controller: it owns the state of one
// in-progress Round (which seats are active, which have submitted,
// when the timer expires) and drives Round-end finalization. The
// controller is a pure domain module — no I/O, no WebSocket, no
// JSON. It cooperates with the Draft store and Ghost provider, both
// passed in at construction.
//
// Round 0 (issue #8) only ever runs one Round at a time; the
// controller's surface is shaped so subsequent slices that add
// Rounds 1..N can drive successive Starts on the same instance
// without rework.
package round

import (
	"errors"
	"sync"
	"time"

	"github.com/quidge/scribble/internal/draft"
	"github.com/quidge/scribble/internal/ghost"
)

// ErrNoActiveRound is returned by Submit, ForceAdvance, and
// Apply when no Round is currently running.
var ErrNoActiveRound = errors.New("round: no active round")

// ErrAlreadyActive is returned by Start when a Round is already
// in progress and has not yet been finalized.
var ErrAlreadyActive = errors.New("round: a round is already active")

// ErrUnknownSeat is returned by Submit when the named seat is not
// part of the active Round.
var ErrUnknownSeat = errors.New("round: unknown seat for active round")

// EndReason discriminates the three Round-end triggers. Consumers
// may surface this in notices or wire payloads; the controller
// itself treats them identically once any of them fires.
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

// Entry is one finalized contribution to a Chain at Round-end.
// Text is the Draft (or canned Ghost content) and Ghost is true
// when the slot was filled by the Ghost provider because the
// Player's Draft was empty.
type Entry struct {
	Player string
	Text   string
	Ghost  bool
}

// EndCallback is invoked once per Round, after finalization, with
// the resolved Entries (one per seat, in seat order) and the
// EndReason. The callback runs synchronously inside the controller
// — implementations should be quick or hand work off to a
// goroutine.
type EndCallback func(round int, entries []Entry, reason EndReason)

// Config wires the controller to its collaborators.
type Config struct {
	// Drafts is the Draft store the controller reads at
	// finalization to extract each seat's text.
	Drafts *draft.Store
	// Ghosts is the Ghost provider used to fill empty slots.
	Ghosts *ghost.Provider
	// OnEnd is invoked once at Round-end with the resolved
	// Entries. Required.
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

// New constructs a Controller. cfg.Drafts, cfg.Ghosts, and cfg.OnEnd
// must be non-nil.
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
	// Seal the Draft store entry — this snapshots the buffer
	// under the store's lock and rejects any further Apply for
	// this (round, player).
	_, _ = c.cfg.Drafts.Submit(c.round, player)
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
	entries := c.finalize(roundNum, seats)
	c.cfg.OnEnd(roundNum, entries, reason)
}

// finalize collects the Entry list for the Round identified by
// roundNum. Each seat with a non-empty Draft contributes its text
// as-is (ADR 0003, "any input at all ships as-is"); each seat with
// an empty Draft gets a Ghost-supplied Entry. Best-effort anti-
// collision is scoped to this Round-end via a single Picker.
func (c *Controller) finalize(roundNum int, seats []string) []Entry {
	out := make([]Entry, 0, len(seats))
	picker := c.cfg.Ghosts.Picker()
	for _, name := range seats {
		snap := c.cfg.Drafts.Get(roundNum, name)
		if snap.Text == "" {
			out = append(out, Entry{
				Player: name,
				Text:   picker.Pick(name, ghost.StarterCaption),
				Ghost:  true,
			})
			continue
		}
		out = append(out, Entry{
			Player: name,
			Text:   snap.Text,
			Ghost:  false,
		})
	}
	return out
}
