package strokes

import (
	"sync"
	"testing"
)

func makeDrawing(values ...[][2]float64) Drawing {
	out := make(Drawing, 0, len(values))
	for _, s := range values {
		stroke := make(Stroke, 0, len(s))
		for _, p := range s {
			stroke = append(stroke, Point{X: p[0], Y: p[1]})
		}
		out = append(out, stroke)
	}
	return out
}

func TestApplyAccumulatesLatestSnapshot(t *testing.T) {
	s := New()
	d1 := makeDrawing([][2]float64{{0.1, 0.1}, {0.2, 0.2}})
	if !s.Apply(0, "Alice", d1) {
		t.Fatalf("first Apply rejected")
	}
	d2 := makeDrawing(
		[][2]float64{{0.1, 0.1}, {0.2, 0.2}},
		[][2]float64{{0.3, 0.3}, {0.4, 0.4}},
	)
	if !s.Apply(0, "Alice", d2) {
		t.Fatalf("second Apply rejected")
	}
	got := s.Get(0, "Alice")
	if len(got.Strokes) != 2 {
		t.Errorf("Get.Strokes len = %d want 2", len(got.Strokes))
	}
}

func TestApplyAfterSubmitRejected(t *testing.T) {
	s := New()
	d := makeDrawing([][2]float64{{0.1, 0.1}})
	_ = s.Apply(0, "Alice", d)
	if _, ok := s.Submit(0, "Alice"); !ok {
		t.Fatalf("Submit returned ok=false")
	}
	more := makeDrawing(
		[][2]float64{{0.1, 0.1}},
		[][2]float64{{0.5, 0.5}},
	)
	if s.Apply(0, "Alice", more) {
		t.Errorf("Apply after Submit should be rejected")
	}
	got := s.Get(0, "Alice")
	if len(got.Strokes) != 1 {
		t.Errorf("strokes after rejected Apply len = %d want 1", len(got.Strokes))
	}
}

func TestSubmitIsIdempotent(t *testing.T) {
	s := New()
	_ = s.Apply(0, "Alice", makeDrawing([][2]float64{{0, 0}}))
	s1, _ := s.Submit(0, "Alice")
	s2, _ := s.Submit(0, "Alice")
	if len(s1.Strokes) != len(s2.Strokes) || !s2.Submitted {
		t.Errorf("repeated Submit diverged: %+v vs %+v", s1, s2)
	}
}

func TestIsEmpty(t *testing.T) {
	s := New()
	if !s.IsEmpty(0, "Alice") {
		t.Errorf("fresh key IsEmpty = false")
	}
	_ = s.Apply(0, "Alice", makeDrawing([][2]float64{{0.1, 0.1}}))
	if s.IsEmpty(0, "Alice") {
		t.Errorf("non-empty Drawing IsEmpty = true")
	}
	_ = s.Apply(0, "Alice", Drawing{})
	if !s.IsEmpty(0, "Alice") {
		t.Errorf("emptied Drawing IsEmpty = false")
	}
}

func TestSubmittedEmptyStillEmpty(t *testing.T) {
	s := New()
	_, _ = s.Submit(0, "Alice")
	if !s.IsEmpty(0, "Alice") {
		t.Errorf("submitted-but-empty Drawing IsEmpty = false")
	}
}

func TestKeysAreIsolatedPerRoundAndPlayer(t *testing.T) {
	s := New()
	_ = s.Apply(0, "Alice", makeDrawing([][2]float64{{0.1, 0.1}}))
	_ = s.Apply(0, "Bob", makeDrawing([][2]float64{{0.2, 0.2}}, [][2]float64{{0.3, 0.3}}))
	_ = s.Apply(1, "Alice", makeDrawing([][2]float64{{0.4, 0.4}}, [][2]float64{{0.5, 0.5}}, [][2]float64{{0.6, 0.6}}))
	if len(s.Get(0, "Alice").Strokes) != 1 {
		t.Errorf("(0,Alice) strokes len wrong")
	}
	if len(s.Get(0, "Bob").Strokes) != 2 {
		t.Errorf("(0,Bob) strokes len wrong")
	}
	if len(s.Get(1, "Alice").Strokes) != 3 {
		t.Errorf("(1,Alice) strokes len wrong")
	}
}

func TestApplyStoresDefensiveCopy(t *testing.T) {
	s := New()
	d := makeDrawing([][2]float64{{0.1, 0.1}})
	_ = s.Apply(0, "Alice", d)
	// Mutate the caller's slice after Apply — the store must not
	// see the mutation.
	d[0][0].X = 0.99
	got := s.Get(0, "Alice")
	if got.Strokes[0][0].X != 0.1 {
		t.Errorf("store leaked caller mutation: %+v", got.Strokes)
	}
}

func TestConcurrentApplyAndGetIsRaceFree(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := makeDrawing([][2]float64{{0.1, 0.1}, {0.2, 0.2}})
			for j := 0; j < 200; j++ {
				_ = s.Apply(0, "Alice", d)
				_ = s.Get(0, "Alice")
			}
		}()
	}
	wg.Wait()
}
