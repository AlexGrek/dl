package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// handleDownload serves GET /d/{path...}
// No auth required — proxies any path from the WebDAV upstream.
// Short URLs: /d/rs/bucket/os_arch/file  →  upstream /rs/bucket/os_arch/file
func (app *App) handleDownload(w http.ResponseWriter, r *http.Request) {
	path := r.PathValue("path")
	app.proxyGet(w, r, "/"+path)
}

// handlePublicRelease serves GET /rs/{path...}
// No auth required — proxies /rs/{path} from the WebDAV upstream.
func (app *App) handlePublicRelease(w http.ResponseWriter, r *http.Request) {
	path := r.PathValue("path")
	app.proxyGet(w, r, "/rs/"+path)
}

// proxyGet fetches upstreamPath from the WebDAV server and streams it to the client.
func (app *App) proxyGet(w http.ResponseWriter, r *http.Request, upstreamPath string) {
	if strings.Contains(upstreamPath, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, app.cfg.WebDAVURL+upstreamPath, nil)
	if err != nil {
		http.Error(w, "request error", http.StatusInternalServerError)
		return
	}
	req.SetBasicAuth(app.cfg.WebDAVUsername, app.cfg.WebDAVPassword)

	// Forward relevant request headers.
	for _, h := range []string{"Range", "If-None-Match", "If-Modified-Since"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}

	resp, err := app.client.Do(req)
	if err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if resp.StatusCode >= 400 {
		http.Error(w, fmt.Sprintf("upstream: %s", resp.Status), http.StatusBadGateway)
		return
	}

	// Forward response headers.
	for _, h := range []string{
		"Content-Type", "Content-Length", "Content-Disposition",
		"Last-Modified", "ETag", "Accept-Ranges", "Content-Range",
	} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
