import { useState, useEffect } from 'preact/hooks';
import { FileBrowser } from './components/FileBrowser';
import { AdminPage } from './components/AdminPage';
import { ReleasePage } from './components/ReleasePage';
import { LoginModal } from './components/LoginModal';
import { hasReleaseScope } from './api';

type Page = 'browser' | 'admin' | 'releases';

function getPage(): Page {
  if (window.location.hash.startsWith('#/admin')) return 'admin';
  if (window.location.hash.startsWith('#/releases')) return 'releases';
  return 'browser';
}

export function App() {
  const [page, setPage] = useState<Page>(getPage);
  const [jwt, setJwt] = useState<string | null>(() => localStorage.getItem('dl_jwt'));
  const [showLogin, setShowLogin] = useState(false);

  useEffect(() => {
    const onHashChange = () => setPage(getPage());
    window.addEventListener('hashchange', onHashChange);
    return () => window.removeEventListener('hashchange', onHashChange);
  }, []);

  function navigate(p: Page) {
    if (p === 'admin') window.location.hash = '/admin';
    else if (p === 'releases') window.location.hash = '/releases';
    else window.location.hash = '/';
  }

  function handleLogin(token: string) {
    setJwt(token);
    localStorage.setItem('dl_jwt', token);
    setShowLogin(false);
  }

  function handleLogout() {
    setJwt(null);
    localStorage.removeItem('dl_jwt');
  }

  return (
    <div id="app-root">
      <header class="topbar">
        <div class="topbar__left">
          <span class="topbar__logo">dl</span>
          <nav class="topbar__nav" aria-label="main">
            <button
              class={`topbar__nav-btn${page === 'browser' ? ' topbar__nav-btn--active' : ''}`}
              id="nav-files"
              onClick={() => navigate('browser')}
            >
              files
            </button>
            {jwt && hasReleaseScope(jwt) && (
              <button
                class={`topbar__nav-btn${page === 'releases' ? ' topbar__nav-btn--active' : ''}`}
                id="nav-releases"
                onClick={() => navigate('releases')}
              >
                releases
              </button>
            )}
            <button
              class={`topbar__nav-btn topbar__nav-btn--admin${page === 'admin' ? ' topbar__nav-btn--active' : ''}`}
              id="nav-admin"
              onClick={() => navigate('admin')}
            >
              admin
            </button>
          </nav>
        </div>
        <div class="topbar__right">
          {jwt ? (
            <button class="btn btn--muted btn--sm" id="btn-logout" onClick={handleLogout}>
              logout
            </button>
          ) : (
            <button class="btn btn--sm" id="btn-login" onClick={() => setShowLogin(true)}>
              login
            </button>
          )}
        </div>
      </header>

      <main class="main-content">
        {page === 'browser' && (
          <FileBrowser jwt={jwt} onLoginRequired={() => setShowLogin(true)} />
        )}
        {page === 'releases' && jwt && hasReleaseScope(jwt) && <ReleasePage jwt={jwt} />}
        {page === 'admin' && <AdminPage />}
      </main>

      {showLogin && (
        <LoginModal onLogin={handleLogin} onClose={() => setShowLogin(false)} />
      )}
    </div>
  );
}
