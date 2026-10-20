# Design system and UI conventions

## Direction

The interface is dark-only, desktop-first, and usable on tablets and phones. Use neutral graphite surfaces, soft off-white text, compact icon navigation, subtle boundaries, and restrained spacing. Reserve the muted steel-blue accent for focus, selection, and small action details. Status colors are desaturated green, amber, and red with subdued backgrounds. Avoid blue-tinted page surfaces, gradients, decorative glows, oversized headings, decorative dashboard statistics, and marketing copy in operational views. The active drag destination is an intentional exception: a blue empty slot with a stronger border and a restrained glow identifies the future position.

Keep existing icon-only actions icon-only, including Refresh, New Project, sidebar controls, and container lifecycle controls. Preserve their accessible names and tooltips. Modernization should improve reading order, spacing, and state clarity without adding decorative UI or replacing action icons with text labels.

The product name is **Yard**, part of the **NoX** environment. Use a text-only Yard wordmark: large on authentication screens, with clear separation above the mobile form, and smaller in the sidebar. The collapsed sidebar retains a compact Yard name that yields to its Expand control on hover/focus. Do not restore the former N badge. The favicon is a white Y on its blue background. Repository, environment-variable and runtime identifiers retain their existing `nox-yard`/`NOX` names.

Use one clear primary action per view. Keep the Projects dashboard focused on projects in a grid or compact list; project details open in a right-side modal drawer and global settings open in a left-side modal drawer. Put future detail and destructive actions in contextual drawers or dialogs. The product language is English only; no theme switcher or localization framework is planned.

## Visual baseline for future changes

The approved direction is a quiet, modern Docker management interface designed for comfortable everyday use. Preserve its character when adding features. The user explicitly rejects generic "AI slop": decorative additions must not replace a clear, useful interface.

- **Dark surfaces:** use graphite and charcoal with small differences in brightness to distinguish the page, sidebar, cards, and controls. Do not turn the canvas navy or introduce saturated blue panels.
- **Comfortable contrast:** use soft off-white for primary text and readable gray for supporting text. Avoid pure-white expanses, neon accents, and excessively faint labels. Target at least 4.5:1 for normal text against its actual background, including muted copy and status labels.
- **Restrained color:** use the steel-blue accent sparingly for keyboard focus, selected controls, and small action details. Most buttons remain neutral. Reserve green, amber, and red for meaningful states; never use status colors as general decoration.
- **Icon actions:** retain the existing icon-only toolbar and lifecycle actions. Do not add visible text beside those icons to make them appear more prominent. Keep accessible names and tooltips descriptive. Existing form submissions and confirmation text remain part of their established flows.
- **Useful hierarchy:** emphasize project names, keep metadata secondary, and align metric labels and values consistently. Use spacing, type weight, and subtle borders before adding more color or containers.
- **Minimal decoration:** no gradient backgrounds, luminous shadows, glass effects, oversized pill controls, ornamental illustrations, extra summary cards, or promotional slogans in the dashboard. Avoid introducing animation solely to make the app feel modern.

Treat this as an evolution of the existing product, not a new landing-page design. New components should look at home beside the existing project cards and drawers.

## Token architecture

`web/src/styles/tokens.css` defines primitive palette values separately from semantic tokens. Components consume semantic tokens rather than hard-coded colors, spacing, radii, shadows, or animation timings. Add new palette values and semantic aliases there before using them in feature CSS.

Recommended naming groups:

| Group | Examples | Purpose |
| --- | --- | --- |
| Canvas and surfaces | `--color-canvas`, `--color-surface-1`, `--color-surface-2` | Page, cards, overlays. |
| Content | `--color-text-primary`, `--color-text-muted`, `--color-border` | Legible text and boundaries. |
| Intent | `--color-accent`, `--color-success`, `--color-warning`, `--color-danger` | Actions and status. |
| Subdued states | `--color-accent-subtle`, `--color-accent-border`, `--color-success-subtle`, `--color-warning-subtle` | Selection and status backgrounds without saturated fills. |
| Layout | `--space-1` through `--space-16`, `--radius-sm`, `--radius-md` | Reusable rhythm and shape. |
| Motion and elevation | `--duration-fast`, `--duration-normal`, `--shadow-card`, `--shadow-panel`, `--color-backdrop` | Subtle card depth, overlay separation, and consistent transitions. |

Use `:root { color-scheme: dark; }` and `<meta name="color-scheme" content="dark">` so native controls and the initial page canvas match the sole supported theme. Keep a single source of truth for dark tokens; do not create unused light-theme overrides. New themes, if ever approved, should override semantic tokens without rewriting component CSS.

The current tokens provide a UI and monospaced font stack, spacing scale, two corner radii, and transition durations. Use `--radius-sm` for controls and badges and `--radius-md` for project cards, overview panels, and confirmation dialogs. Cards use `--shadow-card`; overlays use `--shadow-panel`. Prefer `rem` for type and spacing. Extend the tokens when a repeated visual value appears; keep one-off layout geometry in a local CSS Module if it does not represent a reusable rule. Reduced-motion preferences set shared transition durations to zero and disable drawer entrance animations explicitly.

