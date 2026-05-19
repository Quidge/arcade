package ghost

import (
	"strings"
	"testing"
)

func TestPickReturnsStarterCaption(t *testing.T) {
	p := New(42)
	got := p.Picker().Pick("Alice", StarterCaption)
	if got == "" {
		t.Fatalf("Pick returned empty string")
	}
	if got == genericFallback {
		t.Errorf("Pick returned generic fallback for StarterCaption: %q", got)
	}
	found := false
	for _, e := range starterCaptionLibrary {
		if e == got {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Pick returned %q which is not in the library", got)
	}
}

func TestSameSeedProducesSameSelection(t *testing.T) {
	p1 := New(1234)
	p2 := New(1234)
	for i := 0; i < 5; i++ {
		a := p1.Picker().Pick("Alice", StarterCaption)
		b := p2.Picker().Pick("Alice", StarterCaption)
		if a != b {
			t.Errorf("iter %d: same-seed picks diverged %q vs %q", i, a, b)
		}
	}
}

func TestPickerAvoidsCollisionsBestEffort(t *testing.T) {
	p := New(7)
	picker := p.Picker()
	seen := map[string]int{}
	// Library has 12 entries; ask for fewer than that.
	for i := 0; i < 5; i++ {
		got := picker.Pick("Player", StarterCaption)
		seen[got]++
	}
	for entry, n := range seen {
		if n > 1 {
			t.Errorf("entry %q repeated %d times within picker capacity", entry, n)
		}
	}
}

func TestPickerExhaustionFallsBack(t *testing.T) {
	// Asking for many more entries than the library holds should
	// not panic; it should return *something* for each call.
	p := New(7)
	picker := p.Picker()
	for i := 0; i < len(starterCaptionLibrary)*3; i++ {
		got := picker.Pick("Player", StarterCaption)
		if got == "" {
			t.Fatalf("iter %d: Pick returned empty", i)
		}
	}
}

func TestUnknownSlotKindReturnsGeneric(t *testing.T) {
	p := New(0)
	got := p.Picker().Pick("Alice", GuessCaption)
	if got != genericFallback {
		t.Errorf("GuessCaption pick = %q want generic fallback", got)
	}
	got = p.Picker().Pick("Alice", Drawing)
	if got != genericFallback {
		t.Errorf("Drawing pick = %q want generic fallback", got)
	}
}

func TestPickDrawingReturnsTriangle(t *testing.T) {
	p := New(7)
	got := p.Picker().PickDrawing("Alice")
	if len(got) != 3 {
		t.Fatalf("PickDrawing returned %d strokes, want 3 (triangle)", len(got))
	}
	// Each stroke has at least 2 points (a segment).
	for i, s := range got {
		if len(s) < 2 {
			t.Errorf("stroke %d has %d points, want >= 2", i, len(s))
		}
		for j, pt := range s {
			if pt.X < 0 || pt.X > 1 || pt.Y < 0 || pt.Y > 1 {
				t.Errorf("stroke %d point %d = %+v out of normalized 0..1", i, j, pt)
			}
		}
	}
	// Triangle endpoints meet: each stroke's last point equals the
	// next stroke's first point, and the third closes back to the
	// first.
	for i := 0; i < len(got); i++ {
		end := got[i][len(got[i])-1]
		start := got[(i+1)%len(got)][0]
		if end != start {
			t.Errorf("triangle vertex %d open: stroke %d end=%+v != stroke %d start=%+v",
				i, i, end, (i+1)%len(got), start)
		}
	}
}

func TestPickDrawingIsDefensiveCopy(t *testing.T) {
	p := New(0)
	got := p.Picker().PickDrawing("Alice")
	if len(got) == 0 {
		t.Fatalf("expected non-empty triangle")
	}
	// Mutate the returned strokes — the next call must not see the
	// mutation.
	got[0][0].X = 99.0
	got2 := p.Picker().PickDrawing("Bob")
	if got2[0][0].X == 99.0 {
		t.Errorf("PickDrawing leaked the library's storage")
	}
}

func TestLibraryEntriesAreSingleSentencesIsh(t *testing.T) {
	// Smoke-test the canned library to keep it on-spec — entries
	// are short and don't carry trailing punctuation. Not a hard
	// requirement; a guardrail against accidental commit of a
	// paragraph.
	for _, e := range starterCaptionLibrary {
		if e == "" {
			t.Errorf("library has empty entry")
		}
		if strings.Contains(e, "\n") {
			t.Errorf("library entry contains newline: %q", e)
		}
		if len(e) > 200 {
			t.Errorf("library entry over 200 chars: %q", e)
		}
	}
}
