package main

import (
	"context"
	"net/http"
	"path"
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

// isAncestorPath reports whether ancestor is a strict parent directory of descendant.
func isAncestorPath(ancestor, descendant string) bool {
	a := path.Clean("/" + strings.TrimPrefix(ancestor, "/"))
	d := path.Clean("/" + strings.TrimPrefix(descendant, "/"))
	if a == d {
		return false
	}
	if a == "/" {
		return true
	}
	return strings.HasPrefix(d+"/", a+"/")
}

// IsAncestorOfAccessible reports whether p is a parent directory of any path
// this token grants read access to. Used to return filtered PROPFIND listings
// instead of a flat 403 when the token's root_dir is a sub-path of the requested directory.
func (t *TokenInfo) IsAncestorOfAccessible(p string) bool {
	for _, s := range t.Scopes {
		if s == "read" || s == "write" {
			return true
		}
		if !strings.HasPrefix(s, "read:/") && !strings.HasPrefix(s, "write:/") {
			continue
		}
		colon := strings.Index(s, ":")
		root := s[colon+1:]
		if strings.HasSuffix(root, "*") {
			// Wildcard: p is an ancestor of paths matching the pattern if p is an
			// ancestor of the static prefix before the *.
			prefix := "/" + strings.TrimPrefix(strings.TrimSuffix(root, "*"), "/")
			if isAncestorPath(p, prefix) {
				return true
			}
		} else {
			if isAncestorPath(p, root) {
				return true
			}
		}
	}
	return false
}

// pathUnderScope checks whether upstreamPath is at or under the root embedded in a
// scope string of the form "read:/root" or "write:/root".
// A trailing * in the path enables prefix matching without segment-boundary enforcement:
//
//	"read:/shared-*" matches /shared-foo/, /shared-bar/, etc.
//	"read:/projects/" matches only the /projects/ subtree (exact boundary).
func pathUnderScope(scope, upstreamPath string) bool {
	colon := strings.Index(scope, ":")
	if colon < 0 {
		return false
	}
	root := scope[colon+1:]

	if strings.HasSuffix(root, "*") {
		// Trailing wildcard: plain string-prefix match (no path.Clean normalization so
		// the wildcard position is preserved exactly as written).
		prefix := "/" + strings.TrimPrefix(strings.TrimSuffix(root, "*"), "/")
		p := "/" + strings.TrimPrefix(upstreamPath, "/")
		return strings.HasPrefix(p, prefix)
	}

	root = path.Clean("/" + strings.TrimPrefix(root, "/"))
	if !strings.HasSuffix(root, "/") {
		root += "/"
	}
	p := path.Clean("/" + strings.TrimPrefix(upstreamPath, "/"))
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return strings.HasPrefix(p, root)
}

// CanWriteReleaseBucket returns true if the token grants release-write access to the
// given bucket name.  Exact match ("release-write:bucket") and trailing-wildcard
// ("release-write:prefix*") are both supported, in addition to the global "write" and
// "release-write" (no colon suffix) scopes.
func (t *TokenInfo) CanWriteReleaseBucket(bucket string) bool {
	if t.HasScope("write") || t.HasScope("release-write") {
		return true
	}
	for _, s := range t.Scopes {
		if !strings.HasPrefix(s, "release-write:") {
			continue
		}
		val := strings.TrimPrefix(s, "release-write:")
		if strings.HasSuffix(val, "*") {
			if strings.HasPrefix(bucket, strings.TrimSuffix(val, "*")) {
				return true
			}
		} else if val == bucket {
			return true
		}
	}
	return false
}

// safeSegment returns false if s contains characters that could enable path
// traversal: null bytes, ".." sequences, slashes, or backslashes.
// Use this for individual path components (bucket, version, os_arch, filename).
func safeSegment(s string) bool {
	return s != "" &&
		!strings.Contains(s, "\x00") &&
		!strings.Contains(s, "..") &&
		!strings.ContainsAny(s, "/\\")
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
