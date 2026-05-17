package joincode

import (
	"crypto/rand"
	"strings"
	"testing"
)

func TestEncodeDecodeRoundtripMultipleOf5Bytes(t *testing.T) {
	// 8 bits * 5 bytes == 40 bits == 8 chars, lossless.
	for _, n := range []int{5, 10, 15, 25} {
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			t.Fatalf("rand.Read: %v", err)
		}
		s := Encode(b)
		got, err := Decode(s)
		if err != nil {
			t.Fatalf("Decode(%q): %v", s, err)
		}
		if string(got) != string(b) {
			t.Errorf("roundtrip %d bytes: got %x want %x", n, got, b)
		}
	}
}

func TestDecodeRejectsOffAlphabetChars(t *testing.T) {
	for _, c := range []string{"I", "L", "O", "U", "i", "l", "o", "u", "!", "@"} {
		if _, err := Decode("ABCD" + c); err == nil {
			t.Errorf("Decode accepted off-alphabet char %q", c)
		}
	}
}

func TestParseAcceptsMixedCaseAndDashes(t *testing.T) {
	canon, ok := Parse("a4b-k9p")
	if !ok {
		t.Fatalf("Parse(\"a4b-k9p\") returned ok=false")
	}
	if canon != "A4BK9P" {
		t.Errorf("Parse canonical = %q want %q", canon, "A4BK9P")
	}

	canon2, ok := Parse("A4BK9P")
	if !ok || canon2 != "A4BK9P" {
		t.Errorf("Parse without dash: got %q ok=%v", canon2, ok)
	}

	canon3, ok := Parse("--A4-B-K9-P--")
	if !ok || canon3 != "A4BK9P" {
		t.Errorf("Parse with extra dashes: got %q ok=%v", canon3, ok)
	}
}

func TestParseRejectsConfusables(t *testing.T) {
	for _, in := range []string{"ABCDEI", "ABCDEL", "ABCDEO", "ABCDEU", "abcdei", "ABC-DEL"} {
		if _, ok := Parse(in); ok {
			t.Errorf("Parse(%q) should have failed (contains I/L/O/U)", in)
		}
	}
}

func TestParseRejectsWrongLength(t *testing.T) {
	for _, in := range []string{"", "ABC", "ABCDE", "ABCDEFG", "ABC-DEF-GHJ"} {
		if _, ok := Parse(in); ok {
			t.Errorf("Parse(%q) should have failed (length != %d after stripping dashes)", in, CodeLength)
		}
	}
}

func TestGenerateProducesCanonical(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c := Generate()
		if len(c) != CodeLength {
			t.Fatalf("Generate produced %q (len %d, want %d)", c, len(c), CodeLength)
		}
		for j := 0; j < CodeLength; j++ {
			if !strings.ContainsRune(Alphabet, rune(c[j])) {
				t.Fatalf("Generate produced %q containing off-alphabet char %q", c, c[j])
			}
		}
		if canon, ok := Parse(c); !ok || canon != c {
			t.Fatalf("Parse(Generate()) = %q ok=%v", canon, ok)
		}
		seen[c] = true
	}
	// Probabilistic: 200 6-char codes from a 32^6 ≈ 10^9 space — collisions
	// are astronomically unlikely; we sanity-check >190 unique.
	if len(seen) < 190 {
		t.Fatalf("Generate collided too often: %d unique of 200", len(seen))
	}
}

func TestFormat(t *testing.T) {
	if got := Format("A4BK9P"); got != "A4B-K9P" {
		t.Errorf("Format(A4BK9P) = %q want A4B-K9P", got)
	}
}
