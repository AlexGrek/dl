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

> **Note:** Keys with `webdav-read` or `webdav-write` scopes cannot be exchanged for JWTs. Use them directly with Basic Auth at `/wd/`.

**Response `200 OK`:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

**Errors:**
- `401` — missing or invalid key
- `403` — key has `webdav-read`/`webdav-write` scope (not exchangeable for JWT)

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
  "scopes": ["release-create", "release-write:myapp"],
  "root_dir": ""
}
```

`root_dir` is optional. When set (e.g. `"/myteam"`), path-scoped access is restricted to that subtree.

**Scopes:**

| Scope | Used with | Effect |
|---|---|---|
| `read` | JWT / `/api/v1/wd/` | Read-only WebDAV proxy access (all paths) |
| `read:/path` | JWT / `/api/v1/wd/` | Read-only WebDAV proxy access restricted to `/path` and below |
| `write` | JWT / `/api/v1/wd/` | Read+write WebDAV proxy access (all paths) |
| `write:/path` | JWT / `/api/v1/wd/` | Read+write WebDAV proxy access restricted to `/path` and below |
| `webdav-read` | Basic Auth / `/wd/` | Read-only WebDAV via HTTP Basic Auth — **mutually exclusive with `read`/`write`** |
| `webdav-write` | Basic Auth / `/wd/` | Read+write WebDAV via HTTP Basic Auth — **mutually exclusive with `read`/`write`** |
| `release-create` | JWT | Create new release buckets |
| `release-write` | JWT | Upload to any release bucket |
| `release-write:{bucket}` | JWT | Upload to a specific release bucket |

Wildcard scopes are supported: `read:/shared-*` grants access to all paths whose name starts with `/shared-`.

Multiple scopes can be combined, e.g. `["read:/docs", "release-write:myapp"]`.

> **One key, one access mode.** `webdav-read`/`webdav-write` and `read`/`write` are mutually exclusive — a single key cannot be used for both Basic Auth WebDAV (`/wd/`) and JWT-based access (`/api/v1/wd/`). Create separate keys for each use case.

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

## WebDAV Proxy (JWT)

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

Path-scoped tokens (`read:/path`, `write:/path`) restrict access to the given prefix and its descendants. Requests to files outside the allowed prefix return `403`.

**Filtered PROPFIND for ancestor directories:** When a token's `root_dir` is `/a` and the client does `PROPFIND /api/v1/wd/`, the server returns a filtered `207 Multi-Status` listing that shows only `/a` instead of `403`. This allows navigating to an allowed directory without needing access to its parent.

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

## WebDAV Direct Access (Basic Auth)

```
{ANY METHOD} /wd/{path}
Authorization: Basic <base64(dl:<api_key>)>
```

Alternative WebDAV endpoint that authenticates with HTTP Basic Auth instead of a JWT. Username must be `dl`; password is the raw API key. The key must have `webdav-read` or `webdav-write` scope — regular `read`/`write` keys are rejected here.

**Scope rules:**

| Method | Required scope |
|---|---|
| `GET`, `HEAD`, `PROPFIND`, `OPTIONS` | `webdav-read` or `webdav-write` |
| All other methods | `webdav-write` |

`root_dir` on the API key restricts access to that subtree. Filtered ancestor PROPFIND applies here too.

**Example — mount in macOS Finder or a WebDAV client:**
```
URL:      https://dl.alexgr.space/wd/
Username: dl
Password: dlk_<your api key>
```

**Example — curl:**
```
curl -u dl:dlk_abc123... -X PROPFIND https://dl.alexgr.space/wd/ -H "Depth: 1"
```

**Errors:**
- `401` — missing credentials or wrong username
- `403` — key does not have `webdav-read`/`webdav-write` scope

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
| `GET /r/{bucket}` | none | Release landing page (SPA) |

---

## Public Product Catalog

No authentication required. Responses are cached for up to 2 minutes.

### List all products

```
GET /api/v1/pub/products
```

Returns a summary of every release bucket found on the upstream storage.

**Response `200 OK`:**
```json
[
  {
    "bucket": "myapp",
    "name": "My App",
    "tagline": "A great app",
    "latest": "v1.4.0",
    "targets": ["darwin-arm64", "linux-amd64"],
    "tags": ["cli", "tool"],
    "license": "MIT"
  }
]
```

**Errors:**
- `502` — upstream WebDAV error

---

### Get product detail

```
GET /api/v1/pub/products/{bucket}
```

Returns full product information including all versions, targets, and release metadata.

**Response `200 OK`:**
```json
{
  "bucket": "myapp",
  "name": "My App",
  "tagline": "A great app",
  "description": "Full description...",
  "homepage": "https://example.com",
  "license": "MIT",
  "tags": ["cli"],
  "readme": "# My App\n...",
  "release_doc": "# Releases\n...",
  "versions": [
    {
      "version": "v1.4.0",
      "date": "2026-05-01",
      "notes": "Bug fixes",
      "release_notes": "## v1.4.0\n...",
      "targets": {
        "linux-amd64": [{"name": "myapp", "url": "/rs/myapp/v1.4.0/linux-amd64/myapp"}]
      }
    }
  ]
}
```

**Errors:**
- `400` — invalid bucket name
- `404` — bucket not found on upstream

---

## File Operations

### Delete file

```
DELETE /api/v1/files/{path...}
Authorization: Bearer <jwt>
```

Deletes a file or directory from the upstream storage. Requires the `write` scope (or `write:/prefix` covering the target path). Does **not** go through the WebDAV proxy — this is a plain HTTP DELETE directly to upstream.

**Response `204 No Content`** on success.

**Errors:**
- `400` — path contains `..`
- `401` — missing or invalid JWT
- `403` — insufficient write scope
- `404` — file not found on upstream
- `502` — upstream WebDAV error

**Example:**
```bash
curl -sS --fail -X DELETE \
  https://dl.alexgr.space/api/v1/files/backups/old-archive.tar.gz \
  -H "Authorization: Bearer $TOKEN"
```

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

### Browse files via WebDAV (JWT)

```bash
TOKEN=$(curl -sS -X POST https://dl.alexgr.space/api/v1/auth/token \
  -H "Authorization: Bearer $API_KEY" | jq -r .token)

curl -X PROPFIND https://dl.alexgr.space/api/v1/wd/ \
  -H "Authorization: Bearer $TOKEN" \
  -H "Depth: 1"
```

### Browse files via WebDAV (Basic Auth)

```bash
# Create a webdav-read key first, then:
curl -u dl:$WEBDAV_KEY -X PROPFIND https://dl.alexgr.space/wd/ -H "Depth: 1"
```
