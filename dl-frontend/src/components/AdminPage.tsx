import { useState } from 'preact/hooks';
import { X } from 'lucide-preact';
import { type APIKey, listKeys, createKey, deleteKey } from '../api';
import { ConfirmModal } from './ConfirmModal';

// ── Scope builder helpers ────────────────────────────────────────────────────

type FileAccessType = 'read' | 'write';

interface FileScope {
  kind: 'file';
  access: FileAccessType;
  path: string; // '' = global
}

interface ReleaseWriteScope {
  kind: 'release-write';
  bucket: string;
}

interface ReleasCreateScope {
  kind: 'release-create';
}

type ScopeEntry = FileScope | ReleaseWriteScope | ReleasCreateScope;

function entryToScope(e: ScopeEntry): string {
  if (e.kind === 'file') return e.path ? `${e.access}:${normPath(e.path)}` : e.access;
  if (e.kind === 'release-write') return `release-write:${e.bucket}`;
  return 'release-create';
}

function normPath(p: string): string {
  return '/' + p.replace(/^\/+/, '');
}

function scopeToEntry(s: string): ScopeEntry | null {
  if (s === 'release-create') return { kind: 'release-create' };
  if (s === 'release-write') return { kind: 'release-write', bucket: '' }; // global — show as wildcard
  if (s.startsWith('release-write:')) return { kind: 'release-write', bucket: s.slice('release-write:'.length) };
  if (s === 'read') return { kind: 'file', access: 'read', path: '' };
  if (s === 'write') return { kind: 'file', access: 'write', path: '' };
  if (s.startsWith('read:/')) return { kind: 'file', access: 'read', path: s.slice('read:'.length) };
  if (s.startsWith('write:/')) return { kind: 'file', access: 'write', path: s.slice('write:'.length) };
  return null;
}

// ── Component ────────────────────────────────────────────────────────────────

