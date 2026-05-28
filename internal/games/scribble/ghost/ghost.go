// Package ghost provides canned Entry content for absent Players.
// At Round-end, any seat whose Draft is empty has its slot filled
// by a Ghost-supplied Entry visibly labeled "[Player]'s Ghost" in
// the UI (per ADR 0003).
//
// The Provider is seedable so tests can pin the selection sequence,
// and best-effort anti-collision is layered on top: when several
// Ghosts on the same Round draw from a small library the Picker
// prefers entries not yet handed out. All three slot kinds —
// StarterCaption, GuessCaption, and Drawing — are stocked from
// in-repo indexed-stub libraries sized to gamesession.MaxPlayers,
// so a Round-end fill at the cap hands out distinct content without
// hitting the fallback-to-repeat branch.
package ghost

import (
	"math/rand"
	"strconv"
	"sync"

	"github.com/quidge/scribble/internal/games/scribble/strokes"
)

// SlotKind discriminates the Entry shape a Ghost is being asked
// to supply. Captions (StarterCaption, GuessCaption) are served
// from the in-package string libraries via Pick; Drawings are
// served from drawingLibrary via PickDrawing.
type SlotKind int

const (
	// StarterCaption is the slot kind for Round 0 — the first
	// Caption in a Chain, invented from nothing.
	StarterCaption SlotKind = iota
	// GuessCaption is the slot kind for Captions written in
	// response to a Drawing (even-numbered Rounds after Round 0).
	GuessCaption
	// Drawing is the slot kind for visual Entries. Served by
	// PickDrawing rather than Pick.
	Drawing
)

// ghostLibrarySize is the number of entries in each indexed-stub
// library. Matches gamesession.MaxPlayers so that a Round-end fill
// at the cap can hand out one distinct Ghost Entry per absent seat
// without falling back to the exhaustion branch in Pick.
const ghostLibrarySize = 10

// starterCaptionLibrary, guessCaptionLibrary, and drawingLibrary
// are indexed stubs (e.g. "Ghost starter 1".."Ghost starter 10",
// 10 single-stroke horizontal lines). The stubs exist for
// observability — at any N up to the cap, each absent Player's
// Ghost contributes visibly-distinct content so a reader can tell
// the absences apart in the Reveal. Real curated content is a
// separate concern; see issue #43.
//
// guessCaptionLibrary is kept separate from starterCaptionLibrary
// because the starter shape is expected to stay a flat list while
// the guess shape will plausibly need context-awareness in a
// future refactor.
var (
	starterCaptionLibrary = generateStubCaptions("Ghost starter ", ghostLibrarySize)
	guessCaptionLibrary   = generateStubCaptions("Ghost guess ", ghostLibrarySize)
	drawingLibrary        = generateStubDrawings(ghostLibrarySize)
)

func generateStubCaptions(prefix string, n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = prefix + strconv.Itoa(i+1)
	}
	return out
}

// generateStubDrawings returns n horizontal-line single-stroke
// drawings, evenly spaced top to bottom at y = (i+1) / (n+1) so
// that no entry lands on the top or bottom edge.
func generateStubDrawings(n int) [][]strokes.Stroke {
	out := make([][]strokes.Stroke, n)
	for i := 0; i < n; i++ {
		y := float64(i+1) / float64(n+1)
		out[i] = []strokes.Stroke{
			{{X: 0.1, Y: y}, {X: 0.9, Y: y}},
		}
	}
	return out
}

// genericFallback is returned for slot kinds the library does not
// yet stock. In normal play it is effectively unreachable now that
// every SlotKind has a library; kept as a defense so callers can
// pass any SlotKind without crashing.
const genericFallback = "(ghost — content coming soon)"

// Provider hands out canned Entries on behalf of absent Players.
// The zero value is not usable; obtain one via New.
type Provider struct {
	mu  sync.Mutex
	rng *rand.Rand
}

// New constructs a Provider with the given seed. Pass 0 for a
// deterministic test-reproducible Provider; pass time.Now().UnixNano()
// (or similar) in production wiring for variety across GameSessions.
func New(seed int64) *Provider {
	return &Provider{rng: rand.New(rand.NewSource(seed))}
}

// Picker is a per-Round handle that remembers which entries have
// already been handed out so the Provider can best-effort avoid
// handing out the same Entry twice within one Round-end fill.
type Picker struct {
	provider *Provider
	used     map[string]struct{}
}

// Picker returns a fresh Picker bound to p. Callers create one
// Picker per Round-end and call Pick once per absent Player.
func (p *Provider) Picker() *Picker {
	return &Picker{provider: p, used: map[string]struct{}{}}
}

// PickDrawing returns a canned Ghost Drawing for player. It draws
// from drawingLibrary using the same best-effort anti-collision
// pattern as Pick (track which entries have been handed out by
// this Picker; fall back to a random repeat if the library is
// exhausted).
func (p *Picker) PickDrawing(player string) []strokes.Stroke {
	if len(drawingLibrary) == 0 {
		return nil
	}
	p.provider.mu.Lock()
	defer p.provider.mu.Unlock()
	start := p.provider.rng.Intn(len(drawingLibrary))
	for off := 0; off < len(drawingLibrary); off++ {
		idx := (start + off) % len(drawingLibrary)
		key := drawingKey(idx)
		if _, taken := p.used[key]; !taken {
			p.used[key] = struct{}{}
			return cloneStrokes(drawingLibrary[idx])
		}
	}
	return cloneStrokes(drawingLibrary[start])
}

func drawingKey(idx int) string {
	// A namespaced key so Pick (captions) and PickDrawing can share
	// the same `used` set without colliding with caption strings.
	return "drawing:" + strconv.Itoa(idx)
}

func cloneStrokes(src []strokes.Stroke) []strokes.Stroke {
	if len(src) == 0 {
		return nil
	}
	out := make([]strokes.Stroke, len(src))
	for i, s := range src {
		out[i] = append(strokes.Stroke(nil), s...)
	}
	return out
}

// Pick returns a Ghost Entry for (player, kind). It first attempts
// to find an entry not yet handed out by this Picker; if every
// entry has already been used (library smaller than the absent
// roster) it falls back to a uniformly-random pick. The provider
// is locked while the rng is consulted, so Pick is safe to call
// from any goroutine.
func (p *Picker) Pick(player string, kind SlotKind) string {
	library := libraryFor(kind)
	if len(library) == 0 {
		return genericFallback
	}
	p.provider.mu.Lock()
	defer p.provider.mu.Unlock()

	// Walk the library in a randomly-rotated order so the first
	// unused entry the loop encounters is effectively chosen at
	// random.
	start := p.provider.rng.Intn(len(library))
	for off := 0; off < len(library); off++ {
		cand := library[(start+off)%len(library)]
		if _, taken := p.used[cand]; !taken {
			p.used[cand] = struct{}{}
			return cand
		}
	}
	// Library exhausted; fall back to a random pick without
	// removing it from the used set (subsequent Picks will keep
	// returning duplicates from this branch — acceptable since the
	// only way to reach it is "more absent Players than library
	// entries," which is well past anything we'd actually expect).
	return library[start]
}

func libraryFor(kind SlotKind) []string {
	switch kind {
	case StarterCaption:
		return starterCaptionLibrary
	case GuessCaption:
		return guessCaptionLibrary
	default:
		return nil
	}
}
