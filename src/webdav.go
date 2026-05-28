package main

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
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

// webdavDirectTokenInfo converts a WebDAV direct-access key into a TokenInfo
// by mapping webdav-read/webdav-write scopes and RootDir into path-scoped read/write scopes.
func webdavDirectTokenInfo(record *APIKey) *TokenInfo {
	var hasRead, hasWrite bool
	for _, s := range record.Scopes {
		if s == "webdav-read" {
			hasRead = true
		}
		if s == "webdav-write" {
			hasWrite = true
		}
	}
	if !hasRead && !hasWrite {
		return nil
	}

	var scopes []string
	if record.RootDir != "" {
		dir := "/" + strings.TrimPrefix(record.RootDir, "/")
		scopes = append(scopes, "read:"+dir)
		if hasWrite {
			scopes = append(scopes, "write:"+dir)
		}
	} else {
		scopes = append(scopes, "read")
		if hasWrite {
			scopes = append(scopes, "write")
		}
	}

	return &TokenInfo{KeyID: record.ID, Scopes: scopes}
}

// handleDirectWebDAV proxies WebDAV requests authenticated via HTTP Basic Auth.
// Username must be "dl"; password is the raw API key (webdav-read or webdav-write scope required).
// These keys are never exchanged for JWTs — they authenticate directly here.
func (app *App) handleDirectWebDAV(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok || username != "dl" {
		w.Header().Set("WWW-Authenticate", `Basic realm="dl"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	record, err := app.store.GetAPIKey(password)
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="dl"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	info := webdavDirectTokenInfo(record)
	if info == nil {
		http.Error(w, "forbidden: not a webdav key", http.StatusForbidden)
		return
	}

	bare := strings.TrimPrefix(r.URL.Path, "/wd")
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
			if r.Method == "PROPFIND" && info.IsAncestorOfAccessible(bare) {
				app.filteredPropfind(w, r, bare, info)
				return
			}
			http.Error(w, "read access denied", http.StatusForbidden)
			return
		}
	default:
		if !info.CanWrite(bare) {
			http.Error(w, "write access denied", http.StatusForbidden)
			return
		}
	}

	// Pre-strip /wd so the proxy director (which strips /api/v1/wd) leaves the path intact.
	r2 := r.Clone(r.Context())
	r2.URL.Path = bare
	r2.URL.RawPath = ""
	app.wdProxy.ServeHTTP(w, r2)
}

// davEntry is used to parse a single <D:response> element from a PROPFIND 207 body.
type davEntry struct {
	Href  string `xml:"DAV: href"`
	Inner []byte `xml:",innerxml"`
}

type davMultistatus struct {
	XMLName xml.Name   `xml:"DAV: multistatus"`
	Entries []davEntry `xml:"DAV: response"`
}

// filterPropfindBody parses a PROPFIND 207 response and removes entries the
// token cannot read, keeping the collection being listed itself.
func filterPropfindBody(body []byte, requestedPath string, info *TokenInfo) ([]byte, error) {
	var ms davMultistatus
	if err := xml.Unmarshal(body, &ms); err != nil {
		return nil, err
	}
	cleanReq := path.Clean("/" + strings.TrimPrefix(requestedPath, "/"))

	var out bytes.Buffer
	out.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n")
	out.WriteString(`<D:multistatus xmlns:D="DAV:">`)
	for _, e := range ms.Entries {
		href := path.Clean("/" + strings.TrimPrefix(e.Href, "/"))
		if href == cleanReq || info.CanRead(e.Href) {
			out.WriteString("<D:response>")
			out.Write(e.Inner)
			out.WriteString("</D:response>")
		}
	}
	out.WriteString("</D:multistatus>")
	return out.Bytes(), nil
}

// filteredPropfind fetches the upstream PROPFIND response and strips entries
// the token cannot read. Used when the requested path is an ancestor of the
// token's root_dir rather than inside it.
func (app *App) filteredPropfind(w http.ResponseWriter, r *http.Request, upstreamPath string, info *TokenInfo) {
	base, err := url.Parse(app.cfg.WebDAVURL)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	base.Path = strings.TrimSuffix(base.Path, "/") + "/" + strings.TrimPrefix(upstreamPath, "/")

	req, err := http.NewRequestWithContext(r.Context(), "PROPFIND", base.String(), r.Body)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.SetBasicAuth(app.cfg.WebDAVUsername, app.cfg.WebDAVPassword)
	for _, hdr := range []string{"Depth", "Content-Type"} {
		if v := r.Header.Get(hdr); v != "" {
			req.Header.Set(hdr, v)
		}
	}

	resp, err := app.client.Do(req)
	if err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "upstream read error", http.StatusBadGateway)
		return
	}

	if resp.StatusCode != http.StatusMultiStatus {
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	filtered, err := filterPropfindBody(body, upstreamPath, info)
	if err != nil {
		http.Error(w, "read access denied", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write(filtered)
}

// handleWebDAV proxies all WebDAV methods to the upstream server.
// Read methods require CanRead(path); write methods require CanWrite(path).
// Scopes of the form "read:/prefix" or "write:/prefix" restrict access by path.
// For PROPFIND on a directory that is an ancestor of the token's root, a filtered
// listing is returned instead of 403.
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
			if r.Method == "PROPFIND" && info.IsAncestorOfAccessible(bare) {
				app.filteredPropfind(w, r, bare, info)
				return
			}
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
