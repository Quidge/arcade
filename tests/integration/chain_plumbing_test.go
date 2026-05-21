//go:build integration

package integration_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/quidge/scribble/internal/chain"
	"github.com/quidge/scribble/internal/gamesession"
	"github.com/quidge/scribble/internal/web"
)

// newAppWithServer mirrors newApp but also returns the *web.Server
// so tests can reach the per-room chain.Store via the exported
// accessor.
func newAppWithServer(t *testing.T) (*httptest.Server, *gamesession.Registry, *web.Server) {
	t.Helper()
	reg := gamesession.NewRegistry()
	srvWeb := web.New(reg, "test")
	mux := http.NewServeMux()
	srvWeb.Routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, reg, srvWeb
}

func TestRoundZeroEntriesLandInChainStore(t *testing.T) {
	srv, _, webSrv := newAppWithServer(t)
	canonicalJoinCode := createSession(t, srv)

	alice, _ := dialAs(t, srv, canonicalJoinCode, "Alice")
	defer alice.CloseNow()
	drainToRosterSize(t, alice, 1)
	bob, _ := dialAs(t, srv, canonicalJoinCode, "Bob")
	defer bob.CloseNow()
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	t60 := 60
	startRound(t, alice, &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	sendCmd(t, alice, map[string]any{"type": "draft", "text": "alice's starter caption"})
	sendCmd(t, bob, map[string]any{"type": "draft", "text": "bob's starter caption"})
	sendCmd(t, alice, map[string]any{"type": "submit"})
	sendCmd(t, bob, map[string]any{"type": "submit"})

	_ = readUntilType(t, alice, "round-ended")
	// Give the OnEnd callback a beat to land entries in the chain
	// store (round-ended broadcasts after Append, but the order is
	// observable through the network; let the server stabilize).
	time.Sleep(20 * time.Millisecond)

	cs := webSrv.ChainStoreForCode(canonicalJoinCode)
	if cs == nil {
		t.Fatalf("ChainStoreForCode returned nil")
	}
	if got := cs.N(); got != 2 {
		t.Fatalf("chain.N = %d want 2", got)
	}
	// chains.Entries(0) is Alice's Chain — round 0 starter is Alice.
	chain0 := cs.Entries(0)
	if len(chain0) != 1 {
		t.Fatalf("Entries(0) len = %d want 1: %+v", len(chain0), chain0)
	}
	if chain0[0].Player != "Alice" {
		t.Errorf("Entries(0)[0].Player = %q want Alice", chain0[0].Player)
	}
	if chain0[0].Kind != chain.EntryCaption {
		t.Errorf("Entries(0)[0].Kind = %v want EntryCaption", chain0[0].Kind)
	}
	if chain0[0].Text != "alice's starter caption" {
		t.Errorf("Entries(0)[0].Text = %q want alice's starter caption", chain0[0].Text)
	}
	if chain0[0].Ghost {
		t.Errorf("Entries(0)[0] marked Ghost despite typing: %+v", chain0[0])
	}

	// chains.Entries(1) is Bob's Chain.
	chain1 := cs.Entries(1)
	if len(chain1) != 1 {
		t.Fatalf("Entries(1) len = %d want 1: %+v", len(chain1), chain1)
	}
	if chain1[0].Player != "Bob" {
		t.Errorf("Entries(1)[0].Player = %q want Bob", chain1[0].Player)
	}
	if chain1[0].Text != "bob's starter caption" {
		t.Errorf("Entries(1)[0].Text = %q want bob's starter caption", chain1[0].Text)
	}
}

func TestRoundZeroGhostEntryAttributedAndAppendedAsGhost(t *testing.T) {
	srv, _, webSrv := newAppWithServer(t)
	canonicalJoinCode := createSession(t, srv)

	alice, _ := dialAs(t, srv, canonicalJoinCode, "Alice")
	defer alice.CloseNow()
	drainToRosterSize(t, alice, 1)
	bob, _ := dialAs(t, srv, canonicalJoinCode, "Bob")
	defer bob.CloseNow()
	drainToRosterSize(t, alice, 2)
	drainToRosterSize(t, bob, 2)

	// Bob types nothing; Alice force-advances.
	t60 := 60
	startRound(t, alice, &t60)
	_ = readUntilType(t, alice, "round-state")
	_ = readUntilType(t, bob, "round-state")

	sendCmd(t, alice, map[string]any{"type": "draft", "text": "alice typed"})
	sendCmd(t, alice, map[string]any{"type": "advance"})
	_ = readUntilType(t, alice, "round-ended")
	time.Sleep(20 * time.Millisecond)

	cs := webSrv.ChainStoreForCode(canonicalJoinCode)
	chain1 := cs.Entries(1) // Bob's Chain
	if len(chain1) != 1 {
		t.Fatalf("Entries(1) len = %d want 1", len(chain1))
	}
	if !chain1[0].Ghost {
		t.Errorf("Bob's empty Draft should land as Ghost in chain: %+v", chain1[0])
	}
	if chain1[0].Player != "Bob" {
		t.Errorf("Ghost entry attributed to %q want Bob", chain1[0].Player)
	}
	if chain1[0].Text == "" {
		t.Errorf("Ghost entry text empty")
	}
}
