package main

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey string

const tokenInfoKey ctxKey = "tokenInfo"

type TokenInfo struct {
	KeyID   string
	Scopes  []string
	RootDir string
}

func (t *TokenInfo) HasScope(scope string) bool {
	for _, s := range t.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// HasScopePrefix returns true if any scope equals prefix or starts with "prefix:".
func (t *TokenInfo) HasScopePrefix(prefix string) bool {
	for _, s := range t.Scopes {
		if s == prefix || strings.HasPrefix(s, prefix+":") {
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
			KeyID:   claims.KeyID,
			Scopes:  claims.Scopes,
			RootDir: claims.RootDir,
		}
		ctx := context.WithValue(r.Context(), tokenInfoKey, info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
