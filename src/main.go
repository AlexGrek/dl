package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
)

//go:embed all:static
var staticFS embed.FS

// App holds all shared dependencies.
type App struct {
	cfg     *Config
	store   *Store
	wdProxy *httputil.ReverseProxy
	client  *http.Client
}

func main() {
	secretsPath := flag.String("secrets", ".secrets.yaml", "path to secrets YAML file")
	portOverride := flag.String("port", "", "override port from secrets file")
	flag.Parse()

	cfg, err := loadConfig(*secretsPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if *portOverride != "" {
		cfg.Port = *portOverride
	}

	store, err := openStore(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer store.Close()

	wdProxy, err := newWebDAVProxy(cfg)
	if err != nil {
		log.Fatalf("webdav proxy: %v", err)
	}

	app := &App{
		cfg:     cfg,
		store:   store,
		wdProxy: wdProxy,
		client: &http.Client{
			Timeout: 30 * time.Minute, // large uploads can take a while
		},
	}

	mux := http.NewServeMux()
	app.registerRoutes(mux)

	log.Printf("listening on :%s", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))
}

func (app *App) registerRoutes(mux *http.ServeMux) {
	// Auth — no JWT required; these endpoints accept raw API keys / master key.
	mux.HandleFunc("POST /api/v1/auth/token", app.handleAuthToken)
	mux.HandleFunc("POST /api/v1/auth/keys", app.handleCreateKey)
	mux.HandleFunc("GET /api/v1/auth/keys", app.handleListKeys)
	mux.HandleFunc("DELETE /api/v1/auth/keys/{key}", app.handleDeleteKey)

	// WebDAV proxy — all HTTP methods, JWT required.
	mux.Handle("/api/v1/wd/", app.jwtMiddleware(http.HandlerFunc(app.handleWebDAV)))

	// Release management — JWT required.
	mux.Handle("POST /api/v1/release/create", app.jwtMiddleware(http.HandlerFunc(app.handleReleaseCreate)))
	mux.Handle("PUT /api/v1/release/{bucket}/{os_arch}/{file...}", app.jwtMiddleware(http.HandlerFunc(app.handleReleaseUpload)))

	// Public downloads — no auth.
	mux.HandleFunc("GET /d/{path...}", app.handleDownload)
	mux.HandleFunc("GET /rs/{path...}", app.handlePublicRelease)

	// Preact SPA — catch-all, must be last.
	mux.Handle("/", app.spaHandler())
}

func (app *App) spaHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html><body>Frontend not built. Run: cd dl-frontend && npm run build</body></html>"))
		})
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := sub.Open(name); err != nil {
			// Unknown path — hand off to React Router via index.html.
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
