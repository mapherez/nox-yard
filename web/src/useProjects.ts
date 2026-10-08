import { useCallback, useEffect, useRef, useState } from "react";
import { getProjects, getMetrics, notifySessionExpired, type Project, type ContainerMetrics } from "./api";
import { operationState, subscribeInventoryActions, type InventoryAction } from "./inventoryUpdates";

export function mergeMetrics(projects: Project[], samples: Record<string, ContainerMetrics>): Project[] {
  return projects.map(project => {
    const containers = project.containers.map(container => {
      const sample = samples[container.id];
      return container.state === "running" && sample ? { ...container, ...sample } : container;
    });
    const running = containers.filter(container => container.state === "running");
    const total = (key: "cpuPercent" | "memoryBytes" | "networkRxBytes" | "networkTxBytes") => running.length && running.every(container => container[key] !== null)
      ? running.reduce((sum, container) => sum + (container[key] ?? 0), 0) : null;
    const uptimes = running.flatMap(container => container.uptimeSeconds === null ? [] : [container.uptimeSeconds]);
    return { ...project, containers, cpuPercent: total("cpuPercent"), memoryBytes: total("memoryBytes"), networkRxBytes: total("networkRxBytes"), networkTxBytes: total("networkTxBytes"), uptimeSeconds: uptimes.length ? Math.min(...uptimes) : null };
  });
}

export function useProjects() {
  const [inventory, setInventory] = useState<Project[] | null>(null);
  const [inventoryError, setInventoryError] = useState("");
  const [collectedAt, setCollectedAt] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const [actions, setActions] = useState<Record<number, InventoryAction>>({});
  const refreshRef = useRef<() => void>(() => {});
  const refresh = useCallback(() => refreshRef.current(), []);

  useEffect(() => {
    let active = true;
    let busy = false;
    let dirty = false;
    let metricsBusy = false;
    let metricsDirty = false;
    let revision = 0;
    let source: EventSource | null = null;
    let scheduled: number | undefined;
    let lastRefresh = 0;
    const controller = new AbortController();
    const visible = () => document.visibilityState !== "hidden";

    async function refreshInventory() {
      if (!active || !visible()) return;
      revision++;
      if (busy) { dirty = true; return; }
      busy = true;
      setRefreshing(true);
      try {
        do {
          dirty = false;
          const snapshot = await getProjects(controller.signal);
          if (!active) return;
          setInventory(snapshot.projects);
          setCollectedAt(snapshot.collectedAt);
          setInventoryError("");
          lastRefresh = Date.now();
        } while (dirty && active && visible());
      } catch (cause) {
        if (active) setInventoryError(cause instanceof Error ? cause.message : "Unable to load Docker projects.");
      } finally {
        busy = false;
        if (active) {
          setRefreshing(false);
          // An event during a failed request must still get a reconciliation.
          if (dirty) scheduleRefresh();
        }
      }
    }

    function scheduleRefresh() {
      if (scheduled !== undefined) return;
      scheduled = window.setTimeout(() => { scheduled = undefined; void refreshInventory(); }, 100);
    }

    async function refreshMetrics() {
      if (!active || document.visibilityState === "hidden") return;
      if (metricsBusy) { metricsDirty = true; return; }
      metricsBusy = true;
      metricsDirty = false;
      const startedRevision = revision;
      try {
        const samples = await getMetrics(controller.signal);
        if (active && !busy && revision === startedRevision) setInventory(current => current && mergeMetrics(current, samples));
      } catch { /* Metrics failure must not hide or block inventory. */ }
      finally {
        metricsBusy = false;
        if (active && metricsDirty) void refreshMetrics();
      }
    }

    function connect() {
      source?.close();
      source = null;
      if (!active || document.visibilityState === "hidden") return;
      source = new EventSource("/api/projects/events");
      source.addEventListener("change", event => {
        try {
          const change = JSON.parse((event as MessageEvent<string>).data) as { inventory?: boolean; metrics?: boolean };
          if (change.inventory) scheduleRefresh();
          if (change.metrics) void refreshMetrics();
        } catch { scheduleRefresh(); }
      });
      source.addEventListener("session-expired", () => {
        source?.close();
        notifySessionExpired();
      });
      source.onerror = () => { void refreshInventory(); };
    }

    refreshRef.current = () => { void refreshInventory(); };
    const unsubscribe = subscribeInventoryActions(action => {
      setActions(current => {
        const next = { ...current };
        if (action.finished) delete next[action.id]; else next[action.id] = action;
        return next;
      });
      if (action.finished) void refreshInventory();
    });
    function onVisible() { connect(); if (document.visibilityState === "visible") void refreshInventory(); }
    connect();
    void refreshInventory();
    // Live events are primary. Poll only for disconnected fallback or a slower
    // reconciliation in case an Engine event was missed.
    const interval = window.setInterval(() => {
      if (source?.readyState !== EventSource.OPEN || Date.now() - lastRefresh >= 60_000) {
        void refreshInventory();
        void refreshMetrics();
      }
    }, 15_000);
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      active = false;
      source?.close();
      controller.abort();
      unsubscribe();
      window.clearInterval(interval);
      window.clearTimeout(scheduled);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, []);

  const pending = Object.values(actions);
  const projects = inventory?.map(project => {
    const projectAction = pending.find(action => action.target === project.id);
    const containers = project.containers.map(container => {
      const action = pending.find(item => item.target === `container:${container.id}`) || projectAction;
      return action ? { ...container, operation: operationState(action.operation) || container.operation } : container;
    });
    return { ...project, containers, operation: (projectAction && operationState(projectAction.operation)) || project.operation || containers.find(container => container.operation)?.operation };
  }) ?? null;
  return { projects, inventoryError, collectedAt, refreshing, refresh };
}
