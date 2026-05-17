// Package gamesession holds the lobby-stage domain: a Registry of
// running GameSessions and the per-session Players + Events surface.
//
// This package is wire-format-agnostic. Join/Leave produce typed
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

// MaxPlayers is the hard cap on Players per GameSession.
const MaxPlayers = 8

// ErrDuplicateName is returned by Join when the requested display
// name is already held by a currently-connected Player in the
// GameSession.
var ErrDuplicateName = errors.New("gamesession: display name already in use")

// ErrCapExceeded is returned by Join when the GameSession already
// holds MaxPlayers Players.
var ErrCapExceeded = errors.New("gamesession: game session is full")

// Player is the public roster entry. Host is true for whichever
// Player currently holds the host badge (at most one at a time).
type Player struct {
	Name string `json:"name"`
	Host bool   `json:"host"`
}

// EventKind discriminates the membership transitions that the
// domain emits.
type EventKind int

const (
	// PlayerJoined fires once per successful Join.
	PlayerJoined EventKind = iota
	// PlayerLeft fires once per Leave that actually removed a Player.
	PlayerLeft
)

// Event is the membership-transition record emitted on Join/Leave.
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
	order   []string // join order; used for deterministic Roster()

	events chan Event
}

// Code returns the canonical 6-character join code for this
// GameSession.
func (g *GameSession) Code() string { return g.code }

// Events returns the read end of the event channel. The channel is
// unbuffered and the domain sends non-blockingly, so slow consumers
// drop events rather than stalling Join/Leave.
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

func (g *GameSession) rosterLocked() []Player {
	out := make([]Player, 0, len(g.order))
	for _, name := range g.order {
		out = append(out, g.players[name])
	}
	return out
}

// Join atomically (a) enforces name uniqueness within the current
// Players, (b) enforces the MaxPlayers cap, then (c) adds the new
// Player and emits a PlayerJoined event. The first Player to Join
// an empty GameSession becomes the Host.
func (g *GameSession) Join(name string) (*Player, error) {
	g.mu.Lock()
	if _, dup := g.players[name]; dup {
		g.mu.Unlock()
		return nil, ErrDuplicateName
	}
	if len(g.players) >= MaxPlayers {
		g.mu.Unlock()
		return nil, ErrCapExceeded
	}
	p := Player{Name: name, Host: len(g.players) == 0}
	g.players[name] = p
	g.order = append(g.order, name)
	snapshot := g.rosterLocked()
	g.mu.Unlock()

	g.emit(Event{Kind: PlayerJoined, Player: p, Roster: snapshot})
	return &p, nil
}

// Leave removes the named Player from the GameSession and emits a
// PlayerLeft event. If the name is not present, Leave is a no-op.
//
// Note: Leave does not transfer the Host badge. If the Host leaves,
// the GameSession is briefly host-less; a later slice (ADR 0005)
// will close that gap.
func (g *GameSession) Leave(name string) {
	g.mu.Lock()
	p, ok := g.players[name]
	if !ok {
		g.mu.Unlock()
		return
	}
	delete(g.players, name)
	for i, n := range g.order {
		if n == name {
			g.order = append(g.order[:i], g.order[i+1:]...)
			break
		}
	}
	snapshot := g.rosterLocked()
	g.mu.Unlock()

	g.emit(Event{Kind: PlayerLeft, Player: p, Roster: snapshot})
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
