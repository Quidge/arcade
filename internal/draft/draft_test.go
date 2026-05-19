package draft

import (
	"sync"
	"testing"
)

func TestApplyAccumulatesLatestSnapshot(t *testing.T) {
	s := New()
	if !s.Apply(0, "Alice", "the") {
		t.Fatalf("first Apply rejected")
	}
	if !s.Apply(0, "Alice", "the cat") {
		t.Fatalf("second Apply rejected")
	}
	if got := s.Get(0, "Alice"); got.Text != "the cat" {
		t.Errorf("Get.Text = %q want %q", got.Text, "the cat")
	}
}

func TestApplyAfterSubmitRejected(t *testing.T) {
	s := New()
	_ = s.Apply(0, "Alice", "hello")
	if _, ok := s.Submit(0, "Alice"); !ok {
		t.Fatalf("Submit returned ok=false")
	}
	if s.Apply(0, "Alice", "hello world") {
		t.Errorf("Apply after Submit should be rejected")
	}
	if got := s.Get(0, "Alice"); got.Text != "hello" {
		t.Errorf("text after rejected Apply = %q want %q", got.Text, "hello")
	}
}

func TestSubmitIsIdempotent(t *testing.T) {
	s := New()
	_ = s.Apply(0, "Alice", "ok")
	s1, _ := s.Submit(0, "Alice")
	s2, _ := s.Submit(0, "Alice")
	if s1 != s2 {
		t.Errorf("repeated Submit returned different snapshots: %+v vs %+v", s1, s2)
	}
	if !s2.Submitted {
		t.Errorf("Submit snapshot.Submitted = false")
	}
}

func TestIsEmpty(t *testing.T) {
	s := New()
	if !s.IsEmpty(0, "Alice") {
		t.Errorf("fresh key IsEmpty = false")
	}
	_ = s.Apply(0, "Alice", "a")
	if s.IsEmpty(0, "Alice") {
		t.Errorf("non-empty Draft IsEmpty = true")
	}
	_ = s.Apply(0, "Alice", "")
	if !s.IsEmpty(0, "Alice") {
		t.Errorf("emptied Draft IsEmpty = false")
	}
}

func TestSubmittedEmptyStillEmpty(t *testing.T) {
	s := New()
	// Player submits without typing anything.
	_, _ = s.Submit(0, "Alice")
	if !s.IsEmpty(0, "Alice") {
		t.Errorf("submitted-but-empty Draft IsEmpty = false")
	}
}

func TestKeysAreIsolatedPerRoundAndPlayer(t *testing.T) {
	s := New()
	_ = s.Apply(0, "Alice", "round-zero alice")
	_ = s.Apply(0, "Bob", "round-zero bob")
	_ = s.Apply(1, "Alice", "round-one alice")
	if got := s.Get(0, "Alice").Text; got != "round-zero alice" {
		t.Errorf("(0,Alice) = %q", got)
	}
	if got := s.Get(0, "Bob").Text; got != "round-zero bob" {
		t.Errorf("(0,Bob) = %q", got)
	}
	if got := s.Get(1, "Alice").Text; got != "round-one alice" {
		t.Errorf("(1,Alice) = %q", got)
	}
}

func TestSurvivesDisconnectReconnectKey(t *testing.T) {
	// The store is keyed by (round, player) — there is no
	// connection concept inside it. A Disconnect/Reconnect cycle
	// at the web layer keeps the same key and therefore the same
	// buffer.
	s := New()
	_ = s.Apply(0, "Alice", "halfway through a sent")
	// Simulate a Disconnect: nothing in this store changes.
	// Simulate a Reconnect: server snapshots the Draft, sends to client.
	snap := s.Get(0, "Alice")
	if snap.Text != "halfway through a sent" {
		t.Errorf("snapshot text after pseudo-reconnect = %q", snap.Text)
	}
	// Client resumes typing.
	_ = s.Apply(0, "Alice", "halfway through a sentence")
	if got := s.Get(0, "Alice").Text; got != "halfway through a sentence" {
		t.Errorf("resumed Draft = %q", got)
	}
}

func TestConcurrentApplyAndGetIsRaceFree(t *testing.T) {
	// Smoke test under -race. The store is a small mutex-guarded
	// map; this exists to catch a future refactor that drops the
	// lock.
	s := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = s.Apply(0, "Alice", "stream")
				_ = s.Get(0, "Alice")
			}
		}()
	}
	wg.Wait()
}
