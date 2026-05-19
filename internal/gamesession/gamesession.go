// Package gamesession holds the lobby-stage domain: a Registry of
// running GameSessions and the per-session Players + Events surface.
//
// A Player is a persistent seat: their record survives WebSocket
// disconnects and is only removed by Leave (kick or voluntary quit —
// neither implemented in this slice). A separate Connected flag on
// each Player tracks whether their WebSocket is currently alive.
// The domain exposes three verbs — Join, Reconnect, Disconnect —
// each doing one thing. See ADR 0008.
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

	"github.com/quidge/scribble/internal/joincode"
)

// MaxPlayers is the hard cap on Players per GameSession. The cap
// counts seats, connected or not: a disconnected seat blocks a
// would-be 9th joiner.
const MaxPlayers = 8

// ErrSeatExists is returned by Join when a seat with the requested
// display name already exists. Callers should dispatch to Reconnect
// in that case.
var ErrSeatExists = errors.New("gamesession: seat with that display name already exists")

// ErrCapExceeded is returned by Join when the GameSession already
// holds MaxPlayers seats.
var ErrCapExceeded = errors.New("gamesession: game session is full")

// ErrNotSeated is returned by Reconnect when no seat with the
// requested display name exists.
var ErrNotSeated = errors.New("gamesession: no seat with that display name")

// Player is the public roster entry. Host is true for the seat that
// currently holds the host badge (at most one at a time). Connected
// is true while a live WebSocket is bound to the seat.
type Player struct {
	Name      string `json:"name"`
	Host      bool   `json:"host"`
	Connected bool   `json:"connected"`
}

// EventKind discriminates the seat/connection transitions that the
// domain emits.
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
)

// Event is the transition record emitted on the three domain verbs.
// Roster is a snapshot captured under the GameSession lock at the
// time the transition was applied, so consumers see a consistent
// view without re-locking.
type Event struct {
	Kind   EventKind
	Player Player
	Roster []Player
}

// GameSession is one in-progress lobby. The zero value is not
// usable; callers should obtain a GameSession via Registry.Create.
type GameSession struct {
	code string

	mu      sync.RWMutex
	players map[string]Player
	order   []string // join order; stable across Disconnect/Reconnect

	events chan Event
}

// Code returns the canonical 6-character join code for this
// GameSession.
func (g *GameSession) Code() string { return g.code }

// Events returns the read end of the event channel. The channel is
// unbuffered and the domain sends non-blockingly, so slow consumers
// drop events rather than stalling the verbs.
//
// Callers must consume promptly — only the live state in Roster()
// is authoritative; events are a notification stream.
func (g *GameSession) Events() <-chan Event { return g.events }

// Roster returns the current Players, in join order. The returned
// slice is a fresh copy; callers may mutate it freely.
func (g *GameSession) Roster() []Player {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.rosterLocked()
}

// HasSeat reports whether a seat with the given display name exists
// in the GameSession, regardless of its connection state. The web
// layer uses this to dispatch a fresh WebSocket upgrade to either
// Join or Reconnect, but races between HasSeat and Join/Reconnect
// are tolerated: Join returns ErrSeatExists if a seat appeared
// between the check and the call.
func (g *GameSession) HasSeat(name string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
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
// PlayerReconnected event is emitted. If the seat is already
// Connected=true, Reconnect is a no-op at the domain level — the
// web layer's seatconn registry handles the supersede semantics
// (the older WebSocket is closed by the caller of seatconn.Acquire).
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
	snapshot := g.rosterLocked()
	g.mu.Unlock()

	g.emit(Event{Kind: PlayerReconnected, Player: p, Roster: snapshot})
	return &p, nil
}

// Disconnect marks the named seat as Connected=false and emits a
// PlayerDisconnected event. The seat, its Host status, and its
// position in the join order are preserved. If no seat with the
// name exists, or the seat is already Connected=false, Disconnect
// is a no-op (no event).
func (g *GameSession) Disconnect(name string) {
	g.mu.Lock()
	p, ok := g.players[name]
	if !ok || !p.Connected {
		g.mu.Unlock()
		return
	}
	p.Connected = false
	g.players[name] = p
	snapshot := g.rosterLocked()
	g.mu.Unlock()

	g.emit(Event{Kind: PlayerDisconnected, Player: p, Roster: snapshot})
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
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{sessions: map[string]*GameSession{}}
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
		code:    code,
		players: map[string]Player{},
		events:  make(chan Event),
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
