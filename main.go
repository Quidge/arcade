// Command arcade serves the Arcade web app.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/quidge/arcade/internal/arcade"
	"github.com/quidge/arcade/internal/games/scribble/gamesession"
	scribbleweb "github.com/quidge/arcade/internal/games/scribble/web"
)

// scribbleBasePath is the URL slug Scribble is mounted under. main.go
// owns the Arcade's namespace (ADR 0015): it assigns each Game its
// slug and passes it into the Game's constructor, which uses the slug
// for both route registration and absolute-URL generation.
const scribbleBasePath = "/scribble"

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
	// Env-var convention: infrastructure knobs read by deploy tooling
	// (ADDR, DATA_DIR) follow conventional unprefixed names; product-
	// specific knobs (only consumed by this binary) are prefixed
	// SCRIBBLE_*.
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

	// SCRIBBLE_HOST_DISCONNECT_GRACE_SECONDS exists so the e2e test
	// suite can shrink the 15-second host-migration grace down to 1s
	// without faking the clock. Production sets nothing and gets the
	// default. The integration tier reaches WithHostGraceDuration
	// directly because it constructs the Registry itself; e2e drives
	// a real binary so it needs the env-var seam.
	graceDuration := gamesession.DefaultHostGraceDuration
	if v := os.Getenv("SCRIBBLE_HOST_DISCONNECT_GRACE_SECONDS"); v != "" {
		secs, err := strconv.Atoi(v)
		if err != nil || secs <= 0 {
			log.Fatalf("SCRIBBLE_HOST_DISCONNECT_GRACE_SECONDS must be a positive integer (seconds), got %q", v)
		}
		graceDuration = time.Duration(secs) * time.Second
	}
	registry := gamesession.NewRegistry(gamesession.WithHostGraceDuration(graceDuration))
	// Construct each Game with its slug, then the Arcade shell that
	// owns the root picker. Register the Arcade at "/" and Scribble
	// under its slug; /healthz stays unprefixed (infra knob).
	scribbleGame := scribbleweb.New(registry, gitSHA, scribbleBasePath)
	arcadeShell := arcade.New()

	mux := http.NewServeMux()
	arcadeShell.Routes(mux)
	scribbleGame.Routes(mux)
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
