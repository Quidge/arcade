// Package gamesession holds the lobby-stage domain: a Registry of
// running GameSessions and the per-session Players + Events surface.
//
// A Player is a persistent seat: their record survives WebSocket
// disconnects and is only removed by Leave (Host kick, voluntary
// quit, or the GameSession ending). A separate Connected flag on
// each Player tracks whether their WebSocket is currently alive.
// The domain exposes four seat verbs — Join, Reconnect, Disconnect,
// Leave — and one Host-mobility verb — TransferHost. See ADR 0008
// (seat persistence) and ADR 0005 (Host promotion).
//
// Host auto-migration on Disconnect is also a domain concern: when
// the current Host's seat flips to Connected=false, a grace timer
// starts. If the timer elapses with the Host still disconnected,
// the Host badge migrates per the rules in package hostpromote.
//
// This package is wire-format-agnostic. The verbs produce typed
// Events on the channel returned by GameSession.Events(); the web
// package owns the JSON serialization of those events.
//
// State is in-memory; persistence is deferred to a later slice.
package gamesession

import (
	"errors"
	"sync"
	"time"

	"github.com/quidge/scribble/internal/hostpromote"
	"github.com/quidge/scribble/internal/joincode"
)

// MaxPlayers is the hard cap on Players per GameSession. The cap
// counts seats, connected or not: a disconnected seat blocks a
// would-be 9th joiner.
const MaxPlayers = 8

// DefaultHostGraceDuration is the wait between a Host's Disconnect
// and the auto-migration of the Host badge. ADR 0005 fixes this at
// 15 seconds for production; tests inject a shorter value via
// WithHostGraceDuration.
const DefaultHostGraceDuration = 15 * time.Second

// ErrSeatExists is returned by Join when a seat with the requested
// display name already exists. Callers should dispatch to Reconnect
// in that case.
var ErrSeatExists = errors.New("gamesession: seat with that display name already exists")

// ErrCapExceeded is returned by Join when the GameSession already
// holds MaxPlayers seats.
var ErrCapExceeded = errors.New("gamesession: game session is full")

// ErrNotSeated is returned by Reconnect, Leave, and TransferHost
// when the named seat does not exist.
var ErrNotSeated = errors.New("gamesession: no seat with that display name")

// ErrNotHost is returned by TransferHost when the caller is not
// the current Host.
var ErrNotHost = errors.New("gamesession: caller is not the Host")

// ErrSelfTransfer is returned by TransferHost when from == target.
var ErrSelfTransfer = errors.New("gamesession: cannot transfer Host to self")

// ErrInvalidPhase is returned by Start when the GameSession is not
// in the lobby, or by AdvanceFromRound when no Round is active.
var ErrInvalidPhase = errors.New("gamesession: action not valid in current phase")

// State discriminates the GameSession's coarse phase. Started at
// StateLobby; transitions one-way via Start (→ StateRoundActive)
// and AdvanceFromRound (→ StateRoundComplete). Future slices will
// transition StateRoundComplete back to a new StateRoundActive for
// Rounds 1..N.
type State int

const (
	// StateLobby is the pre-Start phase. Seats can be created or
	// removed via Join/Leave; Host may set the Round timer; the
	// Round controller is dormant.
	StateLobby State = iota
	// StateRoundActive means a Round is in progress. Per ADR 0009,
	// Leave and Kick during this phase collapse to Disconnect.
	StateRoundActive
	// StateRoundComplete is the post-finalization phase for the
	// current Round, before the next Round (or reveal) begins.
	// Issue #8 ends here pending multi-Round slices.
	StateRoundComplete
)

// Player is the public roster entry. Host is true for the seat that
// currently holds the host badge (at most one at a time). Connected
// is true while a live WebSocket is bound to the seat.
type Player struct {
	Name      string `json:"name"`
	Host      bool   `json:"host"`
	Connected bool   `json:"connected"`
}

// EventKind discriminates the seat/connection/host transitions that
// the domain emits.
type EventKind int

