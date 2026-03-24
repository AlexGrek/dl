package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// docTypeToFilename maps URL doc types to upstream filenames.
var docTypeToFilename = map[string]string{
	"readme":  "README.md",
	"release": "RELEASE.md",
}

// GET /api/v1/pub/release/{bucket}/docs/{doctype}
// No auth required. Returns the markdown content of README.md or RELEASE.md
// for the given bucket.
func (app *App) handleGetProductDoc(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	doctype := r.PathValue("doctype")

	filename, ok := docTypeToFilename[doctype]
	if !ok {
		http.Error(w, "invalid doc type: use 'readme' or 'release'", http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(bucket, "/\\") {
		http.Error(w, "invalid bucket", http.StatusBadRequest)
		return
	}

	content := app.fetchMarkdownDoc(
		"/rs/"+bucket+"/"+filename,
		"md:product:"+bucket+":"+doctype,
	)
	if content == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(map[string]string{"content": content})
}

// GET /api/v1/pub/release/{bucket}/versions/{version}/docs/release-notes
// No auth required. Returns the markdown content of release_notes.md for a
// specific version. Use "latest" as {version} to resolve automatically.
func (app *App) handleGetVersionDoc(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	version := r.PathValue("version")

	if strings.ContainsAny(bucket, "/\\") || strings.ContainsAny(version, "/\\") {
		http.Error(w, "invalid bucket or version", http.StatusBadRequest)
		return
	}

	if version == "latest" {
		v, err := app.resolveLatestVersion(bucket)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		version = v
	}

	content := app.fetchMarkdownDoc(
		"/rs/"+bucket+"/"+version+"/release_notes.md",
		"md:version:"+bucket+":"+version+":release-notes",
	)
	if content == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(map[string]string{"content": content})
}

// PUT /api/v1/release/{bucket}/docs/{doctype}
// JWT required with release-write scope for the bucket.
// Body: {"content": "markdown text"}
func (app *App) handleUploadProductDoc(w http.ResponseWriter, r *http.Request) {
	info := tokenFromCtx(r)
	bucket := r.PathValue("bucket")
	doctype := r.PathValue("doctype")

	filename, ok := docTypeToFilename[doctype]
	if !ok {
		http.Error(w, "invalid doc type: use 'readme' or 'release'", http.StatusBadRequest)
		return
	}
	if info == nil || !info.CanWriteReleaseBucket(bucket) {
		http.Error(w, "release-write scope required", http.StatusForbidden)
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if err := app.putWebDAVFile("/rs/"+bucket+"/"+filename, req.Content); err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}

	_ = app.store.DeleteCache("md:product:" + bucket + ":" + doctype)
	_ = app.store.DeleteCache("product-detail:" + bucket)

	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/v1/release/{bucket}/versions/{version}/docs/release-notes
// JWT required with release-write scope for the bucket.
// Body: {"content": "markdown text"}
func (app *App) handleUploadVersionDoc(w http.ResponseWriter, r *http.Request) {
	info := tokenFromCtx(r)
	bucket := r.PathValue("bucket")
	version := r.PathValue("version")

	if info == nil || !info.CanWriteReleaseBucket(bucket) {
		http.Error(w, "release-write scope required", http.StatusForbidden)
		return
	}
	if !safeSegment(version) {
		http.Error(w, "invalid version", http.StatusBadRequest)
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if err := app.putWebDAVFile("/rs/"+bucket+"/"+version+"/release_notes.md", req.Content); err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}

	_ = app.store.DeleteCache("md:version:" + bucket + ":" + version + ":release-notes")
	_ = app.store.DeleteCache("product-detail:" + bucket)

	w.WriteHeader(http.StatusNoContent)
}

// putWebDAVFile writes string content to a file on the WebDAV upstream.
func (app *App) putWebDAVFile(path, content string) error {
	dest, _ := url.JoinPath(app.cfg.WebDAVURL, strings.Split(strings.Trim(path, "/"), "/")...)
	req, err := http.NewRequest(http.MethodPut, dest, strings.NewReader(content))
	if err != nil {
		return err
	}
	req.SetBasicAuth(app.cfg.WebDAVUsername, app.cfg.WebDAVPassword)
	req.Header.Set("Content-Type", "text/markdown; charset=utf-8")

	resp, err := app.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("PUT %s: %s – %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
