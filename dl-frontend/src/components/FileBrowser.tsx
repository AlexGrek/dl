import { useState, useEffect, useRef } from 'preact/hooks';
import { Folder, File, Download, Trash2, RefreshCw, FolderPlus, Upload, Link } from 'lucide-preact';
import {
  type DavEntry,
  propfind,
  uploadFile,
  deleteEntry,
  mkcol,
  formatSize,
  formatDate,
  entryApiPath,
  entryDownloadUrl,
  pathSegments,
} from '../api';
import { ConfirmModal } from './ConfirmModal';

interface Props {
  jwt: string | null;
  path: string;
  onNavigate: (path: string) => void;
  onLoginRequired: () => void;
}

const PAGE_SIZE_OPTIONS = [25, 50, 100, 0] as const; // 0 = All
const PAGE_SIZE_LABELS: Record<number, string> = { 25: '25', 50: '50', 100: '100', 0: 'all' };

export function FileBrowser({ jwt, path, onNavigate, onLoginRequired }: Props) {
  const [entries, setEntries] = useState<DavEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [status, setStatus] = useState('');
  const [dragging, setDragging] = useState(false);
  const [uploading, setUploading] = useState('');
  const [newDirMode, setNewDirMode] = useState(false);
  const [newDirName, setNewDirName] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<DavEntry | null>(null);
  const [copied, setCopied] = useState(false);
  const [copiedHref, setCopiedHref] = useState<string | null>(null);
  const [pageSize, setPageSize] = useState<number>(50);
  const [page, setPage] = useState(0);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const dragCounter = useRef(0);
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const fileCopyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    setPage(0);
    if (jwt) void loadDir(path);
  }, [jwt, path]);

  async function loadDir(upstreamPath: string) {
    if (!jwt) return;
    setLoading(true);
    setError('');
    try {
      const apiPath = `/api/v1/wd${upstreamPath}`;
      const data = await propfind(apiPath, jwt);
      setEntries(data);
      setStatus(`${data.length} item${data.length === 1 ? '' : 's'}`);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (msg.includes('401')) {
        setError('Session expired — please log in again');
      } else {
        setError(msg);
      }
    } finally {
      setLoading(false);
    }
  }

  function navigate(upstreamPath: string) {
    onNavigate(upstreamPath.endsWith('/') ? upstreamPath : upstreamPath + '/');
  }

  async function handleUpload(files: FileList | null) {
    if (!files || !jwt) return;
    for (const file of Array.from(files)) {
      const apiPath = `/api/v1/wd${path}${encodeURIComponent(file.name)}`;
      setUploading(`uploading ${file.name}…`);
      try {
        await uploadFile(apiPath, file, jwt);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Upload failed');
      }
    }
    setUploading('');
    await loadDir(path);
  }

  async function confirmDelete() {
    if (!jwt || !deleteTarget) return;
    const entry = deleteTarget;
    setDeleteTarget(null);
    try {
      await deleteEntry(entryApiPath(entry), jwt);
      await loadDir(path);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Delete failed');
    }
  }

  async function handleMkdir() {
    if (!jwt || !newDirName.trim()) return;
    const name = newDirName.trim().replace(/[/\\]/g, '');
    const apiPath = `/api/v1/wd${path}${encodeURIComponent(name)}/`;
    try {
      await mkcol(apiPath, jwt);
      setNewDirMode(false);
      setNewDirName('');
      await loadDir(path);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create folder');
    }
  }

  function handleCopyFileLink(entry: DavEntry) {
    const url = window.location.origin + entryDownloadUrl(entry);
    void navigator.clipboard.writeText(url).then(() => {
      setCopiedHref(entry.href);
      if (fileCopyTimer.current) clearTimeout(fileCopyTimer.current);
      fileCopyTimer.current = setTimeout(() => setCopiedHref(null), 2000);
    });
  }

  function handleCopyLink() {
    void navigator.clipboard.writeText(window.location.href).then(() => {
      setCopied(true);
      if (copyTimer.current) clearTimeout(copyTimer.current);
      copyTimer.current = setTimeout(() => setCopied(false), 2000);
    });
  }

  // Drag-and-drop
  function onDragEnter(e: DragEvent) {
    e.preventDefault();
    dragCounter.current++;
    if (dragCounter.current === 1) setDragging(true);
  }
  function onDragLeave(e: DragEvent) {
    e.preventDefault();
    dragCounter.current--;
    if (dragCounter.current === 0) setDragging(false);
  }
  function onDragOver(e: DragEvent) { e.preventDefault(); }
  function onDrop(e: DragEvent) {
    e.preventDefault();
    dragCounter.current = 0;
    setDragging(false);
    void handleUpload(e.dataTransfer?.files ?? null);
  }

  if (!jwt) {
    return (
      <div class="auth-prompt">
        <span>not logged in —</span>
        <button class="btn" id="btn-login-prompt" onClick={onLoginRequired}>login</button>
      </div>
    );
  }

  const segments = pathSegments(path);
  const totalPages = pageSize === 0 ? 1 : Math.ceil(entries.length / pageSize);
  const visibleEntries = pageSize === 0 ? entries : entries.slice(page * pageSize, (page + 1) * pageSize);

  return (
    <div
      class="browser"
      onDragEnter={onDragEnter}
      onDragLeave={onDragLeave}
      onDragOver={onDragOver}
      onDrop={onDrop}
    >
      {dragging && (
        <div class="drop-overlay" aria-hidden="true">drop to upload</div>
      )}

      {deleteTarget && (
        <ConfirmModal
          title="delete item"
          message={`Delete "${deleteTarget.name}"? This cannot be undone.`}
          onConfirm={() => void confirmDelete()}
          onCancel={() => setDeleteTarget(null)}
        />
      )}

      <div class="browser__toolbar">
        {/* Breadcrumb */}
        <nav class="breadcrumb" aria-label="path">
          {segments.map((seg, i) => {
            const isLast = i === segments.length - 1;
            return (
              <span key={seg.path}>
                {i > 0 && <span class="breadcrumb__sep">/</span>}
                <button
                  class={`breadcrumb__item${isLast ? ' breadcrumb__item--current' : ''}`}
                  data-path={seg.path}
                  onClick={() => !isLast && navigate(seg.path)}
                >
                  {seg.label}
                </button>
              </span>
            );
          })}
        </nav>

        <div class="browser__toolbar-right">
          <select
            class="input input--sm"
            id="select-page-size"
            value={pageSize}
            onChange={(e) => { setPageSize(Number((e.target as HTMLSelectElement).value)); setPage(0); }}
            title="Items per page"
          >
            {PAGE_SIZE_OPTIONS.map((n) => (
              <option key={n} value={n}>{PAGE_SIZE_LABELS[n]}</option>
            ))}
          </select>
          <button
            class={`btn btn--muted btn--sm${copied ? ' btn--copied' : ''}`}
            id="btn-share"
            onClick={handleCopyLink}
            title="Copy link to this directory"
          >
            <Link size={13} />
            {copied && <span class="btn__copied-label">copied!</span>}
          </button>
          <button
            class="btn btn--muted btn--sm"
            id="btn-refresh"
            onClick={() => void loadDir(path)}
            title="Refresh"
          >
            <RefreshCw size={13} />
          </button>
          <button
            class="btn btn--muted btn--sm"
            id="btn-new-folder"
            onClick={() => setNewDirMode(true)}
          >
            <FolderPlus size={13} /> folder
          </button>
          <button
            class="btn btn--sm"
            id="btn-upload"
            onClick={() => fileInputRef.current?.click()}
          >
            <Upload size={13} /> upload
          </button>
          <input
            ref={fileInputRef}
            type="file"
            id="input-file"
            multiple
            style="display:none"
            onChange={(e) => void handleUpload((e.target as HTMLInputElement).files)}
          />
        </div>
      </div>

      {/* New folder inline form */}
      {newDirMode && (
        <div class="browser__toolbar" style="border-top:none">
          <label class="modal__label" for="input-dirname">new folder name</label>
          <input
            id="input-dirname"
            class="input"
            style="max-width:240px"
            type="text"
            value={newDirName}
            onInput={(e) => setNewDirName((e.target as HTMLInputElement).value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void handleMkdir();
              if (e.key === 'Escape') { setNewDirMode(false); setNewDirName(''); }
            }}
            autoFocus
          />
          <button class="btn btn--sm" id="btn-mkdir-confirm" onClick={() => void handleMkdir()}>
            create
          </button>
          <button
            class="btn btn--muted btn--sm"
            id="btn-mkdir-cancel"
            onClick={() => { setNewDirMode(false); setNewDirName(''); }}
          >
            cancel
          </button>
        </div>
      )}

      {/* File table */}
      {loading ? (
        <div class="empty-state"><span class="spinner" /></div>
      ) : entries.length === 0 && !error ? (
        <div class="empty-state">empty directory</div>
      ) : (
        <table class="file-table">
          <thead>
            <tr>
              <th>name</th>
              <th class="file-table__size">size</th>
              <th class="file-table__date">modified</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {visibleEntries.map((entry) => (
              <tr key={entry.href} data-href={entry.href}>
                <td>
                  <div class="file-table__name">
                    <span class={`file-table__icon${entry.isDir ? ' file-table__icon--dir' : ''}`}>
                      {entry.isDir ? <Folder size={14} /> : <File size={14} />}
                    </span>
                    {entry.isDir ? (
                      <button
                        class="file-table__link"
                        data-path={entry.href}
                        onClick={() => navigate(entry.href)}
                      >
                        {entry.name}
                      </button>
                    ) : (
                      <a
                        class="file-table__link"
                        href={entryDownloadUrl(entry)}
                        download={entry.name}
                      >
                        {entry.name}
                      </a>
                    )}
                  </div>
                </td>
                <td class="file-table__size">
                  {entry.isDir ? '—' : formatSize(entry.size)}
                </td>
                <td class="file-table__date">{formatDate(entry.modified)}</td>
                <td>
                  <div class="file-table__actions">
                    {!entry.isDir && (
                      <>
                        <button
                          class={`btn btn--muted btn--sm${copiedHref === entry.href ? ' btn--copied' : ''}`}
                          data-action="copy-link"
                          data-href={entry.href}
                          title="Copy permalink"
                          onClick={() => handleCopyFileLink(entry)}
                        >
                          <Link size={13} />
                        </button>
                        <a
                          class="btn btn--muted btn--sm"
                          href={entryDownloadUrl(entry)}
                          download={entry.name}
                          data-action="download"
                        >
                          <Download size={13} />
                        </a>
                      </>
                    )}
                    <button
                      class="btn btn--danger btn--sm"
                      data-action="delete"
                      data-href={entry.href}
                      onClick={() => setDeleteTarget(entry)}
                    >
                      <Trash2 size={13} />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {totalPages > 1 && (
        <div class="pager">
          <button
            class="btn btn--muted btn--sm"
            id="btn-prev-page"
            onClick={() => setPage((p) => Math.max(0, p - 1))}
            disabled={page === 0}
          >
            ←
          </button>
          <span class="pager__info">{page + 1} / {totalPages}</span>
          <button
            class="btn btn--muted btn--sm"
            id="btn-next-page"
            onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
            disabled={page >= totalPages - 1}
          >
            →
          </button>
        </div>
      )}

      {uploading && <div class="upload-progress">{uploading}</div>}

      <div class={`status-bar${error ? ' status-bar--error' : ''}`}>
        {error || status}
      </div>
    </div>
  );
}
