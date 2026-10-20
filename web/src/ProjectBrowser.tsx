import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import type { Project } from "./api";
import styles from "./App.module.css";
import tableStyles from "./ProjectTable.module.css";

type Preferences = { layout: "grid" | "list"; sort: "asc" | "desc" | "manual"; order: string[] };
const defaults: Preferences = { layout: "grid", sort: "asc", order: [] };

function readPreferences(key: string): Preferences {
  try {
    const value = JSON.parse(localStorage.getItem(key) || "null");
    return {
      layout: value?.layout === "list" ? "list" : "grid",
      sort: value?.sort === "desc" || value?.sort === "manual" ? value.sort : "asc",
      order: Array.isArray(value?.order) ? [...new Set<string>(value.order.filter((id: unknown) => typeof id === "string"))] : [],
    };
  } catch { return defaults; }
}

function moveTo(ids: string[], id: string, index: number) {
  const next = ids.filter(value => value !== id);
  next.splice(index, 0, id);
  return next;
}

function searchable(value: string) {
  return value.normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLocaleLowerCase();
}

type Drag = {
  id: string;
  project: Project;
  pointerID: number;
  startX: number; startY: number;
  x: number; y: number;
  offsetX: number; offsetY: number;
  width: number; height: number;
  base: string[];
  preview: string[];
  target: number | null;
  active: boolean;
};

