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

	"github.com/quidge/scribble/internal/chain"
	"github.com/quidge/scribble/internal/draft"
	"github.com/quidge/scribble/internal/gamesession"
	"github.com/quidge/scribble/internal/ghost"
	"github.com/quidge/scribble/internal/joincode"
	"github.com/quidge/scribble/internal/presence"
	"github.com/quidge/scribble/internal/round"
	"github.com/quidge/scribble/internal/roundcomplete"
	"github.com/quidge/scribble/internal/seatconn"
	"github.com/quidge/scribble/internal/strokes"
)

const (
	wsWriteTimeout = 5 * time.Second
	maxNameLength  = 32
)

// closePolicy* are the machine-readable Reason strings used in the
// WebSocket close frame for the named rejection cases. Integration
// tests assert on the "session full" / "game already started" /
// "superseded" / "kicked" prefixes. Grouped as a single block so
// readers see the full set of close reasons at a glance; cap-
// exceeded is computed from the constant rather than literal so
// MaxPlayers stays the single source of truth.
var (
	closePolicyCapExceeded = fmt.Sprintf(
		"session full: this game session already has %d players",
		gamesession.MaxPlayers,
	)
	closePolicyGameStarted = "game already started: new joins are not accepted after Start"
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
// over the lobby + Round + reveal WebSocket. Fields are populated
// only for the verbs that need them; unmarshalling tolerates missing
// fields. Strokes carries the full current Drawing for drawing-kind
// draft updates (ADR 0010); Text carries the full current caption
// for text-kind draft updates.
type clientCmd struct {
	Type    string          `json:"type"`
	Target  string          `json:"target,omitempty"`
	Text    string          `json:"text,omitempty"`
	Strokes strokes.Drawing `json:"strokes,omitempty"`
	Seconds *int            `json:"seconds,omitempty"`
}

//go:embed templates
var templatesFS embed.FS

// rosterMsg is the wire-format envelope for a roster snapshot.
type rosterMsg struct {
	Type    string               `json:"type"`
	Players []gamesession.Player `json:"players"`
}

// roundStateMsg carries the active Round's per-Player state.
// Sent broadcast at Round start and unicast on Reconnect-mid-
// Round. The envelope is generalized so future Round types
// (Drawings in Round 1+) reuse the same shape:
//
//   - ContentKind discriminates the slot type for this Round
//     ("caption" | "drawing").
//   - Prompt is the previous Entry on this seat's assigned Chain,
//     or null when there is no prompt (Round 0).
//   - Draft is a polymorphic payload — for "caption" it carries
//     {kind:"text", text:"…"}; for "drawing" it would carry
//     {kind:"strokes", strokes:[…]}.
//
// DeadlineMS is the absolute deadline in epoch milliseconds, or
// null when the Host chose "off."
type roundStateMsg struct {
	Type        string       `json:"type"`
	Round       int          `json:"round"`
	DeadlineMS  *int64       `json:"deadline_ms"`
	ContentKind string       `json:"content_kind"`
	Prompt      *promptMsg   `json:"prompt"`
	Draft       draftPayload `json:"draft"`
	Submitted   bool         `json:"submitted"`
}

// promptMsg is the previous Entry shown to a Player as context
// for the current Round. Kind discriminates the payload: for a
// Caption prompt, Text is set; for a Drawing prompt, Strokes is
// set.
type promptMsg struct {
	Kind    string           `json:"kind"`
	Text    string           `json:"text,omitempty"`
	Strokes []strokes.Stroke `json:"strokes,omitempty"`
}

// draftPayload is the per-Player accumulated Draft delivered with
// a round-state envelope. Kind is "text" or "strokes"; exactly one
// of Text / Strokes is set.
type draftPayload struct {
	Kind    string           `json:"kind"`
	Text    string           `json:"text,omitempty"`
	Strokes []strokes.Stroke `json:"strokes,omitempty"`
}

// roundEndedEntry mirrors a chain.Entry on the wire. Ghost-filled
// slots carry GhostLabel ("X's Ghost"); non-Ghost entries omit it.
// For Caption entries, Text is set; for Drawing entries, Strokes
// is set. The Round 0 slice only ever emits Caption entries.
type roundEndedEntry struct {
	Player     string           `json:"player"`
	Kind       string           `json:"kind"`
	Text       string           `json:"text,omitempty"`
	Strokes    []strokes.Stroke `json:"strokes,omitempty"`
	Ghost      bool             `json:"ghost"`
	GhostLabel string           `json:"ghost_label,omitempty"`
}

// roundEndedMsg notifies clients that the active Round has been
// finalized. Entries are included so future slices (reveal,
// multi-Round chaining) can consume the same envelope; the
// placeholder UI ignores them for now.
type roundEndedMsg struct {
	Type    string            `json:"type"`
	Entries []roundEndedEntry `json:"entries"`
}

// revealEntryMsg is one Entry in a reveal-state's accumulating
// `entries_visible` view. Round is the position of the Entry on the
// Chain (index 0 is the starter Caption, 1 is the first Drawing
// response, etc.). Author is the seat that contributed the Entry —
// or, for Ghost-filled slots, the absent seat whose Ghost stood in.
type revealEntryMsg struct {
	Round      int              `json:"round"`
	Kind       string           `json:"kind"`
	Text       string           `json:"text,omitempty"`
	Strokes    []strokes.Stroke `json:"strokes,omitempty"`
	Ghost      bool             `json:"ghost"`
	GhostLabel string           `json:"ghost_label,omitempty"`
	Author     string           `json:"author"`
}

// revealStateMsg is the full reveal view broadcast on every advance
// and unicast on Reconnect-mid-reveal (ADR 0011). Driver is the
// display name of the seat permitted to send the next reveal-advance
// command, recomputed at send time from (starter, starterConnected,
// currentHost) per ADR 0011's re-eval-per-advance rule. Mode is one
// of "step", "full", or "complete".
type revealStateMsg struct {
	Type           string           `json:"type"`
	ChainIndex     int              `json:"chain_index"`
	Starter        string           `json:"starter"`
	Driver         string           `json:"driver"`
	Mode           string           `json:"mode"`
	EntriesVisible []revealEntryMsg `json:"entries_visible"`
	MoreChains     bool             `json:"more_chains"`
}

// gameEndedMsg is broadcast to every subscriber when the Host ends
// the GameSession. The wire layer follows the broadcast by closing
// each seat's WebSocket with a normal-closure frame.
type gameEndedMsg struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
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
// broadcaster, the per-Round Draft stores (text + strokes), the
// Ghost provider, the Round controller, the Chain store, the
// reveal cursor, and the Host-chosen pending timer setting.
type roomState struct {
	broadcaster  *presence.Broadcaster
	drafts       *draft.Store
	strokeDrafts *strokes.Store
	ghosts       *ghost.Provider
	controller   *round.Controller
	chains       *chain.Store
	revealCursor *revealCursor

	mu sync.Mutex
	// pendingTimerSeconds is the Host's most recent Round-timer
	// selection from the lobby dropdown. Zero means "off."
	pendingTimerSeconds int
	// roundTimerSeconds is the Round-timer setting sealed at Start.
	// It survives across BeginRound transitions so Rounds 1..N use
	// the same timer the Host picked in the lobby.
	roundTimerSeconds int
	// lastEntries is the most recent finalized Entries set —
	// preserved so a Player who Reconnects after Round-end can
	// still see the room's state. Nil before any Round ends.
	lastEntries []chain.Entry
	// seatConns is the per-seat live WebSocket, used to unicast
	// round-state at Round-start. Updated by handleWS as
	// connections come and go.
	seatConns map[string]*websocket.Conn
}

func (r *roomState) bindSeatConn(name string, conn *websocket.Conn) {
	r.mu.Lock()
	if r.seatConns == nil {
		r.seatConns = map[string]*websocket.Conn{}
	}
	r.seatConns[name] = conn
	r.mu.Unlock()
}

func (r *roomState) unbindSeatConn(name string, conn *websocket.Conn) {
	r.mu.Lock()
	if cur, ok := r.seatConns[name]; ok && cur == conn {
		delete(r.seatConns, name)
	}
	r.mu.Unlock()
}

func (r *roomState) seatConn(name string) *websocket.Conn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seatConns[name]
}