const (
	// PlayerJoined fires once per successful Join (new seat created).
	PlayerJoined EventKind = iota
	// PlayerDisconnected fires when a seated Player's live connection
	// is dropped. The seat itself is preserved.
	PlayerDisconnected
	// PlayerReconnected fires when a previously disconnected seat
	// gets a fresh live connection. Does not fire on a same-state
	// no-op (already-connected seat re-bound by the web layer's
	// supersede flow).
	PlayerReconnected
	// PlayerLeft fires when a seat is removed via Leave (Host kick
	// or voluntary "leave game"). The Player field carries the
	// last-known state of the seat before removal; Roster reflects
	// the post-removal state.
	PlayerLeft
	// HostChanged fires when the Host badge moves — voluntary
	// transfer, auto-migrate on grace expiry, or voluntary Leave by
	// the current Host. Player is the new Host. Notice carries the
	// human-readable broadcast text.
	HostChanged
)

// Event is the transition record emitted on the domain verbs.
// Roster is a snapshot captured under the GameSession lock at the
// time the transition was applied, so consumers see a consistent
// view without re-locking. Notice is set only for HostChanged and
// carries the engine-produced broadcast text.
type Event struct {
	Kind   EventKind
	Player Player
	Roster []Player
	Notice string
}

// GameSession is one in-progress lobby. The zero value is not
// usable; callers should obtain a GameSession via Registry.Create.
type GameSession struct {
	code string

	mu      sync.Mutex
	players map[string]Player
	order   []string // join order; stable across Disconnect/Reconnect

	// state is the current coarse phase. Guarded by mu.
	state State
	// roundNum is the active Round number when state is
	// StateRoundActive or StateRoundComplete. Zero in StateLobby.
	roundNum int

	// hostGraceDuration is copied from the Registry at Create time.
	hostGraceDuration time.Duration
	// hostGraceTimer is the live grace timer for the current Host,
	// or nil if no timer is running. Guarded by mu.
	hostGraceTimer *time.Timer
	// hostGraceGen is bumped on every start/cancel so a fired-but-
	// not-yet-run timer closure can detect it was superseded.
	hostGraceGen uint64

	events chan Event
}

// Code returns the canonical 6-character join code for this
// GameSession.
func (g *GameSession) Code() string { return g.code }

// Phase returns the current State and active Round number. The
// Round number is meaningful only in StateRoundActive and
// StateRoundComplete; in StateLobby it is zero.
func (g *GameSession) Phase() (State, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state, g.roundNum
}

// Start transitions the GameSession from StateLobby into Round 0
// (StateRoundActive). caller must be the current Host. Returns
// ErrNotHost if the caller is not the Host, or ErrInvalidPhase if
// the GameSession is not in the lobby. The timer duration is held
// outside this domain (the Round controller owns it); Start carries
// it here only as a forward-compatibility hook for tests/callers
// that want to validate phase + Host atomically.
//
// On success, no Event is emitted on the session's channel —
// callers wire the Round controller separately to broadcast the
// round-start payload.
func (g *GameSession) Start(caller string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != StateLobby {
		return ErrInvalidPhase
	}
	if g.currentHostLocked() != caller {
		return ErrNotHost
	}
	g.state = StateRoundActive
	g.roundNum = 0
	return nil
}

// AdvanceFromRound transitions the GameSession out of an active
// Round. Round 0's terminal target is StateRoundComplete; future
// slices will retarget this to the next StateRoundActive.
// Returns ErrInvalidPhase if no Round is active.
//
// Callers (the web layer) invoke this from the Round controller's
// OnEnd callback, after the Round has been finalized — the rule
// "no Round-end trigger has fired" maps directly to "state was not
// StateRoundActive when AdvanceFromRound was called."
func (g *GameSession) AdvanceFromRound() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != StateRoundActive {
		return ErrInvalidPhase
	}
	g.state = StateRoundComplete
	return nil
}

// Events returns the read end of the event channel. The channel
// is lightly buffered (enough to hold the back-to-back emits of a
// single verb, e.g. PlayerLeft + HostChanged) and the domain
// sends non-blockingly, so slow consumers drop events rather than
// stalling the verbs.
//
// Callers must consume promptly — only the live state in Roster()
// is authoritative; events are a notification stream.
func (g *GameSession) Events() <-chan Event { return g.events }

// Roster returns the current Players, in join order. The returned
// slice is a fresh copy; callers may mutate it freely.
func (g *GameSession) Roster() []Player {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.rosterLocked()
}

