package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// handleDownload serves GET /d/{path...}
// No auth required — proxies any path from the WebDAV upstream.
func (app *App) handleDownload(w http.ResponseWriter, r *http.Request) {
	path := r.PathValue("path")
	app.proxyGet(w, r, "/"+path)
}

// handlePublicRelease serves GET /rs/{path...}
// No auth required. Resolves the "latest" pseudo-version:
//
//	/rs/{bucket}/latest/{rest...} → 302 to /rs/{bucket}/{newestVersion}/{rest...}
func (app *App) handlePublicRelease(w http.ResponseWriter, r *http.Request) {
	path := r.PathValue("path")

	// Split into at most: bucket, maybeLatest, rest
	parts := strings.SplitN(path, "/", 3)
	if len(parts) >= 2 && parts[1] == "latest" {
		bucket := parts[0]
		version, err := app.resolveLatestVersion(bucket)
		if err != nil {
			http.Error(w, "no versions found", http.StatusNotFound)
			return
		}
		redirectSegs := []string{"rs", bucket, version}
		if len(parts) == 3 && parts[2] != "" {
			for _, s := range strings.Split(parts[2], "/") {
				if s != "" {
					redirectSegs = append(redirectSegs, s)
				}
			}
		}
		redirectURL, _ := url.JoinPath("/", redirectSegs...)
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	app.proxyGet(w, r, "/rs/"+path)
}

// resolveLatestVersion PROPFINDs /rs/{bucket}/ and returns the name of the
// child directory with the most recent last-modified time.
func (app *App) resolveLatestVersion(bucket string) (string, error) {
	entries, err := app.propfind1("/rs/" + bucket + "/")
	if err != nil {
		return "", fmt.Errorf("bucket not found: %w", err)
	}

	var latest string
	var latestTime time.Time

	for _, e := range entries {
		if !e.isDir {
			continue
		}
		t, _ := time.Parse(time.RFC1123, e.modified)
		if t.After(latestTime) {
			latestTime = t
			latest = e.name
		}
	}

	if latest == "" {
		return "", fmt.Errorf("no versions in bucket %q", bucket)
	}
	return latest, nil
}

// proxyGet fetches upstreamPath from the WebDAV server and streams it to the client.
func (app *App) proxyGet(w http.ResponseWriter, r *http.Request, upstreamPath string) {
	if strings.Contains(upstreamPath, "..") || strings.Contains(upstreamPath, "\x00") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	encoded, _ := url.JoinPath(app.cfg.WebDAVURL, strings.Split(strings.Trim(upstreamPath, "/"), "/")...)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, encoded, nil)
	if err != nil {
		http.Error(w, "request error", http.StatusInternalServerError)
		return
	}
	req.SetBasicAuth(app.cfg.WebDAVUsername, app.cfg.WebDAVPassword)

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
