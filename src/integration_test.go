package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

var (
	testServer    *httptest.Server
	testBaseURL   string
	testMasterKey = "test-master-key-1234567890"
	testJWTSecret = "test-jwt-secret-1234567890"
	testWDUser    = "testuser"
	testWDPass    = "testpass"
	testApp       *App
)

// newFakeWebDAV starts an in-memory WebDAV server with Basic Auth.
func newFakeWebDAV() *httptest.Server {
	h := &webdav.Handler{
		FileSystem: webdav.NewMemFS(),
		LockSystem: webdav.NewMemLS(),
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != testWDUser || pass != testWDPass {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
}

func TestMain(m *testing.M) {
	fakeWD := newFakeWebDAV()
	defer fakeWD.Close()

	tmpDB, err := os.CreateTemp("", "dl-test-*.db")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp db: %v\n", err)
		os.Exit(1)
	}
	tmpDBPath := tmpDB.Name()
	tmpDB.Close()

	cfg := &Config{
		WebDAVURL:      fakeWD.URL,
		WebDAVUsername: testWDUser,
		WebDAVPassword: testWDPass,
		MasterKey:      testMasterKey,
		JWTSecret:      testJWTSecret,
		DBPath:         tmpDBPath,
		Port:           "0",
	}

	store, err := openStore(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open store: %v\n", err)
		os.Exit(1)
	}

	wdProxy, err := newWebDAVProxy(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create webdav proxy: %v\n", err)
		os.Exit(1)
	}

	testApp = &App{
		cfg:     cfg,
		store:   store,
		wdProxy: wdProxy,
		client:  &http.Client{Timeout: 30 * time.Second},
	}

	mux := http.NewServeMux()
	testApp.registerRoutes(mux)

	testServer = httptest.NewServer(mux)
	testBaseURL = testServer.URL

	code := m.Run()

	testServer.Close()
	store.Close()
	os.Remove(tmpDBPath)
	os.Exit(code)
}

// --- helpers ---

func randSuffix() string {
	return fmt.Sprintf("%06d", rand.Intn(1000000))
}

func getMasterJWT(t *testing.T) string {
	t.Helper()
	return getJWT(t, testMasterKey)
}

func getJWT(t *testing.T, apiKey string) string {
	t.Helper()
	req, err := http.NewRequest("POST", testBaseURL+"/api/v1/auth/token", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/v1/auth/token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /api/v1/auth/token: status %d, body: %s", resp.StatusCode, body)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding token response: %v", err)
	}
	tok, ok := result["token"]
	if !ok || tok == "" {
		t.Fatal("token not found in response")
	}
	return tok
}

func createAPIKey(t *testing.T, description string, scopes []string, rootDir string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"description": description,
		"scopes":      scopes,
		"root_dir":    rootDir,
	})
	req, err := http.NewRequest("POST", testBaseURL+"/api/v1/auth/keys", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testMasterKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/v1/auth/keys: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /api/v1/auth/keys: status %d, body: %s", resp.StatusCode, respBody)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding key response: %v", err)
	}
	key, ok := result["key"]
	if !ok || key == "" {
		t.Fatal("key not found in response")
	}
	return key
}

func deleteAPIKeyFromStore(t *testing.T, key string) {
	t.Helper()
	if err := testApp.store.DeleteAPIKey(key); err != nil {
		t.Logf("warning: failed to delete api key from store: %v", err)
	}
}

// --- tests ---

func TestAuthToken_MasterKey(t *testing.T) {
	jwt := getMasterJWT(t)
	if jwt == "" {
		t.Fatal("expected non-empty JWT")
	}
	claims, err := parseJWT(testApp.cfg.JWTSecret, jwt)
	if err != nil {
		t.Fatalf("parsing JWT: %v", err)
	}
	if claims.KeyID != "master" {
		t.Errorf("expected KeyID=master, got %q", claims.KeyID)
	}
	if len(claims.Scopes) == 0 {
		t.Error("expected non-empty scopes")
	}
}

func TestAuthToken_InvalidKey(t *testing.T) {
	req, _ := http.NewRequest("POST", testBaseURL+"/api/v1/auth/token", nil)
	req.Header.Set("Authorization", "Bearer totally-invalid-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestCreateAndListKeys(t *testing.T) {
	key := createAPIKey(t, "integration-test-key", []string{"read"}, "")
	t.Cleanup(func() { deleteAPIKeyFromStore(t, key) })

	req, _ := http.NewRequest("GET", testBaseURL+"/api/v1/auth/keys", nil)
	req.Header.Set("Authorization", "Bearer "+testMasterKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/auth/keys: status %d", resp.StatusCode)
	}
	var keys []APIKey
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		t.Fatalf("decoding keys: %v", err)
	}
	found := false
	for _, k := range keys {
		if k.ID == key[:12] {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created key %s not found in list", key[:12])
	}

	delReq, _ := http.NewRequest("DELETE", testBaseURL+"/api/v1/auth/keys/"+key, nil)
	delReq.Header.Set("Authorization", "Bearer "+testMasterKey)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE key: expected 204, got %d", delResp.StatusCode)
	}
}

