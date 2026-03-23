---
description: Preact frontend context for the dl project. Apply when working on dl-frontend/ — components, CSS, Vite config, or API integration for the file browser and admin UI.
---

# Frontend Skill — dl

## Stack

| Layer | Tech |
|---|---|
| Build | Vite 8 |
| UI lib | Preact 10 (TypeScript, JSX) |
| Styling | Pure custom CSS (no frameworks) |
| Theme | Console-like dark (monospace, no animations) |
| Dev | `npm run dev` from `dl-frontend/`, Vite proxies `/api` → backend |
| Icons | Lucide |

### Preact Compat

React/ReactDOM are aliased to `preact/compat` in both `vite.config.ts` and `tsconfig.app.json`. Import from `preact/hooks`, not `react`.

---

## Design Language

- **Font**: monospace everywhere
- **Theme**: dark by default, light mode via CSS custom properties
- **Animations**: none — static transitions only
- **Mobile-first**: flexible layout, no fixed widths
- **Density**: compact UI, minimal chrome

### CSS Custom Properties (define in `index.css`)

```css
:root {
  --bg:       /* dark background */
  --bg-alt:   /* slightly lighter surface */
  --fg:       /* primary text */
  --fg-muted: /* secondary/label text */
  --accent:   /* primary accent color */
  --border:   /* border color */
  --radius:   /* global border radius */
}
```

---

## Component Conventions

- **All interactive elements** must have `id`, `class`, or `data-*` attributes — required for testability
- **BEM-style class names**: `.block`, `.block__element`, `.block--modifier`
- **No inline styles** except for dynamic values that cannot be expressed in CSS
- **Reusable components** built from scratch — no component library imports
- **File structure**: one component per file, co-located CSS module or scoped `<style>` if needed

---

## Key Pages (planned)

| Route | Description |
|---|---|
| `/` | File browser — list files, upload button |
| `/admin` | Admin page — generate API keys from master key |
| `/rs/:bucket/:os_arch/` | Release bucket browser |

---

## API Integration

- Dev: Vite proxies `/api` → Go backend (configured in `vite.config.ts`)
- Auth: `Authorization: Bearer <jwt>` header on all `/api/v1` requests
- File listing: WebDAV PROPFIND at `/api/v1/wd/`
- Upload: PUT to `/api/v1/wd/{path}` or `/api/v1/release/{bucket}/{os_arch}/{file}`
- Download: direct link to `/d/{path}` or `/rs/{bucket}/{os_arch}/{file}` (no auth)

---

## Do / Don't

| Do | Don't |
|---|---|
| `id`/`class`/`data-*` on every interactive element | Anonymous or unlabeled buttons/inputs |
| Custom CSS classes with BEM | Inline `style={{}}` for layout |
| `preact/hooks` imports | `react` imports |
| Monospace, compact, functional UI | Rounded cards, shadows, animations |
| Mobile-first CSS (min-width breakpoints) | Desktop-first (max-width breakpoints) |
| Reusable components built in-project | Third-party component library imports |
