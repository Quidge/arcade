// Package web wires the HTTP/WebSocket surface for the lobby and
// Round-0 slices. It owns the route table, the lobby + home
// templates, and the per-GameSession goroutine that translates
// domain events into JSON payloads for the presence broadcaster.
//
// The wire-format JSON lives entirely in this package; the
// gamesession, round, draft, and ghost domains know only typed
// values.
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

	"github.com/quidge/scribble/internal/draft"
	"github.com/quidge/scribble/internal/gamesession"
	"github.com/quidge/scribble/internal/ghost"
	"github.com/quidge/scribble/internal/joincode"
	"github.com/quidge/scribble/internal/presence"
	"github.com/quidge/scribble/internal/round"
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

// noticeMsg / rosterMsg / roundStateMsg / roundEndedMsg are the wire-
// format envelopes for server → client traffic. Each is JSON-tagged
// against the schema described in the slice's docs.
type noticeMsg struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// clientCmd is the schema for messages clients send to the server
// over the lobby + Round-0 WebSocket. Fields are populated only
// for the verbs that need them; unmarshalling tolerates missing
// fields.
type clientCmd struct {
	Type    string `json:"type"`
	Target  string `json:"target,omitempty"`
	Text    string `json:"text,omitempty"`
	Seconds *int   `json:"seconds,omitempty"`
}

//go:embed templates
var templatesFS embed.FS

// rosterMsg is the wire-format envelope for a roster snapshot.
type rosterMsg struct {
	Type    string               `json:"type"`
	Players []gamesession.Player `json:"players"`
}

// roundStateMsg carries the active Round's per-Player state. Sent
// broadcast when a Round starts (draft_text empty, submitted false)
// and unicast when a Player Reconnects mid-Round (their accumulated
// draft + submission flag). DeadlineMS is the absolute deadline in
// epoch milliseconds, or null when the Host chose "off."
type roundStateMsg struct {
	Type       string `json:"type"`
	DeadlineMS *int64 `json:"deadline_ms"`
	DraftText  string `json:"draft_text"`
	Submitted  bool   `json:"submitted"`
}

// roundEndedEntry mirrors round.Entry on the wire. Ghost-filled
// slots carry GhostLabel ("X's Ghost"); non-Ghost entries omit it.
type roundEndedEntry struct {
	Player     string `json:"player"`
	Text       string `json:"text"`
	Ghost      bool   `json:"ghost"`
	GhostLabel string `json:"ghost_label,omitempty"`
}

// roundEndedMsg notifies clients that the active Round has been
// finalized. Entries are included so future slices (reveal,
// multi-Round chaining) can consume the same envelope; the
// placeholder UI ignores them for now.
type roundEndedMsg struct {
	Type    string            `json:"type"`
	Entries []roundEndedEntry `json:"entries"`
}

// Server holds the wired-up Registry, the per-session room states,
// and the parsed templates. Construct via New.
type Server struct {
	registry *gamesession.Registry

	mu    sync.Mutex
	rooms map[string]*roomState

	seats *seatconn.Registry

	tmpl *templates

	// Sourced from the binary's build-time ldflags; used by the
	// base template's footer. Treated as opaque text.
	gitSHA string
}

// roomState bundles the per-GameSession runtime state the web
// layer manages alongside the domain GameSession: the presence
// broadcaster, the Draft store, the Ghost provider, the Round
// controller, and the Host-chosen pending timer setting.
type roomState struct {
	broadcaster *presence.Broadcaster
	drafts      *draft.Store
	ghosts      *ghost.Provider
	controller  *round.Controller

	mu sync.Mutex
	// pendingTimerSeconds is the Host's most recent Round-timer
	// selection from the lobby dropdown. Zero means "off."
	pendingTimerSeconds int
	// lastEntries is the most recent finalized Entries set —
	// preserved so a Player who Reconnects after Round-end can
	// still see the room's state. Nil before any Round ends.
	lastEntries []round.Entry
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
		rooms:    map[string]*roomState{},
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

	room := s.ensureRoom(session)
	b := room.broadcaster
	sub := b.Add(conn)
	defer b.Remove(sub)

	// Send the connecting Player the round-relevant snapshot
	// matching the current Phase. In Lobby this is a no-op; in
	// RoundActive we unicast a round-state with their accumulated
	// Draft; in RoundComplete we unicast a round-ended so the
	// post-Round placeholder renders immediately.
	if err := s.writePhaseSnapshot(conn, session, room, name); err != nil {
		log.Printf("ws phase snapshot to %s: %v", name, err)
		return
	}

	// Read loop: dispatch client commands until the conn closes.
	// A nil command (parse error, unknown type) is logged but does
	// not tear down the connection.
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		s.handleCommand(session, room, canon, name, data)
	}
}

