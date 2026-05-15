// Command scribblepass serves a single hello-world HTML page on /.
package main

import (
	"bytes"
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"
)

const (
	defaultAddr       = ":8080"
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 60 * time.Second
)

//go:embed templates
var templatesFS embed.FS

// gitSHA is the commit the binary was built from. Injected at build time via
// -ldflags "-X main.gitSHA=$(git rev-parse --short HEAD)". Empty for `go run`.
var gitSHA = "unknown"

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

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", homeHandler)

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
