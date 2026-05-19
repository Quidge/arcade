package round

import (
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// endCapture records OnEnd invocations for assertion.
type endCapture struct {
	mu     sync.Mutex
	calls  int
	round  int
	seats  []string
	reason EndReason
}

func (e *endCapture) callback(round int, seats []string, reason EndReason) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	e.round = round
	e.seats = append([]string(nil), seats...)
	e.reason = reason
}

func (e *endCapture) snapshot() (calls int, round int, seats []string, reason EndReason) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls, e.round, append([]string(nil), e.seats...), e.reason
}

func newController(t *testing.T) (*Controller, *endCapture) {
	t.Helper()
	cap := &endCapture{}
	c := New(Config{OnEnd: cap.callback})
	return c, cap
}

func TestStartActivatesRound(t *testing.T) {
	c, _ := newController(t)
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
	c, _ := newController(t)
	if _, err := c.Start(0, []string{"Alice"}, 60); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := c.Start(0, []string{"Alice"}, 60); !errors.Is(err, ErrAlreadyActive) {
		t.Errorf("second Start: err=%v want ErrAlreadyActive", err)
	}
}

func TestAllSubmittedEndsRound(t *testing.T) {
	c, cap := newController(t)
	_, _ = c.Start(0, []string{"Alice", "Bob"}, 0)
	if err := c.Submit("Alice"); err != nil {
		t.Fatalf("Submit Alice: %v", err)
	}
	if !c.Active() {
		t.Errorf("Active after one of two submits = false")
	}
	if err := c.Submit("Bob"); err != nil {
		t.Fatalf("Submit Bob: %v", err)
	}
	calls, round, seats, reason := cap.snapshot()
	if calls != 1 {
		t.Errorf("OnEnd calls = %d want 1", calls)
	}
	if round != 0 {
		t.Errorf("OnEnd round = %d want 0", round)
	}
	if reason != EndAllSubmitted {
		t.Errorf("reason = %v want EndAllSubmitted", reason)
	}
	if len(seats) != 2 || seats[0] != "Alice" || seats[1] != "Bob" {
		t.Errorf("OnEnd seats = %+v want [Alice Bob]", seats)
	}
	if c.Active() {
		t.Errorf("Active after end = true")
	}
}

func TestRepeatedSubmitIdempotent(t *testing.T) {
	c, cap := newController(t)
	_, _ = c.Start(0, []string{"Alice", "Bob"}, 0)
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
	_ = c.Submit("Bob")
	calls, _, _, _ = cap.snapshot()
	if calls != 1 {
		t.Errorf("OnEnd calls = %d want 1", calls)
	}
}

func TestSubmitUnknownSeatRejected(t *testing.T) {
	c, _ := newController(t)
	_, _ = c.Start(0, []string{"Alice"}, 0)
	if err := c.Submit("Bob"); !errors.Is(err, ErrUnknownSeat) {
		t.Errorf("Submit unknown: err=%v want ErrUnknownSeat", err)
	}
}

func TestSubmitOutsideRoundRejected(t *testing.T) {
	c, _ := newController(t)
	if err := c.Submit("Alice"); !errors.Is(err, ErrNoActiveRound) {
		t.Errorf("Submit outside round: err=%v want ErrNoActiveRound", err)
	}
}

func TestForceAdvanceEndsRound(t *testing.T) {
	c, cap := newController(t)
	_, _ = c.Start(0, []string{"Alice", "Bob"}, 60)
	if err := c.ForceAdvance(); err != nil {
		t.Fatalf("ForceAdvance: %v", err)
	}
	calls, _, seats, reason := cap.snapshot()
	if calls != 1 {
		t.Errorf("OnEnd calls = %d want 1", calls)
	}
	if reason != EndForceAdvanced {
		t.Errorf("reason = %v want EndForceAdvanced", reason)
	}
	if len(seats) != 2 {
		t.Errorf("seats len = %d want 2", len(seats))
	}
}

func TestForceAdvanceOutsideRoundRejected(t *testing.T) {
	c, _ := newController(t)
	if err := c.ForceAdvance(); !errors.Is(err, ErrNoActiveRound) {
		t.Errorf("ForceAdvance outside round: err=%v want ErrNoActiveRound", err)
	}
}

func TestTimerExpiryEndsRound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, cap := newController(t)
		_, err := c.Start(0, []string{"Alice", "Bob"}, 30)
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		time.Sleep(31 * time.Second)
		synctest.Wait()
		calls, _, _, reason := cap.snapshot()
		if calls != 1 {
			t.Fatalf("OnEnd calls = %d want 1", calls)
		}
		if reason != EndTimerExpired {
			t.Errorf("reason = %v want EndTimerExpired", reason)
		}
	})
}

func TestTimerOffDoesNotEndOnSilence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, cap := newController(t)
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
		c, cap := newController(t)
		_, _ = c.Start(0, []string{"Alice"}, 60)
		_ = c.Submit("Alice")
		calls1, _, _, reason1 := cap.snapshot()
		if calls1 != 1 || reason1 != EndAllSubmitted {
			t.Fatalf("expected single EndAllSubmitted, got calls=%d reason=%v", calls1, reason1)
		}
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
		c, cap := newController(t)
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
		c, _ := newController(t)
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
	c, _ := newController(t)
	_, _ = c.Start(0, []string{"Alice"}, 0)
	if _, ok := c.Deadline(); ok {
		t.Errorf("Deadline ok=true when timer is off")
	}
}

func TestStartAfterEndReusesController(t *testing.T) {
	// The controller advertises that subsequent Rounds can drive
	// successive Starts on the same instance. Verify a second Start
	// works after the first Round has ended.
	c, cap := newController(t)
	_, _ = c.Start(0, []string{"Alice"}, 0)
	_ = c.Submit("Alice")
	if c.Active() {
		t.Fatalf("Active after Round 0 end = true")
	}
	if _, err := c.Start(1, []string{"Alice", "Bob"}, 0); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	_ = c.Submit("Alice")
	_ = c.Submit("Bob")
	calls, round, seats, _ := cap.snapshot()
	if calls != 2 {
		t.Errorf("OnEnd total calls = %d want 2", calls)
	}
	if round != 1 {
		t.Errorf("second OnEnd round = %d want 1", round)
	}
	if len(seats) != 2 {
		t.Errorf("second OnEnd seats = %+v", seats)
	}
}
