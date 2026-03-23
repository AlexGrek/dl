import { useState } from 'preact/hooks';
import { getToken } from '../api';

interface Props {
  onLogin: (jwt: string) => void;
  onClose: () => void;
}

export function LoginModal({ onLogin, onClose }: Props) {
  const [apiKey, setApiKey] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e: Event) {
    e.preventDefault();
    if (!apiKey.trim()) return;
    setLoading(true);
    setError('');
    try {
      const token = await getToken(apiKey.trim());
      onLogin(token);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally {
      setLoading(false);
    }
  }

  function handleOverlayClick(e: MouseEvent) {
    if ((e.target as HTMLElement).classList.contains('modal-overlay')) onClose();
  }

  return (
    <div class="modal-overlay" onClick={handleOverlayClick}>
      <div class="modal" role="dialog" aria-modal="true" aria-labelledby="login-title">
        <p class="modal__title" id="login-title">login</p>
        <form onSubmit={handleSubmit}>
          <div class="modal__row">
            <label class="modal__label" for="input-apikey">api key</label>
            <input
              id="input-apikey"
              class="input"
              type="password"
              placeholder="dlk_..."
              value={apiKey}
              onInput={(e) => setApiKey((e.target as HTMLInputElement).value)}
              autoFocus
              autoComplete="current-password"
            />
          </div>
          {error && <p class="modal__error">{error}</p>}
          <div class="modal__actions">
            <button type="button" class="btn btn--muted" id="btn-login-cancel" onClick={onClose}>
              cancel
            </button>
            <button type="submit" class="btn" id="btn-login-submit" disabled={loading || !apiKey.trim()}>
              {loading ? <span class="spinner" /> : 'login'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
