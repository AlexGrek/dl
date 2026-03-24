# Release API

`dl` provides a structured release storage system on top of WebDAV. Files are organized as:

```
/rs/{bucket}/{version}/{os_arch}/{filename}
```

For example:
```
/rs/myapp/v1.4.0/linux-amd64/myapp
/rs/myapp/v1.4.0/darwin-arm64/myapp
/rs/myapp/v1.4.0/windows-amd64/myapp.exe
```

Public downloads require no authentication. The landing page at `/r/{bucket}` autodetects the visitor's OS/arch and offers the right download button.

---

## Concepts

| Term | Description |
|---|---|
| **bucket** | Product identifier, e.g. `myapp`. No `/`, `\`, or `..`. Dots are allowed (`my.app`). |
| **version** | Arbitrary version string, e.g. `v1.4.0`. No `/`, `\`, or `..`. |
| **os_arch** | Target platform, e.g. `linux-amd64`, `darwin-arm64`, `windows-amd64`. |
| **latest** | Pseudo-version that 302-redirects to the most recently created version directory. |

---

## Authentication

All write endpoints require a JWT. Exchange an API key for a JWT first:

```bash
TOKEN=$(curl -sS -X POST https://dl.alexgr.space/api/v1/auth/token \
  -H "Authorization: Bearer $DL_API_KEY" | jq -r .token)
```

The JWT is valid for 1 hour.

**Scopes required for release operations:**

| Scope | Grants |
|---|---|
| `release-create` | Create new buckets |
| `release-write:{bucket}` | Upload to a specific bucket |
| `release-write` | Upload to any bucket |

---

## Endpoints

### Create bucket

One-time setup per product. Safe to call again if the bucket already exists.

```
POST /api/v1/release/create
Authorization: Bearer <jwt>
Content-Type: application/json
```

**Body:**
```json
{ "bucket": "myapp" }
```

**Response `201 Created`:**
```json
{ "bucket": "myapp" }
```

**Errors:** `400` invalid name · `403` missing `release-create` scope · `502` upstream error

---

### Upload file — multipart (recommended for CI)

```
POST /api/v1/release/{bucket}/upload
Authorization: Bearer <jwt>
Content-Type: multipart/form-data
```

**Form fields:**

| Field | Type | Description |
|---|---|---|
| `version` | string | e.g. `v1.4.0` |
| `os_arch` | string | e.g. `linux-amd64` |
| `file` | binary | the artifact |

Intermediate version and os_arch directories are created automatically.

**Response `201 Created`:**
```json
{
  "bucket": "myapp",
  "version": "v1.4.0",
  "os_arch": "linux-amd64",
  "file": "myapp"
}
```

**Errors:** `400` missing/invalid fields · `403` missing scope · `502` upstream error

---

### Upload file — streaming PUT

```
PUT /api/v1/release/{bucket}/{version}/{os_arch}/{filename}
Authorization: Bearer <jwt>
Content-Type: application/octet-stream
```

Streams the body directly to upstream storage. Use this for large files or when you want to set `Content-Length` explicitly. Directories are created automatically.

**Response `201 Created`**

**Errors:** `400` missing/invalid path components · `403` missing scope · `502` upstream error

---

### Public release info

No auth required. Returns the latest version and all available targets with their file lists.

```
GET /api/v1/pub/release/{bucket}
```

**Response `200 OK`:**
```json
{
  "bucket": "myapp",
  "latest": "v1.4.0",
  "targets": {
    "linux-amd64":   [{ "name": "myapp",     "size": 8388608 }],
    "darwin-arm64":  [{ "name": "myapp",     "size": 8200000 }],
    "windows-amd64": [{ "name": "myapp.exe", "size": 9000000 }]
  }
}
```

`latest` is the version directory with the most recent last-modified timestamp on the upstream.

**Errors:** `404` bucket not found or has no versions · `502` upstream error

---

## Auto-update API

These endpoints are designed for in-app update checks. No authentication required on any of them.

### Get latest version

```
GET /api/v1/pub/release/{bucket}/latest
```

Returns the latest version string, optional release notes, and the list of available OS/arch targets.
The response is intentionally minimal — compare `version` to the running binary's own version string
to decide whether an update is available.

**Response `200 OK`:**
```json
{
  "bucket": "myapp",
  "version": "v1.5.0",
  "date": "2026-03-24",
  "notes": "Fixed crash on startup; improved memory usage.",
  "targets": ["darwin-arm64", "darwin-amd64", "linux-amd64", "windows-amd64"]
}
```

Fields `date` and `notes` are omitted when no `release.yaml` exists for the version.
`targets` is always present (empty array if none).

**Errors:** `400` invalid bucket · `404` bucket not found or empty · `502` upstream error

---

### List all versions

```
GET /api/v1/pub/release/{bucket}/versions
```

Returns every available version for the bucket, sorted newest-first by the last-modified
timestamp of each version directory on the upstream.

**Response `200 OK`:**
```json
[
  { "version": "v1.5.0", "date": "2026-03-24", "notes": "Fixed crash on startup." },
  { "version": "v1.4.0", "date": "2026-03-10", "notes": "Initial public release." }
]
```

`date` and `notes` are omitted for versions that have no `release.yaml`.

This endpoint is cached for 2 minutes.

**Errors:** `400` invalid bucket · `404` bucket not found · `502` upstream error

---

### List targets for a version

```
GET /api/v1/pub/release/{bucket}/versions/{version}/targets
```

Returns the sorted list of OS/arch target strings available for the given version.
Pass `latest` as `{version}` to resolve the most recently created version automatically.

**Response `200 OK`:**
```json
["darwin-amd64", "darwin-arm64", "linux-amd64", "windows-amd64"]
```

The target strings are directory names under `/rs/{bucket}/{version}/`, conventionally
formatted as `{os}-{arch}`. The actual binary is downloaded at:
```
GET /rs/{bucket}/{version}/{target}/{filename}
```

**Errors:** `400` invalid bucket or version · `404` version not found · `502` upstream error

---

### Auto-update flow example

```bash
CURRENT_VERSION="v1.4.0"
BUCKET="myapp"
BASE="https://dl.alexgr.space"

