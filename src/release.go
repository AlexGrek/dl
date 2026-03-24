package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
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
	if !info.CanWriteReleaseBucket(bucket) {
		http.Error(w, fmt.Sprintf("release-write:%s scope required", bucket), http.StatusForbidden)
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "invalid multipart: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Read parts in order: collect text fields until we hit the file part.
	var version, osArch, filename string
	var fileReader io.Reader

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "multipart read error: "+err.Error(), http.StatusBadRequest)
			return
		}

		switch part.FormName() {
		case "version":
			b, _ := io.ReadAll(io.LimitReader(part, 256))
			version = strings.TrimSpace(string(b))
		case "os_arch":
			b, _ := io.ReadAll(io.LimitReader(part, 256))
			osArch = strings.TrimSpace(string(b))
		case "file":
			filename = part.FileName()
			fileReader = part
		}

		if fileReader != nil {
			break // stream the file part below; leave remaining parts unread
		}
	}

	if version == "" || osArch == "" {
		http.Error(w, "version and os_arch are required", http.StatusBadRequest)
		return
	}
	if !safeSegment(version) || !safeSegment(osArch) {
		http.Error(w, "invalid version or os_arch", http.StatusBadRequest)
		return
	}
	if fileReader == nil {
		http.Error(w, "file field required", http.StatusBadRequest)
		return
	}
	if !safeSegment(filename) {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}

	for _, p := range []string{
		"/rs",
		"/rs/" + bucket,
		"/rs/" + bucket + "/" + version,
		"/rs/" + bucket + "/" + version + "/" + osArch,
	} {
		_ = app.webdavMKCOL(p)
	}

	dest, _ := url.JoinPath(app.cfg.WebDAVURL, "rs", bucket, version, osArch, filename)
	req, err := http.NewRequest(http.MethodPut, dest, fileReader)
	if err != nil {
		http.Error(w, "request error", http.StatusInternalServerError)
		return
	}
	req.SetBasicAuth(app.cfg.WebDAVUsername, app.cfg.WebDAVPassword)
	req.ContentLength = -1 // unknown: streaming, no buffering

	resp, err := app.client.Do(req)
	if err != nil {
		http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		http.Error(w, fmt.Sprintf("upstream: %s", strings.TrimSpace(string(body))), resp.StatusCode)
		return
	}

	_ = app.store.ClearCache()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"bucket":  bucket,
		"version": version,
		"os_arch": osArch,
		"file":    filename,
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
	if !safeSegment(bucket) || !safeSegment(version) || !safeSegment(osArch) {
		http.Error(w, "invalid bucket, version, or os_arch", http.StatusBadRequest)
		return
	}
	// file is a multi-segment wildcard; check for null bytes and .. components.
	if strings.Contains(file, "\x00") || strings.Contains(file, "..") {
		http.Error(w, "invalid file path", http.StatusBadRequest)
		return
	}

	if !info.CanWriteReleaseBucket(bucket) {
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

	_ = app.store.ClearCache()

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
	name     string
	isDir    bool
	size     int64
	modified string // RFC1123 timestamp from getlastmodified
}

// propfind1 does a Depth:1 PROPFIND and returns parsed child entries (excluding the root itself).
func (app *App) propfind1(upstreamPath string) ([]propfindEntry, error) {
	// Encode each segment of the decoded upstreamPath for the HTTP request.
	// Always add a trailing slash so WebDAV servers don't redirect (redirect
	// drops PROPFIND → GET on Go's default HTTP client).
	encoded, _ := url.JoinPath(app.cfg.WebDAVURL, strings.Split(strings.Trim(upstreamPath, "/"), "/")...)
	encoded += "/"
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
			name:     name,
			isDir:    r.Prop.IsCollection != nil,
			size:     r.Prop.Size,
			modified: r.Prop.LastModified,
		})
	}
	return entries, nil
}

