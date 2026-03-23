# dl API Reference

Base URL: `https://dl.alexgr.space`

All protected endpoints use `Authorization: Bearer <token>`. Auth endpoints accept a raw API key or master key; all other protected endpoints require a JWT obtained from `/api/v1/auth/token`.

---

## Authentication

### Exchange key for JWT

```
POST /api/v1/auth/token
Authorization: Bearer <api_key>
```

Accepts any valid API key stored in BoltDB, **or** the master key from `.secrets.yaml`. Returns a signed JWT valid for 1 hour.

**Response `200 OK`:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

**Errors:**
- `401` — missing or invalid key

---

### Create API key

```
POST /api/v1/auth/keys
Authorization: Bearer <master_key>
Content-Type: application/json
```

**Body:**
```json
{
  "description": "CI upload key for offloadmq-agent",
  "scopes": ["release-write:offloadmq-agent"],
  "root_dir": ""
}
```

**Scopes:**

| Scope | Effect |
|---|---|
| `read` | Read via WebDAV proxy (restricted to `root_dir` if set) |
| `write` | Write via WebDAV proxy (restricted to `root_dir` if set) |
| `release-create` | Create new release buckets |
| `release-write:{bucket}` | Upload files to a specific release bucket |

**Response `200 OK`:**
```json
{
  "key": "dlk_abc123...",
  "id": "dlk_abc123"
}
```

The `key` is returned only at creation time — it cannot be retrieved later. Store it securely.

**Errors:**
- `400` — malformed body
- `403` — not the master key

---

### List API keys

```
GET /api/v1/auth/keys
Authorization: Bearer <master_key>
```

**Response `200 OK`:**
```json
[
  {
    "id": "dlk_abc123",
    "description": "CI upload key",
    "scopes": ["release-write:offloadmq-agent"],
    "root_dir": "",
    "created_at": "2026-03-23T20:00:00Z"
  }
]
```

**Errors:**
- `403` — not the master key

---

### Delete API key

```
DELETE /api/v1/auth/keys/{raw_api_key}
Authorization: Bearer <master_key>
```

**Response `204 No Content`**

**Errors:**
- `400` — key param missing
- `403` — not the master key

---

## WebDAV Proxy

```
{ANY METHOD} /api/v1/wd/{path}
Authorization: Bearer <jwt>
```

Full WebDAV proxy to the upstream storage server (Hetzner Storage Box). All standard WebDAV methods are forwarded: `GET`, `PUT`, `DELETE`, `PROPFIND`, `MKCOL`, `COPY`, `MOVE`, `HEAD`, `OPTIONS`, etc.

**Scope rules:**

| Method | Required scope |
|---|---|
| `GET`, `HEAD`, `PROPFIND`, `OPTIONS` | `read` or `write` |
| All other methods | `write` |

If the JWT's `root_dir` is set, all paths must be prefixed by it. Requests outside the root return `403`.

**Example — list directory:**
```
PROPFIND /api/v1/wd/backups/
Authorization: Bearer eyJ...
Depth: 1
```

**Example — upload file:**
```
PUT /api/v1/wd/backups/archive.tar.gz
Authorization: Bearer eyJ...
Content-Type: application/octet-stream

<binary content>
```

**Errors:**
- `401` — missing or invalid JWT
- `403` — insufficient scope or path outside root

---

## Release Buckets

### Create release bucket

```
POST /api/v1/release/create
Authorization: Bearer <jwt with release-create scope>
Content-Type: application/json
```

**Body:**
```json
{
  "bucket": "offloadmq-agent"
}
```

Creates the directory `/rs/offloadmq-agent/` on the upstream WebDAV server. Bucket names must not contain `/`, `\`, or `..`.

**Response `201 Created`:**
```json
{
  "bucket": "offloadmq-agent"
}
```

**Errors:**
- `400` — missing or invalid bucket name
- `403` — missing `release-create` scope
- `502` — upstream WebDAV error

---

### Upload release file

```
PUT /api/v1/release/{bucket}/{os_arch}/{file...}
Authorization: Bearer <jwt with release-write:{bucket} or write scope>
Content-Type: application/octet-stream
```

Streams the request body to the upstream WebDAV server at `/rs/{bucket}/{os_arch}/{file...}`. Intermediate directories are created automatically.

**Example:**
```
PUT /api/v1/release/offloadmq-agent/darwin-arm64/agent.dmg
Authorization: Bearer eyJ...
Content-Type: application/octet-stream
Content-Length: 42000000

<binary content>
```

Stored at upstream: `/rs/offloadmq-agent/darwin-arm64/agent.dmg`

Then publicly downloadable as:
- `GET /rs/offloadmq-agent/darwin-arm64/agent.dmg`
- `GET /d/rs/offloadmq-agent/darwin-arm64/agent.dmg`

**Response `201 Created`**

**Errors:**
- `400` — missing path components
- `401` — missing JWT
- `403` — missing `release-write:{bucket}` or `write` scope
- `502` — upstream WebDAV error

---

## Public Downloads

No authentication required.

### Direct download

```
GET /d/{path...}
```

Streams any file from the WebDAV upstream at `/{path}`. Intended for short, shareable URLs.

**Example:**
```
GET /d/rs/offloadmq-agent/darwin-arm64/agent.dmg
```

Supports `Range` requests for resumable downloads.

**Errors:**
- `400` — path contains `..`
- `404` — file not found on upstream
- `502` — upstream WebDAV error

---

### Release file download

```
GET /rs/{path...}
```

Streams a file from the WebDAV upstream at `/rs/{path}`. Equivalent to `/d/rs/{path}` but with a cleaner URL.

**Example:**
```
GET /rs/offloadmq-agent/darwin-arm64/agent.dmg
```

Supports `Range` requests for resumable downloads.

**Errors:**
- `400` — path contains `..`
- `404` — file not found on upstream
- `502` — upstream WebDAV error

---

## Typical Workflows

### Ship a release from CI

```bash
# 1. Get a JWT for the CI key
TOKEN=$(curl -s -X POST https://dl.alexgr.space/api/v1/auth/token \
  -H "Authorization: Bearer $CI_API_KEY" | jq -r .token)

# 2. Upload the artifact
curl -X PUT https://dl.alexgr.space/api/v1/release/offloadmq-agent/darwin-arm64/agent.dmg \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/octet-stream" \
  --data-binary @agent.dmg

# 3. Share the public link — no token needed
https://dl.alexgr.space/rs/offloadmq-agent/darwin-arm64/agent.dmg
```

### Create a new API key (admin)

```bash
# Use the master key to create a scoped key
curl -s -X POST https://dl.alexgr.space/api/v1/auth/keys \
  -H "Authorization: Bearer $MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{"description":"CI – offloadmq","scopes":["release-write:offloadmq-agent"]}'
```

### Browse files via WebDAV

```bash
# Get a JWT first
TOKEN=$(curl -s -X POST https://dl.alexgr.space/api/v1/auth/token \
  -H "Authorization: Bearer $API_KEY" | jq -r .token)

# Mount with any WebDAV client, or use cadaver/curl
curl -X PROPFIND https://dl.alexgr.space/api/v1/wd/ \
  -H "Authorization: Bearer $TOKEN" \
  -H "Depth: 1"
```
