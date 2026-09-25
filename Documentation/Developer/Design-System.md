# Design system and UI conventions

## Direction

The interface is dark-only, desktop-first, and usable on tablets and phones. Use the current Sealos visual direction as inspiration: clean dark surfaces, compact icon navigation, subtle boundaries, spacious cards and panels, blue accent, and low visual noise. This is a product-specific design, not a pixel copy.

Use one clear primary action per view. Keep the Projects dashboard focused on project cards; project details and global settings open in right-side modal drawers. Put future detail and destructive actions in contextual drawers or dialogs. The product language is English only; no theme switcher or localization framework is planned.

## Token architecture

`web/src/styles/tokens.css` defines primitive palette values separately from semantic tokens. Components consume semantic tokens rather than hard-coded colors, spacing, radii, shadows, or animation timings. Add new palette values and semantic aliases there before using them in feature CSS.

Recommended naming groups:

| Group | Examples | Purpose |
| --- | --- | --- |
| Canvas and surfaces | `--color-canvas`, `--color-surface-1`, `--color-surface-2` | Page, cards, overlays. |
| Content | `--color-text-primary`, `--color-text-muted`, `--color-border` | Legible text and boundaries. |
| Intent | `--color-accent`, `--color-success`, `--color-warning`, `--color-danger` | Actions and status. |
| Layout | `--space-1` through `--space-8`, `--radius-sm` | Reusable rhythm and shape. |
| Motion and elevation | `--duration-fast`, `--duration-normal`, `--shadow-panel` | Consistent transitions and layering. Add z-index tokens when overlays are implemented. |

Use `:root { color-scheme: dark; }` and `<meta name="color-scheme" content="dark">` so native controls and the initial page canvas match the sole supported theme. Keep a single source of truth for dark tokens; do not create unused light-theme overrides. New themes, if ever approved, should override semantic tokens without rewriting component CSS.

The current tokens provide a UI and monospaced font stack, spacing scale, one small corner radius, and transition durations. Use `--radius-sm` for every rounded surface, including cards, badges, controls, and panels. Prefer `rem` for type and spacing. Extend the tokens when a repeated visual value appears; keep one-off layout geometry in a local CSS Module if it does not represent a reusable rule.

The account avatar is intentionally circular to identify the user; this is the only exception to the small-radius rule.

## Component and CSS rules

- Put global reset, tokens, and layout primitives in `web/src/styles/`. The current feature styles live in `web/src/App.module.css`; split them beside new components as they are extracted. Avoid global selectors that style arbitrary descendants in unrelated features.
- Extract shared components for buttons, icon buttons, status badges, project cards, stat items, forms, drawers, confirmation dialogs, tabs, toasts, empty/error states, and log/terminal surfaces when a pattern is reused. The current app shell has a small inline brand, authentication form, and dashboard layout.
- Variants should be explicit component properties (`intent`, `size`, `loading`, `disabled`) and map to token-based CSS classes. A disabled or loading action must have a clear text explanation when the reason matters.
- Keep data fetching and Docker-specific mapping outside presentational components. Views consume typed application models, not raw Engine responses.
- Use native semantic elements. Prefer a native `<dialog>` for modal confirmation and an accessible dialog pattern for detail drawers. Icon-only controls need accessible names; opening and closing overlays must preserve sensible focus.
- Never indicate running, unhealthy, or failed state by color alone. Pair color with text or an icon. Provide visible `:focus-visible` styles, sufficient contrast, and reduced-motion behavior.

## Responsive layout

Use content-driven layouts with CSS Grid/Flexbox and a small number of documented breakpoints. Initial layout targets:

- **Desktop (about 1024px and above):** collapsible left sidebar, multi-column card grid, right-side detail drawers.
- **Tablet (about 640–1023px):** the same sidebar states with fewer card columns; drawers overlay rather than shrink the project grid.
- **Mobile (below about 640px):** a narrow icon sidebar, an expanded sidebar that overlays the page, single-column cards, and full-width drawers.

The wide sidebar shows the logo and navigation labels, with Settings and the account at the bottom. The narrow state shows icons and a circular account initial. The width preference is stored in the browser. Keep the main content inert while the mobile sidebar is expanded; Escape and the backdrop close it. Native modal dialogs provide focus handling and Escape dismissal for drawers.

These values are starting points, not device identities. Check narrow screens, 200% zoom, long project names, many services, and keyboard-only navigation. Logs and terminals should scroll within their own bounded panels rather than widening the page.

## Interaction and copy

- Show a project's aggregate state, health, container count, CPU, memory, and uptime on its card. Keep the card action target and per-card action menu distinct.
- The **New Project** flow is a source step, variable entry, validation/preview, then confirmation. Show service/image/port/volume/network changes before a URL sync or adoption.
- Use present-tense, concise English labels: “Pull images”, “Update project”, “Remove project”. Destructive confirmations must name the affected project and whether volumes will be deleted.
- Mask environment and configuration values that may contain credentials. Reveal only after an explicit action in an authenticated session; avoid copying sensitive values into toasts or URLs.
- Surface job progress and errors in a persistent, reconnectable view. Do not treat a request acknowledgment as successful completion.

## Visual QA for future UI work

Verify desktop, tablet, mobile, keyboard traversal, focus return, contrast, reduced motion, loading/empty/error states, long content, and slow event streams. Prefer native browser behaviors over custom controls when they meet the design need.
