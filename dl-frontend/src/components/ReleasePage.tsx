import { useState, useEffect, useRef } from 'preact/hooks';
import { Folder, FolderPlus, Upload, Download, ChevronRight, ArrowLeft } from 'lucide-preact';
import {
  type DavEntry,
  propfind,
  createReleaseBucket,
  uploadReleaseMultipart,
  getReleaseInfo,
  parseSemver,
  bumpSemver,
  jwtScopes,
  formatSize,
  formatDate,
} from '../api';

interface Props {
  jwt: string;
}

// Navigation state: null means "not yet selected".
// viaLatest=true means the user clicked "latest" — download URLs use the stable /latest/ path.
interface Nav {
  bucket: string | null;
  version: string | null;  // actual resolved version name
  viaLatest: boolean;
  osArch: string | null;
}

export function ReleasePage({ jwt }: Props) {
  const scopes = jwtScopes(jwt);
  const canCreate = scopes.includes('release-create');
  const writableBuckets = scopes
    .filter((s) => s.startsWith('release-write:'))
    .map((s) => s.slice('release-write:'.length));
  const canWriteAllBuckets =
    scopes.includes('write') || scopes.includes('release-write');

  // Bucket list (left panel)
  const [buckets, setBuckets] = useState<DavEntry[]>([]);
  const [loadError, setLoadError] = useState('');
  const [loading, setLoading] = useState(false);

  // Right-panel navigation
  const [nav, setNav] = useState<Nav>({ bucket: null, version: null, viaLatest: false, osArch: null });
  const [entries, setEntries] = useState<DavEntry[]>([]);
  const [browseError, setBrowseError] = useState('');
  const [browseLoading, setBrowseLoading] = useState(false);

  // Mobile: which panel is visible
  const [mobilePanel, setMobilePanel] = useState<'list' | 'detail'>('list');

  // Create bucket form
  const [newBucketMode, setNewBucketMode] = useState(false);
  const [newBucketName, setNewBucketName] = useState('');
  const [createError, setCreateError] = useState('');
  const [creating, setCreating] = useState(false);

  // Upload
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [uploadVersion, setUploadVersion] = useState('');
  const [uploadOsArch, setUploadOsArch] = useState('');
  const [uploading, setUploading] = useState('');
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadError, setUploadError] = useState('');
  // latest version fetched when a bucket is selected (for semver bump UI)
  const [latestKnown, setLatestKnown] = useState('');

  useEffect(() => { void loadBuckets(); }, []);

  // When bucket changes, fetch latest version and pre-fill version input
  useEffect(() => {
    if (!nav.bucket) return;
    setLatestKnown('');
    getReleaseInfo(nav.bucket)
      .then((info) => {
        setLatestKnown(info.latest);
        // Only pre-fill if user hasn't typed anything
        setUploadVersion((prev) => prev === '' || prev === latestKnown ? info.latest : prev);
      })
      .catch(() => { /* bucket may be empty — ignore */ });
  }, [nav.bucket]);

  // Pre-fill os_arch when drilling into a specific target
  useEffect(() => {
    if (nav.osArch) setUploadOsArch(nav.osArch);
  }, [nav.osArch]);

  async function loadBuckets() {
    setLoading(true);
    setLoadError('');
    try {
      const data = await propfind('/api/v1/wd/rs/', jwt);
      setBuckets(data.filter((e) => e.isDir));
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (!msg.includes('404')) setLoadError(msg);
    } finally {
      setLoading(false);
    }
  }

  async function browse(newNav: Nav) {
    setNav(newNav);
    setEntries([]);
    setBrowseError('');
    setBrowseLoading(true);
    setMobilePanel('detail');
    try {
      const { bucket, version, osArch } = newNav;
      let apiPath = `/api/v1/wd/rs/${encodeURIComponent(bucket!)}/`;
      if (version) apiPath += `${encodeURIComponent(version)}/`;
      if (osArch)  apiPath += `${encodeURIComponent(osArch)}/`;
      const data = await propfind(apiPath, jwt);
      setEntries(osArch ? data.filter((e) => !e.isDir) : data.filter((e) => e.isDir));
    } catch (err) {
      setBrowseError(err instanceof Error ? err.message : String(err));
    } finally {
      setBrowseLoading(false);
    }
  }

  function openBucket(name: string) {
    return browse({ bucket: name, version: null, viaLatest: false, osArch: null });
  }

  function openVersion(v: string) {
    return browse({ ...nav, version: v, viaLatest: false, osArch: null });
  }

  // "latest" resolves to the version with the most recent modified date
  async function openLatest(versionEntries: DavEntry[]) {
    const newest = [...versionEntries].sort(
      (a, b) => new Date(b.modified).getTime() - new Date(a.modified).getTime(),
    )[0];
    if (!newest) return;
    return browse({ ...nav, version: newest.name, viaLatest: true, osArch: null });
  }

  function openOsArch(oa: string) {
    return browse({ ...nav, osArch: oa });
  }

  async function handleCreateBucket() {
    if (!newBucketName.trim()) return;
    setCreating(true);
    setCreateError('');
    try {
      await createReleaseBucket(jwt, newBucketName.trim());
      setNewBucketMode(false);
      setNewBucketName('');
      await loadBuckets();
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : 'Failed to create bucket');
    } finally {
      setCreating(false);
    }
  }

  async function handleUpload(files: FileList | null) {
    if (!files || !nav.bucket || !uploadVersion.trim() || !uploadOsArch.trim()) return;
    setUploadError('');
    for (const file of Array.from(files)) {
      setUploading(`uploading ${file.name}…`);
      setUploadProgress(0);
      try {
        await uploadReleaseMultipart(
          jwt, nav.bucket, uploadVersion.trim(), uploadOsArch.trim(), file, setUploadProgress,
        );
      } catch (err) {
        setUploadError(err instanceof Error ? err.message : 'Upload failed');
      }
    }
    setUploading('');
    setUploadProgress(0);
    // Bump the patch version after a successful upload so the next file gets a fresh default
    setLatestKnown(uploadVersion.trim());
    // Refresh current view if we're at file level
    if (nav.osArch) void browse(nav);
  }

  function canWriteBucket(name: string) {
    return canWriteAllBuckets || writableBuckets.includes(name);
  }

  function downloadUrl(entry: DavEntry): string {
    const { bucket, version, viaLatest, osArch } = nav;
    const v = viaLatest ? 'latest' : encodeURIComponent(version!);
    return `/rs/${encodeURIComponent(bucket!)}/${v}/${encodeURIComponent(osArch!)}/${encodeURIComponent(entry.name)}`;
  }

  // ── Breadcrumb ──────────────────────────────────────────────────────────────

  function Breadcrumb() {
    const { bucket, version, viaLatest, osArch } = nav;
    if (!bucket) return null;
    const versionLabel = viaLatest ? `latest (${version})` : version;
    return (
      <div class="release__breadcrumb">
        <button class="release__crumb" onClick={() => void openBucket(bucket)}>{bucket}</button>
        {version && (
          <>
            <ChevronRight size={12} class="release__crumb-sep" />
            <button
              class={`release__crumb${!osArch ? ' release__crumb--current' : ''}${viaLatest ? ' release__crumb--latest' : ''}`}
              onClick={() => !osArch ? undefined : void browse({ ...nav, osArch: null })}
            >
              {versionLabel}
            </button>
          </>
        )}
        {osArch && (
          <>
            <ChevronRight size={12} class="release__crumb-sep" />
            <span class="release__crumb release__crumb--current">{osArch}</span>
          </>
        )}
      </div>
    );
  }

  // ── Right panel content ─────────────────────────────────────────────────────

  function RightContent() {
    const { bucket, version, osArch } = nav;

    if (!bucket) {
      return <p class="release__empty release__right-empty">select a bucket</p>;
    }

    const backNav: Nav | null =
      osArch  ? { ...nav, osArch: null } :
      version ? { bucket, version: null, viaLatest: false, osArch: null } :
      null;

    return (
      <>
        <Breadcrumb />

        {backNav && (
          <button
            class="btn btn--muted btn--sm release__back-btn"
            id="btn-back"
            onClick={() => void browse(backNav)}
          >
            <ArrowLeft size={13} /> back
          </button>
        )}

        {browseLoading ? (
          <div class="empty-state" style="padding:20px 0"><span class="spinner" /></div>
        ) : browseError ? (
          <p class="error-msg">{browseError}</p>
        ) : entries.length === 0 ? (
          <p class="release__empty">
            {osArch ? 'no files' : version ? 'no os/arch targets' : 'no versions'}
          </p>
        ) : osArch ? (
          // File listing
          <table class="file-table">
            <thead>
              <tr>
                <th>file</th>
                <th class="file-table__size">size</th>
                <th class="file-table__date">modified</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => (
                <tr key={e.href} data-href={e.href}>
                  <td>{e.name}</td>
                  <td class="file-table__size">{formatSize(e.size)}</td>
                  <td class="file-table__date">{formatDate(e.modified)}</td>
                  <td>
                    <div class="file-table__actions" style="opacity:1">
                      <a
                        class="btn btn--muted btn--sm"
                        href={downloadUrl(e)}
                        download={e.name}
                        data-action="download"
                      >
                        <Download size={13} />
                      </a>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          // Version or os/arch directory listing
          <div class="release__bucket-list">
            {/* "latest" pseudo-entry — only at version level, when there are entries */}
            {!version && entries.length > 0 && (
              <button
                class="release__bucket-item release__bucket-item--latest"
                data-entry="latest"
                onClick={() => void openLatest(entries)}
              >
                <Folder size={14} />
                <span class="release__bucket-name">latest</span>
                <span class="release__latest-badge">→ {
                  [...entries].sort(
                    (a, b) => new Date(b.modified).getTime() - new Date(a.modified).getTime(),
                  )[0]?.name
                }</span>
                <ChevronRight size={12} class="release__bucket-chevron" />
              </button>
            )}
            {entries.map((e) => (
              <button
                key={e.name}
                class="release__bucket-item"
                data-entry={e.name}
                onClick={() => version ? void openOsArch(e.name) : void openVersion(e.name)}
              >
                <Folder size={14} />
                <span class="release__bucket-name">{e.name}</span>
                <ChevronRight size={12} class="release__bucket-chevron" />
              </button>
            ))}
          </div>
        )}

        {/* Upload section */}
        {canWriteBucket(bucket) && (
          <div class="release__upload-section">
            <p class="admin__section-title" style="margin-bottom:10px">upload to {bucket}</p>

            {/* Version row with semver bumpers */}
            <div class="release__upload-field">
              <label class="modal__label" for="input-upload-version">version</label>
              <div class="release__version-row">
                <input
                  id="input-upload-version"
                  class="input"
                  type="text"
                  placeholder="v1.0.0"
                  value={uploadVersion}
                  style="max-width:130px"
                  onInput={(e) => setUploadVersion((e.target as HTMLInputElement).value)}
                />
                {parseSemver(uploadVersion) && (
                  <div class="release__bump-btns">
                    {(['major', 'minor', 'patch'] as const).map((part) => (
                      <button
                        key={part}
                        type="button"
                        class="btn btn--muted btn--sm release__bump-btn"
                        data-bump={part}
                        onClick={() => setUploadVersion(bumpSemver(uploadVersion, part))}
                        title={`bump ${part} → ${bumpSemver(uploadVersion, part)}`}
                      >
                        +{part}
                      </button>
                    ))}
                  </div>
                )}
                {latestKnown && (
                  <span class="release__latest-hint">latest: {latestKnown}</span>
                )}
              </div>
            </div>

            {/* os/arch row */}
            <div class="release__upload-field">
              <label class="modal__label" for="input-upload-osarch">os / arch</label>
              <input
                id="input-upload-osarch"
                class="input"
                type="text"
                placeholder="linux-amd64"
                value={uploadOsArch}
                style="max-width:180px"
                onInput={(e) => setUploadOsArch((e.target as HTMLInputElement).value)}
              />
            </div>

            <div style="margin-top:8px">
              <button
                class="btn btn--sm"
                id="btn-release-upload"
                disabled={!uploadVersion.trim() || !uploadOsArch.trim() || !!uploading}
                onClick={() => fileInputRef.current?.click()}
              >
                <Upload size={13} /> choose file
              </button>
              <input
                ref={fileInputRef}
                type="file"
                id="input-release-file"
                multiple
                style="display:none"
                onChange={(e) => void handleUpload((e.target as HTMLInputElement).files)}
              />
            </div>
            {uploading && (
              <div class="upload-progress">
                {uploading}{uploadProgress > 0 ? ` ${uploadProgress}%` : ''}
              </div>
            )}
            {uploadError && <p class="error-msg">{uploadError}</p>}
          </div>
        )}
      </>
    );
  }

  // ── Left panel ──────────────────────────────────────────────────────────────

  return (
    <div
      class={`release admin-theme release__layout${mobilePanel === 'detail' ? ' release__layout--detail' : ''}`}
    >
      {/* Left: bucket list */}
      <div class="release__left">
        <div class="release__panel-header">
          <span class="admin__section-title" style="margin-bottom:0">buckets</span>
          {canCreate && (
            <button
              class="btn btn--sm btn--muted"
              id="btn-new-bucket"
              onClick={() => setNewBucketMode(true)}
            >
              <FolderPlus size={13} />
            </button>
          )}
        </div>

        {newBucketMode && (
          <div class="release__inline-form">
            <input
              id="input-bucket-name"
              class="input"
              type="text"
              placeholder="bucket name"
              value={newBucketName}
              onInput={(e) => setNewBucketName((e.target as HTMLInputElement).value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') void handleCreateBucket();
                if (e.key === 'Escape') { setNewBucketMode(false); setNewBucketName(''); }
              }}
              autoFocus
            />
            <div style="display:flex;gap:4px">
              <button class="btn btn--sm" id="btn-bucket-confirm" onClick={() => void handleCreateBucket()} disabled={creating}>
                {creating ? <span class="spinner" /> : 'create'}
              </button>
              <button class="btn btn--muted btn--sm" id="btn-bucket-cancel" onClick={() => { setNewBucketMode(false); setNewBucketName(''); }}>
                cancel
              </button>
            </div>
            {createError && <p class="error-msg" style="margin:4px 0 0">{createError}</p>}
          </div>
        )}

        {loading ? (
          <div class="empty-state" style="padding:20px 0"><span class="spinner" /></div>
        ) : loadError ? (
          <p class="error-msg">{loadError}</p>
        ) : buckets.length === 0 ? (
          <p class="release__empty">no buckets yet</p>
        ) : (
          <div class="release__bucket-list">
            {buckets.map((b) => (
              <button
                key={b.name}
                class={`release__bucket-item${nav.bucket === b.name ? ' release__bucket-item--active' : ''}`}
                data-bucket={b.name}
                onClick={() => void openBucket(b.name)}
              >
                <Folder size={14} />
                <span class="release__bucket-name">{b.name}</span>
                <ChevronRight size={12} class="release__bucket-chevron" />
              </button>
            ))}
          </div>
        )}
      </div>

      {/* Right: contents + upload */}
      <div class="release__right">
        <button
          class="btn btn--muted btn--sm release__mobile-back"
          id="btn-back-buckets"
          onClick={() => { setMobilePanel('list'); }}
        >
          <ArrowLeft size={13} /> buckets
        </button>
        <RightContent />
      </div>
    </div>
  );
}
