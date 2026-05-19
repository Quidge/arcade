package round

import (
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quidge/scribble/internal/draft"
	"github.com/quidge/scribble/internal/ghost"
)

// endCapture records OnEnd invocations for assertion.
type endCapture struct {
	mu      sync.Mutex
	calls   int
	round   int
	entries []Entry
	reason  EndReason
}

func (e *endCapture) callback(round int, entries []Entry, reason EndReason) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	e.round = round
	e.entries = append([]Entry(nil), entries...)
	e.reason = reason
}

func (e *endCapture) snapshot() (calls int, round int, entries []Entry, reason EndReason) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls, e.round, append([]Entry(nil), e.entries...), e.reason
}

func newController(t *testing.T) (*Controller, *draft.Store, *ghost.Provider, *endCapture) {
	t.Helper()
	drafts := draft.New()
	ghosts := ghost.New(1)
	cap := &endCapture{}
	c := New(Config{
		Drafts: drafts,
		Ghosts: ghosts,
		OnEnd:  cap.callback,
	})
	return c, drafts, ghosts, cap
}

func TestStartActivatesRound(t *testing.T) {
	c, _, _, _ := newController(t)
	if c.Active() {
		t.Errorf("Active before Start = true")
	}
	_, err := c.Start(0, []string{"Alice", "Bob"}, 60)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !c.Active() {
		t.Errorf("Active after Start = false")
	}
	if !c.HasSeat("Alice") {
		t.Errorf("HasSeat(Alice) = false")
	}
	if c.HasSeat("Charlie") {
		t.Errorf("HasSeat(Charlie) = true")
	}
}

func TestStartTwiceWithoutEndReturnsErr(t *testing.T) {
	c, _, _, _ := newController(t)
	if _, err := c.Start(0, []string{"Alice"}, 60); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := c.Start(0, []string{"Alice"}, 60); !errors.Is(err, ErrAlreadyActive) {
		t.Errorf("second Start: err=%v want ErrAlreadyActive", err)
	}
}

func TestAllSubmittedEndsRound(t *testing.T) {
	c, drafts, _, cap := newController(t)
	_, _ = c.Start(0, []string{"Alice", "Bob"}, 0)
	_ = drafts.Apply(0, "Alice", "alice's caption")
	_ = drafts.Apply(0, "Bob", "bob's caption")
	if err := c.Submit("Alice"); err != nil {
		t.Fatalf("Submit Alice: %v", err)
	}
	if c.Active() != true {
		t.Errorf("Active after one of two submits = false")
	}
	if err := c.Submit("Bob"); err != nil {
		t.Fatalf("Submit Bob: %v", err)
	}
	calls, round, entries, reason := cap.snapshot()
	if calls != 1 {
		t.Errorf("OnEnd calls = %d want 1", calls)
	}
	if round != 0 {
		t.Errorf("OnEnd round = %d want 0", round)
	}
	if reason != EndAllSubmitted {
		t.Errorf("reason = %v want EndAllSubmitted", reason)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v want 2", entries)
	}
	want := map[string]string{"Alice": "alice's caption", "Bob": "bob's caption"}
	for _, e := range entries {
		if e.Ghost {
			t.Errorf("entry %+v marked Ghost", e)
		}
		if e.Text != want[e.Player] {
			t.Errorf("entry %s text = %q want %q", e.Player, e.Text, want[e.Player])
		}
	}
	if c.Active() {
		t.Errorf("Active after end = true")
	}
}

func TestRepeatedSubmitIdempotent(t *testing.T) {
	c, drafts, _, cap := newController(t)
	_, _ = c.Start(0, []string{"Alice", "Bob"}, 0)
	_ = drafts.Apply(0, "Alice", "a")
	if err := c.Submit("Alice"); err != nil {
		t.Fatalf("Submit Alice 1: %v", err)
	}
	if err := c.Submit("Alice"); err != nil {
		t.Errorf("Submit Alice 2: %v", err)
	}
	calls, _, _, _ := cap.snapshot()
	if calls != 0 {
		t.Errorf("OnEnd fired before all submitted: calls=%d", calls)
	}
	// Bob completes the round.
	_ = drafts.Apply(0, "Bob", "b")
	_ = c.Submit("Bob")
	calls, _, _, _ = cap.snapshot()
	if calls != 1 {
		t.Errorf("OnEnd calls = %d want 1", calls)
	}
}

func TestSubmitUnknownSeatRejected(t *testing.T) {
	c, _, _, _ := newController(t)
	_, _ = c.Start(0, []string{"Alice"}, 0)
	if err := c.Submit("Bob"); !errors.Is(err, ErrUnknownSeat) {
		t.Errorf("Submit unknown: err=%v want ErrUnknownSeat", err)
	}
}

func TestSubmitOutsideRoundRejected(t *testing.T) {
	c, _, _, _ := newController(t)
	if err := c.Submit("Alice"); !errors.Is(err, ErrNoActiveRound) {
		t.Errorf("Submit outside round: err=%v want ErrNoActiveRound", err)
	}
}

