package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	metaCacheTTL = 5 * time.Minute
	listCacheTTL = 2 * time.Minute
)

// ── YAML metadata types ──

// ProductMeta is parsed from /rs/{bucket}/product.yaml.
type ProductMeta struct {
	Name        string   `yaml:"name" json:"name"`
	Tagline     string   `yaml:"tagline" json:"tagline"`
	Description string   `yaml:"description" json:"description"`
	Homepage    string   `yaml:"homepage" json:"homepage,omitempty"`
	License     string   `yaml:"license" json:"license,omitempty"`
	Tags        []string `yaml:"tags" json:"tags"`
}

// ReleaseMeta is parsed from /rs/{bucket}/{version}/release.yaml.
type ReleaseMeta struct {
	Date  string `yaml:"date" json:"date,omitempty"`
	Notes string `yaml:"notes" json:"notes"`
}

// ── API response types ──

type ProductSummary struct {
	Bucket  string   `json:"bucket"`
	Name    string   `json:"name"`
	Tagline string   `json:"tagline"`
	Latest  string   `json:"latest,omitempty"`
	Targets []string `json:"targets"`
	Tags    []string `json:"tags"`
	License string   `json:"license,omitempty"`
}

type ProductDetail struct {
	Bucket      string          `json:"bucket"`
	Name        string          `json:"name"`
	Tagline     string          `json:"tagline"`
	Description string          `json:"description"`
	Homepage    string          `json:"homepage,omitempty"`
	License     string          `json:"license,omitempty"`
	Tags        []string        `json:"tags"`
	Readme      string          `json:"readme,omitempty"`
	ReleaseDoc  string          `json:"release_doc,omitempty"`
	Versions    []VersionDetail `json:"versions"`
}

type VersionDetail struct {
	Version      string                   `json:"version"`
	Date         string                   `json:"date,omitempty"`
	Notes        string                   `json:"notes,omitempty"`
	ReleaseNotes string                   `json:"release_notes,omitempty"`
	Targets      map[string][]ReleaseFile `json:"targets"`
}

// ── Handlers ──

// GET /api/v1/pub/products
func (app *App) handleListProducts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")

	if cached, ok := app.store.GetCache("products-list"); ok {
		w.Write(cached)
		return
	}

	buckets, err := app.propfindDirs("/rs/")
	if err != nil {
		log.Printf("handleListProducts: propfind /rs/: %v", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}

	products := make([]ProductSummary, 0, len(buckets))
	for _, b := range buckets {
		meta := app.fetchProductMeta(b)
		summary := ProductSummary{Bucket: b}
		if meta != nil {
			summary.Name = meta.Name
			summary.Tagline = meta.Tagline
			summary.Tags = meta.Tags
			summary.License = meta.License
		}
		if summary.Name == "" {
			summary.Name = b
		}
		if summary.Tags == nil {
			summary.Tags = []string{}
		}

		if latest, err := app.resolveLatestVersion(b); err == nil {
			summary.Latest = latest
			if targets, err := app.listLatestTargets(b, latest); err == nil {
				for t := range targets {
					summary.Targets = append(summary.Targets, t)
				}
				sort.Strings(summary.Targets)
			}
		}
		if summary.Targets == nil {
			summary.Targets = []string{}
		}

		products = append(products, summary)
	}

	data, err := json.Marshal(products)
	if err != nil {
		log.Printf("handleListProducts: marshal error: %v", err)
		w.Write([]byte("[]"))
		return
	}
	app.store.PutCache("products-list", data, listCacheTTL)
	w.Write(data)
}

