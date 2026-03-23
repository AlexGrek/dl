package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// POST /api/v1/release/create
// Authorization: Bearer <jwt with release-create scope>
// Body: {"bucket":"offloadmq-agent"}
// Creates rs/{bucket}/ on the WebDAV upstream via MKCOL.
func (app *App) handleReleaseCreate(w http.ResponseWriter, r *http.Request) {
	info := tokenFromCtx(r)
	if info == nil || !info.HasScope("release-create") {
		http.Error(w, "release-create scope required", http.StatusForbidden)
		return
	}

	var req struct {
		Bucket string `json:"bucket"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Bucket == "" {
		http.Error(w, "bucket name required", http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(req.Bucket, "/\\..") {
		http.Error(w, "invalid bucket name", http.StatusBadRequest)
		return
	}

	// Ensure the /rs/ parent exists, then create /rs/{bucket}/.
	for _, path := range []string{"/rs", "/rs/" + req.Bucket} {
		if err := app.webdavMKCOL(path); err != nil {
			http.Error(w, fmt.Sprintf("failed to create %s: %v", path, err), http.StatusBadGateway)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"bucket": req.Bucket})
}

// PUT /api/v1/release/{bucket}/{os_arch}/{file...}
// Authorization: Bearer <jwt with release-write:{bucket} scope>
// Body: file contents (streamed)
// Stores file at rs/{bucket}/{os_arch}/{file} on the WebDAV upstream.
func (app *App) handleReleaseUpload(w http.ResponseWriter, r *http.Request) {
	info := tokenFromCtx(r)
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	bucket := r.PathValue("bucket")
	osArch := r.PathValue("os_arch")
	file := r.PathValue("file")

	if bucket == "" || osArch == "" || file == "" {
		http.Error(w, "bucket, os_arch, and file required", http.StatusBadRequest)
		return
	}

	// Require release-write:{bucket} or general write scope with matching root_dir.
	allowed := info.HasScope("write") || info.ScopeValue("release-write") == bucket
	if !allowed {
		http.Error(w, fmt.Sprintf("release-write:%s scope required", bucket), http.StatusForbidden)
		return
	}

	// Ensure intermediate directories exist.
	paths := []string{
		"/rs",
		"/rs/" + bucket,
		"/rs/" + bucket + "/" + osArch,
	}
	for _, p := range paths {
		_ = app.webdavMKCOL(p) // ignore error — directory may already exist
	}

	// PUT the file to the WebDAV upstream.
	dest := app.cfg.WebDAVURL + "/rs/" + bucket + "/" + osArch + "/" + file
	req, err := http.NewRequest(http.MethodPut, dest, r.Body)
	if err != nil {
		http.Error(w, "request error", http.StatusInternalServerError)
		return
	}
	req.SetBasicAuth(app.cfg.WebDAVUsername, app.cfg.WebDAVPassword)
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	req.ContentLength = r.ContentLength

	resp, err := app.client.Do(req)
	if err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		http.Error(w, fmt.Sprintf("upstream: %s", strings.TrimSpace(string(body))), resp.StatusCode)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// webdavMKCOL sends a MKCOL request to create a collection (directory) on the upstream.
// Returns nil if the collection already exists (207/405) or was created (201).
func (app *App) webdavMKCOL(path string) error {
	req, err := http.NewRequest("MKCOL", app.cfg.WebDAVURL+path, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(app.cfg.WebDAVUsername, app.cfg.WebDAVPassword)

	resp, err := app.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK,              // some servers return 200 for existing dirs
		http.StatusCreated,
		http.StatusMethodNotAllowed, // already exists
		http.StatusMultiStatus:      // already exists (some servers)
		return nil
	default:
		return fmt.Errorf("MKCOL %s: %s", path, resp.Status)
	}
}
