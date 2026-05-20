// Package roundcomplete owns the Telestrations alternation rules
// at Round-end: which content kind (Caption vs Drawing) a Round
// requires from each seat, which Ghost slot kind fills an absent
// seat's slot, and how to materialize each seat's chain.Entry
// from the appropriate Draft store with Ghost fallback (per
// CONTEXT.md's Round and Ghost definitions, ADR 0003).
//
// The package is pure of WebSocket, JSON, and GameSession
// concerns: callers hand in the two Draft stores and a Ghost
// Picker, callers receive []chain.Entry. The web layer's
// onRoundEnd uses Materialize to obtain the Round-end Entries
// before appending them to the Chain store, broadcasting, and
// advancing the GameSession phase.
//
// ContentKindForRound is the canonical home for the Round-parity
// content rule. Beyond materialization the web layer consults it
// to choose which Draft store to write into during streaming
// (the "draft" command), to seal on Submit, and to read for the
// unicast round-state snapshot — so the parity rule has one home
// across all four call sites.
//
// Test surface (ADR 0006): Materialize and the per-kind helpers
// are unit-testable in isolation using the real draft, strokes,
// and ghost constructors — no WebSocket, no GameSession, no
// Chain store required.
package roundcomplete

import (
	"github.com/quidge/scribble/internal/chain"
	"github.com/quidge/scribble/internal/draft"
	"github.com/quidge/scribble/internal/ghost"
	"github.com/quidge/scribble/internal/strokes"
)

// ContentKindForRound maps a Round number to the content kind
// every seat fills during that Round. Telestrations alternates
// Caption → Drawing: even-numbered Rounds (0, 2, 4, …) are
// Captions; odd-numbered Rounds (1, 3, 5, …) are Drawings.
func ContentKindForRound(r int) string {
	if r%2 == 0 {
		return "caption"
	}
	return "drawing"
}

// Materialize assembles one chain.Entry per seat for the
// just-ended Round, taking each seat's content from the
// appropriate Draft store and falling back to a Ghost-supplied
// Entry for any seat whose Draft is empty (per CONTEXT.md's
// Round definition and ADR 0003).
//
// roundNum drives two related decisions:
//   - which content kind the Round produces (Caption or Drawing),
//     consulted via ContentKindForRound;
//   - for Caption Rounds, which Ghost slot kind fills the gap —
//     StarterCaption at Round 0 (the Chain's first Caption,
//     invented from nothing), GuessCaption at later Caption
//     Rounds (written in response to the preceding Drawing).
//
// textDrafts and strokeDrafts are both required because the
// caller holds both; Materialize picks which to consult by
// parity. seats is the seat list the Round controller closed
// over, in join order — the returned slice preserves that order.
func Materialize(
	roundNum int,
	seats []string,
	textDrafts *draft.Store,
	strokeDrafts *strokes.Store,
	picker *ghost.Picker,
) []chain.Entry {
	switch ContentKindForRound(roundNum) {
	case "caption":
		return materializeCaptions(roundNum, seats, textDrafts, picker)
	case "drawing":
		return materializeDrawings(roundNum, seats, strokeDrafts, picker)
	}
	// Unreachable: ContentKindForRound returns one of the two
	// above for every int.
	return nil
}

// materializeCaptions builds one Caption chain.Entry per seat. A
// seat whose Draft text is empty receives a Ghost Entry whose
// slot kind is StarterCaption at Round 0 (the Chain's first
// Caption, invented from nothing) or GuessCaption at later
// Caption Rounds (written in response to the immediately-preceding
// Drawing).
func materializeCaptions(
	roundNum int,
	seats []string,
	drafts *draft.Store,
	picker *ghost.Picker,
) []chain.Entry {
	ghostKind := ghost.StarterCaption
	if roundNum > 0 {
		ghostKind = ghost.GuessCaption
	}
	entries := make([]chain.Entry, 0, len(seats))
	for _, seat := range seats {
		snap := drafts.Get(roundNum, seat)
		if snap.Text == "" {
			entries = append(entries, chain.Entry{
				Player: seat,
				Kind:   chain.EntryCaption,
				Ghost:  true,
				Text:   picker.Pick(seat, ghostKind),
			})
			continue
		}
		entries = append(entries, chain.Entry{
			Player: seat,
			Kind:   chain.EntryCaption,
			Ghost:  false,
			Text:   snap.Text,
		})
	}
	return entries
}

// materializeDrawings builds one Drawing chain.Entry per seat. A
// seat whose stroke Draft is empty receives a Ghost Drawing from
// the canned library — Drawing Ghosts do not require a SlotKind
// dispatch since the Ghost package exposes PickDrawing directly.
func materializeDrawings(
	roundNum int,
	seats []string,
	drafts *strokes.Store,
	picker *ghost.Picker,
) []chain.Entry {
	entries := make([]chain.Entry, 0, len(seats))
	for _, seat := range seats {
		snap := drafts.Get(roundNum, seat)
		if len(snap.Strokes) == 0 {
			entries = append(entries, chain.Entry{
				Player:  seat,
				Kind:    chain.EntryDrawing,
				Ghost:   true,
				Strokes: picker.PickDrawing(seat),
			})
			continue
		}
		entries = append(entries, chain.Entry{
			Player:  seat,
			Kind:    chain.EntryDrawing,
			Ghost:   false,
			Strokes: []strokes.Stroke(snap.Strokes),
		})
	}
	return entries
}
