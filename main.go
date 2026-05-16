// Command scribble serves a single hello-world HTML page on /.
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"
)

const (
	defaultAddr       = ":8080"
	defaultDataDir    = "/data"
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 60 * time.Second
)

//go:embed templates
var templatesFS embed.FS

// gitSHA and buildTime are injected at build time via
// -ldflags "-X main.gitSHA=... -X main.buildTime=...".
var (
	gitSHA    = "dev"
	buildTime = "unknown"
)

var startedAt = time.Now()

var homeTmpl = template.Must(template.ParseFS(templatesFS,
	"templates/base.tmpl",
	"templates/pages/home.tmpl",
))

type homeData struct {
	Title   string
	Heading string
	Message string
	Year    int
	GitSHA  string
}

type healthResponse struct {
	GitSHA    string `json:"git_sha"`
	BuildTime string `json:"build_time"`
	StartedAt string `json:"started_at"`
	Uptime    string `json:"uptime"`
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

func homeHandler(w http.ResponseWriter, r *http.Request) {
	render(w, homeTmpl, "base.tmpl", homeData{
		Title:   "Home",
		Heading: "hello, world",
		Message: "A tiny Go server serving a single page from the standard library.",
		Year:    time.Now().Year(),
		GitSHA:  gitSHA,
	})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		GitSHA:    gitSHA,
		BuildTime: buildTime,
		StartedAt: startedAt.UTC().Format(time.RFC3339),
		Uptime:    time.Since(startedAt).Round(time.Second).String(),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("encode healthz: %v", err)
	}
}

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = defaultDataDir
	}
	// dataDir is currently unused; wired in so future SQLite code finds the
	// established path convention without re-litigating it.
	log.Printf("data dir %q (currently unused)", dataDir)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", homeHandler)
	mux.HandleFunc("GET /healthz", healthHandler)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	log.Printf("listening on %q", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
