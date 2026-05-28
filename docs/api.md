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
  "description": "CI upload key for myapp",
  "scopes": ["release-create", "release-write:myapp"]
}
```

**Scopes:**

| Scope | Effect |
|---|---|
| `read` | Read-only WebDAV proxy access (all paths) |
| `read:/path` | Read-only WebDAV proxy access restricted to `/path` and below |
| `write` | Read+write WebDAV proxy access (all paths) |
| `write:/path` | Read+write WebDAV proxy access restricted to `/path` and below |
| `release-create` | Create new release buckets |
| `release-write` | Upload to any release bucket |
| `release-write:{bucket}` | Upload to a specific release bucket |

Multiple scopes can be combined, e.g. `["read:/docs", "release-write:myapp"]`.

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
    "scopes": ["release-write:myapp"],
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

Path-scoped tokens (`read:/path`, `write:/path`) restrict access to the given prefix and its descendants. Requests outside the allowed paths return `403`.

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

**Example — delete file:**
```
DELETE /api/v1/wd/backups/archive.tar.gz
Authorization: Bearer eyJ...
```

Requires the `write` scope. Returns whatever status the upstream WebDAV server sends (typically `204 No Content` on success).

**Errors:**
- `401` — missing or invalid JWT
- `403` — insufficient scope or path outside root

---

## Release Buckets

Files are stored at `/rs/{bucket}/{version}/{os_arch}/{filename}`. The pseudo-version `latest` redirects to the most recently created version.

See **[docs/release.md](release.md)** for the full release API reference, endpoint details, and an example release script.

**Quick reference:**

| Endpoint | Auth | Description |
|---|---|---|
| `POST /api/v1/release/create` | JWT `release-create` | Create a bucket |
| `POST /api/v1/release/{bucket}/upload` | JWT `release-write:{bucket}` | Multipart upload |
| `PUT /api/v1/release/{bucket}/{version}/{os_arch}/{file...}` | JWT `release-write:{bucket}` | Streaming upload |
| `GET /api/v1/pub/release/{bucket}` | none | Latest version + target list (with files) |
| `GET /api/v1/pub/release/{bucket}/latest` | none | Latest version + metadata for auto-update |
| `GET /api/v1/pub/release/{bucket}/versions` | none | All versions, newest-first |
| `GET /api/v1/pub/release/{bucket}/versions/{version}/targets` | none | OS/arch list for a version |
| `GET /api/v1/pub/release/{bucket}/docs/{doctype}` | none | Product-level markdown doc (`readme` or `release`) |
| `GET /api/v1/pub/release/{bucket}/versions/{version}/docs/release-notes` | none | Per-version release notes markdown |
| `PUT /api/v1/release/{bucket}/docs/{doctype}` | JWT `release-write:{bucket}` | Create or update product markdown doc |
| `PUT /api/v1/release/{bucket}/versions/{version}/docs/release-notes` | JWT `release-write:{bucket}` | Create or update version release notes |
| `GET /rs/{bucket}/{version}/{os_arch}/{file}` | none | Download file |
| `GET /rs/{bucket}/latest/{os_arch}/{file}` | none | Download latest (302 redirect) |
| `GET /r/{bucket}` | none | Release landing page |

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
GET /d/backups/archive.tar.gz
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

**Examples:**
```
GET /rs/myapp/v1.4.0/darwin-arm64/myapp
GET /rs/myapp/latest/linux-amd64/myapp
```

`latest` issues a `302` redirect to the actual version URL. Supports `Range` requests for resumable downloads.

**Errors:**
- `400` — path contains `..`
- `404` — file not found on upstream
- `502` — upstream WebDAV error

---

## Typical Workflows

### Ship a release from CI

See [docs/release.md](release.md) for a full release script. Quick example:

```bash
TOKEN=$(curl -sS -X POST https://dl.alexgr.space/api/v1/auth/token \
  -H "Authorization: Bearer $DL_API_KEY" | jq -r .token)

curl -sS --fail \
  -X POST https://dl.alexgr.space/api/v1/release/myapp/upload \
  -H "Authorization: Bearer $TOKEN" \
  -F "version=v1.4.0" \
  -F "os_arch=linux-amd64" \
  -F "file=@dist/myapp"

# Public link — no token needed
# https://dl.alexgr.space/rs/myapp/v1.4.0/linux-amd64/myapp
# https://dl.alexgr.space/r/myapp   ← landing page
```

### Create a new API key (admin)

```bash
curl -sS -X POST https://dl.alexgr.space/api/v1/auth/keys \
  -H "Authorization: Bearer $MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{"description":"CI – myapp","scopes":["release-create","release-write:myapp"]}'
```

### Browse files via WebDAV

```bash
TOKEN=$(curl -sS -X POST https://dl.alexgr.space/api/v1/auth/token \
  -H "Authorization: Bearer $API_KEY" | jq -r .token)

curl -X PROPFIND https://dl.alexgr.space/api/v1/wd/ \
  -H "Authorization: Bearer $TOKEN" \
  -H "Depth: 1"
```
