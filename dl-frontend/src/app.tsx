import { useState, useEffect } from 'preact/hooks';
import { FileBrowser } from './components/FileBrowser';
import { AdminPage } from './components/AdminPage';
import { ReleasePage } from './components/ReleasePage';
import { ReleaseLandingPage } from './components/ReleaseLandingPage';
import { LoginModal } from './components/LoginModal';
import { hasReleaseScope } from './api';

type Page = 'browser' | 'admin' | 'releases' | 'release-landing';

interface RouteState {
  page: Page;
  filePath: string;
  releaseBucket: string;
}

function parsePath(pathname: string): RouteState {
  if (pathname.startsWith('/admin')) return { page: 'admin', filePath: '/', releaseBucket: '' };
  if (pathname.startsWith('/releases')) return { page: 'releases', filePath: '/', releaseBucket: '' };
  if (pathname.startsWith('/files/')) return { page: 'browser', filePath: pathname.slice('/files'.length), releaseBucket: '' };
  const releaseMatch = pathname.match(/^\/r\/([^/]+)\/?$/);
  if (releaseMatch) return { page: 'release-landing', filePath: '/', releaseBucket: releaseMatch[1] };
  return { page: 'browser', filePath: '/', releaseBucket: '' };
}

function filePathToUrl(p: string): string {
  return p === '/' ? '/' : `/files${p}`;
}

export function App() {
  const [route, setRoute] = useState<RouteState>(() => parsePath(window.location.pathname));
  const [jwt, setJwt] = useState<string | null>(() => localStorage.getItem('dl_jwt'));
  const [showLogin, setShowLogin] = useState(false);

  useEffect(() => {
    const onPop = () => setRoute(parsePath(window.location.pathname));
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);

  function push(url: string, newRoute: RouteState) {
    history.pushState(null, '', url);
    setRoute(newRoute);
  }

  function navigatePage(p: Page) {
    if (p === 'admin') push('/admin', { page: 'admin', filePath: '/', releaseBucket: '' });
    else if (p === 'releases') push('/releases', { page: 'releases', filePath: '/', releaseBucket: '' });
    else {
      const fp = route.page === 'browser' ? route.filePath : '/';
      push(filePathToUrl(fp), { page: 'browser', filePath: fp, releaseBucket: '' });
    }
  }

  function navigateFile(filePath: string) {
    push(filePathToUrl(filePath), { page: 'browser', filePath, releaseBucket: '' });
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

  const { page, filePath, releaseBucket } = route;

  return (
    <div id="app-root">
      <header class="topbar">
        <div class="topbar__left">
          <span class="topbar__logo">dl</span>
          <nav class="topbar__nav" aria-label="main">
            <button
              class={`topbar__nav-btn${page === 'browser' ? ' topbar__nav-btn--active' : ''}`}
              id="nav-files"
              onClick={() => navigatePage('browser')}
            >
              files
            </button>
            {jwt && hasReleaseScope(jwt) && (
              <button
                class={`topbar__nav-btn${page === 'releases' ? ' topbar__nav-btn--active' : ''}`}
                id="nav-releases"
                onClick={() => navigatePage('releases')}
              >
                releases
              </button>
            )}
            <button
              class={`topbar__nav-btn topbar__nav-btn--admin${page === 'admin' ? ' topbar__nav-btn--active' : ''}`}
              id="nav-admin"
              onClick={() => navigatePage('admin')}
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
          <FileBrowser
            jwt={jwt}
            path={filePath}
            onNavigate={navigateFile}
            onLoginRequired={() => setShowLogin(true)}
          />
        )}
        {page === 'releases' && jwt && hasReleaseScope(jwt) && <ReleasePage jwt={jwt} />}
        {page === 'release-landing' && <ReleaseLandingPage bucket={releaseBucket} />}
        {page === 'admin' && <AdminPage />}
      </main>

      {showLogin && (
        <LoginModal onLogin={handleLogin} onClose={() => setShowLogin(false)} />
      )}
    </div>
  );
}
