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
