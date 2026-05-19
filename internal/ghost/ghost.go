// Package ghost provides canned Entry content for absent Players.
// At Round-end, any seat whose Draft is empty has its slot filled
// by a Ghost-supplied Entry visibly labeled "[Player]'s Ghost" in
// the UI (per ADR 0003).
//
// The Provider is seedable so tests can pin the selection sequence,
// and best-effort anti-collision is layered on top: when several
// Ghosts on the same Round draw from a small library the Picker
// prefers entries not yet handed out. Other slot kinds (guess
// Caption, Drawing) are accepted by the interface so future slices
// can extend without rework; only StarterCaption is exercised by
// this slice.
package ghost

import (
	"math/rand"
	"sync"
)

// SlotKind discriminates the Entry shape a Ghost is being asked
// to supply. Round 0 only uses StarterCaption; later slices add
// GuessCaption (the response to a Drawing) and Drawing (the
// response to a Caption).
type SlotKind int

const (
	// StarterCaption is the slot kind for Round 0 — the first
	// Caption in a Chain, invented from nothing.
	StarterCaption SlotKind = iota
	// GuessCaption is the slot kind for Captions written in
	// response to a Drawing. Stubbed for future use.
	GuessCaption
	// Drawing is the slot kind for visual Entries. Stubbed for
	// future use; this slice never picks a Drawing Ghost.
	Drawing
)

// starterCaptionLibrary is the in-repo canned starter Caption
// library. Phrasing is deliberately on the goofy side of plausible
// so Ghost Entries read as part of the joke when they land in a
// Chain. Keep entries short (one sentence) per the encouraged
// shape from CONTEXT.md.
var starterCaptionLibrary = []string{
	"a cat playing a tiny piano",
	"the moon eating a hamburger",
	"two squirrels reviewing a contract",
	"a wizard losing an argument with a goose",
	"the inventor of the spoon takes a victory lap",
	"a robot reading a love letter in a forest",
	"a librarian discovers a secret door behind the encyclopedias",
	"three penguins waiting in line for an espresso",
	"a knight fighting an aggressively polite ghost",
	"the world's smallest dragon and its enormous lunch",
	"a marching band made entirely of frogs",
	"someone's grandmother accidentally invents jazz",
}

// genericFallback is returned for slot kinds the library does not
// yet stock. Round 0 never reaches it — kept here so callers can
// pass any SlotKind without crashing while later slices grow the
// library.
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
	default:
		return nil
	}
}
