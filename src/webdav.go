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
// Requires a valid JWT with at minimum read scope; write methods require write scope.
// If TokenInfo.RootDir is set, requests are restricted to paths under that root.
func (app *App) handleWebDAV(w http.ResponseWriter, r *http.Request) {
	info := tokenFromCtx(r)
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead, "PROPFIND", "OPTIONS":
		if !info.HasScope("read") && !info.HasScope("write") {
			http.Error(w, "read scope required", http.StatusForbidden)
			return
		}
	default:
		if !info.HasScope("write") {
			http.Error(w, "write scope required", http.StatusForbidden)
			return
		}
	}

	if info.RootDir != "" {
		bare := strings.TrimPrefix(r.URL.Path, "/api/v1/wd")
		root := "/" + strings.TrimPrefix(info.RootDir, "/")
		if !strings.HasPrefix(bare, root) {
			http.Error(w, "path outside allowed root", http.StatusForbidden)
			return
		}
	}

	app.wdProxy.ServeHTTP(w, r)
}
