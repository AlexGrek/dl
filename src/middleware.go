package main

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey string

const tokenInfoKey ctxKey = "tokenInfo"

type TokenInfo struct {
	KeyID  string
	Scopes []string
}

// CanRead returns true if the token grants read access to the given upstream path.
// Scopes checked: "read", "write", "read:/prefix", "write:/prefix".
// "write" implies read for the same path.
func (t *TokenInfo) CanRead(path string) bool {
	for _, s := range t.Scopes {
		if s == "read" || s == "write" {
			return true
		}
		if strings.HasPrefix(s, "read:/") || strings.HasPrefix(s, "write:/") {
			if pathUnderScope(s, path) {
				return true
			}
		}
	}
	return false
}

// CanWrite returns true if the token grants write access to the given upstream path.
// Scopes checked: "write", "write:/prefix".
func (t *TokenInfo) CanWrite(path string) bool {
	for _, s := range t.Scopes {
		if s == "write" {
			return true
		}
		if strings.HasPrefix(s, "write:/") {
			if pathUnderScope(s, path) {
				return true
			}
		}
	}
	return false
}

// HasScope checks for an exact scope match, or a prefix match for "release-write".
func (t *TokenInfo) HasScope(scope string) bool {
	for _, s := range t.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// ScopeValue returns the value after "prefix:" for the first matching scope.
func (t *TokenInfo) ScopeValue(prefix string) string {
	for _, s := range t.Scopes {
		if strings.HasPrefix(s, prefix+":") {
			return strings.TrimPrefix(s, prefix+":")
		}
	}
	return ""
}

// pathUnderScope checks whether upstreamPath is at or under the root embedded in a
// scope string of the form "read:/root" or "write:/root".
func pathUnderScope(scope, upstreamPath string) bool {
	colon := strings.Index(scope, ":")
	if colon < 0 {
		return false
	}
	root := scope[colon+1:]
	root = "/" + strings.TrimPrefix(root, "/")
	if !strings.HasSuffix(root, "/") {
		root += "/"
	}
	p := "/" + strings.TrimPrefix(upstreamPath, "/")
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return strings.HasPrefix(p, root)
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, prefix))
}

func tokenFromCtx(r *http.Request) *TokenInfo {
	info, _ := r.Context().Value(tokenInfoKey).(*TokenInfo)
	return info
}

func (app *App) jwtMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := extractBearerToken(r)
		if tokenStr == "" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		claims, err := parseJWT(app.cfg.JWTSecret, tokenStr)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		info := &TokenInfo{
			KeyID:  claims.KeyID,
			Scopes: claims.Scopes,
		}
		ctx := context.WithValue(r.Context(), tokenInfoKey, info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
