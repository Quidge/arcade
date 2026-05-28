// Package arcade is the Arcade shell: it owns the root "/" as a
// game-picker that lists the mounted Games, and an Arcade-level 404
// for unknown root-level paths. It has its own minimal embedded
// layout, deliberately not shared with any Game — each Game owns its
// full HTML (ADR 0015). The shell does not run any gameplay and owns
// no Players, Hosts, or sessions; those live inside individual Games.
package arcade

import (
	"bytes"
	"embed"
	"html/template"
	"log"
	"net/http"
)

//go:embed templates
var templatesFS embed.FS

// MountedGame is the picker's view of one Game mounted in the Arcade:
// the base path it lives under and the name shown in the picker.
// main.go owns the slugs (ADR 0015) and supplies these, so the slug
// has a single source of truth rather than being duplicated between
// main.go's mount and the picker template.
type MountedGame struct {
	Slug  string // base path, e.g. "/scribble"
	Title string // display name shown in the picker, e.g. "Scribble"
}

// Server renders the Arcade picker and 404. Construct via New.
type Server struct {
	games  []MountedGame
	picker *template.Template
	nf     *template.Template
}

// New constructs the Arcade shell. games is what main.go has mounted,
// in the order the picker should list them.
func New(games []MountedGame) *Server {
	return &Server{
		games:  games,
		picker: parseTmpl("templates/picker.tmpl"),
		nf:     parseTmpl("templates/not_found.tmpl"),
	}
}

func parseTmpl(page string) *template.Template {
	return template.Must(template.ParseFS(templatesFS,
		"templates/base.tmpl",
		page,
	))
}

// Routes registers the Arcade shell's routes on mux. The picker is
// served at the exact root ("/{$}"); any other path that no Game
// claims — including a path beneath a Game's slug that the Game
// doesn't route (e.g. "/scribble/typo") — falls through to the
// catch-all "/" and renders the Arcade 404, not the Game's own.
// Game routes registered under their own slugs are more specific and
// win over the catch-all per net/http precedence.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.handlePicker)
	mux.HandleFunc("GET /", s.handleNotFound)
}

type baseData struct {
	Title string
}

type pickerData struct {
	baseData
	Games []MountedGame
}

func (s *Server) handlePicker(w http.ResponseWriter, r *http.Request) {
	render(w, s.picker, "base.tmpl", http.StatusOK, pickerData{
		baseData: baseData{Title: "Arcade"},
		Games:    s.games,
	})
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	render(w, s.nf, "base.tmpl", http.StatusNotFound, baseData{Title: "Not found"})
}

func render(w http.ResponseWriter, tmpl *template.Template, name string, status int, data any) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("arcade render %s: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		log.Printf("arcade write %s: %v", name, err)
	}
}