// HasSeat reports whether a seat with the given display name exists
// in the GameSession, regardless of its connection state. The web
// layer uses this to dispatch a fresh WebSocket upgrade to either
// Join or Reconnect, but races between HasSeat and Join/Reconnect
// are tolerated: Join returns ErrSeatExists if a seat appeared
// between the check and the call.
func (g *GameSession) HasSeat(name string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.players[name]
	return ok
}

func (g *GameSession) rosterLocked() []Player {
	out := make([]Player, 0, len(g.order))
	for _, name := range g.order {
		out = append(out, g.players[name])
	}
	return out
}

// promotionInputLocked translates the roster to the engine's input
// shape.
func (g *GameSession) promotionInputLocked() []hostpromote.Player {
	out := make([]hostpromote.Player, 0, len(g.order))
	for _, name := range g.order {
		p := g.players[name]
		out = append(out, hostpromote.Player{Name: p.Name, Connected: p.Connected})
	}
	return out
}

// currentHostLocked returns the name of the current Host, or ""
// if no seat currently holds the badge.
func (g *GameSession) currentHostLocked() string {
	for _, name := range g.order {
		if g.players[name].Host {
			return name
		}
	}
	return ""
}

// Join atomically creates a new seat with the given name and binds
// it to a live connection (Connected=true). The first seat in an
// empty GameSession receives Host=true. Returns ErrSeatExists if a
// seat with the name already exists (caller should dispatch to
// Reconnect) or ErrCapExceeded if the GameSession already holds
// MaxPlayers seats. On success, a PlayerJoined event is emitted
// with a roster snapshot.
func (g *GameSession) Join(name string) (*Player, error) {
	g.mu.Lock()
	if _, exists := g.players[name]; exists {
		g.mu.Unlock()
		return nil, ErrSeatExists
	}
	if len(g.players) >= MaxPlayers {
		g.mu.Unlock()
		return nil, ErrCapExceeded
	}
	p := Player{Name: name, Host: len(g.players) == 0, Connected: true}
	g.players[name] = p
	g.order = append(g.order, name)
	snapshot := g.rosterLocked()
	g.mu.Unlock()

	g.emit(Event{Kind: PlayerJoined, Player: p, Roster: snapshot})
	return &p, nil
}

// Reconnect re-binds an existing seat to a live connection. If the
// seat was previously Connected=false, it flips to true and a
// PlayerReconnected event is emitted. If this seat held the Host
// badge and a Host-grace timer was running, the timer is canceled
// so the auto-migration does not fire. If the seat is already
// Connected=true, Reconnect is a no-op at the domain level — the
// web layer's seatconn registry handles the supersede semantics.
// Returns ErrNotSeated if no seat with the name exists.
func (g *GameSession) Reconnect(name string) (*Player, error) {
	g.mu.Lock()
	p, ok := g.players[name]
	if !ok {
		g.mu.Unlock()
		return nil, ErrNotSeated
	}
	if p.Connected {
		g.mu.Unlock()
		return &p, nil
	}
	p.Connected = true
	g.players[name] = p
	if p.Host {
		g.cancelHostGraceLocked()
	}
	snapshot := g.rosterLocked()
	g.mu.Unlock()

	g.emit(Event{Kind: PlayerReconnected, Player: p, Roster: snapshot})
	return &p, nil
}

// Disconnect marks the named seat as Connected=false and emits a
// PlayerDisconnected event. The seat, its Host status, and its
// position in the join order are preserved. If the disconnecting
// seat is the current Host, the auto-migrate grace timer starts.
// If no seat with the name exists, or the seat is already
// Connected=false, Disconnect is a no-op (no event).
func (g *GameSession) Disconnect(name string) {
	g.mu.Lock()
	p, ok := g.players[name]
	if !ok || !p.Connected {
		g.mu.Unlock()
		return
	}
	p.Connected = false
	g.players[name] = p
	if p.Host {
		g.startHostGraceLocked(name)
	}
	snapshot := g.rosterLocked()
	g.mu.Unlock()

	g.emit(Event{Kind: PlayerDisconnected, Player: p, Roster: snapshot})
}

