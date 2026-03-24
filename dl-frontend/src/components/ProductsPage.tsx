import { useState, useEffect } from 'preact/hooks';
import { Download, ChevronLeft, ExternalLink, Package, Tag, Terminal } from 'lucide-preact';
import {
  type ProductSummary,
  type ProductDetail,
  type ReleaseFile,
  listProducts,
  getProductDetail,
  detectPlatform,
  formatSize,
  formatDate,
} from '../api';
import { Markdown } from './Markdown';

interface Props {
  bucket: string; // empty = list view, non-empty = detail view
  onNavigate: (bucket: string) => void;
}

function platformLabel(target: string): string {
  const map: Record<string, string> = {
    'linux-amd64': 'Linux (x86-64)',
    'linux-arm64': 'Linux (ARM64)',
    'darwin-amd64': 'macOS (Intel)',
    'darwin-arm64': 'macOS (Apple Silicon)',
    'windows-amd64': 'Windows (x86-64)',
    'windows-arm64': 'Windows (ARM64)',
  };
  return map[target] ?? target;
}

function platformShort(target: string): string {
  const [os] = target.split('-');
  const map: Record<string, string> = {
    linux: 'linux',
    darwin: 'macos',
    windows: 'windows',
  };
  return map[os] ?? target;
}

/** Pick the primary download file (largest non-checksum). */
function pickFile(files: ReleaseFile[]): ReleaseFile | null {
  const candidates = files.filter(
    (f) => !f.name.endsWith('.sha256') && !f.name.endsWith('.sha512'),
  );
  if (!candidates.length) return files[0] ?? null;
  return candidates.reduce((a, b) => (a.size >= b.size ? a : b));
}

export function ProductsPage({ bucket, onNavigate }: Props) {
  if (bucket) {
    return <ProductDetailView bucket={bucket} onBack={() => onNavigate('')} />;
  }
  return <ProductListView onSelect={(b) => onNavigate(b)} />;
}

// ── List View ──

function ProductListView({ onSelect }: { onSelect: (bucket: string) => void }) {
  const [products, setProducts] = useState<ProductSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    listProducts()
      .then(setProducts)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : 'Failed to load'))
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div class="products">
        <div class="empty-state">
          <span class="spinner" />
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div class="products">
        <p class="error-msg">{error}</p>
      </div>
    );
  }

  if (!products.length) {
    return (
      <div class="products">
        <div class="products__header">
          <Terminal size={16} class="products__header-icon" />
          <span class="products__title">products</span>
        </div>
        <div class="empty-state">No products available.</div>
      </div>
    );
  }

  // De-duplicate platforms across targets (e.g. linux-amd64 + linux-arm64 → "linux")
  function uniquePlatforms(targets: string[]): string[] {
    const seen = new Set<string>();
    for (const t of targets) {
      seen.add(platformShort(t));
    }
    return Array.from(seen).sort();
  }

  return (
    <div class="products">
      <div class="products__header">
        <Terminal size={16} class="products__header-icon" />
        <span class="products__title">products</span>
        <span class="products__count">{products.length} available</span>
      </div>
      <div class="products__grid">
        {products.map((p) => (
          <button
            key={p.bucket}
            class="pcard"
            data-bucket={p.bucket}
            onClick={() => onSelect(p.bucket)}
          >
            <div class="pcard__head">
              <Package size={15} class="pcard__icon" />
              <span class="pcard__name">{p.name}</span>
              {p.latest && <code class="pcard__version">{p.latest}</code>}
            </div>
            {p.tagline && <p class="pcard__tagline">{p.tagline}</p>}
            <div class="pcard__footer">
              {p.targets.length > 0 && (
                <div class="pcard__platforms">
                  {uniquePlatforms(p.targets).map((pl) => (
                    <span key={pl} class="pcard__platform">
                      {pl}
                    </span>
                  ))}
                </div>
              )}
              <div class="pcard__meta">
                {p.license && <span class="pcard__license">{p.license}</span>}
                {p.tags.length > 0 && (
                  <span class="pcard__tags">
                    {p.tags.map((t) => (
                      <span key={t} class="tag">
                        {t}
                      </span>
                    ))}
                  </span>
                )}
              </div>
            </div>
          </button>
        ))}
      </div>
    </div>
  );
}

// ── Detail View ──

