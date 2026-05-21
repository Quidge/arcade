//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/quidge/scribble/internal/joincode"
)

// Behaviors moved here from e2e per ADR 0013: rendered-HTML
// assertions that don't need a real browser belong in the
// integration tier. Specifically: that the lobby page exposes the
// share-panel and share-link elements so the client-side JS can
// populate the share URL, and that the canonical join code appears
// in the rendered page so a user landing on /g/<code> sees a page
// that identifies the session.

func TestLobbyHTMLContainsShareLinkScaffold(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	resp, err := http.Get(srv.URL + "/g/" + joincode.Format(code))
	if err != nil {
		t.Fatalf("GET /g/%s: %v", code, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	html := string(body)

	if !strings.Contains(html, `id="share-panel"`) {
		t.Errorf("rendered lobby missing share-panel scaffold")
	}
	if !strings.Contains(html, `id="share-link"`) {
		t.Errorf("rendered lobby missing share-link input")
	}
	// The join code (hyphenated) is the natural identifier a user
	// would copy out of the share-link; it should appear somewhere
	// in the rendered HTML (page title, header, etc.) so a screen-
	// reader user can confirm which session this is.
	formatted := joincode.Format(code)
	if !strings.Contains(html, formatted) {
		t.Errorf("rendered lobby missing join code %q in HTML", formatted)
	}
}

func TestLobbyHTMLAcceptsLowercaseCode(t *testing.T) {
	// The Parse step in joincode accepts mixed case. handleLobby
	// renders the lobby directly (no redirect) once Parse succeeds,
	// so a lowercase URL hits 200 with the canonical (upper-case)
	// join code embedded in the rendered HTML.
	srv, _ := newApp(t)
	code := createSession(t, srv)

	formatted := joincode.Format(code)
	lower := strings.ToLower(formatted)
	resp, err := http.Get(srv.URL + "/g/" + lower)
	if err != nil {
		t.Fatalf("GET lowercase: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("lowercase URL status = %d want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), formatted) {
		t.Errorf("rendered HTML for lowercase URL missing canonical code %q", formatted)
	}
}
