# Design system and UI conventions

**Status:** implementation guide. No UI component or stylesheet exists yet.

## Direction

The interface is dark-only, desktop-first, and usable on tablets and phones. Use the current Sealos visual direction as inspiration: clean dark surfaces, compact icon navigation, subtle boundaries, spacious cards and panels, blue accent, and low visual noise. This is a product-specific design, not a pixel copy.

Use one clear primary action per view. Keep operational status visible without making the dashboard dense. Put detail and destructive actions in contextual drawers or dialogs. The product language is English only; no theme switcher or localization framework is planned.

## Token architecture

Create `web/src/styles/tokens.css` before adding feature styles. Define primitive palette values separately from semantic tokens. Components must consume semantic tokens rather than hard-coded colors, spacing, radii, shadows, or animation timings.

Recommended naming groups:

| Group | Examples | Purpose |
| --- | --- | --- |
| Canvas and surfaces | `--color-canvas`, `--color-surface-1`, `--color-surface-2` | Page, cards, overlays. |
| Content | `--color-text-primary`, `--color-text-muted`, `--color-border` | Legible text and boundaries. |
| Intent | `--color-accent`, `--color-success`, `--color-warning`, `--color-danger` | Actions and status. |
| Layout | `--space-1` through `--space-8`, `--radius-sm`, `--radius-lg` | Reusable rhythm and shape. |
| Motion and elevation | `--duration-fast`, `--shadow-panel`, `--z-dialog` | Consistent transitions and layering. |

Use `:root { color-scheme: dark; }` and `<meta name="color-scheme" content="dark">` so native controls and the initial page canvas match the sole supported theme. Keep a single source of truth for dark tokens; do not create unused light-theme overrides. New themes, if ever approved, should override semantic tokens without rewriting component CSS.

Define typography tokens for a system UI font stack, a monospaced stack for logs/terminal/YAML, and a small, consistent type scale. Prefer `rem` for type and spacing. Token values should be chosen and checked against real interface content when UI implementation begins; the names above are conventions, not final visual values.

## Component and CSS rules

- Put global reset, tokens, and layout primitives in `web/src/styles/`. Keep feature styles in CSS Modules beside the relevant component. Avoid global selectors that style arbitrary descendants in unrelated features.
- Establish shared components for buttons, icon buttons, status badges, project cards, stat items, forms, drawers, confirmation dialogs, tabs, toasts, empty/error states, and log/terminal surfaces before duplicating a pattern.
- Variants should be explicit component properties (`intent`, `size`, `loading`, `disabled`) and map to token-based CSS classes. A disabled or loading action must have a clear text explanation when the reason matters.
- Keep data fetching and Docker-specific mapping outside presentational components. Views consume typed application models, not raw Engine responses.
- Use native semantic elements. Prefer a native `<dialog>` for modal confirmation and an accessible dialog pattern for detail drawers. Icon-only controls need accessible names; opening and closing overlays must preserve sensible focus.
- Never indicate running, unhealthy, or failed state by color alone. Pair color with text or an icon. Provide visible `:focus-visible` styles, sufficient contrast, and reduced-motion behavior.

## Responsive layout

Use content-driven layouts with CSS Grid/Flexbox and a small number of documented breakpoints. Initial layout targets:

- **Desktop (about 1024px and above):** compact fixed sidebar, multi-column card grid, right-side detail drawer.
- **Tablet (about 640–1023px):** narrower navigation and fewer card columns; detail panel must not obscure essential navigation.
- **Mobile (below about 640px):** single-column cards, compact navigation, full-screen detail view and dialogs where needed.

These values are starting points, not device identities. Check narrow screens, 200% zoom, long project names, many services, and keyboard-only navigation. Logs and terminals should scroll within their own bounded panels rather than widening the page.

## Interaction and copy

- Show a project's aggregate state, health, container count, CPU, memory, and uptime on its card. Keep the card action target and per-card action menu distinct.
- The **New Project** flow is a source step, variable entry, validation/preview, then confirmation. Show service/image/port/volume/network changes before a URL sync or adoption.
- Use present-tense, concise English labels: “Pull images”, “Update project”, “Remove project”. Destructive confirmations must name the affected project and whether volumes will be deleted.
- Mask environment and configuration values that may contain credentials. Reveal only after an explicit action in an authenticated session; avoid copying sensitive values into toasts or URLs.
- Surface job progress and errors in a persistent, reconnectable view. Do not treat a request acknowledgment as successful completion.

## Visual QA for future UI work

Verify desktop, tablet, mobile, keyboard traversal, focus return, contrast, reduced motion, loading/empty/error states, long content, and slow event streams. Prefer native browser behaviors over custom controls when they meet the design need.