// GET /api/v1/pub/products/{bucket}
func (app *App) handleGetProduct(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	if strings.ContainsAny(bucket, "/\\") {
		http.Error(w, "invalid bucket", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")

	cacheKey := "product-detail:" + bucket
	if cached, ok := app.store.GetCache(cacheKey); ok {
		w.Write(cached)
		return
	}

	meta := app.fetchProductMeta(bucket)
	detail := ProductDetail{Bucket: bucket}
	if meta != nil {
		detail.Name = meta.Name
		detail.Tagline = meta.Tagline
		detail.Description = meta.Description
		detail.Homepage = meta.Homepage
		detail.License = meta.License
		detail.Tags = meta.Tags
	}
	if detail.Name == "" {
		detail.Name = bucket
	}
	if detail.Tags == nil {
		detail.Tags = []string{}
	}

	detail.Readme = app.fetchMarkdownDoc("/rs/"+bucket+"/README.md", "md:product:"+bucket+":readme")
	detail.ReleaseDoc = app.fetchMarkdownDoc("/rs/"+bucket+"/RELEASE.md", "md:product:"+bucket+":release")

	// List all version directories, sorted newest-first by modified date.
	versionEntries, err := app.propfind1("/rs/" + bucket + "/")
	if err != nil {
		// Bucket probably doesn't exist.
		http.Error(w, "product not found", http.StatusNotFound)
		return
	}

	sort.Slice(versionEntries, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC1123, versionEntries[i].modified)
		tj, _ := time.Parse(time.RFC1123, versionEntries[j].modified)
		return ti.After(tj)
	})

	detail.Versions = make([]VersionDetail, 0)
	for _, ve := range versionEntries {
		if !ve.isDir {
			continue // skip files like product.yaml
		}
		vd := VersionDetail{
			Version: ve.name,
			Targets: map[string][]ReleaseFile{},
		}

		if rm := app.fetchReleaseMeta(bucket, ve.name); rm != nil {
			vd.Date = rm.Date
			vd.Notes = rm.Notes
		}

		vd.ReleaseNotes = app.fetchMarkdownDoc(
			"/rs/"+bucket+"/"+ve.name+"/release_notes.md",
			"md:version:"+bucket+":"+ve.name+":release-notes",
		)

		if targets, err := app.listLatestTargets(bucket, ve.name); err == nil {
			vd.Targets = targets
		}

		detail.Versions = append(detail.Versions, vd)
	}

	data, err := json.Marshal(detail)
	if err != nil {
		log.Printf("handleGetProduct %s: marshal error: %v", bucket, err)
		// detail is already partially populated; write it without caching.
		json.NewEncoder(w).Encode(detail)
		return
	}
	app.store.PutCache(cacheKey, data, listCacheTTL)
	w.Write(data)
}

// ── Markdown doc fetching + caching ──

// fetchMarkdownDoc fetches a markdown file from WebDAV and caches it in BoltDB.
// Returns empty string on miss (also cached to avoid repeated lookups).
func (app *App) fetchMarkdownDoc(webdavPath, cacheKey string) string {
	if cached, ok := app.store.GetCache(cacheKey); ok {
		var content string
		if json.Unmarshal(cached, &content) == nil {
			return content // empty string = cached miss
		}
	}

	data, err := app.fetchWebDAVFile(webdavPath)
	if err != nil {
		miss, _ := json.Marshal("")
		app.store.PutCache(cacheKey, miss, metaCacheTTL)
		return ""
	}

	content := strings.TrimSpace(string(data))
	jsonData, _ := json.Marshal(content)
	app.store.PutCache(cacheKey, jsonData, metaCacheTTL)
	return content
}

// ── WebDAV file fetching + caching ──

// fetchWebDAVFile fetches a single file from the WebDAV upstream.
func (app *App) fetchWebDAVFile(path string) ([]byte, error) {
	u, _ := url.JoinPath(app.cfg.WebDAVURL, strings.Split(strings.Trim(path, "/"), "/")...)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(app.cfg.WebDAVUsername, app.cfg.WebDAVPassword)

	resp, err := app.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<10)) // 256KB max
}

// fetchProductMeta fetches and caches product.yaml for a bucket.
func (app *App) fetchProductMeta(bucket string) *ProductMeta {
	cacheKey := "meta:product:" + bucket
	if cached, ok := app.store.GetCache(cacheKey); ok {
		var meta ProductMeta
		if json.Unmarshal(cached, &meta) == nil && meta.Name != "" {
			return &meta
		}
		return nil // cached miss sentinel
	}

	data, err := app.fetchWebDAVFile("/rs/" + bucket + "/product.yaml")
	if err != nil {
		// Cache the miss so we don't retry immediately.
		app.store.PutCache(cacheKey, []byte("{}"), metaCacheTTL)
		return nil
	}

	var meta ProductMeta
	if yaml.Unmarshal(data, &meta) != nil {
		return nil
	}

	if jsonData, err := json.Marshal(meta); err == nil {
		app.store.PutCache(cacheKey, jsonData, metaCacheTTL)
	}
	return &meta
}

// fetchReleaseMeta fetches and caches release.yaml for a specific version.
func (app *App) fetchReleaseMeta(bucket, version string) *ReleaseMeta {
	cacheKey := "meta:release:" + bucket + ":" + version
	if cached, ok := app.store.GetCache(cacheKey); ok {
		var meta ReleaseMeta
		if json.Unmarshal(cached, &meta) == nil && meta.Notes != "" {
			return &meta
		}
		// Cached empty → no release.yaml exists.
		if string(cached) == "{}" {
			return nil
		}
	}

	data, err := app.fetchWebDAVFile("/rs/" + bucket + "/" + version + "/release.yaml")
	if err != nil {
		app.store.PutCache(cacheKey, []byte("{}"), metaCacheTTL)
		return nil
	}

	var meta ReleaseMeta
	if yaml.Unmarshal(data, &meta) != nil {
		return nil
	}

	if jsonData, err := json.Marshal(meta); err == nil {
		app.store.PutCache(cacheKey, jsonData, metaCacheTTL)
	}
	return &meta
}
