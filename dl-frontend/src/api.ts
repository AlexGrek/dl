export interface DavEntry {
  href: string;   // upstream path, e.g. "/dir1/file.txt"
  name: string;
  isDir: boolean;
  size: number;
  modified: string;
}

export interface APIKey {
  id: string;
  description: string;
  scopes: string[];
  root_dir?: string;
  created_at: string;
  last_login?: string;
}

// ── Auth ──

export async function getToken(apiKey: string): Promise<string> {
  const res = await fetch('/api/v1/auth/token', {
    method: 'POST',
    headers: { Authorization: `Bearer ${apiKey}` },
  });
  if (!res.ok) throw new Error('Invalid API key');
  const data = (await res.json()) as { token: string };
  return data.token;
}

// ── WebDAV ──

export async function propfind(apiPath: string, jwt: string): Promise<DavEntry[]> {
  const res = await fetch(apiPath, {
    method: 'PROPFIND',
    headers: {
      Authorization: `Bearer ${jwt}`,
      Depth: '1',
    },
  });
  if (!res.ok) throw new Error(`PROPFIND ${res.status}`);
  const text = await res.text();
  return parsePropfind(text, apiPath);
}

function parsePropfind(xml: string, requestApiPath: string): DavEntry[] {
  const parser = new DOMParser();
  const doc = parser.parseFromString(xml, 'application/xml');
  const ns = 'DAV:';
  const responses = Array.from(doc.getElementsByTagNameNS(ns, 'response'));

  // The upstream path that corresponds to the request
  // e.g., requestApiPath = "/api/v1/wd/foo/" → upstreamBase = "/foo/"
  const upstreamBase = requestApiPath.replace(/^\/api\/v1\/wd/, '') || '/';
  const normalizedBase = upstreamBase.endsWith('/') ? upstreamBase : upstreamBase + '/';

  return responses
    .map((r): DavEntry | null => {
      const hrefRaw = r.getElementsByTagNameNS(ns, 'href')[0]?.textContent ?? '';
      const resourcetype = r.getElementsByTagNameNS(ns, 'resourcetype')[0];
      const isDir = !!resourcetype?.getElementsByTagNameNS(ns, 'collection')[0];
      const size = parseInt(
        r.getElementsByTagNameNS(ns, 'getcontentlength')[0]?.textContent ?? '0',
        10,
      );
      const modified = r.getElementsByTagNameNS(ns, 'getlastmodified')[0]?.textContent ?? '';

      // Normalize href to just its path component
      let href: string;
      try {
        href = new URL(hrefRaw).pathname;
      } catch {
        href = hrefRaw;
      }
      href = decodeURIComponent(href);

      const name = href.replace(/\/$/, '').split('/').pop() ?? '';

      return { href, name, isDir, size, modified };
    })
    .filter((e): e is DavEntry => {
      if (!e) return false;
      // Filter out the current directory itself
      const ep = e.href.endsWith('/') ? e.href : e.href + '/';
      return ep !== normalizedBase && e.name !== '';
    })
    .sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
}

export async function uploadFile(
  apiPath: string,
  file: File,
  jwt: string,
  onProgress?: (pct: number) => void,
): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('PUT', apiPath);
    xhr.setRequestHeader('Authorization', `Bearer ${jwt}`);
    xhr.setRequestHeader('Content-Type', file.type || 'application/octet-stream');
    if (onProgress) {
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100));
      };
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve();
      else reject(new Error(`Upload failed: ${xhr.status}`));
    };
    xhr.onerror = () => reject(new Error('Upload error'));
    xhr.send(file);
  });
}