export function AdminPage() {
  const [masterKey, setMasterKey] = useState('');
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [loading, setLoading] = useState(false);

  // Scope builder state
  const [scopeEntries, setScopeEntries] = useState<ScopeEntry[]>([]);
  // "add file scope" form
  const [addFileAccess, setAddFileAccess] = useState<FileAccessType>('read');
  const [addFilePath, setAddFilePath] = useState('');
  // "add release-write" form
  const [addBucket, setAddBucket] = useState('');

  // New key form
  const [newDesc, setNewDesc] = useState('');
  const [creating, setCreating] = useState(false);
  const [createdKey, setCreatedKey] = useState('');
  const [createError, setCreateError] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<APIKey | null>(null);

  async function handleLoad() {
    if (!masterKey.trim()) return;
    setLoading(true);
    setLoadError('');
    try {
      const data = await listKeys(masterKey.trim());
      setKeys(data);
      setLoaded(true);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : 'Failed to load keys');
    } finally {
      setLoading(false);
    }
  }

  async function confirmDeleteKey() {
    if (!deleteTarget || !masterKey.trim()) return;
    const keyId = deleteTarget.id;
    setDeleteTarget(null);
    try {
      await deleteKey(masterKey.trim(), keyId);
      setKeys((prev) => prev.filter((k) => k.id !== keyId));
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : 'Delete failed');
    }
  }

  function addFileScope() {
    const path = addFilePath.trim();
    const entry: FileScope = { kind: 'file', access: addFileAccess, path };
    const scope = entryToScope(entry);
    if (scopeEntries.some((e) => entryToScope(e) === scope)) return;
    setScopeEntries((prev) => [...prev, entry]);
    setAddFilePath('');
  }

  function addReleaseWriteScope() {
    const bucket = addBucket.trim();
    if (!bucket) return;
    const entry: ReleaseWriteScope = { kind: 'release-write', bucket };
    if (scopeEntries.some((e) => entryToScope(e) === entryToScope(entry))) return;
    setScopeEntries((prev) => [...prev, entry]);
    setAddBucket('');
  }

  function toggleReleaseCreate() {
    const has = scopeEntries.some((e) => e.kind === 'release-create');
    if (has) {
      setScopeEntries((prev) => prev.filter((e) => e.kind !== 'release-create'));
    } else {
      setScopeEntries((prev) => [...prev, { kind: 'release-create' }]);
    }
  }

  function removeScope(idx: number) {
    setScopeEntries((prev) => prev.filter((_, i) => i !== idx));
  }

  async function handleCreate(e: Event) {
    e.preventDefault();
    if (!masterKey.trim() || !newDesc.trim()) return;
    const scopes = scopeEntries.map(entryToScope);
    setCreating(true);
    setCreateError('');
    setCreatedKey('');
    try {
      const res = await createKey(masterKey.trim(), newDesc.trim(), scopes);
      setCreatedKey(res.key);
      setNewDesc('');
      setScopeEntries([]);
      await handleLoad();
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : 'Failed to create key');
    } finally {
      setCreating(false);
    }
  }

  const hasReleaseCreate = scopeEntries.some((e) => e.kind === 'release-create');

  return (
    <div class="admin admin-theme">
      {deleteTarget && (
        <ConfirmModal
          title="delete api key"
          message={`Delete key "${deleteTarget.id}" (${deleteTarget.description})? This cannot be undone.`}
          onConfirm={() => void confirmDeleteKey()}
          onCancel={() => setDeleteTarget(null)}
        />
      )}

      {/* Master key */}
      <section class="admin__section">
        <p class="admin__section-title">master key</p>
        <div class="admin__key-row">
          <input
            id="input-master-key"
            class="input"
            type="password"
            placeholder="master key…"
            value={masterKey}
            onInput={(e) => setMasterKey((e.target as HTMLInputElement).value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void handleLoad(); }}
            style="max-width:340px"
          />
          <button
            class="btn"
            id="btn-load-keys"
            onClick={() => void handleLoad()}
            disabled={loading || !masterKey.trim()}
          >
            {loading ? <span class="spinner" /> : 'load'}
          </button>
        </div>
        {loadError && <p class="error-msg">{loadError}</p>}
      </section>

      {loaded && (
        <>
          {/* API Keys table */}
          <section class="admin__section">
            <p class="admin__section-title">api keys ({keys.length})</p>
            {keys.length === 0 ? (
              <p class="release__empty">no keys found</p>
            ) : (
              <table class="key-table">
                <thead>
                  <tr>
                    <th>id</th>
                    <th>description</th>
                    <th>scopes</th>
                    <th>created</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {keys.map((key) => (
                    <tr key={key.id} data-key-id={key.id}>
                      <td style="color:var(--fg-muted);font-size:11px">{key.id}</td>
                      <td>{key.description}</td>
                      <td>
                        <div class="scope-tags">
                          {key.scopes.map((s) => {
                            const e = scopeToEntry(s);
                            return (
                              <span
                                class={`tag scope-tag${e?.kind === 'file' ? ` scope-tag--${e.access}` : e?.kind === 'release-write' || e?.kind === 'release-create' ? ' scope-tag--release' : ''}`}
                                key={s}
                              >
                                {s}
                              </span>
                            );
                          })}
                        </div>
                      </td>
                      <td class="file-table__date">
                        {key.created_at ? key.created_at.slice(0, 10) : '—'}
                      </td>
                      <td>
                        <div class="key-table__actions">
                          <button
                            class="btn btn--danger btn--sm"
                            data-action="delete-key"
                            data-key-id={key.id}
                            onClick={() => setDeleteTarget(key)}
                          >
                            delete
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>

          {/* New key created banner */}
          {createdKey && (
            <div class="new-key-banner" id="new-key-banner">
              <p class="new-key-banner__label">new key — copy now, won't be shown again</p>
              <code id="new-key-value">{createdKey}</code>
            </div>
          )}

          {/* Create key form */}
          <section class="admin__section">
            <p class="admin__section-title">create key</p>
            <form onSubmit={(e) => void handleCreate(e)} style="display:flex;flex-direction:column;gap:14px;max-width:540px">

              <div>
                <label class="modal__label" for="input-key-desc">description</label>
                <input
                  id="input-key-desc"
                  class="input"
                  type="text"
                  placeholder="CI – my-app"
                  value={newDesc}
                  onInput={(e) => setNewDesc((e.target as HTMLInputElement).value)}
                />
              </div>

              {/* Scope builder */}
              <div class="scope-builder">
                <label class="modal__label">scopes</label>

                {/* Current scopes */}
                {scopeEntries.length > 0 && (
                  <div class="scope-tags" style="margin-bottom:8px">
                    {scopeEntries.map((entry, i) => (
                      <span
                        key={entryToScope(entry)}
                        class={`tag scope-tag${entry.kind === 'file' ? ` scope-tag--${entry.access}` : ' scope-tag--release'}`}
                      >
                        {entryToScope(entry)}
                        <button
                          type="button"
                          class="scope-tag__remove"
                          data-scope={entryToScope(entry)}
                          onClick={() => removeScope(i)}
                          aria-label="remove scope"
                        >
                          <X size={10} />
                        </button>
                      </span>
                    ))}
                  </div>
                )}

                {/* Add file access scope */}
                <div class="scope-builder__row">
                  <span class="scope-builder__label">file access</span>
                  <select
                    class="input scope-builder__select"
                    id="select-file-access"
                    value={addFileAccess}
                    onChange={(e) => setAddFileAccess((e.target as HTMLSelectElement).value as FileAccessType)}
                  >
                    <option value="read">read</option>
                    <option value="write">write</option>
                  </select>
                  <input
                    class="input"
                    id="input-file-path"
                    type="text"
                    placeholder="/path (empty = global)"
                    value={addFilePath}
                    style="flex:1"
                    onInput={(e) => setAddFilePath((e.target as HTMLInputElement).value)}
                    onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addFileScope(); } }}
                  />
                  <button
                    type="button"
                    class="btn btn--muted btn--sm"
                    id="btn-add-file-scope"
                    onClick={addFileScope}
                  >
                    + add
                  </button>
                </div>

                {/* Add release-write scope */}
                <div class="scope-builder__row">
                  <span class="scope-builder__label">release bucket</span>
                  <input
                    class="input"
                    id="input-release-bucket"
                    type="text"
                    placeholder="bucket name"
                    value={addBucket}
                    style="flex:1"
                    onInput={(e) => setAddBucket((e.target as HTMLInputElement).value)}
                    onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addReleaseWriteScope(); } }}
                  />
                  <button
                    type="button"
                    class="btn btn--muted btn--sm"
                    id="btn-add-release-scope"
                    onClick={addReleaseWriteScope}
                    disabled={!addBucket.trim()}
                  >
                    + add
                  </button>
                </div>

                {/* release-create toggle */}
                <label class="scope-builder__checkbox" for="check-release-create">
                  <input
                    type="checkbox"
                    id="check-release-create"
                    checked={hasReleaseCreate}
                    onChange={toggleReleaseCreate}
                  />
                  <span>release-create</span>
                  <span class="scope-builder__hint">(can create new release buckets)</span>
                </label>
              </div>

              {createError && <p class="error-msg">{createError}</p>}

              <div>
                <button
                  type="submit"
                  class="btn"
                  id="btn-create-key"
                  disabled={creating || !newDesc.trim()}
                >
                  {creating ? <span class="spinner" /> : 'create key'}
                </button>
              </div>
            </form>
          </section>
        </>
      )}
    </div>
  );
}