// Leave removes the named seat from the GameSession in the lobby
// phase. Post-Start (per ADR 0009), Leave collapses to Disconnect:
// the seat persists, Connected flips to false, and the existing
// Disconnect path (including Host-grace migration if the leaver
// was Host) takes over. PlayerLeft fires only in the lobby case.
//
// In the lobby, if the leaving seat held the Host badge, the
// promotion engine selects a new Host (skipping currently-
// disconnected seats per ADR 0005) and HostChanged fires in
// addition to PlayerLeft. Returns ErrNotSeated if no such seat
// exists.
func (g *GameSession) Leave(name string) error {
	g.mu.Lock()
	seat, ok := g.players[name]
	if !ok {
		g.mu.Unlock()
		return ErrNotSeated
	}
	// Post-Start: collapse to Disconnect per ADR 0009. The seat
	// stays, Connected flips, Ghost picks up empty Drafts at
	// Round-end. Already-disconnected seats are a no-op (matches
	// Disconnect's existing semantics).
	if g.state != StateLobby {
		if !seat.Connected {
			g.mu.Unlock()
			return nil
		}
		seat.Connected = false
		g.players[name] = seat
		if seat.Host {
			g.startHostGraceLocked(name)
		}
		snapshot := g.rosterLocked()
		g.mu.Unlock()
		g.emit(Event{Kind: PlayerDisconnected, Player: seat, Roster: snapshot})
		return nil
	}
	wasHost := seat.Host

	// Clear the badge first (the leaving Host must not carry the
	// badge into removal — see issue #7 acceptance criteria).
	if wasHost {
		seat.Host = false
		g.players[name] = seat
		// The leaving seat is the Host whose disconnect timer (if
		// any) is now moot — they're not absent, they're gone.
		g.cancelHostGraceLocked()
	}

	// Compute the new Host (if needed) before removing the seat,
	// so the engine sees the same join-order view that the seat
	// was sitting in.
	var decision hostpromote.Decision
	if wasHost {
		decision = hostpromote.Decide(
			name,
			g.promotionInputLocked(),
			hostpromote.HostLeftVoluntarily,
			"",
		)
	}

	// Remove the seat from the map and the join order.
	delete(g.players, name)
	for i, n := range g.order {
		if n == name {
			g.order = append(g.order[:i], g.order[i+1:]...)
			break
		}
	}

	// Apply the new Host badge.
	if wasHost && decision.NewHost != "" {
		newHost := g.players[decision.NewHost]
		newHost.Host = true
		g.players[decision.NewHost] = newHost
	}

	left := Player{Name: name, Host: false, Connected: seat.Connected}
	snapshot := g.rosterLocked()

	// Resolve the new-Host player for the HostChanged event under
	// the lock so the snapshot and the named seat agree.
	var newHostSeat Player
	hasHostChange := wasHost && decision.NewHost != ""
	if hasHostChange {
		newHostSeat = g.players[decision.NewHost]
	}
	notice := decision.Notice
	g.mu.Unlock()

	g.emit(Event{Kind: PlayerLeft, Player: left, Roster: snapshot})
	if hasHostChange {
		g.emit(Event{Kind: HostChanged, Player: newHostSeat, Roster: snapshot, Notice: notice})
	}
	return nil
}

// TransferHost atomically moves the Host badge from the current
// holder to target. from must equal the current Host name; target
// must be an existing seat distinct from from. Emits HostChanged
// on success. Returns ErrNotHost, ErrSelfTransfer, or ErrNotSeated
// as appropriate; no event is emitted on error.
func (g *GameSession) TransferHost(from, target string) error {
	g.mu.Lock()
	if from == target {
		g.mu.Unlock()
		return ErrSelfTransfer
	}
	cur := g.currentHostLocked()
	if cur != from {
		g.mu.Unlock()
		return ErrNotHost
	}
	if _, ok := g.players[target]; !ok {
		g.mu.Unlock()
		return ErrNotSeated
	}

	decision := hostpromote.Decide(
		from,
		g.promotionInputLocked(),
		hostpromote.VoluntaryTransfer,
		target,
	)
	if decision.NewHost == "" {
		// Engine rejected the transfer (shouldn't happen given the
		// pre-checks above, but be defensive).
		g.mu.Unlock()
		return ErrNotSeated
	}

	oldHost := g.players[from]
	oldHost.Host = false
	g.players[from] = oldHost
	newHost := g.players[decision.NewHost]
	newHost.Host = true
	g.players[decision.NewHost] = newHost

	// Any Host-grace timer was for the previous Host; the badge
	// has moved, so cancel it.
	g.cancelHostGraceLocked()

	snapshot := g.rosterLocked()
	g.mu.Unlock()

	g.emit(Event{Kind: HostChanged, Player: newHost, Roster: snapshot, Notice: decision.Notice})
	return nil
}

