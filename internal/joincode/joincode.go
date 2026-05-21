// Package joincode implements the codec for GameSession join codes —
// short, human-readable identifiers used in URLs and shared by Hosts
// to invite other Players (ADR 0002).
//
// # Forms
//
// A join code has two forms:
//
//   - Canonical form — 6 characters, uppercase, no dash. The internal
//     representation; used as the registry key, in storage, and
//     anywhere a code is compared. Name variables canonicalJoinCode.
//   - Display form — 7 characters, uppercase, dashed after char 3
//     (e.g. ABC-123). The form shown to users. Name variables
//     displayJoinCode.
//
// Convert with Parse (any input → canonical) and Format (canonical →
// display). Unvalidated input — e.g. an HTTP path value — should be
// named rawJoinCode until Parse has accepted it.
//
// # Alphabet
//
// The 32-character alphabet excludes I, L, O, and U to avoid visual
// confusion with 1, 1, 0, and V:
//
//	0123456789ABCDEFGHJKMNPQRSTVWXYZ
//
// When mirroring this alphabet in another language (regex, HTML
// pattern, etc.), enumerate it directly: character-range shortcuts
// like A-H plus J-N easily and silently re-admit excluded letters.
// Any source file that duplicates the alphabet MUST carry a comment
// above the duplicating expression pointing back to this package
// doc.
package joincode

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
)

// Alphabet is the canonical Crockford-derived join-code alphabet;
// see the package doc for forms, mirroring rules, and exclusions.
const Alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// CodeLength is the canonical length of a generated join code (in
// characters).
const CodeLength = 6

var decodeTable = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	for i, c := range Alphabet {
		t[byte(c)] = int8(i)
		// Accept lowercase on decode.
		if c >= 'A' && c <= 'Z' {
			t[byte(c-'A'+'a')] = int8(i)
		}
	}
	return t
}()

// Encode turns a byte slice into a Crockford-base32 string. Bytes
// are packed big-endian, 5 bits per output character; if the input
// is not a multiple of 5 bytes the trailing bits are padded with
// zero bits.
func Encode(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	outLen := (len(b)*8 + 4) / 5
	out := make([]byte, outLen)
	var buf uint64
	var bits uint
	bi := 0
	for _, x := range b {
		buf = (buf << 8) | uint64(x)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out[bi] = Alphabet[(buf>>bits)&0x1f]
			bi++
		}
	}
	if bits > 0 {
		out[bi] = Alphabet[(buf<<(5-bits))&0x1f]
	}
	return string(out)
}

// Decode is the inverse of Encode. Input is case-insensitive and may
// not contain any off-alphabet characters; dashes are not stripped.
// The returned slice has length len(s)*5/8 (any trailing bits are
// dropped).
func Decode(s string) ([]byte, error) {
	if len(s) == 0 {
		return nil, nil
	}
	out := make([]byte, len(s)*5/8)
	var buf uint64
	var bits uint
	bi := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		v := decodeTable[c]
		if v < 0 {
			return nil, fmt.Errorf("joincode: invalid character %q at offset %d", c, i)
		}
		buf = (buf << 5) | uint64(v)
		bits += 5
		if bits >= 8 {
			bits -= 8
			if bi < len(out) {
				out[bi] = byte((buf >> bits) & 0xff)
				bi++
			}
		}
	}
	return out, nil
}

// Generate produces a fresh CodeLength-character canonical join code
// from crypto/rand.
func Generate() string {
	var buf [CodeLength]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(fmt.Sprintf("joincode: rand.Read: %v", err))
	}
	out := make([]byte, CodeLength)
	for i, x := range buf {
		out[i] = Alphabet[int(x)%len(Alphabet)]
	}
	return string(out)
}

// ErrInvalid is returned by Parse when the input is not a valid
// CodeLength-character join code. It is exposed primarily for
// error-equality checks in callers.
var ErrInvalid = errors.New("joincode: invalid code")

// Parse normalizes a user-supplied code into its canonical form
// (uppercase, no dash) and reports whether it is well-formed.
// Input is case-insensitive and any dash characters are ignored.
// Off-alphabet characters (notably I, L, O, U) cause ok=false.
func Parse(input string) (string, bool) {
	stripped := strings.ReplaceAll(input, "-", "")
	if len(stripped) != CodeLength {
		return "", false
	}
	out := make([]byte, CodeLength)
	for i := 0; i < CodeLength; i++ {
		c := stripped[i]
		if c >= 'a' && c <= 'z' {
			c = c - 'a' + 'A'
		}
		if decodeTable[c] < 0 {
			return "", false
		}
		out[i] = c
	}
	return string(out), true
}

// Format renders a canonical code in its display form: uppercase
// with a dash after the third character. Input must be canonical;
// non-canonical input is returned unchanged.
func Format(canon string) string {
	if len(canon) != CodeLength {
		return canon
	}
	return canon[:3] + "-" + canon[3:]
}
