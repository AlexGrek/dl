import { useState } from 'preact/hooks';
import { Copy } from 'lucide-preact';
import { type ReleaseFile, formatSize } from '../api';

type Tab = 'curl' | 'wget' | 'powershell';

const TABS: { id: Tab; label: string }[] = [
  { id: 'curl',       label: 'curl' },
  { id: 'wget',       label: 'wget' },
  { id: 'powershell', label: 'powershell' },
];

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

/** Pick the primary download file (largest non-checksum). */
function pickFile(files: ReleaseFile[]): ReleaseFile | null {
  const candidates = files.filter(
    (f) => !f.name.endsWith('.sha256') && !f.name.endsWith('.sha512'),
  );
  if (!candidates.length) return files[0] ?? null;
  return candidates.reduce((a, b) => (a.size >= b.size ? a : b));
}

function buildCommand(tab: Tab, url: string, filename: string): string {
  switch (tab) {
    case 'curl':       return `curl -LO "${url}"`;
    case 'wget':       return `wget "${url}"`;
    case 'powershell': return `Invoke-WebRequest -Uri "${url}" -OutFile "${filename}"`;
  }
}

function CopyField({ value, id }: { value: string; id?: string }) {
  const [copied, setCopied] = useState(false);
  function copy() {
    navigator.clipboard.writeText(value).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }
  return (
    <div class="copy-field">
      <code class="copy-field__text" id={id}>{value}</code>
      <button
        class={`btn btn--sm copy-field__btn${copied ? ' btn--copied' : ''}`}
        onClick={copy}
        title="Copy"
        data-action="copy"
      >
        {copied ? <span class="btn__copied-label">copied</span> : <Copy size={12} />}
      </button>
    </div>
  );
}

interface Props {
  /** bucket name */
  bucket: string;
  /** resolved version string OR "latest" */
  version: string;
  /** map of target → files, same shape as ReleaseInfo.targets / VersionDetail.targets */
  targets: Record<string, ReleaseFile[]>;
  /** pre-selected target (from platform detection); empty string = no detection */
  detected: string;
  /** base URL builder: (target, filename) → absolute URL */
  buildUrl: (target: string, filename: string) => string;
}

export function InstallBlock({ targets, detected, buildUrl }: Props) {
  const targetKeys = Object.keys(targets).sort();
  if (!targetKeys.length) return null;

  const defaultTarget = detected && targets[detected] ? detected : targetKeys[0];
  const [selectedTarget, setSelectedTarget] = useState(defaultTarget);
  const [tab, setTab] = useState<Tab>('curl');

  const files = targets[selectedTarget] ?? [];
  const file = pickFile(files);
  if (!file) return null;

  const url = buildUrl(selectedTarget, file.name);
  const command = buildCommand(tab, url, file.name);

  return (
    <div class="install-block">
      <div class="install-block__header">
        <select
          class="input input--sm install-block__target-select"
          id="select-install-target"
          value={selectedTarget}
          onChange={(e) => setSelectedTarget((e.target as HTMLSelectElement).value)}
        >
          {targetKeys.map((t) => (
            <option key={t} value={t}>
              {platformLabel(t)}
              {t === detected ? ' ✓' : ''}
            </option>
          ))}
        </select>
        <span class="install-block__file-hint">
          {file.name}{file.size > 0 && <> · {formatSize(file.size)}</>}
        </span>
      </div>

      <div class="install-block__tabs" role="tablist">
        {TABS.map(({ id, label }) => (
          <button
            key={id}
            role="tab"
            class={`install-block__tab${tab === id ? ' install-block__tab--active' : ''}`}
            data-tab={id}
            onClick={() => setTab(id)}
          >
            {label}
          </button>
        ))}
      </div>

      <CopyField value={command} id={`cmd-${tab}-${selectedTarget}`} />
    </div>
  );
}