// startHostGraceLocked starts (or restarts) the auto-migration
// grace timer for the named seat. Callers hold g.mu.
func (g *GameSession) startHostGraceLocked(name string) {
	if g.hostGraceTimer != nil {
		g.hostGraceTimer.Stop()
	}
	g.hostGraceGen++
	gen := g.hostGraceGen
	g.hostGraceTimer = time.AfterFunc(g.hostGraceDuration, func() {
		g.handleHostGraceExpire(name, gen)
	})
}

// cancelHostGraceLocked stops the in-flight grace timer (if any)
// and bumps the generation so a closure that has already started
// running but has not yet entered handleHostGraceExpire sees it is
// stale. Callers hold g.mu.
func (g *GameSession) cancelHostGraceLocked() {
	if g.hostGraceTimer != nil {
		g.hostGraceTimer.Stop()
		g.hostGraceTimer = nil
	}
	g.hostGraceGen++
}

// handleHostGraceExpire is invoked by time.AfterFunc when the
// grace window elapses. It re-checks all state under the lock —
// the timer may have fired after a Reconnect, Leave, or transfer
// supersedes it — and migrates the Host badge if the state still
// warrants it.
func (g *GameSession) handleHostGraceExpire(name string, gen uint64) {
	g.mu.Lock()
	if gen != g.hostGraceGen {
		// Stale fire: a later start/cancel superseded this timer.
		g.mu.Unlock()
		return
	}
	g.hostGraceTimer = nil
	g.hostGraceGen++

	seat, ok := g.players[name]
	if !ok || seat.Connected || !seat.Host {
		// State changed between fire and acquiring the lock.
		g.mu.Unlock()
		return
	}

	decision := hostpromote.Decide(
		name,
		g.promotionInputLocked(),
		hostpromote.DisconnectGraceExpired,
		"",
	)
	if decision.NewHost == "" || decision.NewHost == name {
		g.mu.Unlock()
		return
	}

	seat.Host = false
	g.players[name] = seat
	newSeat := g.players[decision.NewHost]
	newSeat.Host = true
	g.players[decision.NewHost] = newSeat

	snapshot := g.rosterLocked()
	notice := decision.Notice
	g.mu.Unlock()

	g.emit(Event{Kind: HostChanged, Player: newSeat, Roster: snapshot, Notice: notice})
}

// emit pushes an event non-blockingly. If no consumer is parked on
// the receive side, the event is dropped — the wire layer is
// expected to reconcile against Roster() rather than rely on a
// complete event log.
func (g *GameSession) emit(e Event) {
	select {
	case g.events <- e:
	default:
	}
}

// Registry is the process-wide table of GameSessions. The zero
// value is not usable; callers should obtain a Registry via
// NewRegistry.
type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*GameSession

	hostGraceDuration time.Duration
}

// RegistryOption configures a Registry at construction time.
type RegistryOption func(*Registry)

// WithHostGraceDuration overrides the default 15-second auto-
// migrate grace window. Intended for tests that don't want to
// wait 15 real seconds for the timer to fire.
func WithHostGraceDuration(d time.Duration) RegistryOption {
	return func(r *Registry) { r.hostGraceDuration = d }
}

// NewRegistry constructs an empty Registry.
func NewRegistry(opts ...RegistryOption) *Registry {
	r := &Registry{
		sessions:          map[string]*GameSession{},
		hostGraceDuration: DefaultHostGraceDuration,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Create generates a fresh GameSession with a unique join code and
// registers it. The returned GameSession has no Players yet — the
// first WebSocket connection will claim Host.
func (r *Registry) Create() *GameSession {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Generate until we find a code not already in use. Collisions
	// are astronomically unlikely at our scale, but guarding here
	// makes the invariant explicit.
	var code string
	for {
		code = joincode.Generate()
		if _, exists := r.sessions[code]; !exists {
			break
		}
	}

	g := &GameSession{
		code:              code,
		players:           map[string]Player{},
		events:            make(chan Event, 16),
		hostGraceDuration: r.hostGraceDuration,
	}
	r.sessions[code] = g
	return g
}

// Lookup returns the GameSession with the given canonical code,
// or (nil, false) if no such session exists.
func (r *Registry) Lookup(code string) (*GameSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.sessions[code]
	return g, ok
}
