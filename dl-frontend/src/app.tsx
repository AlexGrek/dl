import { useState, useEffect } from 'preact/hooks';
import { FileBrowser } from './components/FileBrowser';
import { AdminPage } from './components/AdminPage';
import { ReleasePage } from './components/ReleasePage';
import { ReleaseLandingPage } from './components/ReleaseLandingPage';
import { ProductsPage } from './components/ProductsPage';
import { LoginModal } from './components/LoginModal';
import { hasReleaseScope } from './api';

type Page = 'products' | 'browser' | 'admin' | 'releases' | 'release-landing';

interface RouteState {
  page: Page;
  filePath: string;
  releaseBucket: string;
  productBucket: string;
}

function parsePath(pathname: string): RouteState {
  const base = { filePath: '/', releaseBucket: '', productBucket: '' };
  if (pathname.startsWith('/admin')) return { ...base, page: 'admin' };
  if (pathname.startsWith('/releases')) return { ...base, page: 'releases' };
  if (pathname.startsWith('/files/')) return { ...base, page: 'browser', filePath: pathname.slice('/files'.length) };
  if (pathname === '/files') return { ...base, page: 'browser' };
  const productMatch = pathname.match(/^\/products\/([^/]+)\/?$/);
  if (productMatch) return { ...base, page: 'products', productBucket: decodeURIComponent(productMatch[1]) };
  if (pathname.startsWith('/products')) return { ...base, page: 'products' };
  const releaseMatch = pathname.match(/^\/r\/([^/]+)\/?$/);
  if (releaseMatch) return { ...base, page: 'release-landing', releaseBucket: releaseMatch[1] };
  // Default: products
  return { ...base, page: 'products' };
}

function filePathToUrl(p: string): string {
  return p === '/' ? '/files' : `/files${p}`;
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

  const base = { filePath: '/', releaseBucket: '', productBucket: '' };

  function navigatePage(p: Page) {
    if (p === 'admin') push('/admin', { ...base, page: 'admin' });
    else if (p === 'releases') push('/releases', { ...base, page: 'releases' });
    else if (p === 'products') push('/products', { ...base, page: 'products' });
    else {
      const fp = route.page === 'browser' ? route.filePath : '/';
      push(filePathToUrl(fp), { ...base, page: 'browser', filePath: fp });
    }
  }

  function navigateFile(filePath: string) {
    push(filePathToUrl(filePath), { ...base, page: 'browser', filePath });
  }

  function navigateProduct(bucket: string) {
    if (bucket) {
      push(`/products/${encodeURIComponent(bucket)}`, { ...base, page: 'products', productBucket: bucket });
    } else {
      push('/products', { ...base, page: 'products' });
    }
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

  const { page, filePath, releaseBucket, productBucket } = route;

  return (
    <div id="app-root">
      <header class="topbar">
        <div class="topbar__left">
          <span class="topbar__logo">dl</span>
          <nav class="topbar__nav" aria-label="main">
            <button
              class={`topbar__nav-btn${page === 'products' ? ' topbar__nav-btn--active' : ''}`}
              id="nav-products"
              onClick={() => navigatePage('products')}
            >
              products
            </button>
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
        {page === 'products' && (
          <ProductsPage bucket={productBucket} onNavigate={navigateProduct} />
        )}
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