export async function deleteEntry(apiPath: string, jwt: string): Promise<void> {
  const res = await fetch(apiPath, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${jwt}` },
  });
  if (!res.ok) throw new Error(`Delete failed: ${res.status}`);
}

export async function mkcol(apiPath: string, jwt: string): Promise<void> {
  const res = await fetch(apiPath, {
    method: 'MKCOL',
    headers: { Authorization: `Bearer ${jwt}` },
  });
  if (!res.ok) throw new Error(`MKCOL failed: ${res.status}`);
}

// ── API Key management (master key) ──

export async function listKeys(masterKey: string): Promise<APIKey[]> {
  const res = await fetch('/api/v1/auth/keys', {
    headers: { Authorization: `Bearer ${masterKey}` },
  });
  if (!res.ok) throw new Error(`Failed to list keys: ${res.status}`);
  const data = (await res.json()) as APIKey[];
  return Array.isArray(data) ? data : [];
}

export interface CreateKeyResponse {
  key: string;
  id: string;
}

export async function createKey(
  masterKey: string,
  description: string,
  scopes: string[],
  rootDir?: string,
): Promise<CreateKeyResponse> {
  const res = await fetch('/api/v1/auth/keys', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${masterKey}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ description, scopes, root_dir: rootDir || undefined }),
  });
  if (!res.ok) throw new Error(`Failed to create key: ${res.status}`);
  return (await res.json()) as CreateKeyResponse;
}

export async function deleteKey(masterKey: string, rawKey: string): Promise<void> {
  const res = await fetch(`/api/v1/auth/keys/${encodeURIComponent(rawKey)}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${masterKey}` },
  });
  if (!res.ok) throw new Error(`Failed to delete key: ${res.status}`);
}

// ── Public release info (no auth) ──

export interface ReleaseFile {
  name: string;
  size: number;
}

export interface ReleaseInfo {
  bucket: string;
  latest: string;
  targets: Record<string, ReleaseFile[]>;
}

export async function getReleaseInfo(bucket: string): Promise<ReleaseInfo> {
  const res = await fetch(`/api/v1/pub/release/${encodeURIComponent(bucket)}`);
  if (res.status === 404) throw new Error('not found');
  if (!res.ok) throw new Error(`Failed to load release info: ${res.status}`);
  return (await res.json()) as ReleaseInfo;
}

/** Detect the current platform as a likely os/arch string, e.g. "linux-amd64". */
export function detectPlatform(): string {
  const ua = navigator.userAgent.toLowerCase();
  const isArm = ua.includes('arm64') || ua.includes('aarch64');
  const arch = isArm ? 'arm64' : 'amd64';
  if (ua.includes('win')) return `windows-${arch}`;
  if (ua.includes('mac') || ua.includes('darwin')) return `darwin-${arch}`;
  if (ua.includes('linux') || ua.includes('android')) return `linux-${arch}`;
  return '';
}

// ── Product catalog (public) ──

export interface ProductSummary {
  bucket: string;
  name: string;
  tagline: string;
  latest: string;
  targets: string[];
  tags: string[];
  license: string;
}

export interface VersionDetail {
  version: string;
  date: string;
  notes: string;
  release_notes: string;
  targets: Record<string, ReleaseFile[]>;
}

export interface ProductDetail {
  bucket: string;
  name: string;
  tagline: string;
  description: string;
  homepage: string;
  license: string;
  tags: string[];
  readme: string;
  release_doc: string;
  versions: VersionDetail[];
}

export async function listProducts(): Promise<ProductSummary[]> {
  const res = await fetch('/api/v1/pub/products');
  if (!res.ok) throw new Error(`Failed to list products: ${res.status}`);
  return (await res.json()) as ProductSummary[];
}

export async function getProductDetail(bucket: string): Promise<ProductDetail> {
  const res = await fetch(`/api/v1/pub/products/${encodeURIComponent(bucket)}`);
  if (!res.ok) throw new Error(`Failed to load product: ${res.status}`);
  return (await res.json()) as ProductDetail;
}

// ── Markdown docs (public read, JWT write) ──

export async function getDoc(
  bucket: string,
  doctype: 'readme' | 'release',
): Promise<string> {
  const res = await fetch(
    `/api/v1/pub/release/${encodeURIComponent(bucket)}/docs/${doctype}`,
  );
  if (res.status === 404) return '';
  if (!res.ok) throw new Error(`Failed to load doc: ${res.status}`);
  const data = (await res.json()) as { content: string };
  return data.content;
}

export async function getVersionDoc(bucket: string, version: string): Promise<string> {
  const res = await fetch(
    `/api/v1/pub/release/${encodeURIComponent(bucket)}/versions/${encodeURIComponent(version)}/docs/release-notes`,
  );
  if (res.status === 404) return '';
  if (!res.ok) throw new Error(`Failed to load release notes: ${res.status}`);
  const data = (await res.json()) as { content: string };
  return data.content;
}

export async function saveDoc(
  jwt: string,
  bucket: string,
  doctype: 'readme' | 'release',
  content: string,
): Promise<void> {
  const res = await fetch(
    `/api/v1/release/${encodeURIComponent(bucket)}/docs/${doctype}`,
    {
      method: 'PUT',
      headers: { Authorization: `Bearer ${jwt}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ content }),
    },
  );
  if (!res.ok) throw new Error(`Failed to save doc: ${res.status}`);
}

