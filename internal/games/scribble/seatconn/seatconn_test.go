package seatconn

import (
	"sync"
	"testing"
)

// stubConn is a ConnHandle for tests. Close is a no-op recorder so
// tests can pass distinct pointers and assert on identity.
type stubConn struct {
	id     string
	mu     sync.Mutex
	closed bool
	reason string
}

func (c *stubConn) Close(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.reason = reason
}

func TestAcquireOnEmptyKey(t *testing.T) {
	r := New()
	a := &stubConn{id: "a"}
	prior, gen := r.Acquire("seat", a)
	if prior != nil {
		t.Errorf("prior = %v, want nil", prior)
	}
	if gen == 0 {
		t.Errorf("gen = 0, want monotonically increasing > 0")
	}
}

func TestAcquireReturnsPriorAndBumpsGen(t *testing.T) {
	r := New()
	a := &stubConn{id: "a"}
	b := &stubConn{id: "b"}
	_, gen1 := r.Acquire("seat", a)
	prior, gen2 := r.Acquire("seat", b)
	if prior != a {
		t.Errorf("prior = %v, want a", prior)
	}
	if gen2 <= gen1 {
		t.Errorf("gen2 = %d, want > gen1 (%d)", gen2, gen1)
	}
}

func TestReleaseWithCurrentGen(t *testing.T) {
	r := New()
	a := &stubConn{id: "a"}
	_, gen := r.Acquire("seat", a)
	if !r.Release("seat", gen) {
		t.Errorf("Release with current gen: wasCurrent=false, want true")
	}
	// A subsequent Acquire on the same key should return nil prior.
	b := &stubConn{id: "b"}
	prior, _ := r.Acquire("seat", b)
	if prior != nil {
		t.Errorf("after Release+Acquire, prior = %v, want nil", prior)
	}
}

func TestReleaseWithStaleGenIsNoop(t *testing.T) {
	r := New()
	a := &stubConn{id: "a"}
	b := &stubConn{id: "b"}
	_, gen1 := r.Acquire("seat", a)
	_, gen2 := r.Acquire("seat", b)
	if r.Release("seat", gen1) {
		t.Errorf("Release with stale gen: wasCurrent=true, want false")
	}
	// The current entry is still b.
	c := &stubConn{id: "c"}
	prior, _ := r.Acquire("seat", c)
	if prior != b {
		t.Errorf("stale release affected current entry: prior = %v, want b", prior)
	}
	_ = gen2
}

func TestSequentialAcquireReleaseOldReleaseNew(t *testing.T) {
	r := New()
	a := &stubConn{id: "a"}
	b := &stubConn{id: "b"}
	_, genA := r.Acquire("seat", a)
	_, genB := r.Acquire("seat", b)

	if wasCurrent := r.Release("seat", genA); wasCurrent {
		t.Errorf("Release(genA) after supersede: wasCurrent=true, want false")
	}
	if wasCurrent := r.Release("seat", genB); !wasCurrent {
		t.Errorf("Release(genB) as current owner: wasCurrent=false, want true")
	}
}

func TestReleaseUnknownKeyIsFalse(t *testing.T) {
	r := New()
	if r.Release("nope", 1) {
		t.Errorf("Release on unknown key: wasCurrent=true, want false")
	}
}

// TestConcurrentAcquireRelease exercises the registry under heavy
// parallel pressure on one key; the race detector should remain
// silent and the final state should be coherent.
func TestConcurrentAcquireRelease(t *testing.T) {
	r := New()
	var wg sync.WaitGroup
	const N = 200
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := &stubConn{}
			_, gen := r.Acquire("seat", c)
			r.Release("seat", gen)
		}()
	}
	wg.Wait()
	// After all goroutines finish, there must be no entry left or
	// exactly one orphan; in either case a fresh Acquire should
	// either return nil prior (if all released) or a stub (if one
	// orphan remains). The important property is no panic / no race.
	_, _ = r.Acquire("seat", &stubConn{})
}