// handleCommand parses one inbound text frame from a seat's WS
// connection and dispatches the named verb against the session.
// selfName is the display name of the seat that owns the
// connection — authorization checks (e.g. only Host can kick) are
// applied here. Errors are logged and discarded; the WS remains
// open so the client can retry.
func (s *Server) handleCommand(session *gamesession.GameSession, room *roomState, canon, selfName string, data []byte) {
	var cmd clientCmd
	if err := json.Unmarshal(data, &cmd); err != nil {
		log.Printf("ws cmd parse from %s: %v", selfName, err)
		return
	}
	b := room.broadcaster
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
		// generic notice in that case. Post-Start, Leave collapses
		// to Disconnect (ADR 0009) and there is no seat removal —
		// we still close the leaver's own WS so their tab returns
		// to name entry.
		wasHost := currentHost(session) == selfName
		if err := session.Leave(selfName); err != nil {
			log.Printf("leave %s: %v", selfName, err)
			return
		}
		s.seats.Close(canon+"/"+selfName, "")
		if !wasHost {
			broadcastNotice(b, fmt.Sprintf("%s left the game", selfName))
		}
	case "timer":
		if currentHost(session) != selfName {
			return
		}
		st, _ := session.Phase()
		if st != gamesession.StateLobby {
			return
		}
		secs := 0
		if cmd.Seconds != nil && *cmd.Seconds > 0 {
			secs = *cmd.Seconds
		}
		room.mu.Lock()
		room.pendingTimerSeconds = secs
		room.mu.Unlock()
	case "start":
		if currentHost(session) != selfName {
			return
		}
		if err := session.Start(selfName); err != nil {
			log.Printf("start by %s: %v", selfName, err)
			return
		}
		room.mu.Lock()
		secs := room.pendingTimerSeconds
		room.mu.Unlock()
		s.startRound(session, room, 0, secs)
	case "draft":
		st, roundNum := session.Phase()
		if st != gamesession.StateRoundActive {
			return
		}
		if !room.controller.HasSeat(selfName) {
			return
		}
		room.drafts.Apply(roundNum, selfName, cmd.Text)
	case "submit":
		st, _ := session.Phase()
		if st != gamesession.StateRoundActive {
			return
		}
		if err := room.controller.Submit(selfName); err != nil {
			log.Printf("submit by %s: %v", selfName, err)
		}
	case "advance":
		if currentHost(session) != selfName {
			return
		}
		if err := room.controller.ForceAdvance(); err != nil {
			log.Printf("force-advance by %s: %v", selfName, err)
		}
	default:
		log.Printf("ws cmd unknown type %q from %s", cmd.Type, selfName)
	}
}

// startRound kicks off the Round controller for round 0 and
// broadcasts the initial round-state.
func (s *Server) startRound(session *gamesession.GameSession, room *roomState, roundNum int, timerSeconds int) {
	seats := make([]string, 0)
	for _, p := range session.Roster() {
		seats = append(seats, p.Name)
	}
	deadline, err := room.controller.Start(roundNum, seats, timerSeconds)
	if err != nil {
		log.Printf("round.Start: %v", err)
		return
	}
	var deadlineMS *int64
	if !deadline.IsZero() {
		ms := deadline.UnixMilli()
		deadlineMS = &ms
	}
	payload, err := json.Marshal(roundStateMsg{
		Type:       "round-state",
		DeadlineMS: deadlineMS,
		DraftText:  "",
		Submitted:  false,
	})
	if err != nil {
		log.Printf("marshal round-state: %v", err)
		return
	}
	room.broadcaster.Broadcast(payload)
}