export async function saveVersionDoc(
  jwt: string,
  bucket: string,
  version: string,
  content: string,
): Promise<void> {
  const res = await fetch(
    `/api/v1/release/${encodeURIComponent(bucket)}/versions/${encodeURIComponent(version)}/docs/release-notes`,
    {
      method: 'PUT',
      headers: { Authorization: `Bearer ${jwt}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ content }),
    },
  );
  if (!res.ok) throw new Error(`Failed to save release notes: ${res.status}`);
}

// ── Release management ──

export async function createReleaseBucket(jwt: string, bucket: string): Promise<void> {
  const res = await fetch('/api/v1/release/create', {
    method: 'POST',
    headers: { Authorization: `Bearer ${jwt}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ bucket }),
  });
  if (!res.ok) throw new Error(`Failed to create bucket: ${res.status}`);
}

export async function uploadRelease(
  jwt: string,
  bucket: string,
  version: string,
  osArch: string,
  file: File,
  onProgress?: (pct: number) => void,
): Promise<void> {
  const apiPath = `/api/v1/release/${encodeURIComponent(bucket)}/${encodeURIComponent(version)}/${encodeURIComponent(osArch)}/${encodeURIComponent(file.name)}`;
  await uploadFile(apiPath, file, jwt, onProgress);
}

export async function uploadReleaseMultipart(
  jwt: string,
  bucket: string,
  version: string,
  osArch: string,
  file: File,
  onProgress?: (pct: number) => void,
): Promise<void> {
  const form = new FormData();
  form.append('version', version);
  form.append('os_arch', osArch);
  form.append('file', file, file.name);

  await new Promise<void>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `/api/v1/release/${encodeURIComponent(bucket)}/upload`);
    xhr.setRequestHeader('Authorization', `Bearer ${jwt}`);
    if (onProgress) {
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100));
      };
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve();
      else reject(new Error(`Upload failed: ${xhr.status} ${xhr.responseText.slice(0, 200)}`));
    };
    xhr.onerror = () => reject(new Error('Upload error'));
    xhr.send(form);
  });
}

// ── Semver helpers ──

export function parseSemver(v: string): [number, number, number] | null {
  const m = v.match(/^v?(\d+)\.(\d+)\.(\d+)/);
  if (!m) return null;
  return [parseInt(m[1], 10), parseInt(m[2], 10), parseInt(m[3], 10)];
}

export function bumpSemver(v: string, part: 'major' | 'minor' | 'patch'): string {
  const parsed = parseSemver(v);
  if (!parsed) return v;
  const [major, minor, patch] = parsed;
  const prefix = v.trimStart().startsWith('v') ? 'v' : '';
  if (part === 'major') return `${prefix}${major + 1}.0.0`;
  if (part === 'minor') return `${prefix}${major}.${minor + 1}.0`;
  return `${prefix}${major}.${minor}.${patch + 1}`;
}

/** Decode JWT payload and return scopes array (no signature verification). */
export function jwtScopes(token: string): string[] {
  try {
    const payload = token.split('.')[1];
    const json = atob(payload.replace(/-/g, '+').replace(/_/g, '/'));
    const claims = JSON.parse(json) as { scopes?: string[] };
    return Array.isArray(claims.scopes) ? claims.scopes : [];
  } catch {
    return [];
  }
}

export function hasReleaseScope(token: string): boolean {
  const scopes = jwtScopes(token);
  return scopes.some(
    (s) => s === 'release-create' || s === 'release-write' || s.startsWith('release-write:'),
  );
}

// ── Utilities ──

export function formatSize(bytes: number): string {
  if (!bytes) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let n = bytes;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  return `${n.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

export function formatDate(s: string): string {
  if (!s) return '—';
  const d = new Date(s);
  if (isNaN(d.getTime())) return s;
  return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' });
}

/** Given a DavEntry's href (upstream path), build the /api/v1/wd API path */
export function entryApiPath(entry: DavEntry): string {
  const p = entry.href.startsWith('/') ? entry.href : '/' + entry.href;
  return `/api/v1/wd${p}`;
}

/** Given a DavEntry's href (upstream path), build a public /d/ download URL */
export function entryDownloadUrl(entry: DavEntry): string {
  const p = entry.href.startsWith('/') ? entry.href : '/' + entry.href;
  return `/d${p}`;
}

/** Split an upstream path into breadcrumb segments */
export function pathSegments(upstreamPath: string): { label: string; path: string }[] {
  const parts = upstreamPath.split('/').filter(Boolean);
  const segments: { label: string; path: string }[] = [{ label: 'root', path: '/' }];
  let acc = '';
  for (const p of parts) {
    acc += '/' + p;
    segments.push({ label: p, path: acc + '/' });
  }
  return segments;
}
