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
	"fmt"
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
	"github.com/quidge/scribble/internal/seatconn"
)

const (
	wsWriteTimeout = 5 * time.Second
	maxNameLength  = 32
)

// closePolicyCapExceeded / closePolicySuperseded / closePolicyKicked
// are the machine-readable Reason strings used in the WebSocket
// close frame for the named rejection cases. Integration tests
// assert on these.
const (
	closePolicyCapExceeded = "session full: this game session already has 8 players"
	closePolicySuperseded  = "superseded: another connection took over this seat"
	closePolicyKicked      = "kicked: the host removed you from this game session"
)

// rosterMsg / noticeMsg are the two wire-format envelopes the
// lobby slice broadcasts. Roster carries the live seat snapshot
// after every domain transition; Notice carries the human-readable
// text accompanying Host changes, kicks, and voluntary leaves.
type noticeMsg struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// clientCmd is the schema for messages clients send to the server
// over the lobby WebSocket. Type is one of "transfer-host",
// "kick", "leave"; Target is the display name of the affected
// seat for the first two and unused for "leave".
type clientCmd struct {
	Type   string `json:"type"`
	Target string `json:"target,omitempty"`
}

//go:embed templates
var templatesFS embed.FS

// rosterMsg is the wire-format envelope for a roster snapshot.
// It mirrors the schema described in the lobby slice's wire-format
// section:
//
//	{"type":"roster","players":[{"name":..., "host":..., "connected":...}]}
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

	seats *seatconn.Registry

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
		seats:    seatconn.New(),
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

// wsConnHandle adapts *websocket.Conn to the seatconn.ConnHandle
// surface (a single Close(reason) method). The status code is fixed
// at PolicyViolation, which is what the lobby's other policy-based
// rejections use.
type wsConnHandle struct{ c *websocket.Conn }

func (h wsConnHandle) Close(reason string) {
	_ = h.c.Close(websocket.StatusPolicyViolation, reason)
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
	defer func() { _ = conn.CloseNow() }()

	// Acquire the seat-connection slot *before* binding to the
	// domain. If another connection holds this seat, supersede it
	// by closing its handle; the prior handler's deferred Release
	// will return wasCurrent=false and skip its Disconnect, leaving
	// the seat's connection state for us to own.
	key := canon + "/" + name
	prior, gen := s.seats.Acquire(key, wsConnHandle{c: conn})
	if prior != nil {
		prior.Close(closePolicySuperseded)
	}
	releaseDone := false
	defer func() {
		if releaseDone {
			return
		}
		if wasCurrent := s.seats.Release(key, gen); wasCurrent {
			session.Disconnect(name)
		}
	}()

	// Dispatch by seat existence. If a seat with this name already
	// exists, this is a Reconnect; otherwise it's a fresh Join.
	// We tolerate a race between HasSeat and Join: if a Join
	// returns ErrSeatExists, fall back to Reconnect.
	var domainErr error
	if session.HasSeat(name) {
		_, domainErr = session.Reconnect(name)
	} else {
		_, domainErr = session.Join(name)
		if errors.Is(domainErr, gamesession.ErrSeatExists) {
			_, domainErr = session.Reconnect(name)
		}
	}
	if domainErr != nil {
		var reason string
		switch {
		case errors.Is(domainErr, gamesession.ErrCapExceeded):
			reason = closePolicyCapExceeded
		default:
			reason = "join failed"
		}
		// Release before close so wasCurrent=true does not fire a
		// stray Disconnect on a seat that was never bound.
		s.seats.Release(key, gen)
		releaseDone = true
		_ = conn.Close(websocket.StatusPolicyViolation, reason)
		return
	}

	// Write the current roster directly to this conn before
	// subscribing. The pump's broadcast for our Join/Reconnect (if
	// any) went out to existing subscribers (not yet including us),
	// so we owe this conn its initial snapshot.
	if err := writeRoster(conn, session.Roster()); err != nil {
		log.Printf("ws initial write: %v", err)
		return
	}

	b := s.ensureRoom(session)
	sub := b.Add(conn)
	defer b.Remove(sub)

	// Read loop: dispatch client commands until the conn closes.
	// A nil command (parse error, unknown type) is logged but does
	// not tear down the connection.
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		s.handleCommand(session, b, canon, name, data)
	}
}