// onRoundEnd is invoked by the Round controller's OnEnd callback.
// It records the entries, advances the session phase, and
// broadcasts a round-ended envelope so clients can swap to the
// post-Round placeholder.
func (s *Server) onRoundEnd(session *gamesession.GameSession, room *roomState, roundNum int, entries []round.Entry) {
	if err := session.AdvanceFromRound(); err != nil {
		log.Printf("AdvanceFromRound on Round-end: %v", err)
		return
	}
	room.mu.Lock()
	room.lastEntries = entries
	room.mu.Unlock()

	wireEntries := make([]roundEndedEntry, 0, len(entries))
	for _, e := range entries {
		w := roundEndedEntry{Player: e.Player, Text: e.Text, Ghost: e.Ghost}
		if e.Ghost {
			w.GhostLabel = e.Player + "'s Ghost"
		}
		wireEntries = append(wireEntries, w)
	}
	payload, err := json.Marshal(roundEndedMsg{Type: "round-ended", Entries: wireEntries})
	if err != nil {
		log.Printf("marshal round-ended: %v", err)
		return
	}
	room.broadcaster.Broadcast(payload)
}

// writePhaseSnapshot sends the per-Player state matching the
// current Phase to a freshly-connected (or freshly-reconnected)
// seat. In the lobby it is a no-op — the caller has already
// written the roster. In an active Round it sends a unicast
// round-state with the seat's accumulated Draft + submitted flag
// + the active deadline. In RoundComplete it sends a unicast
// round-ended.
func (s *Server) writePhaseSnapshot(conn *websocket.Conn, session *gamesession.GameSession, room *roomState, name string) error {
	st, roundNum := session.Phase()
	switch st {
	case gamesession.StateLobby:
		return nil
	case gamesession.StateRoundActive:
		snap := room.drafts.Get(roundNum, name)
		deadline, ok := room.controller.Deadline()
		var deadlineMS *int64
		if ok {
			ms := deadline.UnixMilli()
			deadlineMS = &ms
		}
		payload, err := json.Marshal(roundStateMsg{
			Type:       "round-state",
			DeadlineMS: deadlineMS,
			DraftText:  snap.Text,
			Submitted:  snap.Submitted,
		})
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
		defer cancel()
		return conn.Write(ctx, websocket.MessageText, payload)
	case gamesession.StateRoundComplete:
		room.mu.Lock()
		entries := append([]round.Entry(nil), room.lastEntries...)
		room.mu.Unlock()
		wireEntries := make([]roundEndedEntry, 0, len(entries))
		for _, e := range entries {
			w := roundEndedEntry{Player: e.Player, Text: e.Text, Ghost: e.Ghost}
			if e.Ghost {
				w.GhostLabel = e.Player + "'s Ghost"
			}
			wireEntries = append(wireEntries, w)
		}
		payload, err := json.Marshal(roundEndedMsg{Type: "round-ended", Entries: wireEntries})
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
		defer cancel()
		return conn.Write(ctx, websocket.MessageText, payload)
	}
	return nil
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

// ensureRoom returns (and lazily creates) the room state for g.
// On creation it spins up the per-session pump goroutine and the
// Round controller with the OnEnd callback wired to the web
// layer's round-ended broadcaster.
func (s *Server) ensureRoom(g *gamesession.GameSession) *roomState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.rooms[g.Code()]; ok {
		return r
	}
	drafts := draft.New()
	ghosts := ghost.New(time.Now().UnixNano())
	room := &roomState{
		broadcaster: presence.New(),
		drafts:      drafts,
		ghosts:      ghosts,
	}
	// The controller's OnEnd callback closes over the GameSession +
	// roomState so the round-ended broadcast and the session phase
	// advance happen in one place.
	room.controller = round.New(round.Config{
		Drafts: drafts,
		Ghosts: ghosts,
		OnEnd: func(roundNum int, entries []round.Entry, _ round.EndReason) {
			s.onRoundEnd(g, room, roundNum, entries)
		},
	})
	s.rooms[g.Code()] = room
	go s.runPump(g, room.broadcaster)
	return room
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
