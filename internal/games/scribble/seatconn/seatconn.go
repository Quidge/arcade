// Package seatconn is a small, deep registry that tracks the
// current live connection bound to each seat in a GameSession.
//
// The web layer composes the registry key as "<code>/<name>"; the
// registry itself treats keys as opaque strings and knows nothing
// about GameSessions or WebSockets. It exists to solve one race
// condition: between an outgoing handler's deferred Disconnect and
// an incoming handler's Reconnect for the same seat, we need a
// monotonically increasing generation token so the outgoing handler
// can tell whether it is still the current owner.
//
// Acquire registers a new connection as the current owner, returns
// the prior connection (if any) for the caller to close, and a
// generation token. Release removes the entry only if the supplied
// generation still matches; the returned boolean tells the caller
// whether they were the still-current owner (i.e., whether the
// caller should fire the domain-level Disconnect).
package seatconn

import "sync"

// ConnHandle is the minimal surface seatconn needs to close a
// superseded connection. The websocket package's *websocket.Conn
// is the production implementation; tests can supply a stub.
type ConnHandle interface {
	Close(reason string)
}

// Registry maps seat keys to the current (ConnHandle, generation)
// pair. The zero value is not usable; obtain one via New.
type Registry struct {
	mu      sync.Mutex
	entries map[string]entry
	nextGen uint64
}

type entry struct {
	conn ConnHandle
	gen  uint64
}

// New constructs an empty Registry.
func New() *Registry {
	return &Registry{entries: map[string]entry{}}
}

// Acquire atomically registers conn as the current live connection
// for key. The returned prior is the previously-current connection
// (nil if none), which the caller is expected to close with a
// "superseded" reason. The returned gen is a monotonically
// increasing token unique within this Registry; the caller must
// supply it back to Release on handler exit.
func (r *Registry) Acquire(key string, conn ConnHandle) (prior ConnHandle, gen uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextGen++
	gen = r.nextGen
	if prev, ok := r.entries[key]; ok {
		prior = prev.conn
	}
	r.entries[key] = entry{conn: conn, gen: gen}
	return prior, gen
}

// Release removes the entry for key if the supplied gen still
// matches the registered generation. Returns wasCurrent=true if the
// caller was still the current owner at the moment of release (and
// should therefore fire the domain-level Disconnect for the seat);
// wasCurrent=false if the caller was superseded by a later Acquire
// (and should leave the seat's connection state alone — the new
// owner is responsible for it).
func (r *Registry) Release(key string, gen uint64) (wasCurrent bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[key]
	if !ok {
		return false
	}
	if e.gen != gen {
		return false
	}
	delete(r.entries, key)
	return true
}

// Close removes the entry for key and closes the registered
// connection with reason. The deletion happens under the lock so
// the deferred Release in that connection's handler observes the
// entry as gone and reports wasCurrent=false — preventing a stray
// domain-level Disconnect from running on a seat that the caller
// has already removed from the domain. No-op if no entry exists.
func (r *Registry) Close(key string, reason string) {
	r.mu.Lock()
	e, ok := r.entries[key]
	if ok {
		delete(r.entries, key)
	}
	r.mu.Unlock()
	if ok {
		e.conn.Close(reason)
	}
}
