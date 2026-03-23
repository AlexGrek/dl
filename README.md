This is a starting point for a file download/upload service, that works as a WebDAV proxy with simple preact web UI.

## Deployment

- helm, k3s, should go to https://dl.alexgr.space
- .secrets.yaml with actual connection info, git-untracked (secret)
- stateful set with persistent BoltDB volume
- env-configurable everything, but secrets are mounted as YAML file and read as file

## Build

- amd64-only docker buildx
- docker hub: `grekodocker/dl`

## Frontend

- preact
- no UI frameworks
- pure custom CSS classes
- console-like dark theme UI
- monospace fonts
- fast, no animations
- a lot of custom reusable components
- simplicity and maximum preact performance
- id, class, data-* on all interactive elements to make it testable
- mobile-first, flexible

It should only list files, basically, and allow uploading to specific routes

## Backend

Simple-as-fuck max memory efficiency golang framework.

Hosts react frontend, embedded into the same docker image, at / (with react router support)

Hosts api (set up proxy api in vite dev server) at `api/v1`

Serves files and dirs as simple HTTP downloads directly at `/d/` (short for "download" to make URLs as short as possible)

Serve files as full WEbDAV proxy at `api/v1/wd`

Serve special routes for "releases" at `api/v1/release`:
- /create (register a release bucket)
- /{release_bucket}/{os_and_arch}/... release files go there ...
- files are in fact stored in /rs/{release_bucket}/{os_and_arch}/...

Example: offloadmq-agent should be at `/api/v1/release/offloadmq-agent/darwin-arm64/agent.dmg`,
also downloadable as `https://dl.alexgr.space/d/rs/offloadmq-agent/darwin-arm64/agent.dmg` without auth.

By default, /rs/ is downloadable with no auth, but API key is needed to be exchanged for JWT token, then this JWT token (short-lived) allows uploading. Each API keys allows uploading to specific directory, or allows releasing specific bucket, or allows creating release buckets. Keys are stored in **BoltDB**.

### API keys

Master key (super long) allows log in to generate more simpler API keys, that have predefined scopes. Those scopes are encoded in JWT tokens that these keys can get.

Stored in embedded persistent **BoltDB**.

## Claude

1. Create roles for preact frontend and go backend
2. Implement super simple backend
3. Create integration tests for backend
4. Create extensive API docs in /docs
5. Create super simple frontend main page
6. Create admin page with API keys generator from main API key
7. Create simple file browser from reusable components, created on other stages
8. Write good README