The account avatar and small status dots are intentionally circular. Status dots always accompany a text label.

### Styling source files

| File | Responsibility |
| --- | --- |
| [`tokens.css`](../../web/src/styles/tokens.css) | Canonical palette, semantic colors, spacing, radii, shadows, motion, and forced-color overrides. Read actual values here rather than maintaining a second palette in documentation. |
| [`global.css`](../../web/src/styles/global.css) | Base page styles, native control typography, focus outlines, text selection, and scroll locking. |
| [`App.module.css`](../../web/src/App.module.css) | Authentication, app shell, project cards, settings, details, dialogs, and log/terminal surfaces. |
| [`NewProjectDrawer.module.css`](../../web/src/NewProjectDrawer.module.css) | Source selection, Compose fields, environment forms, and deployment preview. |
| [`ProjectConfiguration.module.css`](../../web/src/ProjectConfiguration.module.css) | Inline variable controls and collapsed host-file editors. |
| [`ProjectSchedule.module.css`](../../web/src/ProjectSchedule.module.css) | Daily project and Yard schedule controls. |
| [`LogsPanel.module.css`](../../web/src/LogsPanel.module.css) | Full-height log output, search highlights and snapshot export confirmation. |
| [`SettingsDrawer.module.css`](../../web/src/SettingsDrawer.module.css) | Grouped Settings sections, responsive directory form, path examples and update facts. |
| [`main.tsx`](../../web/src/main.tsx) | Global stylesheet and self-hosted icon-font imports. |

## Component and CSS rules

- Put global reset, tokens, and layout primitives in `web/src/styles/`. The current feature styles live in `web/src/App.module.css`; split them beside new components as they are extracted. Avoid global selectors that style arbitrary descendants in unrelated features.
- Extract shared components for buttons, icon buttons, status badges, project cards, stat items, forms, drawers, confirmation dialogs, tabs, toasts, empty/error states, and log/terminal surfaces when a pattern is reused. The current app shell has a small inline brand, authentication form, and dashboard layout.
- Variants should be explicit component properties (`intent`, `size`, `loading`, `disabled`) and map to token-based CSS classes. A disabled or loading action must have a clear text explanation when the reason matters.
- Keep data fetching and Docker-specific mapping outside presentational components. Views consume typed application models, not raw Engine responses.
- Use native semantic elements. Prefer a native `<dialog>` for modal confirmation and an accessible dialog pattern for detail drawers. Icon-only controls need accessible names; opening and closing overlays must preserve sensible focus.
- Use the self-hosted `@phosphor-icons/web` font for interface icons. Import only the `bold` and `fill` weights in `web/src/main.tsx`. Choose `fill` for solid shapes such as Play, Stop, and Projects; use `bold` for line-based actions and navigation. Render decorative glyphs with `aria-hidden="true"` and give icon-only buttons an accessible name. Size icons with `font-size` and inherit their color from the control; do not draw replacement SVG paths or override the Phosphor font family.
- Never indicate running, unhealthy, or failed state by color alone. Pair color with text or an icon. Provide visible `:focus-visible` styles, sufficient contrast, and reduced-motion behavior.
- Show pending Docker actions in the existing status badge (`starting`, `stopping`, `restarting`, `removing`, or `updating`) using the subdued warning treatment. Keep lifecycle controls icon-only and disable conflicting actions until completion. Live inventory and metric updates should preserve cards, drawer focus, and scroll position rather than replacing the view with a loading screen.
- File uploads use a single clickable field with a subtle dashed border, a Phosphor upload/file icon, and format/size guidance. Show the selected filename and a replacement hint inside the field. Keep the native file input visually hidden but keyboard-accessible, associate its visible label and hint, and show focus/validation on the visible field. Reuse this treatment for Compose and environment-file uploads; do not expose the browser's separate "Choose file / No file chosen" chrome or advertise drag-and-drop unless implemented.

## Responsive layout

Use content-driven layouts with CSS Grid/Flexbox and a small number of documented breakpoints. Initial layout targets:

- **Wide desktop (1280px and above):** collapsible left sidebar, multi-column card grid or compact table rows, right-side project details, and left-side settings. Search spans the content width above the header.
- **Tablet/small desktop (768–1279px):** no persistent sidebar. A menu button immediately before Projects opens the full navigation as an overlay; outside tap, leftward touch swipe, Escape and the close control dismiss it. The project collection keeps the available width. Search remains above the header; compact table rows scroll horizontally when needed.
- **Mobile (below 768px):** the same overlay navigation and menu button beside Projects. The full-width dashboard header and the search immediately below it remain visible while projects scroll beneath their opaque canvas background. Cards use one column. List keeps compact table rows and scrolls horizontally inside its own region; metadata never stacks into cards. Drawers fill the viewport.

