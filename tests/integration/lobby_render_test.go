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

func TestHomeHTMLContainsHostFormAndJoinForm(t *testing.T) {
	srv, _ := newApp(t)

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
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

	// Host form: existing POST /g remains the create path.
	if !strings.Contains(html, `action="/g"`) || !strings.Contains(html, `method="POST"`) {
		t.Errorf("rendered home missing POST /g host form")
	}
	if !strings.Contains(html, "Host a new game") {
		t.Errorf("rendered home missing 'Host a new game' button label")
	}

	// Join form: code input with the agreed-on attributes.
	wantInputAttrs := []string{
		`id="join-code"`,
		`autocomplete="off"`,
		`autocapitalize="characters"`,
		`pattern=`,
		`maxlength="7"`,
		`inputmode="text"`,
	}
	for _, a := range wantInputAttrs {
		if !strings.Contains(html, a) {
			t.Errorf("rendered home missing join-code attribute %q", a)
		}
	}
	if !strings.Contains(html, `id="join-error"`) {
		t.Errorf("rendered home missing join-error band")
	}
}

func TestLobbyHEADReturns200ForExistingCode(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)

	req, err := http.NewRequest(http.MethodHead, srv.URL+"/g/"+joincode.Format(code), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("HEAD existing code status = %d want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("HEAD response body should be empty, got %d bytes", len(body))
	}
}

func TestLobbyHEADReturns404ForUnknownCode(t *testing.T) {
	srv, _ := newApp(t)

	req, err := http.NewRequest(http.MethodHead, srv.URL+"/g/Z9Z-Z9Z", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("HEAD unknown code status = %d want 404", resp.StatusCode)
	}
}

func TestLobbyHEADReturns404ForMalformedCode(t *testing.T) {
	srv, _ := newApp(t)

	req, err := http.NewRequest(http.MethodHead, srv.URL+"/g/not-a-code", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("HEAD malformed code status = %d want 404", resp.StatusCode)
	}
}

func TestLobbyHEADIsCaseAndDashTolerant(t *testing.T) {
	srv, _ := newApp(t)
	code := createSession(t, srv)
	formatted := joincode.Format(code) // dashed, upper
	variants := []string{
		formatted,
		strings.ToLower(formatted),
		code, // canonical, no dash
	}
	for _, v := range variants {
		req, err := http.NewRequest(http.MethodHead, srv.URL+"/g/"+v, nil)
		if err != nil {
			t.Fatalf("NewRequest %q: %v", v, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("HEAD %q: %v", v, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("HEAD %q status = %d want 200", v, resp.StatusCode)
		}
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
