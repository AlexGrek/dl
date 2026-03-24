import { useState, useEffect } from 'preact/hooks';
import { Download, Package, Link } from 'lucide-preact';
import { type ReleaseInfo, type ReleaseFile, getReleaseInfo, detectPlatform, formatSize } from '../api';
import { InstallBlock } from './InstallBlock';

interface Props {
  bucket: string;
}

// Pick the most likely download file in a target (largest non-.sha* file).
function pickFile(files: ReleaseFile[]): ReleaseFile | null {
  const candidates = files.filter((f) => !f.name.endsWith('.sha256') && !f.name.endsWith('.sha512'));
  if (!candidates.length) return files[0] ?? null;
  return candidates.reduce((a, b) => (a.size >= b.size ? a : b));
}

function CopyUrlBtn({ url, target }: { url: string; target: string }) {
  const [copied, setCopied] = useState(false);
  function copy() {
    navigator.clipboard.writeText(url).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }
  return (
    <button
      class={`btn btn--muted btn--sm${copied ? ' btn--copied' : ''}`}
      onClick={copy}
      title="Copy download URL"
      data-action="copy-url"
      data-target={target}
    >
      {copied ? <span class="btn__copied-label">copied</span> : <Link size={12} />}
    </button>
  );
}

function platformLabel(target: string): string {
  const map: Record<string, string> = {
    'linux-amd64':   'Linux (x86-64)',
    'linux-arm64':   'Linux (ARM64)',
    'darwin-amd64':  'macOS (Intel)',
    'darwin-arm64':  'macOS (Apple Silicon)',
    'windows-amd64': 'Windows (x86-64)',
    'windows-arm64': 'Windows (ARM64)',
  };
  return map[target] ?? target;
}

export function ReleaseLandingPage({ bucket }: Props) {
  const [info, setInfo] = useState<ReleaseInfo | null>(null);
  const [error, setError] = useState('');
  const detected = detectPlatform();

  useEffect(() => {
    getReleaseInfo(bucket)
      .then(setInfo)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : 'Failed to load'));
  }, [bucket]);

  if (error) {
    return (
      <div class="landing">
        <p class="error-msg">Release bucket "{bucket}" not found.</p>
      </div>
    );
  }

  if (!info) {
    return (
      <div class="landing">
        <div class="empty-state"><span class="spinner" /></div>
      </div>
    );
  }

  const targets = Object.entries(info.targets ?? {}).sort(([a], [b]) => a.localeCompare(b));
  const detectedFiles = detected ? (info.targets[detected] ?? null) : null;
  const detectedFile = detectedFiles ? pickFile(detectedFiles) : null;

  function downloadUrl(target: string, file: ReleaseFile): string {
    return `/rs/${encodeURIComponent(bucket)}/latest/${encodeURIComponent(target)}/${encodeURIComponent(file.name)}`;
  }

  function absDownloadUrl(target: string, filename: string): string {
    return `${window.location.origin}/rs/${encodeURIComponent(bucket)}/latest/${encodeURIComponent(target)}/${encodeURIComponent(filename)}`;
  }

  return (
    <div class="landing">
      <div class="landing__hero">
        <Package size={32} class="landing__icon" />
        <h1 class="landing__title">{bucket}</h1>
        <p class="landing__version">latest: <code>{info.latest}</code></p>
      </div>

      <div class="landing__primary">
        {detectedFile ? (
          <>
            <a
              class="btn landing__download-btn"
              id="btn-download-detected"
              href={downloadUrl(detected, detectedFile)}
              download={detectedFile.name}
            >
              <Download size={16} />
              Download for {platformLabel(detected)}
            </a>
            <p class="landing__file-hint">
              {detectedFile.name}
              {detectedFile.size > 0 && <> · {formatSize(detectedFile.size)}</>}
            </p>
          </>
        ) : (
          <p class="landing__no-detect">Platform not detected — select below.</p>
        )}
        {targets.length > 0 && (
          <InstallBlock
            bucket={bucket}
            version="latest"
            targets={info.targets}
            detected={detected}
            buildUrl={absDownloadUrl}
          />
        )}
      </div>

      {targets.length > 0 && (
        <div class="landing__targets">
          <p class="landing__targets-title">all downloads — {info.latest}</p>
          <table class="landing__table">
            <tbody>
              {targets.map(([target, files]) => {
                const file = pickFile(files);
                if (!file) return null;
                const isDetected = target === detected;
                return (
                  <tr
                    key={target}
                    class={isDetected ? 'landing__row--detected' : ''}
                    data-target={target}
                  >
                    <td class="landing__target-name">
                      {platformLabel(target)}
                      {isDetected && <span class="landing__detected-badge">detected</span>}
                    </td>
                    <td class="landing__file-name">{file.name}</td>
                    <td class="landing__file-size">{file.size > 0 ? formatSize(file.size) : '—'}</td>
                    <td class="landing__actions">
                      <a
                        class="btn btn--sm"
                        href={downloadUrl(target, file)}
                        download={file.name}
                        data-action="download"
                        data-target={target}
                      >
                        <Download size={13} />
                      </a>
                      <CopyUrlBtn url={absDownloadUrl(target, file.name)} target={target} />
                      {files.filter((f) => f !== file).map((extra) => (
                        <a
                          key={extra.name}
                          class="btn btn--muted btn--sm"
                          href={downloadUrl(target, extra)}
                          download={extra.name}
                          data-action="download-extra"
                          title={extra.name}
                        >
                          {extra.name.split('.').pop()}
                        </a>
                      ))}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
