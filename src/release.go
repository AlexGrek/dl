package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	if strings.Contains(req.Bucket, "..") || strings.ContainsAny(req.Bucket, "/\\\x00") {
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

// POST /api/v1/release/{bucket}/upload
// Content-Type: multipart/form-data
// Fields: version (string), os_arch (string), file (binary)
// Authorization: Bearer <jwt with release-write:{bucket} scope>
func (app *App) handleReleaseMultipartUpload(w http.ResponseWriter, r *http.Request) {
	info := tokenFromCtx(r)
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	bucket := r.PathValue("bucket")
	if !info.HasScope("write") && !info.HasScope("release-write") &&
		info.ScopeValue("release-write") != bucket {
		http.Error(w, fmt.Sprintf("release-write:%s scope required", bucket), http.StatusForbidden)
		return
	}

	if err := r.ParseMultipartForm(256 << 20); err != nil {
		http.Error(w, "invalid multipart: "+err.Error(), http.StatusBadRequest)
		return
	}

	version := strings.TrimSpace(r.FormValue("version"))
	osArch := strings.TrimSpace(r.FormValue("os_arch"))
	if version == "" || osArch == "" {
		http.Error(w, "version and os_arch are required", http.StatusBadRequest)
		return
	}
	if strings.Contains(version, "..") || strings.ContainsAny(version, "/\\") ||
		strings.Contains(osArch, "..") || strings.ContainsAny(osArch, "/\\") {
		http.Error(w, "invalid version or os_arch", http.StatusBadRequest)
		return
	}

	f, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file field required", http.StatusBadRequest)
		return
	}
	defer f.Close()

	for _, p := range []string{
		"/rs",
		"/rs/" + bucket,
		"/rs/" + bucket + "/" + version,
		"/rs/" + bucket + "/" + version + "/" + osArch,
	} {
		_ = app.webdavMKCOL(p)
	}

	dest, _ := url.JoinPath(app.cfg.WebDAVURL, "rs", bucket, version, osArch, header.Filename)
	req, err := http.NewRequest(http.MethodPut, dest, f)
	if err != nil {
		http.Error(w, "request error", http.StatusInternalServerError)
		return
	}
	req.SetBasicAuth(app.cfg.WebDAVUsername, app.cfg.WebDAVPassword)
	req.ContentLength = header.Size
	if ct := header.Header.Get("Content-Type"); ct != "" && ct != "application/octet-stream" {
		req.Header.Set("Content-Type", ct)
	}

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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"bucket":  bucket,
		"version": version,
		"os_arch": osArch,
		"file":    header.Filename,
	})
}

