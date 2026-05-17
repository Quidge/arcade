// Package web wires the HTTP/WebSocket surface for the lobby
// slice. It owns the route table, the lobby + home templates, and
// the goroutine that translates GameSession events into JSON
// payloads for the presence broadcaster.
//
// The wire-format JSON lives entirely in this package; the
// gamesession domain knows only typed events.
package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/quidge/scribble/internal/gamesession"
	"github.com/quidge/scribble/internal/joincode"
	"github.com/quidge/scribble/internal/presence"
)

const (
	wsWriteTimeout = 5 * time.Second
	maxNameLength  = 32
)

// closePolicyDuplicateName / closePolicyCapExceeded are the
// machine-readable Reason strings used in the WebSocket close
// frame for the named rejection cases. Integration tests assert
// on these.
const (
	closePolicyDuplicateName = "duplicate name: that display name is already taken in this session"
	closePolicyCapExceeded   = "session full: this game session already has 8 players"
)

//go:embed templates
var templatesFS embed.FS

// rosterMsg is the wire-format envelope for a roster snapshot.
// It mirrors the schema described in the lobby slice's wire-format
// section: {"type":"roster","players":[{"name":..., "host":...}]}.
type rosterMsg struct {
	Type    string               `json:"type"`
	Players []gamesession.Player `json:"players"`
}

// Server holds the wired-up Registry, the per-session broadcasters,
// and the parsed templates. Construct via New.
type Server struct {
	registry *gamesession.Registry

	mu    sync.Mutex
	rooms map[string]*presence.Broadcaster

	tmpl *templates

	// Sourced from the binary's build-time ldflags; used by the
	// base template's footer. Treated as opaque text.
	gitSHA string
}

type templates struct {
	home  *template.Template
	lobby *template.Template
	nf    *template.Template
}

// New constructs a Server. The caller may share registry across
// instances if multiple muxes are in play.
func New(registry *gamesession.Registry, gitSHA string) *Server {
	t := &templates{
		home:  parseTmpl("templates/pages/home.tmpl"),
		lobby: parseTmpl("templates/pages/lobby.tmpl"),
		nf:    parseTmpl("templates/pages/not_found.tmpl"),
	}
	return &Server{
		registry: registry,
		rooms:    map[string]*presence.Broadcaster{},
		tmpl:     t,
		gitSHA:   gitSHA,
	}
}

func parseTmpl(page string) *template.Template {
	return template.Must(template.ParseFS(templatesFS,
		"templates/base.tmpl",
		page,
	))
}

// Routes registers the slice's routes on mux. Existing routes on
// mux are left alone; the caller is expected to also register the
// home and /healthz handlers.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("POST /g", s.handleCreate)
	mux.HandleFunc("GET /g/{code}", s.handleLobby)
	mux.HandleFunc("GET /g/{code}/ws", s.handleWS)
}

type baseData struct {
	Title  string
	Year   int
	GitSHA string
}

type homeData struct {
	baseData
}

type lobbyData struct {
	baseData
	Code        string // canonical, no dash
	DisplayCode string // dashed display form
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	render(w, s.tmpl.home, "base.tmpl", homeData{
		baseData: s.baseData("Home"),
	})
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	g := s.registry.Create()
	// Spawn the pump now so it's already consuming by the time the
	// creator's WebSocket connects. Events emitted before any
	// subscribers are joined are still consumed (and broadcast into
	// the void), which keeps the channel non-stalled.
	s.ensureRoom(g)
	http.Redirect(w, r, "/g/"+joincode.Format(g.Code()), http.StatusSeeOther)
}

func (s *Server) handleLobby(w http.ResponseWriter, r *http.Request) {
	canon, ok := joincode.Parse(r.PathValue("code"))
	if !ok {
		s.renderNotFound(w)
		return
	}
	if _, found := s.registry.Lookup(canon); !found {
		s.renderNotFound(w)
		return
	}
	render(w, s.tmpl.lobby, "base.tmpl", lobbyData{
		baseData:    s.baseData("Lobby"),
		Code:        canon,
		DisplayCode: joincode.Format(canon),
	})
}

func (s *Server) renderNotFound(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	render(w, s.tmpl.nf, "base.tmpl", homeData{
		baseData: s.baseData("Not found"),
	})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	canon, ok := joincode.Parse(r.PathValue("code"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	session, found := s.registry.Lookup(canon)
	if !found {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" || len(name) > maxNameLength {
		http.Error(w, "invalid display name", http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Same-origin is the realistic case (lobby page opened the
		// socket). Browsers send Origin; tests dialing httptest
		// servers send a localhost Origin. Accept all for now;
		// tightening is a later concern.
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		log.Printf("ws accept: %v", err)
		return
	}
	// Ownership of conn is ours from here.
	defer func() { _ = conn.CloseNow() }()

	if _, err := session.Join(name); err != nil {
		var reason string
		switch {
		case errors.Is(err, gamesession.ErrDuplicateName):
			reason = closePolicyDuplicateName
		case errors.Is(err, gamesession.ErrCapExceeded):
			reason = closePolicyCapExceeded
		default:
			reason = "join failed"
		}
		_ = conn.Close(websocket.StatusPolicyViolation, reason)
		return
	}
	defer session.Leave(name)

	// Write the current roster directly to this conn before
	// subscribing. The pump's broadcast for this Join went out to
	// existing subscribers (not yet including us), so we owe this
	// conn its initial snapshot.
	if err := writeRoster(conn, session.Roster()); err != nil {
		log.Printf("ws initial write: %v", err)
		return
	}

	b := s.ensureRoom(session)
	_ = b.Subscribe(r.Context(), conn)
}

// ensureRoom returns (and lazily creates) the broadcaster for g.
// On creation it starts the per-session pump goroutine.
func (s *Server) ensureRoom(g *gamesession.GameSession) *presence.Broadcaster {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.rooms[g.Code()]; ok {
		return b
	}
	b := presence.New()
	s.rooms[g.Code()] = b
	go s.runPump(g, b)
	return b
}

// runPump consumes session events and broadcasts each one as a
// JSON roster snapshot.
func (s *Server) runPump(g *gamesession.GameSession, b *presence.Broadcaster) {
	for e := range g.Events() {
		payload, err := json.Marshal(rosterMsg{
			Type:    "roster",
			Players: e.Roster,
		})
		if err != nil {
			log.Printf("marshal roster: %v", err)
			continue
		}
		b.Broadcast(payload)
	}
}

func writeRoster(conn *websocket.Conn, players []gamesession.Player) error {
	payload, err := json.Marshal(rosterMsg{
		Type:    "roster",
		Players: players,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, payload)
}

func (s *Server) baseData(title string) baseData {
	return baseData{
		Title:  title,
		Year:   time.Now().Year(),
		GitSHA: s.gitSHA,
	}
}

func render(w http.ResponseWriter, tmpl *template.Template, name string, data any) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := buf.WriteTo(w); err != nil {
		log.Printf("write %s: %v", name, err)
	}
}
