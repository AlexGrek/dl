package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func newWebDAVProxy(cfg *Config) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(cfg.WebDAVURL)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	orig := proxy.Director
	proxy.Director = func(req *http.Request) {
		orig(req)
		// Strip /api/v1/wd prefix so the upstream sees the bare path.
		path := strings.TrimPrefix(req.URL.Path, "/api/v1/wd")
		if path == "" {
			path = "/"
		}
		req.URL.Path = path
		req.URL.RawPath = ""
		req.SetBasicAuth(cfg.WebDAVUsername, cfg.WebDAVPassword)
		req.Host = target.Host
	}
	return proxy, nil
}

// handleWebDAV proxies all WebDAV methods to the upstream server.
// Read methods require CanRead(path); write methods require CanWrite(path).
// Scopes of the form "read:/prefix" or "write:/prefix" restrict access by path.
func (app *App) handleWebDAV(w http.ResponseWriter, r *http.Request) {
	info := tokenFromCtx(r)
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	bare := strings.TrimPrefix(r.URL.Path, "/api/v1/wd")
	if bare == "" {
		bare = "/"
	}
	if strings.Contains(bare, "\x00") || strings.Contains(bare, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead, "PROPFIND", "OPTIONS":
		if !info.CanRead(bare) {
			http.Error(w, "read access denied", http.StatusForbidden)
			return
		}
	default:
		if !info.CanWrite(bare) {
			http.Error(w, "write access denied", http.StatusForbidden)
			return
		}
	}

	app.wdProxy.ServeHTTP(w, r)
}