export function ProjectBrowser({ projects, username, selectedID, onSelect, header, notices, children, rowCells }: {
  projects: Project[] | null;
  username: string;
  selectedID: string | null;
  onSelect: (id: string) => void;
  header: ReactNode;
  notices: ReactNode;
  children: (project: Project) => ReactNode;
  rowCells: (project: Project) => ReactNode;
}) {
  // Storage is origin/host-local and scoped to the signed-in administrator.
  const storageKey = `nox-yard-project-display:${username}`;
  const [preferences, setPreferences] = useState(() => readPreferences(storageKey));
  const [query, setQuery] = useState("");
  const [menuOpen, setMenuOpen] = useState(false);
  const [drag, setDrag] = useState<Drag | null>(null);
  const [announcement, setAnnouncement] = useState("");
  const collectionRef = useRef<HTMLElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const sortButtonRef = useRef<HTMLButtonElement>(null);
  const dragRef = useRef<Drag | null>(null);
  const pendingFocus = useRef<string | null>(null);
  const previousRects = useRef(new Map<string, DOMRect>());
  const latest = useRef({ projects, order: [] as string[] });

  const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });
  const alphabetical = [...(projects ?? [])].sort((a, b) => collator.compare(a.name, b.name) || a.id.localeCompare(b.id));
  const positions = new Map(preferences.order.map((id, index) => [id, index]));
  const ordered = preferences.sort === "manual"
    ? alphabetical.sort((a, b) => (positions.get(a.id) ?? Infinity) - (positions.get(b.id) ?? Infinity))
    : preferences.sort === "desc" ? alphabetical.reverse() : alphabetical;
  const terms = searchable(query).trim().split(/\s+/).filter(Boolean);
  const visible = ordered.filter(project => {
    const names = searchable([project.name, ...project.containers.flatMap(container => [container.name, container.service || ""])].join(" "));
    return terms.every(term => names.includes(term));
  });
  latest.current = { projects, order: ordered.map(project => project.id) };
  const displayed = drag?.active ? drag.preview.flatMap(id => {
    const project = visible.find(project => project.id === id);
    return project ? [project] : [];
  }) : visible;

  useEffect(() => {
    try { localStorage.setItem(storageKey, JSON.stringify(preferences)); }
    catch { /* Browsing and reordering still work without persistent storage. */ }
  }, [storageKey, preferences]);

  function commitOrder(ids: string[], moved: string) {
    // A filtered reorder uses the visible slots and leaves hidden projects in place.
    const included = new Set(ids);
    let slot = 0;
    const order = latest.current.order.map(id => included.has(id) ? ids[slot++] : id);
    pendingFocus.current = moved;
    setPreferences(current => ({ ...current, sort: "manual", order }));
    const project = latest.current.projects?.find(project => project.id === moved);
    setAnnouncement(`${project?.name || "Project"} moved to position ${ids.indexOf(moved) + 1} of ${ids.length}.`);
  }
  const commitRef = useRef(commitOrder);
  commitRef.current = commitOrder;

  useLayoutEffect(() => {
    if (!pendingFocus.current) return;
    collectionRef.current?.querySelector<HTMLButtonElement>(`[data-project-id="${CSS.escape(pendingFocus.current)}"] [data-drag-handle]`)?.focus({ preventScroll: true });
    pendingFocus.current = null;
  }, [preferences.order]);

  useEffect(() => {
    if (!menuOpen) return;
    menuRef.current?.querySelector<HTMLButtonElement>("[role=menuitemradio][aria-checked=true], [role=menuitemradio]")?.focus();
    const dismiss = (event: globalThis.PointerEvent) => {
      if (!menuRef.current?.contains(event.target as Node)) setMenuOpen(false);
    };
    document.addEventListener("pointerdown", dismiss);
    return () => document.removeEventListener("pointerdown", dismiss);
  }, [menuOpen]);

  // Animate the contents, keeping slot geometry stable for drag hit testing.
  useLayoutEffect(() => {
    const next = new Map<string, DOMRect>();
    for (const item of collectionRef.current?.querySelectorAll<HTMLElement>("[data-project-id]") ?? []) {
      const id = item.dataset.projectId!;
      const rect = item.getBoundingClientRect();
      const previous = previousRects.current.get(id);
      if (previous && drag?.active && id !== drag.id && !matchMedia("(prefers-reduced-motion: reduce)").matches) {
        for (const child of item.children) {
          child.getAnimations().forEach(animation => animation.cancel());
          child.animate([{ transform: `translate(${previous.left - rect.left}px, ${previous.top - rect.top}px)` }, { transform: "translate(0, 0)" }], { duration: 180, easing: "ease-out" });
        }
      }
      next.set(id, rect);
    }
    previousRects.current = next;
  }, [displayed.map(project => project.id).join("|"), preferences.layout, drag?.active, drag?.id]);

  useEffect(() => {
    let frame = 0;
    const finish = (commit: boolean) => {
      const current = dragRef.current;
      cancelAnimationFrame(frame);
      frame = 0;
      dragRef.current = null;
      setDrag(null);
      if (current?.active) {
        if (commit && current.target !== null) commitRef.current(current.preview, current.id);
        else setAnnouncement("Reordering canceled.");
      }
      if (current) collectionRef.current?.querySelector<HTMLButtonElement>(`[data-project-id="${CSS.escape(current.id)}"] [data-drag-handle]`)?.focus({ preventScroll: true });
    };
    const locate = (current: Drag) => {
      const slots = Array.from(collectionRef.current?.querySelectorAll<HTMLElement>("[data-project-id]") ?? []);
      const hit = document.elementFromPoint(current.x, current.y);
      const target = hit && collectionRef.current?.contains(hit) ? slots.findIndex(item => {
        const rect = item.getBoundingClientRect();
        return current.x >= rect.left && current.x <= rect.right && current.y >= rect.top && current.y <= rect.bottom;
      }) : -1;
      current.target = target < 0 || target === current.base.indexOf(current.id) ? null : target;
      current.preview = current.target === null ? current.base : moveTo(current.base, current.id, target);
      setDrag({ ...current });
    };
    const scroll = () => {
      const current = dragRef.current;
      if (current?.active) {
        const edge = 64;
        const amount = current.y < edge ? -Math.ceil((edge - current.y) / 5)
          : current.y > innerHeight - edge ? Math.ceil((current.y - innerHeight + edge) / 5) : 0;
        if (amount) {
          window.scrollBy(0, Math.max(-18, Math.min(18, amount)));
          locate(current);
        }
      }
      frame = dragRef.current?.active ? requestAnimationFrame(scroll) : 0;
    };
    const pointerMove = (event: globalThis.PointerEvent) => {
      const current = dragRef.current;
      if (!current || current.pointerID !== event.pointerId) return;
      current.x = event.clientX; current.y = event.clientY;
      if (!current.active && Math.hypot(current.x - current.startX, current.y - current.startY) < 5) return;
      event.preventDefault();
      current.active = true;
      locate(current);
      if (!frame) frame = requestAnimationFrame(scroll);
    };
    const pointerUp = (event: globalThis.PointerEvent) => {
      if (dragRef.current?.pointerID === event.pointerId) finish(true);
    };
    const cancel = () => finish(false);
    const escape = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape" && dragRef.current) { event.preventDefault(); event.stopPropagation(); cancel(); }
    };
    document.addEventListener("pointermove", pointerMove, { passive: false });
    document.addEventListener("pointerup", pointerUp);
    document.addEventListener("pointercancel", cancel);
    document.addEventListener("keydown", escape, true);
    window.addEventListener("blur", cancel);
    window.addEventListener("resize", cancel);
    return () => {
      cancelAnimationFrame(frame);
      document.removeEventListener("pointermove", pointerMove);
      document.removeEventListener("pointerup", pointerUp);
      document.removeEventListener("pointercancel", cancel);
      document.removeEventListener("keydown", escape, true);
      window.removeEventListener("blur", cancel);
      window.removeEventListener("resize", cancel);
    };
  }, []);

  // Live inventory can remove/add projects mid-gesture. Cancel rather than commit
  // an order based on stale slots. Metric-only updates leave the gesture intact.
  const visibleIdentity = visible.map(project => project.id).join("|");
  useEffect(() => {
    dragRef.current = null;
    setDrag(null);
  }, [visibleIdentity, preferences.layout, query]);

  function startDrag(event: PointerEvent<HTMLButtonElement>, project: Project) {
    if (!event.isPrimary || event.button !== 0) return;
    event.preventDefault();
    event.currentTarget.focus({ preventScroll: true });
    const row = event.currentTarget.closest<HTMLElement>("[data-project-id]")!;
    const rect = row.getBoundingClientRect();
    const left = preferences.layout === "list" ? row.firstElementChild!.getBoundingClientRect().left : rect.left;
    const base = visible.map(project => project.id);
    dragRef.current = {
      id: project.id, project, pointerID: event.pointerId,
      startX: event.clientX, startY: event.clientY, x: event.clientX, y: event.clientY,
      offsetX: event.clientX - left, offsetY: event.clientY - rect.top,
      width: rect.width, height: rect.height, base, preview: base, target: null, active: false,
    };
  }

  function keyboardMove(event: KeyboardEvent<HTMLButtonElement>, id: string) {
    if (!["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    const ids = visible.map(project => project.id);
    const index = ids.indexOf(id);
    const target = event.key === "Home" ? 0 : event.key === "End" ? ids.length - 1
      : Math.max(0, Math.min(ids.length - 1, index + (["ArrowLeft", "ArrowUp"].includes(event.key) ? -1 : 1)));
    if (target !== index) commitOrder(moveTo(ids, id, target), id);
  }

  function menuKeys(event: KeyboardEvent) {
    if (event.key === "Escape") { event.preventDefault(); setMenuOpen(false); sortButtonRef.current?.focus(); }
    if (event.key === "Tab") setMenuOpen(false);
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    const items = Array.from(menuRef.current?.querySelectorAll<HTMLButtonElement>("[role=menuitemradio]") ?? []);
    const current = items.indexOf(document.activeElement as HTMLButtonElement);
    items[event.key === "Home" ? 0 : event.key === "End" ? items.length - 1 : (current + (event.key === "ArrowDown" ? 1 : items.length - 1)) % items.length]?.focus();
  }

  function grip(project: Project, ghost = false) {
    const className = `${styles.projectDragHandle} ${preferences.layout === "list" ? tableStyles.dragHandle : ""}`;
    if (ghost) return <span className={className}><i className="ph-bold ph-dots-six-vertical" aria-hidden="true" /></span>;
    return <button type="button" className={className} data-drag-handle aria-label={`Reorder ${project.name}. Use arrow keys to move, Home or End for first or last.`} title={`Drag to reorder ${project.name}, or use arrow keys`} onPointerDown={event => startDrag(event, project)} onKeyDown={event => keyboardMove(event, project.id)} onClick={event => { event.preventDefault(); event.stopPropagation(); }}><i className="ph-bold ph-dots-six-vertical" aria-hidden="true" /></button>;
  }

  function columns() {
    return <colgroup>
      <col className={tableStyles.gripColumn} /><col className={tableStyles.nameColumn} />
      <col className={tableStyles.stateColumn} /><col className={tableStyles.healthColumn} />
      <col className={tableStyles.kindColumn} /><col className={tableStyles.countColumn} />
      <col className={tableStyles.cpuColumn} /><col className={tableStyles.memoryColumn} /><col className={tableStyles.uptimeColumn} />
    </colgroup>;
  }

  function cells(project: Project, ghost = false) {
    return <>
      <td>{grip(project, ghost)}</td>
      <th scope="row">{ghost ? <span className={tableStyles.projectName}>{project.name}</span> : <button type="button" className={tableStyles.projectName} title={project.name} aria-haspopup="dialog" aria-controls="project-drawer" aria-expanded={selectedID === project.id}>{project.name}</button>}</th>
      {rowCells(project)}
    </>;
  }

  return <>
    <div className={styles.projectHeadingGroup}>
      <div className={styles.projectSearch} role="search" aria-label="Search Docker projects">
        <i className="ph-bold ph-magnifying-glass" aria-hidden="true" />
        <input ref={searchRef} type="search" aria-label="Search projects and containers" placeholder="Search projects and containers…" value={query} onChange={event => setQuery(event.target.value)} onKeyDown={event => { if (event.key === "Escape" && query) { event.preventDefault(); setQuery(""); } }} />
        {query && <button type="button" aria-label="Clear search" title="Clear search" onClick={() => { setQuery(""); searchRef.current?.focus(); }}><i className="ph-bold ph-x" aria-hidden="true" /></button>}
      </div>
      {header}
    </div>
    {projects && projects.length > 0 && <div className={styles.projectToolbar}>
      <p className={styles.projectResultSummary} role="status">{terms.length ? `${visible.length} of ${projects.length} projects` : <><i className="ph-bold ph-dots-six-vertical" aria-hidden="true" />Drag to reorder</>}</p>
      <div className={styles.projectViewActions}>
        <div className={styles.projectViewToggle} role="group" aria-label="Project view">
          {(["grid", "list"] as const).map(layout => <button key={layout} type="button" aria-label={`${layout === "grid" ? "Grid" : "List"} view`} title={`${layout === "grid" ? "Grid" : "List"} view`} aria-pressed={preferences.layout === layout} onClick={() => setPreferences(current => ({ ...current, layout }))}><i className={`ph-bold ${layout === "grid" ? "ph-squares-four" : "ph-rows"}`} aria-hidden="true" /></button>)}
        </div>
        <div ref={menuRef} className={styles.projectSort} onKeyDown={menuKeys}>
          <button ref={sortButtonRef} type="button" className={styles.projectSortButton} aria-label="Sort projects" title="Sort projects" aria-haspopup="menu" aria-expanded={menuOpen} aria-controls="project-sort-menu" onClick={() => setMenuOpen(open => !open)} onKeyDown={event => { if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); setMenuOpen(true); } }}><i className="ph-bold ph-sort-ascending" aria-hidden="true" /><span>Sort</span><i className="ph-bold ph-caret-down" aria-hidden="true" /></button>
          {menuOpen && <div id="project-sort-menu" className={styles.projectSortMenu} role="menu" aria-label="Sort projects">
            {(["asc", "desc"] as const).map(sort => <button key={sort} type="button" role="menuitemradio" aria-checked={preferences.sort === sort} onClick={() => { setPreferences(current => ({ ...current, sort })); setMenuOpen(false); sortButtonRef.current?.focus(); setAnnouncement(sort === "asc" ? "Projects sorted A to Z." : "Projects sorted Z to A."); }}><i className={`ph-bold ${sort === "asc" ? "ph-sort-ascending" : "ph-sort-descending"}`} aria-hidden="true" /><span>{sort === "asc" ? "Name: A to Z" : "Name: Z to A"}</span>{preferences.sort === sort && <i className="ph-bold ph-check" aria-hidden="true" />}</button>)}
          </div>}
        </div>
      </div>
    </div>}
    {notices}
    {projects && projects.length > 0 && visible.length === 0 && <section className={styles.projectNoResults} aria-labelledby="no-results-title"><i className="ph-bold ph-magnifying-glass" aria-hidden="true" /><h2 id="no-results-title">No matching projects</h2><p>Try another project or container name.</p><button type="button" className={styles.secondaryButton} onClick={() => { setQuery(""); searchRef.current?.focus(); }}>Clear search</button></section>}
    {visible.length > 0 && (preferences.layout === "list" ? <section className={tableStyles.viewport} aria-label="Docker projects" data-layout="list" tabIndex={0}>
      <table className={tableStyles.projectTable}>
        <caption className={styles.visuallyHidden}>Docker projects. Drag the reorder grip or use its arrow keys to change the order.</caption>
        {columns()}
        <thead><tr><th scope="col"><span className={styles.visuallyHidden}>Reorder</span></th>{["Name", "Status", "Health", "Type", "Containers", "CPU", "Memory", "Uptime"].map(label => <th key={label} scope="col">{label}</th>)}</tr></thead>
        <tbody ref={node => { collectionRef.current = node; }}>{displayed.map(project => <tr key={project.id} data-project-id={project.id} data-dragging={drag?.active && drag.id === project.id} data-drop-target={drag?.active && drag.id === project.id && drag.target !== null} onClick={() => onSelect(project.id)}>{cells(project)}</tr>)}</tbody>
      </table>
    </section> : <section ref={collectionRef} className={styles.projectGrid} aria-label="Docker projects" data-layout="grid">
      {displayed.map(project => <div key={project.id} data-project-id={project.id} className={styles.projectItem} data-dragging={drag?.active && drag.id === project.id} data-drop-target={drag?.active && drag.id === project.id && drag.target !== null}>
        <button type="button" className={styles.projectCard} aria-haspopup="dialog" aria-controls="project-drawer" aria-expanded={selectedID === project.id} onClick={() => onSelect(project.id)}>{children(project)}</button>
        {grip(project)}
      </div>)}
    </section>)}
    <p className={styles.visuallyHidden} role="status">{announcement}</p>
    {drag?.active && createPortal(<div className={tableStyles.dragLayer} aria-hidden="true"><div className={styles.projectDragGhost} data-drag-ghost data-layout={preferences.layout} style={{ width: drag.width, height: drag.height, transform: `translate3d(${drag.x - drag.offsetX}px, ${drag.y - drag.offsetY}px, 0)` }}>
      {preferences.layout === "list" ? <table className={`${tableStyles.projectTable} ${tableStyles.ghostTable}`}>{columns()}<tbody><tr>{cells(drag.project, true)}</tr></tbody></table> : <><div className={styles.projectCard}>{children(drag.project)}</div>{grip(drag.project, true)}</>}
    </div></div>, document.body)}
  </>;
}