// closeAllSeats writes a normal-closure frame to every live seat
// connection in the room. Called after the game-ended broadcast so
// each subscriber sees the envelope before its WebSocket closes.
func (r *roomState) closeAllSeats() {
	r.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(r.seatConns))
	for _, c := range r.seatConns {
		conns = append(conns, c)
	}
	r.mu.Unlock()
	for _, c := range conns {
		_ = c.Close(websocket.StatusNormalClosure, "game ended")
	}
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
	mux.HandleFunc("GET /g/{rawJoinCode}", s.handleLobby)
	mux.HandleFunc("GET /g/{rawJoinCode}/ws", s.handleWS)
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
	MaxPlayers  int    // exposed to the client-side rejection copy
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

// handleLobby renders the lobby page. Registered as a GET route; Go
// 1.22's net/http dispatches HEAD requests to the same handler with
// an empty body, and the homepage's join-by-code probe depends on
// that — do not add an explicit method check (per ADR 0014).
func (s *Server) handleLobby(w http.ResponseWriter, r *http.Request) {
	canonicalJoinCode, ok := joincode.Parse(r.PathValue("rawJoinCode"))
	if !ok {
		s.renderNotFound(w)
		return
	}
	if _, found := s.registry.Lookup(canonicalJoinCode); !found {
		s.renderNotFound(w)
		return
	}
	render(w, s.tmpl.lobby, "base.tmpl", lobbyData{
		baseData:    s.baseData("Lobby"),
		Code:        canonicalJoinCode,
		DisplayCode: joincode.Format(canonicalJoinCode),
		MaxPlayers:  gamesession.MaxPlayers,
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
	canonicalJoinCode, ok := joincode.Parse(r.PathValue("rawJoinCode"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	session, found := s.registry.Lookup(canonicalJoinCode)
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
	key := canonicalJoinCode + "/" + name
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
		case errors.Is(domainErr, gamesession.ErrGameStarted):
			reason = closePolicyGameStarted
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

	// Register this conn for per-seat unicast. The unbind on
	// handler exit only deletes the entry if this conn is still
	// the current owner — supersede races (a later Acquire on the
	// same seat) leave the new owner's binding intact.
	room.bindSeatConn(name, conn)
	defer room.unbindSeatConn(name, conn)

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
		s.handleCommand(session, room, canonicalJoinCode, name, data)
	}
}

// handleCommand parses one inbound text frame from a seat's WS
// connection and dispatches the named verb against the session.
// selfName is the display name of the seat that owns the
// connection — authorization checks (e.g. only Host can kick) are
// applied here. Errors are logged and discarded; the WS remains
// open so the client can retry.
func (s *Server) handleCommand(session *gamesession.GameSession, room *roomState, canonicalJoinCode, selfName string, data []byte) {
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
		s.seats.Close(canonicalJoinCode+"/"+target, closePolicyKicked)
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
		s.seats.Close(canonicalJoinCode+"/"+selfName, "")
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
		room.roundTimerSeconds = secs
		room.mu.Unlock()
		seats := rosterNames(session)
		// Pin the chain.Store roster at Round-0 start. Init is
		// idempotent so subsequent Rounds (which call it via
		// BeginRound) are no-ops.
		room.chains.Init(seats)
		s.startRound(session, room, 0, secs, seats)
	case "draft":
		st, roundNum := session.Phase()
		if st != gamesession.StateRoundActive {
			return
		}
		if !room.controller.HasSeat(selfName) {
			return
		}
		// Dispatch on the Round's content kind. The parity rule
		// (even Rounds → caption, odd → drawing) lives in
		// roundcomplete alongside the materialization that consumes
		// it; here we just consult it to pick the right Draft store.
		switch roundcomplete.ContentKindForRound(roundNum) {
		case "caption":
			room.drafts.Apply(roundNum, selfName, cmd.Text)
		case "drawing":
			room.strokeDrafts.Apply(roundNum, selfName, cmd.Strokes)
		}
	case "submit":
		st, roundNum := session.Phase()
		if st != gamesession.StateRoundActive {
			return
		}
		// Seal the appropriate Draft *before* informing the
		// controller, so the snapshot inside OnEnd matches what
		// the seat last sent. The controller no longer touches
		// any Draft store directly.
		switch roundcomplete.ContentKindForRound(roundNum) {
		case "caption":
			room.drafts.Submit(roundNum, selfName)
		case "drawing":
			room.strokeDrafts.Submit(roundNum, selfName)
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
	case "reveal-advance":
		st, _ := session.Phase()
		if st != gamesession.StateReveal {
			return
		}
		ci, _, mode := room.revealCursor.Step()
		if mode == revealModeComplete {
			return
		}
		starter := room.chains.Starter(ci)
		if !canAdvanceReveal(selfName, starter, session) {
			log.Printf("reveal-advance denied actor=%s starter=%s", selfName, starter)
			return
		}
		room.revealCursor.Advance()
		s.broadcastRevealState(session, room)
	case "end-game":
		if currentHost(session) != selfName {
			return
		}
		st, _ := session.Phase()
		if st == gamesession.StateEnded {
			return
		}
		if err := session.End(selfName); err != nil {
			log.Printf("end-game by %s: %v", selfName, err)
			return
		}
		payload, err := json.Marshal(gameEndedMsg{Type: "game-ended", Reason: "host"})
		if err != nil {
			log.Printf("marshal game-ended: %v", err)
			return
		}
		room.broadcaster.Broadcast(payload)
		room.closeAllSeats()
	default:
		log.Printf("ws cmd unknown type %q from %s", cmd.Type, selfName)
	}
}

// Reveal pacing currently lives across this cluster of free
// functions (canAdvanceReveal / seatConnected / driverFor) plus
// buildRevealStateMsg below and the revealCursor primitive in
// reveal_cursor.go. We considered lifting them into a single deep
// Reveal module that owns the cursor, driver re-evaluation, and
// snapshot composition (see scratch/improve-arch-session-1.md
// candidate #1). Deferred: no current scenario forces the move and
// the wire-side orchestration is straightforward. Reconsider if
// Reveal grows new pacing controls (rewind, skip-to-full, jump to
// a specific Chain) — that is when the locality cost of leaving it
// spread becomes real.

// canAdvanceReveal returns true if actor is allowed to send a
// reveal-advance command for the Chain whose starter is named. Per
// ADR 0011 the rule is re-evaluated on every command: the starter
// drives when Connected; the current Host drives otherwise. A
// starter who Reconnects mid-reveal-of-their-own-Chain regains the
// driver role on the next attempted advance.
func canAdvanceReveal(actor, starter string, session *gamesession.GameSession) bool {
	if seatConnected(session, starter) {
		return actor == starter
	}
	return actor == currentHost(session)
}

// seatConnected reports whether the named seat is currently
// connected. An unknown seat returns false.
func seatConnected(session *gamesession.GameSession, name string) bool {
	for _, p := range session.Roster() {
		if p.Name == name {
			return p.Connected
		}
	}
	return false
}

// driverFor returns the display name of the current driver for the
// Chain whose starter is named, computed at call time per ADR 0011.
func driverFor(session *gamesession.GameSession, starter string) string {
	if seatConnected(session, starter) {
		return starter
	}
	return currentHost(session)
}

// startRound kicks off the Round controller for roundNum and
// unicasts a personalized round-state to each connected seat. The
// envelope is unicast (not broadcast) because the per-seat Prompt
// and Draft fields differ per seat in Rounds 1+. Round 0 sends
// the same content_kind="caption" / prompt=null payload to every
// seat, but the unicast path is used uniformly for shape symmetry.
func (s *Server) startRound(session *gamesession.GameSession, room *roomState, roundNum int, timerSeconds int, seats []string) {
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
	_ = session // session reserved for future per-seat dispatch
	for _, seat := range seats {
		msg := buildRoundStateMsg(room, roundNum, seat, deadlineMS)
		payload, err := json.Marshal(msg)
		if err != nil {
			log.Printf("marshal round-state: %v", err)
			continue
		}
		conn := room.seatConn(seat)
		if conn == nil {
			// Disconnected seats get the round-state on Reconnect
			// via writePhaseSnapshot; nothing to write now.
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
		if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
			log.Printf("unicast round-state to %s: %v", seat, err)
		}
		cancel()
	}
}

// buildRoundStateMsg constructs the round-state payload for one
// seat at roundNum. The content kind is resolved through
// roundcomplete.ContentKindForRound (even Rounds → caption, odd →
// drawing) and the prompt, when present, is read from chain.Store
// via PromptFor (the previous Entry on the seat's currently-
// assigned Chain). The Draft field carries the seat's accumulated
// input from the matching Draft store.
func buildRoundStateMsg(room *roomState, roundNum int, seat string, deadlineMS *int64) roundStateMsg {
	kind := roundcomplete.ContentKindForRound(roundNum)
	msg := roundStateMsg{
		Type:        "round-state",
		Round:       roundNum,
		DeadlineMS:  deadlineMS,
		ContentKind: kind,
	}
	if roundNum > 0 {
		if prev, ok := room.chains.PromptFor(roundNum, seat); ok {
			msg.Prompt = promptFromEntry(prev)
		}
	}
	switch kind {
	case "caption":
		snap := room.drafts.Get(roundNum, seat)
		msg.Draft = draftPayload{Kind: "text", Text: snap.Text}
		msg.Submitted = snap.Submitted
	case "drawing":
		snap := room.strokeDrafts.Get(roundNum, seat)
		msg.Draft = draftPayload{Kind: "strokes", Strokes: []strokes.Stroke(snap.Strokes)}
		msg.Submitted = snap.Submitted
	}
	return msg
}

// promptFromEntry maps a chain.Entry to its wire-format promptMsg.
func promptFromEntry(e chain.Entry) *promptMsg {
	p := &promptMsg{}
	switch e.Kind {
	case chain.EntryCaption:
		p.Kind = "caption"
		p.Text = e.Text
	case chain.EntryDrawing:
		p.Kind = "drawing"
		p.Strokes = e.Strokes
	}
	return p
}

// onRoundEnd is invoked by the Round controller's OnEnd callback.
// It delegates Entry materialization (Draft snapshot + Ghost-fill
// per the Telestrations alternation rule) to roundcomplete, then
// orchestrates the wire-side fallout: appends entries to the
// Chain store, advances the session phase, broadcasts a
// round-ended envelope so clients can swap, and either begins the
// next Round (with per-seat unicast round-state) or transitions
// into the reveal phase (broadcast reveal-state).
//
// The callback is the only writer of chain.Store from production
// code paths; roundcomplete.Materialize is pure and the chain
// append loop here is the side-effect.
func (s *Server) onRoundEnd(session *gamesession.GameSession, room *roomState, roundNum int, seats []string) {
	entries := roundcomplete.Materialize(roundNum, seats, room.drafts, room.strokeDrafts, room.ghosts.Picker())

	// Plumb finalized entries into the Chain store.
	for _, e := range entries {
		if err := room.chains.Append(roundNum, e.Player, e); err != nil {
			log.Printf("chain.Append round=%d seat=%s: %v", roundNum, e.Player, err)
		}
	}

	if err := session.AdvanceFromRound(); err != nil {
		log.Printf("AdvanceFromRound on Round-end: %v", err)
		return
	}
	room.mu.Lock()
	room.lastEntries = entries
	secs := room.roundTimerSeconds
	room.mu.Unlock()

	payload, err := json.Marshal(roundEndedMsg{
		Type:    "round-ended",
		Entries: wireEntriesFromChain(entries),
	})
	if err != nil {
		log.Printf("marshal round-ended: %v", err)
		return
	}
	room.broadcaster.Broadcast(payload)

	// Decide what comes next: another Round, or the reveal phase.
	// The chain.Store roster is N seats, and the GameSession runs N
	// Rounds (one Entry per seat per Chain).
	n := room.chains.N()
	host := currentHost(session)
	if roundNum+1 < n {
		if err := session.BeginRound(host, roundNum+1); err != nil {
			log.Printf("BeginRound %d by %s: %v", roundNum+1, host, err)
			return
		}
		s.startRound(session, room, roundNum+1, secs, seats)
		return
	}

	if err := session.BeginReveal(host); err != nil {
		log.Printf("BeginReveal by %s: %v", host, err)
		return
	}
	chainLens := make([]int, n)
	for i := 0; i < n; i++ {
		chainLens[i] = room.chains.Len(i)
	}
	room.mu.Lock()
	room.revealCursor = newRevealCursor(chainLens)
	room.mu.Unlock()
	s.broadcastRevealState(session, room)
}

// broadcastRevealState builds the current reveal-state envelope and
// fans it out to every subscriber.
func (s *Server) broadcastRevealState(session *gamesession.GameSession, room *roomState) {
	msg := buildRevealStateMsg(session, room)
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Printf("marshal reveal-state: %v", err)
		return
	}
	room.broadcaster.Broadcast(payload)
}

// buildRevealStateMsg snapshots the current cursor state and the
// matching Chain entries into a reveal-state envelope. The cursor
// is queried under no lock — its mutator (Advance) runs on the same
// goroutine as buildRevealStateMsg's callers, so the read is
// consistent.
//
// On the terminal "complete" mode we still emit the last Chain's
// full entries so the client has something coherent to render until
// the Host taps End game (per the issue's spec).
func buildRevealStateMsg(session *gamesession.GameSession, room *roomState) revealStateMsg {
	ci, ei, mode := room.revealCursor.Step()
	n := room.chains.N()
	msg := revealStateMsg{Type: "reveal-state", Mode: mode}
	var visible []chain.Entry
	var displayCi int
	switch mode {
	case revealModeStep:
		full := room.chains.Entries(ci)
		end := ei + 1
		if end > len(full) {
			end = len(full)
		}
		visible = full[:end]
		displayCi = ci
		msg.MoreChains = ci+1 < n
	case revealModeFull:
		visible = room.chains.Entries(ci)
		displayCi = ci
		msg.MoreChains = ci+1 < n
	case revealModeComplete:
		last := n - 1
		if last < 0 {
			msg.ChainIndex = -1
			return msg
		}
		visible = room.chains.Entries(last)
		displayCi = last
		msg.MoreChains = false
	}
	msg.ChainIndex = displayCi
	msg.Starter = room.chains.Starter(displayCi)
	msg.Driver = driverFor(session, msg.Starter)
	msg.EntriesVisible = wireRevealEntries(visible)
	return msg
}

// wireRevealEntries maps Chain entries to the reveal wire shape.
// The entry's position on the Chain is its Round number (index 0 is
// the starter Caption from Round 0).
func wireRevealEntries(entries []chain.Entry) []revealEntryMsg {
	out := make([]revealEntryMsg, 0, len(entries))
	for i, e := range entries {
		w := revealEntryMsg{
			Round:  i,
			Ghost:  e.Ghost,
			Author: e.Player,
		}
		switch e.Kind {
		case chain.EntryCaption:
			w.Kind = "caption"
			w.Text = e.Text
		case chain.EntryDrawing:
			w.Kind = "drawing"
			w.Strokes = e.Strokes
		}
		if e.Ghost {
			w.GhostLabel = e.Player + "'s Ghost"
		}
		out = append(out, w)
	}
	return out
}

// wireEntriesFromChain maps a slice of chain.Entry to the wire
// shape, including the GhostLabel for Ghost-filled slots and the
// per-Kind payload (Text vs Strokes).
func wireEntriesFromChain(entries []chain.Entry) []roundEndedEntry {
	out := make([]roundEndedEntry, 0, len(entries))
	for _, e := range entries {
		w := roundEndedEntry{Player: e.Player, Ghost: e.Ghost}
		switch e.Kind {
		case chain.EntryCaption:
			w.Kind = "caption"
			w.Text = e.Text
		case chain.EntryDrawing:
			w.Kind = "drawing"
			w.Strokes = e.Strokes
		}
		if e.Ghost {
			w.GhostLabel = e.Player + "'s Ghost"
		}
		out = append(out, w)
	}
	return out
}

// rosterNames returns the connected-or-not seat names in join
// order. Used by Round start to seed the controller's seat list
// and the chain.Store's roster.
func rosterNames(session *gamesession.GameSession) []string {
	roster := session.Roster()
	out := make([]string, 0, len(roster))
	for _, p := range roster {
		out = append(out, p.Name)
	}
	return out
}

// writePhaseSnapshot sends the per-Player state matching the
// current Phase to a freshly-connected (or freshly-reconnected)
// seat. In the lobby it is a no-op — the caller has already
// written the roster. In an active Round it sends a unicast
// round-state with the seat's accumulated Draft + submitted flag
// + the active deadline. In RoundComplete it sends a unicast
// round-ended.
//
// We considered lifting this switch into a separate PhaseSnapshot
// module (see scratch/improve-arch-session-1.md candidate #3).
// Deferred: writePhaseSnapshot is a thin dispatch over already-
// deep builders (buildRoundStateMsg, buildRevealStateMsg, the
// roundEndedMsg marshal), has exactly one caller, and runs on
// the only transport ADR 0007 commits us to. Extracting it would
// relocate rather than concentrate complexity (deletion-test
// failure) and would not unlock any test that the per-builder
// unit tests do not already cover. Reconsider when a second
// caller appears (e.g. a polling REST or admin "fetch state"
// endpoint), a second transport joins WS (SSE, long-poll), or
// snapshot composition gains behavior beyond switch-and-marshal
// (diff-since-last, compression, per-client capability
// negotiation).
func (s *Server) writePhaseSnapshot(conn *websocket.Conn, session *gamesession.GameSession, room *roomState, name string) error {
	st, roundNum := session.Phase()
	switch st {
	case gamesession.StateLobby:
		return nil
	case gamesession.StateRoundActive:
		deadline, ok := room.controller.Deadline()
		var deadlineMS *int64
		if ok {
			ms := deadline.UnixMilli()
			deadlineMS = &ms
		}
		msg := buildRoundStateMsg(room, roundNum, name, deadlineMS)
		payload, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
		defer cancel()
		return conn.Write(ctx, websocket.MessageText, payload)
	case gamesession.StateRoundComplete:
		room.mu.Lock()
		entries := append([]chain.Entry(nil), room.lastEntries...)
		room.mu.Unlock()
		payload, err := json.Marshal(roundEndedMsg{
			Type:    "round-ended",
			Entries: wireEntriesFromChain(entries),
		})
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
		defer cancel()
		return conn.Write(ctx, websocket.MessageText, payload)
	case gamesession.StateReveal:
		msg := buildRevealStateMsg(session, room)
		payload, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), wsWriteTimeout)
		defer cancel()
		return conn.Write(ctx, websocket.MessageText, payload)
	case gamesession.StateEnded:
		return nil
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

// ChainStoreForCode returns the chain.Store managed by this
// Server for the GameSession with the given canonical code, or
// nil if no room has been created. Exposed for integration tests
// that need to assert on Chain plumbing without driving the wire
// format.
func (s *Server) ChainStoreForCode(canonicalJoinCode string) *chain.Store {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rooms[canonicalJoinCode]
	if !ok {
		return nil
	}
	return r.chains
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
	strokeDrafts := strokes.New()
	ghosts := ghost.New(time.Now().UnixNano())
	chains := chain.New()
	// The reveal cursor is constructed up-front with an empty
	// chainLens slice so it is immediately in the "complete" state.
	// A future slice will reseat it at the StateRoundComplete →
	// StateReveal transition with the actual per-Chain lengths.
	cursor := newRevealCursor(nil)
	room := &roomState{
		broadcaster:  presence.New(),
		drafts:       drafts,
		strokeDrafts: strokeDrafts,
		ghosts:       ghosts,
		chains:       chains,
		revealCursor: cursor,
	}
	// The controller's OnEnd callback closes over the GameSession +
	// roomState so Entry assembly, chain plumbing, the round-ended
	// broadcast, and the session phase advance all happen in one
	// place. The controller itself no longer touches drafts or
	// ghosts directly.
	room.controller = round.New(round.Config{
		OnEnd: func(roundNum int, seats []string, _ round.EndReason) {
			s.onRoundEnd(g, room, roundNum, seats)
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