// PUT /api/v1/release/{bucket}/{version}/{os_arch}/{file...}
// Authorization: Bearer <jwt with release-write:{bucket} scope>
// Body: file contents (streamed)
// Stores file at rs/{bucket}/{version}/{os_arch}/{file} on the WebDAV upstream.
func (app *App) handleReleaseUpload(w http.ResponseWriter, r *http.Request) {
	info := tokenFromCtx(r)
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	bucket := r.PathValue("bucket")
	version := r.PathValue("version")
	osArch := r.PathValue("os_arch")
	file := r.PathValue("file")

	if bucket == "" || version == "" || osArch == "" || file == "" {
		http.Error(w, "bucket, version, os_arch, and file required", http.StatusBadRequest)
		return
	}

	// Require release-write:{bucket}, release-write (global), or write (global).
	allowed := info.HasScope("write") || info.HasScope("release-write") ||
		info.ScopeValue("release-write") == bucket
	if !allowed {
		http.Error(w, fmt.Sprintf("release-write:%s scope required", bucket), http.StatusForbidden)
		return
	}

	// Ensure intermediate directories exist.
	paths := []string{
		"/rs",
		"/rs/" + bucket,
		"/rs/" + bucket + "/" + version,
		"/rs/" + bucket + "/" + version + "/" + osArch,
	}
	for _, p := range paths {
		_ = app.webdavMKCOL(p) // ignore error — directory may already exist
	}

	// PUT the file to the WebDAV upstream.
	dest, _ := url.JoinPath(app.cfg.WebDAVURL, "rs", bucket, version, osArch, file)
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

// GET /api/v1/pub/release/{bucket}
// No auth required. Returns the latest version and all available os/arch targets with files.
func (app *App) handlePublicReleaseInfo(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	if strings.ContainsAny(bucket, "/\\") {
		http.Error(w, "invalid bucket", http.StatusBadRequest)
		return
	}

	latest, err := app.resolveLatestVersion(bucket)
	if err != nil {
		http.Error(w, "bucket not found or has no versions", http.StatusNotFound)
		return
	}

	targets, err := app.listLatestTargets(bucket, latest)
	if err != nil {
		http.Error(w, "error listing targets", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(map[string]any{
		"bucket":  bucket,
		"latest":  latest,
		"targets": targets,
	})
}

// ReleaseFile describes a single file in a release target.
type ReleaseFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// listLatestTargets PROPFINDs /rs/{bucket}/{version}/ and returns a map of
// os_arch → []ReleaseFile for every child directory.
func (app *App) listLatestTargets(bucket, version string) (map[string][]ReleaseFile, error) {
	base := "/rs/" + bucket + "/" + version + "/"

	// List os/arch directories.
	oaDirs, err := app.propfindDirs(base)
	if err != nil {
		return nil, err
	}

	targets := make(map[string][]ReleaseFile, len(oaDirs))
	for _, oa := range oaDirs {
		files, err := app.propfindFiles("/rs/" + bucket + "/" + version + "/" + oa + "/")
		if err != nil {
			continue // skip broken targets
		}
		targets[oa] = files
	}
	return targets, nil
}

// propfindDirs returns the names of child directories at upstreamPath.
func (app *App) propfindDirs(upstreamPath string) ([]string, error) {
	entries, err := app.propfind1(upstreamPath)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.isDir && e.name != "" {
			dirs = append(dirs, e.name)
		}
	}
	return dirs, nil
}

// propfindFiles returns ReleaseFile entries for files at upstreamPath.
func (app *App) propfindFiles(upstreamPath string) ([]ReleaseFile, error) {
	entries, err := app.propfind1(upstreamPath)
	if err != nil {
		return nil, err
	}
	var files []ReleaseFile
	for _, e := range entries {
		if !e.isDir && e.name != "" {
			files = append(files, ReleaseFile{Name: e.name, Size: e.size})
		}
	}
	return files, nil
}

type propfindEntry struct {
	name  string
	isDir bool
	size  int64
}

// propfind1 does a Depth:1 PROPFIND and returns parsed child entries (excluding the root itself).
func (app *App) propfind1(upstreamPath string) ([]propfindEntry, error) {
	// Encode each segment of the decoded upstreamPath for the HTTP request.
	encoded, _ := url.JoinPath(app.cfg.WebDAVURL, strings.Split(strings.Trim(upstreamPath, "/"), "/")...)
	req, err := http.NewRequest("PROPFIND", encoded, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(app.cfg.WebDAVUsername, app.cfg.WebDAVPassword)
	req.Header.Set("Depth", "1")

	resp, err := app.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("PROPFIND %s: %s", upstreamPath, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	type xmlProp struct {
		IsCollection *struct{} `xml:"resourcetype>collection"`
		Size         int64     `xml:"getcontentlength"`
		LastModified string    `xml:"getlastmodified"`
	}
	type xmlResponse struct {
		Href string  `xml:"href"`
		Prop xmlProp `xml:"propstat>prop"`
	}
	type xmlMultistatus struct {
		Responses []xmlResponse `xml:"response"`
	}

	var ms xmlMultistatus
	if err := xml.Unmarshal(body, &ms); err != nil {
		return nil, err
	}

	normalizedRoot := upstreamPath
	if !strings.HasSuffix(normalizedRoot, "/") {
		normalizedRoot += "/"
	}

	var entries []propfindEntry
	for _, r := range ms.Responses {
		href := r.Href
		if i := strings.Index(href, "/rs/"); i >= 0 {
			href = href[i:]
		}
		// URL-decode so we can compare against the decoded normalizedRoot.
		if decoded, err := url.PathUnescape(href); err == nil {
			href = decoded
		}
		href = strings.TrimPrefix(href, normalizedRoot)
		name := strings.Trim(href, "/")
		if name == "" || strings.Contains(name, "/") {
			continue // skip root and deep entries
		}
		entries = append(entries, propfindEntry{
			name:  name,
			isDir: r.Prop.IsCollection != nil,
			size:  r.Prop.Size,
		})
	}
	return entries, nil
}

// webdavMKCOL sends a MKCOL request to create a collection (directory) on the upstream.
// Returns nil if the collection already exists (207/405) or was created (201).
func (app *App) webdavMKCOL(path string) error {
	encoded, _ := url.JoinPath(app.cfg.WebDAVURL, strings.Split(strings.Trim(path, "/"), "/")...)
	req, err := http.NewRequest("MKCOL", encoded, nil)
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