# 1. Check for an update.
LATEST=$(curl -sS "${BASE}/api/v1/pub/release/${BUCKET}/latest")
LATEST_VERSION=$(echo "$LATEST" | jq -r .version)

if [[ "$LATEST_VERSION" == "$CURRENT_VERSION" ]]; then
  echo "Already up to date."
  exit 0
fi

echo "Update available: $LATEST_VERSION"
echo "$(echo "$LATEST" | jq -r '.notes // ""')"

# 2. Confirm the target for this machine is available.
TARGET="linux-amd64"
TARGETS=$(curl -sS "${BASE}/api/v1/pub/release/${BUCKET}/versions/${LATEST_VERSION}/targets")
if ! echo "$TARGETS" | jq -e --arg t "$TARGET" 'index($t) != null' > /dev/null; then
  echo "No build available for $TARGET" >&2
  exit 1
fi

# 3. Download the new binary.
curl -L -o myapp.new \
  "${BASE}/rs/${BUCKET}/${LATEST_VERSION}/${TARGET}/myapp"
chmod +x myapp.new
mv myapp.new /usr/local/bin/myapp

echo "Updated to $LATEST_VERSION."
```

---

## Markdown Documentation Endpoints

Buckets support three optional markdown files that are displayed in the UI and cached in BoltDB.

| File | Location | Purpose |
|---|---|---|
| `README.md` | `/rs/{bucket}/README.md` | Product overview / main documentation |
| `RELEASE.md` | `/rs/{bucket}/RELEASE.md` | Product-level changelog or release notes |
| `release_notes.md` | `/rs/{bucket}/{version}/release_notes.md` | Per-version detailed release notes |

These files are served through dedicated endpoints and also embedded in the `GET /api/v1/pub/products/{bucket}` response.

### Read product markdown doc

```
GET /api/v1/pub/release/{bucket}/docs/{doctype}
```

`{doctype}` is `readme` (→ `README.md`) or `release` (→ `RELEASE.md`).

**Response `200 OK`:**
```json
{ "content": "# myapp\n\nThis is my project..." }
```

**Errors:** `400` invalid doctype · `404` file does not exist · `502` upstream error

---

### Read version release notes

```
GET /api/v1/pub/release/{bucket}/versions/{version}/docs/release-notes
```

Pass `latest` as `{version}` to resolve the most recently created version automatically.

**Response `200 OK`:**
```json
{ "content": "## v1.5.0\n\n- Fixed crash\n- New feature..." }
```

**Errors:** `400` invalid bucket/version · `404` file does not exist · `502` upstream error

---

### Write product markdown doc

```
PUT /api/v1/release/{bucket}/docs/{doctype}
Authorization: Bearer <jwt>
Content-Type: application/json
```

Requires `release-write:{bucket}`, `release-write`, or `write` scope.

**Body:**
```json
{ "content": "# myapp\n\nMarkdown content here." }
```

**Response `204 No Content`** on success.

**Errors:** `400` invalid doctype or body · `403` missing scope · `502` upstream error

---

### Write version release notes

```
PUT /api/v1/release/{bucket}/versions/{version}/docs/release-notes
Authorization: Bearer <jwt>
Content-Type: application/json
```

Requires `release-write:{bucket}`, `release-write`, or `write` scope.

**Body:**
```json
{ "content": "## Changes\n\n- Bug fix #42\n- Performance improvement" }
```

**Response `204 No Content`** on success.

**Errors:** `400` invalid version or body · `403` missing scope · `502` upstream error

---

### Caching behaviour

All three doc files are cached in BoltDB with a 5-minute TTL (same as other product metadata). Writing via the PUT endpoints immediately evicts the relevant cache entries, so the next read reflects the new content. The full `GET /api/v1/pub/products/{bucket}` response also includes `readme` and `release_doc` fields (populated from cache), and each version object includes `release_notes`.

---

### Public download

No auth required.

```
GET /rs/{bucket}/{version}/{os_arch}/{filename}
GET /rs/{bucket}/latest/{os_arch}/{filename}
```

`latest` issues a `302` redirect to the actual version URL. `Range` requests are supported for resumable downloads.

**Examples:**
```
GET /rs/myapp/v1.4.0/linux-amd64/myapp
GET /rs/myapp/latest/darwin-arm64/myapp
```

---

### Release landing page

```
GET /r/{bucket}
```

Browser page. Autodetects the visitor's OS and architecture, highlights the matching target, and shows a download button. Displays a table of all targets for manual selection.

---

## Example: release script

Save as `scripts/release.sh`. Requires `curl` and `jq`.

```bash
#!/usr/bin/env bash
# Usage: ./release.sh <version> <os_arch> <file> [<file> ...]
#
# Environment variables:
#   DL_API_KEY   API key with release-write:{bucket} and release-create scopes
#   DL_BUCKET    Release bucket name (default: derived from repo name)
#   DL_BASE_URL  Server base URL (default: https://dl.alexgr.space)
#
# Examples:
#   DL_API_KEY=dlk_... ./release.sh v1.4.0 linux-amd64 ./dist/myapp
#   DL_API_KEY=dlk_... ./release.sh v1.4.0 windows-amd64 ./dist/myapp.exe ./dist/myapp.exe.sha256
#
set -euo pipefail

