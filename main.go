// Command scribble serves the scribble web app.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/quidge/scribble/internal/gamesession"
	"github.com/quidge/scribble/internal/web"
)

const (
	defaultAddr       = ":8080"
	defaultDataDir    = "/data"
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 60 * time.Second
)

// gitSHA and builtAt are injected at build time via
// -ldflags "-X main.gitSHA=... -X main.builtAt=...".
var (
	gitSHA  = "dev"
	builtAt = "unknown"
)

var startedAt = time.Now()

type healthResponse struct {
	GitSHA    string `json:"git_sha"`
	BuiltAt   string `json:"built_at"`
	StartedAt string `json:"started_at"`
	Uptime    string `json:"uptime"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		GitSHA:    gitSHA,
		BuiltAt:   builtAt,
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

	registry := gamesession.NewRegistry()
	srvWeb := web.New(registry, gitSHA)

	mux := http.NewServeMux()
	srvWeb.Routes(mux)
	mux.HandleFunc("GET /healthz", healthHandler)

	// Read/Write timeouts are intentionally omitted: applying them to
	// the mux would also kill long-lived WebSocket connections.
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}

	log.Printf("listening on %q", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
