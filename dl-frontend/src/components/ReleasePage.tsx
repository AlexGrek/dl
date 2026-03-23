import { useState, useEffect, useRef } from 'preact/hooks';
import { Folder, FolderPlus, Upload, Download, ChevronRight, ArrowLeft } from 'lucide-preact';
import {
  type DavEntry,
  propfind,
  createReleaseBucket,
  uploadRelease,
  jwtScopes,
  formatSize,
  formatDate,
} from '../api';

interface Props {
  jwt: string;
}

export function ReleasePage({ jwt }: Props) {
  const scopes = jwtScopes(jwt);
  const canCreate = scopes.includes('release-create');
  const writableBuckets = scopes
    .filter((s) => s.startsWith('release-write:'))
    .map((s) => s.slice('release-write:'.length));
  const canWriteAll = scopes.includes('write');

  // Bucket list
  const [buckets, setBuckets] = useState<DavEntry[]>([]);
  const [loadError, setLoadError] = useState('');
  const [loading, setLoading] = useState(false);

  // Selected bucket / os_arch browsing
  const [bucket, setBucket] = useState<string | null>(null);
  const [osArch, setOsArch] = useState<string | null>(null);
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
  const [uploadOsArch, setUploadOsArch] = useState('');
  const [uploading, setUploading] = useState('');
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadError, setUploadError] = useState('');

  useEffect(() => { void loadBuckets(); }, []);

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

  async function openBucket(name: string) {
    setBucket(name);
    setOsArch(null);
    setEntries([]);
    setBrowseError('');
    setBrowseLoading(true);
    setMobilePanel('detail');
    try {
      const data = await propfind(`/api/v1/wd/rs/${encodeURIComponent(name)}/`, jwt);
      setEntries(data.filter((e) => e.isDir));
    } catch (err) {
      setBrowseError(err instanceof Error ? err.message : String(err));
    } finally {
      setBrowseLoading(false);
    }
  }

  async function openOsArch(name: string) {
    setOsArch(name);
    setBrowseError('');
    setBrowseLoading(true);
    try {
      const data = await propfind(
        `/api/v1/wd/rs/${encodeURIComponent(bucket!)}/${encodeURIComponent(name)}/`,
        jwt,
      );
      setEntries(data.filter((e) => !e.isDir));
    } catch (err) {
      setBrowseError(err instanceof Error ? err.message : String(err));
    } finally {
      setBrowseLoading(false);
    }
  }

  function goBackOsArch() {
    setOsArch(null);
    void openBucket(bucket!);
  }

  function goBackBuckets() {
    setBucket(null);
    setOsArch(null);
    setEntries([]);
    setMobilePanel('list');
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
    if (!files || !bucket || !uploadOsArch.trim()) return;
    setUploadError('');
    for (const file of Array.from(files)) {
      setUploading(`uploading ${file.name}…`);
      setUploadProgress(0);
      try {
        await uploadRelease(jwt, bucket, uploadOsArch.trim(), file, setUploadProgress);
      } catch (err) {
        setUploadError(err instanceof Error ? err.message : 'Upload failed');
      }
    }
    setUploading('');
    setUploadProgress(0);
    if (osArch) await openOsArch(osArch);
  }

  const canWriteAllBuckets = canWriteAll || scopes.includes('release-write');

  function canWriteBucket(name: string) {
    return canWriteAllBuckets || writableBuckets.includes(name);
  }

  // ── Panels ──

  const leftPanel = (
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
              class={`release__bucket-item${bucket === b.name ? ' release__bucket-item--active' : ''}`}
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
  );

  const rightPanel = (
    <div class="release__right">
      {/* Mobile back to bucket list */}
      <button
        class="btn btn--muted btn--sm release__mobile-back"
        id="btn-back-buckets"
        onClick={goBackBuckets}
      >
        <ArrowLeft size={13} /> buckets
      </button>

      {!bucket ? (
        <p class="release__empty release__right-empty">select a bucket</p>
      ) : (
        <>
          {/* Breadcrumb */}
          <div class="release__breadcrumb">
            <button
              class="release__crumb"
              data-bucket={bucket}
              onClick={() => { setOsArch(null); void openBucket(bucket); }}
            >
              {bucket}
            </button>
            {osArch && (
              <>
                <ChevronRight size={12} class="release__crumb-sep" />
                <span class="release__crumb release__crumb--current">{osArch}</span>
              </>
            )}
          </div>

          {/* Content */}
          {browseLoading ? (
            <div class="empty-state" style="padding:20px 0"><span class="spinner" /></div>
          ) : browseError ? (
            <p class="error-msg">{browseError}</p>
          ) : entries.length === 0 ? (
            <p class="release__empty">{osArch ? 'no files' : 'no os/arch directories'}</p>
          ) : osArch ? (
            <>
              <button
                class="btn btn--muted btn--sm"
                id="btn-back-osarch"
                style="margin-bottom:8px"
                onClick={goBackOsArch}
              >
                <ArrowLeft size={13} /> {bucket}
              </button>
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
                            href={`/rs/${encodeURIComponent(bucket)}/${encodeURIComponent(osArch)}/${encodeURIComponent(e.name)}`}
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
            </>
          ) : (
            <div class="release__bucket-list">
              {entries.map((e) => (
                <button
                  key={e.name}
                  class="release__bucket-item"
                  data-osarch={e.name}
                  onClick={() => void openOsArch(e.name)}
                >
                  <Folder size={14} />
                  <span class="release__bucket-name">{e.name}</span>
                  <ChevronRight size={12} class="release__bucket-chevron" />
                </button>
              ))}
            </div>
          )}

          {/* Upload */}
          {canWriteBucket(bucket) && (
            <div class="release__upload-section">
              <p class="admin__section-title" style="margin-bottom:8px">upload</p>
              <div class="release__upload-row">
                <input
                  id="input-osarch"
                  class="input"
                  type="text"
                  placeholder="os/arch (e.g. linux-amd64)"
                  value={uploadOsArch}
                  style="max-width:200px"
                  onInput={(e) => setUploadOsArch((e.target as HTMLInputElement).value)}
                />
                <button
                  class="btn btn--sm"
                  id="btn-release-upload"
                  disabled={!uploadOsArch.trim() || !!uploading}
                  onClick={() => fileInputRef.current?.click()}
                >
                  <Upload size={13} /> upload
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
      )}
    </div>
  );

  return (
    <div
      class={`release admin-theme release__layout${mobilePanel === 'detail' ? ' release__layout--detail' : ''}`}
    >
      {leftPanel}
      {rightPanel}
    </div>
  );
}
