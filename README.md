# dl

A file download/upload service that acts as a WebDAV proxy with a Preact web UI. Live at **https://dl.alexgr.space**.

Single Go binary. Embeds the compiled frontend. Backed by a remote WebDAV server (Hetzner Storage Box). API keys stored in BoltDB. Short-lived JWTs for uploads. Publicly accessible downloads with no auth.

---

## How it works

```
browser / curl
     │
     ▼
https://dl.alexgr.space   (Traefik + cert-manager TLS)
     │
     ▼
dl pod  :8080   (Go binary, embeds Preact frontend)
     │
     ├── GET /                → Preact SPA (React Router)
     ├── GET /d/{path}        → stream file from WebDAV (public)
     ├── GET /rs/{path}       → stream release file from WebDAV (public)
     ├── POST /api/v1/auth/*  → key exchange, key management
     ├── /api/v1/wd/*         → full WebDAV proxy (JWT required)
     └── /api/v1/release/*    → release bucket management (JWT required)
          │
          ▼
     Hetzner Storage Box (WebDAV)
     u545759-sub2.your-storagebox.de
```

---

## Quick start

### Download a release file (no auth)

```bash
curl https://dl.alexgr.space/rs/offloadmq-agent/darwin-arm64/agent.dmg -o agent.dmg
# or via the short /d/ URL:
curl https://dl.alexgr.space/d/rs/offloadmq-agent/darwin-arm64/agent.dmg -o agent.dmg
```

### Upload a release from CI

```bash
# 1. Exchange your API key for a short-lived JWT (1 hour)
TOKEN=$(curl -s -X POST https://dl.alexgr.space/api/v1/auth/token \
  -H "Authorization: Bearer $DL_API_KEY" | jq -r .token)

# 2. Upload
curl -X PUT https://dl.alexgr.space/api/v1/release/my-app/darwin-arm64/app.dmg \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/octet-stream" \
  --data-binary @app.dmg

# 3. Publicly available immediately
curl https://dl.alexgr.space/rs/my-app/darwin-arm64/app.dmg
```

---

## Auth

### Concepts

| Thing | What it is |
|---|---|
| **Master key** | Long secret in `.secrets.yaml`. Never stored in DB. Used only to manage API keys. |
| **API key** | Scoped credential (`dlk_...`) stored in BoltDB (SHA-256 hashed). Exchanged for a JWT. |
| **JWT** | HS256 token, 1-hour TTL. Carries scopes and optional `root_dir`. Used on all protected endpoints. |

### Scopes

| Scope | Grants |
|---|---|
| `read` | PROPFIND, GET via WebDAV proxy (constrained to `root_dir` if set) |
| `write` | PUT, DELETE, MKCOL etc. via WebDAV proxy (constrained to `root_dir` if set) |
| `release-create` | Create new release buckets |
| `release-write:{bucket}` | Upload files to a specific release bucket only |

### Flow

```
Master key ──► POST /api/v1/auth/keys ──► create scoped API key (dlk_...)
                                                    │
Any API key ──► POST /api/v1/auth/token ──► JWT ───► protected endpoints
```

### Manage API keys (requires master key)

```bash
# Create a key scoped to uploading to one bucket
curl -X POST https://dl.alexgr.space/api/v1/auth/keys \
  -H "Authorization: Bearer $MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{"description": "CI – my-app", "scopes": ["release-write:my-app"]}'

# List all keys
curl https://dl.alexgr.space/api/v1/auth/keys \
  -H "Authorization: Bearer $MASTER_KEY"

# Delete a key (pass the raw key value)
curl -X DELETE https://dl.alexgr.space/api/v1/auth/keys/dlk_... \
  -H "Authorization: Bearer $MASTER_KEY"
```

---

## API