// handleCommand parses one inbound text frame from a seat's WS
// connection and dispatches the named verb against the session.
// selfName is the display name of the seat that owns the
// connection — authorization checks (e.g. only Host can kick)
// are applied here. Errors are logged and discarded; the WS
// remains open so the client can retry.
func (s *Server) handleCommand(session *gamesession.GameSession, b *presence.Broadcaster, canon, selfName string, data []byte) {
	var cmd clientCmd
	if err := json.Unmarshal(data, &cmd); err != nil {
		log.Printf("ws cmd parse from %s: %v", selfName, err)
		return
	}
	switch cmd.Type {
	case "transfer-host":
		target := strings.TrimSpace(cmd.Target)
		if target == "" {
			return
		}
		if err := session.TransferHost(selfName, target); err != nil {
			log.Printf("transfer-host %s->%s: %v", selfName, target, err)
		}
	case "kick":
		target := strings.TrimSpace(cmd.Target)
		if target == "" || target == selfName {
			return
		}
		// Only the current Host may kick. Re-check inside the
		// switch in case the badge moved between the client
		// rendering the button and the message arriving.
		if currentHost(session) != selfName {
			return
		}
		if err := session.Leave(target); err != nil {
			log.Printf("kick %s by %s: %v", target, selfName, err)
			return
		}
		s.seats.Close(canon+"/"+target, closePolicyKicked)
		broadcastNotice(b, fmt.Sprintf("%s was kicked from the game", target))
	case "leave":
		// A seat may always leave itself. If the leaver was the
		// Host the engine-produced HostChanged notice will
		// announce both the leave and the migration — skip the
		// generic notice in that case.
		wasHost := currentHost(session) == selfName
		if err := session.Leave(selfName); err != nil {
			log.Printf("leave %s: %v", selfName, err)
			return
		}
		// Close the leaver's own WS so the seat's connection slot
		// is freed and the client returns to name entry. Using
		// seats.Close here mirrors the kick path: the deferred
		// Release in the read loop's parent will observe the entry
		// gone and skip the no-op Disconnect on a removed seat.
		s.seats.Close(canon+"/"+selfName, "")
		if !wasHost {
			broadcastNotice(b, fmt.Sprintf("%s left the game", selfName))
		}
	default:
		log.Printf("ws cmd unknown type %q from %s", cmd.Type, selfName)
	}
}

// currentHost returns the display name of the seat that currently
// holds the Host badge in session, or "" if none.
func currentHost(session *gamesession.GameSession) string {
	for _, p := range session.Roster() {
		if p.Host {
			return p.Name
		}
	}
	return ""
}

func broadcastNotice(b *presence.Broadcaster, text string) {
	payload, err := json.Marshal(noticeMsg{Type: "notice", Text: text})
	if err != nil {
		log.Printf("marshal notice: %v", err)
		return
	}
	b.Broadcast(payload)
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

// runPump consumes session events and broadcasts each one. Every
// event carries a fresh roster snapshot, broadcast unconditionally
// so clients reconcile post-transition state. HostChanged events
// additionally broadcast the engine-produced Notice as a separate
// message so clients can render a transient banner.
func (s *Server) runPump(g *gamesession.GameSession, b *presence.Broadcaster) {
	for e := range g.Events() {
		rosterPayload, err := json.Marshal(rosterMsg{
			Type:    "roster",
			Players: e.Roster,
		})
		if err != nil {
			log.Printf("marshal roster: %v", err)
			continue
		}
		b.Broadcast(rosterPayload)
		if e.Notice != "" {
			broadcastNotice(b, e.Notice)
		}
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
