package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const jwtDuration = time.Hour

// Claims are the JWT payload fields.
type Claims struct {
	jwt.RegisteredClaims
	KeyID  string   `json:"kid"`
	Scopes []string `json:"scopes"`
}

func generateAPIKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "dlk_" + base64.RawURLEncoding.EncodeToString(b), nil
}

func issueJWT(secret string, record *APIKey) (string, error) {
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(jwtDuration)),
		},
		KeyID:  record.ID,
		Scopes: record.Scopes,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func parseJWT(secret, tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, jwt.ErrTokenInvalidClaims
	}
	return claims, nil
}

// POST /api/v1/auth/token
// Authorization: Bearer <api_key>
// Response: {"token": "<jwt>"}
func (app *App) handleAuthToken(w http.ResponseWriter, r *http.Request) {
	apiKey := extractBearerToken(r)
	if apiKey == "" {
		http.Error(w, "missing api key", http.StatusUnauthorized)
		return
	}

	var record *APIKey
	if apiKey == app.cfg.MasterKey {
		record = &APIKey{
			ID:     "master",
			Scopes: []string{"read", "write", "release-create", "release-write"},
		}
	} else {
		var err error
		record, err = app.store.GetAPIKey(apiKey)
		if err != nil {
			http.Error(w, "invalid api key", http.StatusUnauthorized)
			return
		}
	}

	tok, err := issueJWT(app.cfg.JWTSecret, record)
	if err != nil {
		http.Error(w, "token error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": tok})
}

// POST /api/v1/auth/keys
// Authorization: Bearer <master_key>
// Body: {"description":"...","scopes":[...],"root_dir":"..."}
// Response: {"key":"<api_key>","id":"..."}
func (app *App) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	if extractBearerToken(r) != app.cfg.MasterKey {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req struct {
		Description string   `json:"description"`
		Scopes      []string `json:"scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	key, err := generateAPIKey()
	if err != nil {
		http.Error(w, "key generation failed", http.StatusInternalServerError)
		return
	}
	record := &APIKey{
		ID:          key[:12],
		Description: req.Description,
		Scopes:      req.Scopes,
		CreatedAt:   time.Now(),
	}
	if err := app.store.PutAPIKey(key, record); err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"key": key, "id": record.ID})
}

// GET /api/v1/auth/keys
// Authorization: Bearer <master_key>
// Response: [{"id":"...","description":"...","scopes":[...],"created_at":"..."}]
func (app *App) handleListKeys(w http.ResponseWriter, r *http.Request) {
	if extractBearerToken(r) != app.cfg.MasterKey {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	keys, err := app.store.ListAPIKeys()
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if keys == nil {
		keys = []APIKey{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

// DELETE /api/v1/auth/keys/{id}
// Authorization: Bearer <master_key>
func (app *App) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	if extractBearerToken(r) != app.cfg.MasterKey {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	if err := app.store.DeleteAPIKey(key); err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
