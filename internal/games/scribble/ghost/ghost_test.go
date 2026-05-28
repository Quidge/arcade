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
		t.Errorf("Pick returned %q which is not in the starter library", got)
	}
}

func TestPickReturnsGuessCaption(t *testing.T) {
	p := New(42)
	got := p.Picker().Pick("Alice", GuessCaption)
	if got == "" {
		t.Fatalf("Pick returned empty string for GuessCaption")
	}
	if got == genericFallback {
		t.Errorf("Pick returned generic fallback for GuessCaption: %q", got)
	}
	found := false
	for _, e := range guessCaptionLibrary {
		if e == got {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Pick returned %q which is not in the guess library", got)
	}
}

func TestStarterAndGuessCaptionsDiffer(t *testing.T) {
	// The two libraries hold visibly-distinct stubs so that a
	// reader can tell a starter-Caption Ghost from a
	// guess-Caption Ghost at a glance.
	for i, s := range starterCaptionLibrary {
		for _, g := range guessCaptionLibrary {
			if s == g {
				t.Errorf("starter[%d] %q collides with a guess library entry", i, s)
			}
		}
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
	// Library has ghostLibrarySize entries; ask for fewer.
	for i := 0; i < ghostLibrarySize-1; i++ {
		got := picker.Pick("Player", StarterCaption)
		seen[got]++
	}
	for entry, n := range seen {
		if n > 1 {
			t.Errorf("entry %q repeated %d times within picker capacity", entry, n)
		}
	}
}

func TestPickerAvoidsCollisionsAtFullCapacity(t *testing.T) {
	// At N=10 every absent Player should get a distinct Ghost
	// caption — the library size equals the cap so the
	// fallback-to-repeat branch is never hit.
	p := New(7)
	picker := p.Picker()
	seen := map[string]struct{}{}
	for i := 0; i < ghostLibrarySize; i++ {
		got := picker.Pick("Player", StarterCaption)
		if _, dup := seen[got]; dup {
			t.Errorf("entry %q repeated within library size at iter %d", got, i)
		}
		seen[got] = struct{}{}
	}
	if len(seen) != ghostLibrarySize {
		t.Errorf("expected %d distinct picks, got %d", ghostLibrarySize, len(seen))
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

func TestDrawingSlotKindThroughPickReturnsGeneric(t *testing.T) {
	// The Drawing slot kind is served by PickDrawing (returns
	// strokes), not by Pick (returns text). When something asks
	// Pick(_, Drawing) it should get the defensive generic
	// fallback because there is no caption library for Drawings.
	p := New(0)
	got := p.Picker().Pick("Alice", Drawing)
	if got != genericFallback {
		t.Errorf("Drawing pick via Pick = %q want generic fallback", got)
	}
}

func TestPickDrawingReturnsLibraryEntry(t *testing.T) {
	p := New(7)
	got := p.Picker().PickDrawing("Alice")
	if len(got) == 0 {
		t.Fatalf("PickDrawing returned empty drawing")
	}
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
}

func TestPickDrawingDistinctAtFullCapacity(t *testing.T) {
	// At N=10 each absent Player should get a distinct Ghost
	// Drawing. With ten horizontal lines at different y values,
	// distinctness is observable by the y-coordinate of the
	// stroke's first point.
	p := New(7)
	picker := p.Picker()
	seen := map[float64]struct{}{}
	for i := 0; i < ghostLibrarySize; i++ {
		got := picker.PickDrawing("p")
		if len(got) == 0 || len(got[0]) == 0 {
			t.Fatalf("PickDrawing returned empty at iter %d", i)
		}
		y := got[0][0].Y
		if _, dup := seen[y]; dup {
			t.Errorf("drawing y=%v repeated within capacity at iter %d", y, i)
		}
		seen[y] = struct{}{}
	}
	if len(seen) != ghostLibrarySize {
		t.Errorf("expected %d distinct drawings, got %d", ghostLibrarySize, len(seen))
	}
}

func TestPickDrawingIsDefensiveCopy(t *testing.T) {
	p := New(0)
	got := p.Picker().PickDrawing("Alice")
	if len(got) == 0 {
		t.Fatalf("expected non-empty drawing")
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
	// Smoke-test the canned libraries to keep them on-spec —
	// entries are short and don't carry trailing punctuation.
	for _, lib := range [][]string{starterCaptionLibrary, guessCaptionLibrary} {
		for _, e := range lib {
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
}

func TestLibrariesMatchCapSize(t *testing.T) {
	if len(starterCaptionLibrary) != ghostLibrarySize {
		t.Errorf("starterCaptionLibrary len = %d want %d", len(starterCaptionLibrary), ghostLibrarySize)
	}
	if len(guessCaptionLibrary) != ghostLibrarySize {
		t.Errorf("guessCaptionLibrary len = %d want %d", len(guessCaptionLibrary), ghostLibrarySize)
	}
	if len(drawingLibrary) != ghostLibrarySize {
		t.Errorf("drawingLibrary len = %d want %d", len(drawingLibrary), ghostLibrarySize)
	}
}