Full reference: [`docs/api.md`](docs/api.md)

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/auth/token` | API key | Exchange key → JWT |
| `POST` | `/api/v1/auth/keys` | Master key | Create API key |
| `GET` | `/api/v1/auth/keys` | Master key | List API keys |
| `DELETE` | `/api/v1/auth/keys/{key}` | Master key | Delete API key |
| `*` | `/api/v1/wd/{path}` | JWT | Full WebDAV proxy |
| `POST` | `/api/v1/release/create` | JWT (`release-create`) | Create release bucket |
| `PUT` | `/api/v1/release/{bucket}/{os_arch}/{file}` | JWT (`release-write:{bucket}`) | Upload release file |
| `GET` | `/rs/{path}` | none | Download release file |
| `GET` | `/d/{path}` | none | Download any file (short URL) |
| `GET` | `/` | none | Preact SPA |

---

## Backend

Pure `net/http` stdlib. No web framework. Max memory efficiency — all file transfers use `io.Copy` with no intermediate buffering.

### Source layout

```
src/
├── main.go         # App struct, router, SPA handler, //go:embed all:static
├── config.go       # loadConfig() — reads nested secrets: block from YAML
├── store.go        # BoltDB wrapper — SHA-256 hashed key lookup
├── auth.go         # JWT issue/parse, auth HTTP handlers
├── middleware.go   # jwtMiddleware, TokenInfo, scope helpers
├── webdav.go       # httputil.ReverseProxy — strips prefix, injects Basic Auth
├── release.go      # MKCOL bucket creation, streamed PUT upload
├── download.go     # Streamed GET proxy for /d/ and /rs/
└── static/         # Compiled Preact app (embedded via go:embed)
```

### WebDAV proxy

`/api/v1/wd/*` is a thin `httputil.ReverseProxy` pointing at the Hetzner Storage Box. The proxy strips the `/api/v1/wd` prefix and injects `Authorization: Basic` for the upstream. All WebDAV methods pass through — `PROPFIND`, `MKCOL`, `COPY`, `MOVE`, `LOCK`, etc. The JWT middleware enforces read/write scope before the request reaches the proxy. If the token has a `root_dir`, requests outside that directory return 403.

### Release routes

Release files live on the WebDAV server under `/rs/{bucket}/{os_arch}/{file}`. The upload handler (`PUT /api/v1/release/{bucket}/{os_arch}/{file...}`) creates intermediate `MKCOL` directories as needed, then streams the request body directly to the upstream with a WebDAV `PUT`. The download routes (`/rs/` and `/d/`) proxy the response back with `io.Copy` — no temp files, no memory buffering.

### BoltDB

Single bucket: `apikeys`. Key is `hex(sha256(raw_api_key))`. Value is a JSON-encoded `APIKey` struct:

```json
{
  "id": "dlk_q1-bq6M",
  "description": "CI – my-app",
  "scopes": ["release-write:my-app"],
  "root_dir": "",
  "created_at": "2026-03-23T21:10:00Z"
}
```

The raw key is returned once on creation and never stored.

---

## Frontend

Preact + TypeScript + Vite. No UI frameworks. Pure custom CSS. Console-like dark theme, monospace fonts, no animations. Mobile-first.

The compiled frontend is embedded into the Go binary at build time via `//go:embed all:static`. The SPA handler serves `index.html` for any unknown path, enabling client-side routing.

---

## Configuration

`.secrets.yaml` is git-ignored and serves double duty: the Go binary reads it directly, and `make deploy` passes it to helm with `-f .secrets.yaml`.

```yaml
secrets:
  webdav_url: "https://your-storagebox.example.com"
  webdav_username: "your-username"
  webdav_password: "your-password"
  master_key: "long-random-string"
  jwt_secret: "long-random-string"
  db_path: "/data/dl.db"   # /data/ is the persistent volume mount
  port: "8080"
```

---

## Build & deploy

### Local dev

```bash
# Backend only (uses placeholder frontend)
make run

# Frontend dev server with HMR (proxies /api/v1 to backend)
cd dl-frontend && npm run dev
```

### Full build

```bash
make build    # npm run build → copy to src/static/ → go build
make test     # go test ./src/... (integration tests, no external deps)
```

### Deploy to production

```bash
make deploy
```

This does three things in sequence:

1. `docker buildx build --platform linux/amd64 -t grekodocker/dl:<git-sha> --push .`
   Multi-stage build: Node 22 Alpine (frontend) → Go 1.24 Alpine (backend + embed) → Debian bookworm-slim (runtime)

2. `helm upgrade --install dl ./dl-chart --create-namespace --namespace dl --set image.tag=<git-sha> -f .secrets.yaml`

The image tag is always the short git commit SHA so every deployment is traceable.

### Helm chart

```
dl-chart/
├── Chart.yaml
├── values.yaml                  # defaults — override with -f .secrets.yaml
└── templates/
    ├── statefulset.yaml         # single replica, PVC at /data/, secret at /etc/dl/
    ├── service.yaml             # headless ClusterIP (StatefulSet stable identity)
    ├── ingress.yaml             # Traefik + cert-manager TLS
    └── secret.yaml              # renders secrets.yaml content into a K8s Secret
```

The Kubernetes Secret mounts at `/etc/dl/secrets.yaml`. The binary reads it via `-secrets /etc/dl/secrets.yaml` (set in the Dockerfile `ENTRYPOINT`). The secret has `helm.sh/resource-policy: keep` so it survives `helm uninstall`.

```bash
make helm-install    # deploy without rebuilding the image
make helm-uninstall  # remove release (PVC and secret are kept)
```

---

## Tests

Integration tests in `src/integration_test.go`. A fake in-memory WebDAV server (`golang.org/x/net/webdav`) is started per test run — no real credentials, no network, no cleanup. All 13 tests run in under a second.

```bash
make test
# ok  github.com/greko/dl/src  0.5s
```