function ProductDetailView({ bucket, onBack }: { bucket: string; onBack: () => void }) {
  const [detail, setDetail] = useState<ProductDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const detected = detectPlatform();

  useEffect(() => {
    setLoading(true);
    setError('');
    getProductDetail(bucket)
      .then(setDetail)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : 'Failed to load'))
      .finally(() => setLoading(false));
  }, [bucket]);

  if (loading) {
    return (
      <div class="products">
        <div class="empty-state">
          <span class="spinner" />
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div class="products">
        <button class="btn btn--muted btn--sm pd__back" id="btn-product-back" onClick={onBack}>
          <ChevronLeft size={13} /> products
        </button>
        <p class="error-msg">{error}</p>
      </div>
    );
  }

  if (!detail) return null;

  const latest = detail.versions.length > 0 ? detail.versions[0] : null;
  const detectedFiles = latest && detected ? (latest.targets[detected] ?? null) : null;
  const detectedFile = detectedFiles ? pickFile(detectedFiles) : null;

  return (
    <div class="products">
      <button class="btn btn--muted btn--sm pd__back" id="btn-product-back" onClick={onBack}>
        <ChevronLeft size={13} /> products
      </button>

      {/* Header */}
      <div class="pd__header">
        <div class="pd__title-row">
          <Package size={22} class="pd__pkg-icon" />
          <h1 class="pd__name">{detail.name}</h1>
          {latest && <code class="pd__latest">{latest.version}</code>}
        </div>
        {detail.tagline && <p class="pd__tagline">{detail.tagline}</p>}
      </div>

      {/* Meta bar */}
      <div class="pd__meta-bar">
        {detail.license && (
          <span class="pd__meta-item">
            <Tag size={12} /> {detail.license}
          </span>
        )}
        {detail.homepage && (
          <a
            class="pd__meta-item pd__link"
            href={detail.homepage}
            target="_blank"
            rel="noopener noreferrer"
          >
            <ExternalLink size={12} /> homepage
          </a>
        )}
        {detail.tags.length > 0 && (
          <span class="pd__meta-tags">
            {detail.tags.map((t) => (
              <span key={t} class="tag">
                {t}
              </span>
            ))}
          </span>
        )}
      </div>

      {/* Description (plain text from product.yaml) */}
      {detail.description && !detail.readme && (
        <pre class="pd__description">{detail.description.trim()}</pre>
      )}

      {/* README.md — rendered markdown */}
      {detail.readme && <Markdown content={detail.readme} class="pd__readme" />}

      {/* Quick install for detected platform */}
      {detectedFile && (
        <div class="pd__install">
          <span class="pd__install-label">
            quick install — {platformLabel(detected)}
          </span>
          <a
            class="btn pd__install-btn"
            id="btn-quick-install"
            href={dlUrl(bucket, 'latest', detected, detectedFile.name)}
            download={detectedFile.name}
          >
            <Download size={15} />
            {detectedFile.name}
            {detectedFile.size > 0 && (
              <span class="pd__file-size">{formatSize(detectedFile.size)}</span>
            )}
          </a>
        </div>
      )}

      {/* RELEASE.md — rendered markdown */}
      {detail.release_doc && (
        <div class="pd__section">
          <span class="pd__section-title">release notes</span>
          <Markdown content={detail.release_doc} class="pd__release-doc" />
        </div>
      )}

      {/* Version history */}
      {detail.versions.length > 0 && (
        <div class="pd__versions">
          <span class="pd__section-title">versions</span>
          <VersionBlock
            key={detail.versions[0].version}
            bucket={bucket}
            version={detail.versions[0]}
            isLatest={true}
            detected={detected}
          />
          {detail.versions.length > 1 && (
            <>
              <span class="pd__section-title pd__section-title--sub">older versions</span>
              {detail.versions.slice(1).map((v) => (
                <VersionBlock
                  key={v.version}
                  bucket={bucket}
                  version={v}
                  isLatest={false}
                  detected={detected}
                />
              ))}
            </>
          )}
        </div>
      )}
    </div>
  );
}

// ── Version Block ──

function VersionBlock({
  bucket,
  version: v,
  isLatest,
  detected,
}: {
  bucket: string;
  version: ProductDetail['versions'][0];
  isLatest: boolean;
  detected: string;
}) {
  const targets = Object.entries(v.targets).sort(([a], [b]) => a.localeCompare(b));

  return (
    <div class="vblock" data-version={v.version}>
      <div class="vblock__header">
        <span class="vblock__name">{v.version}</span>
        {v.date && <span class="vblock__date">{formatDate(v.date)}</span>}
        {isLatest && <span class="vblock__badge">latest</span>}
      </div>

      {v.notes && <pre class="vblock__notes">{v.notes.trim()}</pre>}
      {v.release_notes && <Markdown content={v.release_notes} class="vblock__release-notes" />}

      {targets.length > 0 && (
        <table class="vblock__table">
          <tbody>
            {targets.map(([target, files]) => (
              <tr
                key={target}
                class={target === detected ? 'vblock__row--detected' : ''}
                data-target={target}
              >
                <td class="vblock__target">{platformLabel(target)}</td>
                <td class="vblock__files">
                  {files.map((f) => (
                    <a
                      key={f.name}
                      class="btn btn--muted btn--sm"
                      href={dlUrl(bucket, v.version, target, f.name)}
                      download={f.name}
                    >
                      <Download size={11} /> {f.name}
                      {f.size > 0 && (
                        <span class="pd__file-size">{formatSize(f.size)}</span>
                      )}
                    </a>
                  ))}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

function dlUrl(bucket: string, version: string, target: string, file: string): string {
  return `/rs/${encodeURIComponent(bucket)}/${encodeURIComponent(version)}/${encodeURIComponent(target)}/${encodeURIComponent(file)}`;
}