// VersionInfo describes a single release version for the auto-update API.
type VersionInfo struct {
	Version string `json:"version"`
	Date    string `json:"date,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

// GET /api/v1/pub/release/{bucket}/latest
// No auth required. Returns the latest version string, release metadata (date, notes),
// and the sorted list of available os/arch targets.
//
// Intended for auto-update checks: compare the returned "version" to the running binary's
// version to decide whether an update is available.
func (app *App) handlePublicReleaseLatest(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	if strings.ContainsAny(bucket, "/\\") {
		http.Error(w, "invalid bucket", http.StatusBadRequest)
		return
	}

	version, err := app.resolveLatestVersion(bucket)
	if err != nil {
		http.Error(w, "bucket not found or has no versions", http.StatusNotFound)
		return
	}

	result := map[string]any{
		"bucket":  bucket,
		"version": version,
	}

	if rm := app.fetchReleaseMeta(bucket, version); rm != nil {
		if rm.Date != "" {
			result["date"] = rm.Date
		}
		if rm.Notes != "" {
			result["notes"] = rm.Notes
		}
	}

	var targetNames []string
	if targets, err := app.listLatestTargets(bucket, version); err == nil {
		for t := range targets {
			targetNames = append(targetNames, t)
		}
		sort.Strings(targetNames)
	}
	if targetNames == nil {
		targetNames = []string{}
	}
	result["targets"] = targetNames

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(result)
}

// GET /api/v1/pub/release/{bucket}/versions
// No auth required. Returns all available versions for the bucket, sorted newest-first
// by the last-modified timestamp of the version directory.
//
// Each entry includes the version string and, if a release.yaml exists, its date and notes.
func (app *App) handlePublicReleaseVersions(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	if strings.ContainsAny(bucket, "/\\") {
		http.Error(w, "invalid bucket", http.StatusBadRequest)
		return
	}

	cacheKey := "versions:" + bucket
	if cached, ok := app.store.GetCache(cacheKey); ok {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(cached)
		return
	}

	entries, err := app.propfind1("/rs/" + bucket + "/")
	if err != nil {
		http.Error(w, "bucket not found", http.StatusNotFound)
		return
	}

	// Sort version directories newest-first by last-modified.
	sort.Slice(entries, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC1123, entries[i].modified)
		tj, _ := time.Parse(time.RFC1123, entries[j].modified)
		return ti.After(tj)
	})

	versions := make([]VersionInfo, 0)
	for _, e := range entries {
		if !e.isDir {
			continue
		}
		vi := VersionInfo{Version: e.name}
		if rm := app.fetchReleaseMeta(bucket, e.name); rm != nil {
			vi.Date = rm.Date
			vi.Notes = rm.Notes
		}
		versions = append(versions, vi)
	}

	data, _ := json.Marshal(versions)
	app.store.PutCache(cacheKey, data, listCacheTTL)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

// GET /api/v1/pub/release/{bucket}/versions/{version}/targets
// No auth required. Returns the sorted list of os/arch target strings available for
// the given version. Use the pseudo-version "latest" to resolve the most recent version.
//
// Each target string is a directory name under /rs/{bucket}/{version}/, conventionally
// formatted as "{os}-{arch}", e.g. "linux-amd64", "darwin-arm64", "windows-amd64".
func (app *App) handlePublicReleaseTargetList(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	version := r.PathValue("version")

	if strings.ContainsAny(bucket, "/\\") || strings.ContainsAny(version, "/\\") {
		http.Error(w, "invalid bucket or version", http.StatusBadRequest)
		return
	}

	if version == "latest" {
		v, err := app.resolveLatestVersion(bucket)
		if err != nil {
			http.Error(w, "bucket not found or has no versions", http.StatusNotFound)
			return
		}
		version = v
	}

	oaDirs, err := app.propfindDirs("/rs/" + bucket + "/" + version + "/")
	if err != nil {
		http.Error(w, "version not found", http.StatusNotFound)
		return
	}

	sort.Strings(oaDirs)
	if oaDirs == nil {
		oaDirs = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(oaDirs)
}

// webdavMKCOL sends a MKCOL request to create a collection (directory) on the upstream.
// Returns nil if the collection already exists (207/405) or was created (201).
func (app *App) webdavMKCOL(path string) error {
	encoded, _ := url.JoinPath(app.cfg.WebDAVURL, strings.Split(strings.Trim(path, "/"), "/")...)
	encoded += "/"
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