func TestCreateKey_Forbidden(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"description": "should-fail",
		"scopes":      []string{"read"},
	})
	req, _ := http.NewRequest("POST", testBaseURL+"/api/v1/auth/keys", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer not-the-master-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestWebDAV_Unauthorized(t *testing.T) {
	req, _ := http.NewRequest("PROPFIND", testBaseURL+"/api/v1/wd/", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestWebDAV_ReadScope(t *testing.T) {
	key := createAPIKey(t, "read-only-key", []string{"read"}, "")
	t.Cleanup(func() { deleteAPIKeyFromStore(t, key) })

	jwt := getJWT(t, key)

	// PROPFIND should work.
	propReq, _ := http.NewRequest("PROPFIND", testBaseURL+"/api/v1/wd/", nil)
	propReq.Header.Set("Authorization", "Bearer "+jwt)
	propReq.Header.Set("Depth", "1")
	propResp, err := http.DefaultClient.Do(propReq)
	if err != nil {
		t.Fatal(err)
	}
	defer propResp.Body.Close()
	if propResp.StatusCode != http.StatusMultiStatus && propResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(propResp.Body)
		t.Errorf("PROPFIND: expected 207 or 200, got %d: %s", propResp.StatusCode, body)
	}

	// PUT should be blocked — no write scope.
	putReq, _ := http.NewRequest("PUT", testBaseURL+"/api/v1/wd/shouldfail.txt", strings.NewReader("data"))
	putReq.Header.Set("Authorization", "Bearer "+jwt)
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusForbidden {
		t.Errorf("PUT with read-only: expected 403, got %d", putResp.StatusCode)
	}
}

func TestWebDAV_WriteScope(t *testing.T) {
	key := createAPIKey(t, "write-key", []string{"read", "write"}, "")
	t.Cleanup(func() { deleteAPIKeyFromStore(t, key) })

	jwt := getJWT(t, key)
	suffix := randSuffix()
	dirPath := "/test-wd-" + suffix
	filePath := dirPath + "/testfile.txt"
	content := "hello " + suffix

	// MKCOL to create a directory.
	mkcolReq, _ := http.NewRequest("MKCOL", testBaseURL+"/api/v1/wd"+dirPath, nil)
	mkcolReq.Header.Set("Authorization", "Bearer "+jwt)
	mkcolResp, err := http.DefaultClient.Do(mkcolReq)
	if err != nil {
		t.Fatal(err)
	}
	mkcolResp.Body.Close()
	if mkcolResp.StatusCode != http.StatusCreated {
		t.Fatalf("MKCOL: expected 201, got %d", mkcolResp.StatusCode)
	}

	// PUT a file.
	putReq, _ := http.NewRequest("PUT", testBaseURL+"/api/v1/wd"+filePath, strings.NewReader(content))
	putReq.Header.Set("Authorization", "Bearer "+jwt)
	putReq.Header.Set("Content-Type", "text/plain")
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putResp.Body.Close()
	if putResp.StatusCode >= 300 {
		t.Fatalf("PUT file: status %d", putResp.StatusCode)
	}

	// GET it back.
	getReq, _ := http.NewRequest("GET", testBaseURL+"/api/v1/wd"+filePath, nil)
	getReq.Header.Set("Authorization", "Bearer "+jwt)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	body, _ := io.ReadAll(getResp.Body)
	if string(body) != content {
		t.Errorf("GET file: expected %q, got %q", content, string(body))
	}
}

func TestReleaseCreate(t *testing.T) {
	jwt := getMasterJWT(t)
	bucket := "itest-" + randSuffix()

	body, _ := json.Marshal(map[string]string{"bucket": bucket})
	req, _ := http.NewRequest("POST", testBaseURL+"/api/v1/release/create", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("release create: expected 201, got %d: %s", resp.StatusCode, respBody)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if result["bucket"] != bucket {
		t.Errorf("expected bucket=%q, got %q", bucket, result["bucket"])
	}
}

func TestReleaseUpload(t *testing.T) {
	jwt := getMasterJWT(t)
	bucket := "itest-" + randSuffix()
	version := "v1.0.0"
	osArch := "linux_amd64"
	fileName := "testbin"
	content := "binary-content-" + randSuffix()

	// Create bucket.
	createBody, _ := json.Marshal(map[string]string{"bucket": bucket})
	createReq, _ := http.NewRequest("POST", testBaseURL+"/api/v1/release/create", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+jwt)
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	createResp.Body.Close()

	// Upload.
	uploadURL := fmt.Sprintf("%s/api/v1/release/%s/%s/%s/%s", testBaseURL, bucket, version, osArch, fileName)
	uploadReq, _ := http.NewRequest("PUT", uploadURL, strings.NewReader(content))
	uploadReq.Header.Set("Authorization", "Bearer "+jwt)
	uploadReq.Header.Set("Content-Type", "application/octet-stream")
	uploadResp, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		t.Fatal(err)
	}
	uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("release upload: expected 201, got %d", uploadResp.StatusCode)
	}

	// Verify via /rs/ public route.
	rsURL := fmt.Sprintf("%s/rs/%s/%s/%s/%s", testBaseURL, bucket, version, osArch, fileName)
	getResp, err := http.Get(rsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	gotBody, _ := io.ReadAll(getResp.Body)
	if string(gotBody) != content {
		t.Errorf("GET /rs/: expected %q, got %q", content, string(gotBody))
	}
}

func TestDownload_PublicRelease(t *testing.T) {
	jwt := getMasterJWT(t)
	bucket := "itest-" + randSuffix()
	version := "v1.0.0"
	osArch := "linux_amd64"
	fileName := "pubfile"
	content := "public-release-" + randSuffix()

	// Create bucket + upload.
	createBody, _ := json.Marshal(map[string]string{"bucket": bucket})
	createReq, _ := http.NewRequest("POST", testBaseURL+"/api/v1/release/create", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+jwt)
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	createResp.Body.Close()

	uploadURL := fmt.Sprintf("%s/api/v1/release/%s/%s/%s/%s", testBaseURL, bucket, version, osArch, fileName)
	uploadReq, _ := http.NewRequest("PUT", uploadURL, strings.NewReader(content))
	uploadReq.Header.Set("Authorization", "Bearer "+jwt)
	uploadResp, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		t.Fatal(err)
	}
	uploadResp.Body.Close()

	// Public download at /rs/ — no auth.
	rsURL := fmt.Sprintf("%s/rs/%s/%s/%s/%s", testBaseURL, bucket, version, osArch, fileName)
	resp, err := http.Get(rsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /rs/: expected 200, got %d", resp.StatusCode)
	}
	gotBody, _ := io.ReadAll(resp.Body)
	if string(gotBody) != content {
		t.Errorf("expected %q, got %q", content, string(gotBody))
	}
}

func TestDownload_Direct(t *testing.T) {
	jwt := getMasterJWT(t)
	suffix := randSuffix()
	dirName := "test-dl-" + suffix
	fileName := "file.txt"
	content := "direct-dl-" + suffix

	// Create dir + file via WebDAV proxy.
	mkcolReq, _ := http.NewRequest("MKCOL", testBaseURL+"/api/v1/wd/"+dirName, nil)
	mkcolReq.Header.Set("Authorization", "Bearer "+jwt)
	mkcolResp, err := http.DefaultClient.Do(mkcolReq)
	if err != nil {
		t.Fatal(err)
	}
	mkcolResp.Body.Close()

	putReq, _ := http.NewRequest("PUT", testBaseURL+"/api/v1/wd/"+dirName+"/"+fileName, strings.NewReader(content))
	putReq.Header.Set("Authorization", "Bearer "+jwt)
	putResp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	putResp.Body.Close()

	// Public download at /d/ — no auth.
	dlURL := fmt.Sprintf("%s/d/%s/%s", testBaseURL, dirName, fileName)
	resp, err := http.Get(dlURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /d/: expected 200, got %d: %s", resp.StatusCode, body)
	}
	gotBody, _ := io.ReadAll(resp.Body)
	if string(gotBody) != content {
		t.Errorf("expected %q, got %q", content, string(gotBody))
	}
}

func TestDownload_NotFound(t *testing.T) {
	resp, err := http.Get(testBaseURL + "/d/nonexistent-" + randSuffix())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestSPA_Fallback(t *testing.T) {
	resp, err := http.Get(testBaseURL + "/some-spa-route-" + randSuffix())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "html") {
		t.Errorf("expected HTML content, got: %.200s", string(body))
	}
}