func TestForceAdvanceEndsRound(t *testing.T) {
	c, drafts, _, cap := newController(t)
	_, _ = c.Start(0, []string{"Alice", "Bob"}, 60)
	_ = drafts.Apply(0, "Alice", "alice")
	// Bob has empty draft → Ghost.
	if err := c.ForceAdvance(); err != nil {
		t.Fatalf("ForceAdvance: %v", err)
	}
	calls, _, entries, reason := cap.snapshot()
	if calls != 1 {
		t.Errorf("OnEnd calls = %d want 1", calls)
	}
	if reason != EndForceAdvanced {
		t.Errorf("reason = %v want EndForceAdvanced", reason)
	}
	var sawGhost, sawAlice bool
	for _, e := range entries {
		if e.Player == "Alice" {
			sawAlice = true
			if e.Ghost {
				t.Errorf("Alice ghost-filled despite non-empty draft: %+v", e)
			}
			if e.Text != "alice" {
				t.Errorf("Alice text = %q", e.Text)
			}
		}
		if e.Player == "Bob" {
			if !e.Ghost {
				t.Errorf("Bob not ghost-filled despite empty draft: %+v", e)
			}
			if e.Text == "" {
				t.Errorf("Bob ghost text empty")
			}
			sawGhost = true
		}
	}
	if !sawAlice || !sawGhost {
		t.Errorf("missing entries: %+v", entries)
	}
}

func TestForceAdvanceOutsideRoundRejected(t *testing.T) {
	c, _, _, _ := newController(t)
	if err := c.ForceAdvance(); !errors.Is(err, ErrNoActiveRound) {
		t.Errorf("ForceAdvance outside round: err=%v want ErrNoActiveRound", err)
	}
}

func TestEvenOneCharShipsAsIs(t *testing.T) {
	c, drafts, _, cap := newController(t)
	_, _ = c.Start(0, []string{"Alice"}, 0)
	_ = drafts.Apply(0, "Alice", "x")
	_ = c.Submit("Alice")
	_, _, entries, _ := cap.snapshot()
	if len(entries) != 1 || entries[0].Ghost {
		t.Errorf("one-char draft should ship as-is, got %+v", entries)
	}
	if entries[0].Text != "x" {
		t.Errorf("entry text = %q want x", entries[0].Text)
	}
}

func TestTimerExpiryEndsRound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, drafts, _, cap := newController(t)
		_, err := c.Start(0, []string{"Alice", "Bob"}, 30)
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		_ = drafts.Apply(0, "Alice", "alice typed something")
		// Bob never types.
		time.Sleep(31 * time.Second)
		synctest.Wait()
		calls, _, entries, reason := cap.snapshot()
		if calls != 1 {
			t.Fatalf("OnEnd calls = %d want 1", calls)
		}
		if reason != EndTimerExpired {
			t.Errorf("reason = %v want EndTimerExpired", reason)
		}
		var sawAlice, sawBob bool
		for _, e := range entries {
			if e.Player == "Alice" {
				sawAlice = true
				if e.Ghost {
					t.Errorf("Alice ghost-filled despite typed draft")
				}
			}
			if e.Player == "Bob" {
				sawBob = true
				if !e.Ghost {
					t.Errorf("Bob not ghost-filled")
				}
			}
		}
		if !sawAlice || !sawBob {
			t.Errorf("entries missing: %+v", entries)
		}
	})
}

func TestTimerOffDoesNotEndOnSilence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _, _, cap := newController(t)
		_, _ = c.Start(0, []string{"Alice"}, 0)
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		calls, _, _, _ := cap.snapshot()
		if calls != 0 {
			t.Errorf("OnEnd fired without trigger: %d", calls)
		}
		if !c.Active() {
			t.Errorf("round ended on its own with timer off")
		}
	})
}

func TestAllSubmittedBeforeTimerSuppressesTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, drafts, _, cap := newController(t)
		_, _ = c.Start(0, []string{"Alice"}, 60)
		_ = drafts.Apply(0, "Alice", "a")
		_ = c.Submit("Alice")
		calls1, _, _, reason1 := cap.snapshot()
		if calls1 != 1 || reason1 != EndAllSubmitted {
			t.Fatalf("expected single EndAllSubmitted, got calls=%d reason=%v", calls1, reason1)
		}
		// Advance past where the timer would have fired.
		time.Sleep(120 * time.Second)
		synctest.Wait()
		calls2, _, _, _ := cap.snapshot()
		if calls2 != 1 {
			t.Errorf("timer fired after all-submitted: calls=%d", calls2)
		}
	})
}

func TestForceAdvanceCancelsTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _, _, cap := newController(t)
		_, _ = c.Start(0, []string{"Alice"}, 60)
		_ = c.ForceAdvance()
		time.Sleep(120 * time.Second)
		synctest.Wait()
		calls, _, _, _ := cap.snapshot()
		if calls != 1 {
			t.Errorf("OnEnd calls = %d want 1", calls)
		}
	})
}

func TestDeadlineMatchesTimerSeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _, _, _ := newController(t)
		start := time.Now()
		deadline, err := c.Start(0, []string{"Alice"}, 45)
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		if got := deadline.Sub(start); got != 45*time.Second {
			t.Errorf("deadline offset = %v want 45s", got)
		}
		got, ok := c.Deadline()
		if !ok {
			t.Fatalf("Deadline ok=false")
		}
		if !got.Equal(deadline) {
			t.Errorf("Deadline() = %v want %v", got, deadline)
		}
	})
}

func TestDeadlineOffWhenTimerZero(t *testing.T) {
	c, _, _, _ := newController(t)
	_, _ = c.Start(0, []string{"Alice"}, 0)
	if _, ok := c.Deadline(); ok {
		t.Errorf("Deadline ok=true when timer is off")
	}
}