The wide sidebar shows the Yard wordmark and navigation labels, with Settings and the account at the bottom. Its Hide button remains beside the brand on wide desktop. The narrow state shows a compact Yard wordmark, navigation icons and a circular account initial; hovering over or focusing the brand reveals the Expand control, and hovering over or focusing the account reveals Sign out. Store the wide-desktop width preference in the browser. Below 1280px, always open the expanded overlay without changing that preference. Keep the main content inert and background scrolling locked while the menu is open; move focus into the menu, contain keyboard traversal and return focus to its trigger on dismissal. Crossing the navigation breakpoint clears the overlay state. Native modal dialogs provide focus handling and Escape dismissal for detail/settings drawers. A shared drawer layout keeps each header visible while its content scrolls, and an open drawer locks background scrolling on all viewports.

On touch devices, drawers also close by swiping toward their entry edge: left for Settings and the open overlay sidebar on mobile/tablet/small desktop, right for project details. `useDrawerSwipe` handles the gesture for these panels. Keep `touch-action: pan-y pinch-zoom` on each panel and its scrolling content so vertical scrolling remains native. A short or canceled swipe returns the panel to its open position; buttons, links, and form controls do not start a swipe.

These values are starting points, not device identities. Check narrow screens, 200% zoom, long project names, many services, and keyboard-only navigation. Logs and terminals should scroll within their own bounded panels rather than widening the page.

Project, Settings and New Project drawers always share a width: `min(50dvw, 60rem)` at viewports of at least 1280px, `min(56rem, calc(100dvw - 2rem))` from 768px through 1279px, and fullscreen below 768px. Keep the close button's Phosphor X centered with grid alignment. Emphasize the three project tabs with 1rem semibold text and a muted selected background. Within Details, keep actions first, then overview/container information, configuration/scheduling and history. Use section headings and subtle panel borders within the existing palette. Schedule time/save and result metadata may sit side by side. Ports and environment cells use row-major wrapping grids, never newspaper-style columns; adapt to the drawer's own width. Logs and terminal flex to fill the remaining height, with internal output scrolling.

Settings uses the same bordered sections, restrained Phosphor heading icons and compact action buttons. Keep Projects directory and Yard updates distinct. Put Save beside the labeled directory field when the content width allows it; show new-project/log path examples separately from helper text. Keep Check now near the update heading and present status, last checked and build as wrapping metadata columns. Adapt to the Settings content container, retain visible labels and associated help/errors, and stack naturally on mobile.

## Interaction and copy

- Show a project's aggregate state, health, container count, CPU, memory, and uptime on its card. Every Compose card opens the same overview and container picker, including single-container projects; standalone cards open container details. Keep the card action target and per-card action menu distinct when an action menu is added.
- The dashboard has a compact visible Projects heading and icon actions. Keep inventory counts and the update time in a secondary metadata row. Card metrics use tabular numerals and subtle separators; project names remain the strongest text within each card. Size compact metrics from the card's own inline size, including drag ghosts, rather than the viewport: narrow multi-column desktop/tablet cards need the same content-sized memory track and reduced spacing as mobile cards. Allow longer values to wrap instead of clipping them.
- Put the Grid/List toggle and Sort menu in a compact toolbar above results. The same project information and shared order apply to both views. Search filters parent projects by project/container/service name. Keep empty search results distinct from an empty Docker inventory.
- List uses a semantic table with column headers, approximately 44px rows, subtle horizontal separators and a focusable horizontal scroll region. Keep the reorder grip visible during horizontal scrolling; pin project names on wider screens and let them scroll on mobile to leave room for the other columns. Keep the ghost and blue drop marker at row height.
- Separate the reorder grip from the details button so clicks, text selection and vertical touch scrolling retain their normal behavior. Drag with Pointer Events, use a floating ghost and a blue empty destination slot, and shift neighboring projects to preview insertion. Support keyboard arrows/Home/End and Escape cancellation. Respect reduced motion for shift animations. View and order preferences are browser-local per administrator; see [Features](Features.md).
- The **New Project** flow is a source step, variable entry, validation/preview, then confirmation. Show service/image/port/volume/network changes before a URL sync or adoption.
- Use present-tense, concise English labels: “Pull images”, “Recreate”, “Remove project”. Save writes configuration; Recreate applies it. Keep the legacy Update operation out of browser menus. Destructive confirmations must name the affected project and whether volumes will be deleted.
- Mask environment and configuration values that may contain credentials. Reveal only after an explicit action in an authenticated session; avoid copying sensitive values into toasts or URLs.
- Surface job progress and errors in a persistent, reconnectable view. Do not treat a request acknowledgment as successful completion.

## Visual QA for future UI work

Verify desktop, tablet, mobile, keyboard traversal, focus return, contrast, reduced motion, loading/empty/error states, long content, and slow event streams. Prefer native browser behaviors over custom controls when they meet the design need.

For styling changes, review the dashboard with running, partial, and stopped projects, long names, and missing metric values. Check the expanded and collapsed sidebar, project/settings/new-project drawers, and authentication screens. Include narrow layouts down to 320px and ensure content stays within the viewport. Inspect normal, hovered, focused, selected, and disabled controls; check contrast using the rendered foreground and background colors. Preserve status text, visible keyboard focus, and system forced-color support.

Use isolated API fixtures for visual review when a live Docker host is unavailable. Keep fixture data and screenshots out of production code, and distinguish visual validation from real Docker-operation testing. Record completed validation in [Progress](Progress.md).