VERSION="${1:?Usage: $0 <version> <os_arch> <file> [<file> ...]}"
OS_ARCH="${2:?missing os_arch}"
shift 2
FILES=("$@")

BASE_URL="${DL_BASE_URL:-https://dl.alexgr.space}"
BUCKET="${DL_BUCKET:-$(basename "$(git rev-parse --show-toplevel 2>/dev/null || echo myapp)")}"

if [[ -z "${DL_API_KEY:-}" ]]; then
  echo "error: DL_API_KEY is not set" >&2
  exit 1
fi

if [[ ${#FILES[@]} -eq 0 ]]; then
  echo "error: at least one file required" >&2
  exit 1
fi

# ── Auth ──────────────────────────────────────────────────────────────────────

echo "→ Authenticating..."
TOKEN=$(curl -sS --fail \
  -X POST "${BASE_URL}/api/v1/auth/token" \
  -H "Authorization: Bearer ${DL_API_KEY}" | jq -r .token)

if [[ -z "$TOKEN" || "$TOKEN" == "null" ]]; then
  echo "error: failed to obtain JWT — check DL_API_KEY" >&2
  exit 1
fi

# ── Ensure bucket exists ──────────────────────────────────────────────────────

echo "→ Ensuring bucket '${BUCKET}' exists..."
curl -sS --fail \
  -X POST "${BASE_URL}/api/v1/release/create" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"bucket\":\"${BUCKET}\"}" \
  -o /dev/null

# ── Upload files ──────────────────────────────────────────────────────────────

for FILE in "${FILES[@]}"; do
  if [[ ! -f "$FILE" ]]; then
    echo "error: file not found: $FILE" >&2
    exit 1
  fi
  FILENAME=$(basename "$FILE")
  echo "→ Uploading ${FILENAME} (${OS_ARCH}) as ${VERSION}..."

  HTTP_STATUS=$(curl -sS --write-out "%{http_code}" -o /tmp/dl_upload_resp \
    -X POST "${BASE_URL}/api/v1/release/${BUCKET}/upload" \
    -H "Authorization: Bearer ${TOKEN}" \
    -F "version=${VERSION}" \
    -F "os_arch=${OS_ARCH}" \
    -F "file=@${FILE};filename=${FILENAME}")

  if [[ "$HTTP_STATUS" != "201" ]]; then
    echo "error: upload failed (HTTP ${HTTP_STATUS})" >&2
    cat /tmp/dl_upload_resp >&2
    exit 1
  fi

  echo "  ✓ ${BASE_URL}/rs/${BUCKET}/${VERSION}/${OS_ARCH}/${FILENAME}"
done

# ── Summary ───────────────────────────────────────────────────────────────────

echo ""
echo "Released ${BUCKET} ${VERSION} for ${OS_ARCH}"
echo "  Latest:  ${BASE_URL}/rs/${BUCKET}/latest/${OS_ARCH}/${FILENAME}"
echo "  Landing: ${BASE_URL}/r/${BUCKET}"
```

### Using from GitHub Actions

```yaml
- name: Upload release
  env:
    DL_API_KEY: ${{ secrets.DL_API_KEY }}
    DL_BUCKET: myapp
  run: |
    chmod +x scripts/release.sh
    ./scripts/release.sh "${{ github.ref_name }}" linux-amd64   dist/myapp-linux-amd64
    ./scripts/release.sh "${{ github.ref_name }}" darwin-arm64  dist/myapp-darwin-arm64
    ./scripts/release.sh "${{ github.ref_name }}" windows-amd64 dist/myapp-windows-amd64.exe
```

### Uploading multiple targets in parallel

```bash
VERSION="v1.4.0"
pids=()

for target in linux-amd64 darwin-arm64 windows-amd64; do
  ext=""
  [[ "$target" == windows* ]] && ext=".exe"
  DL_BUCKET=myapp ./scripts/release.sh "$VERSION" "$target" "dist/myapp-${target}${ext}" &
  pids+=($!)
done

for pid in "${pids[@]}"; do wait "$pid"; done
echo "All targets uploaded."
```

---

## Example: create a CI API key (one-time admin setup)

```bash
curl -sS -X POST https://dl.alexgr.space/api/v1/auth/keys \
  -H "Authorization: Bearer $MASTER_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "description": "CI – myapp releases",
    "scopes": ["release-create", "release-write:myapp"]
  }' | jq .
```

Store the returned `key` as a secret (`DL_API_KEY`) in your CI environment. It cannot be retrieved again.
