import { trackInventoryAction } from "./inventoryUpdates";

export type Bootstrap = {
  needsSetup: boolean;
  authenticated: boolean;
  username?: string;
  csrfToken?: string;
};

export type Container = {
  id: string;
  name: string;
  service?: string;
  image: string;
  state: string;
  operation?: string;
  health: string;
  terminalAvailable: boolean | null;
  cpuPercent: number | null;
  memoryBytes: number | null;
  networkRxBytes: number | null;
  networkTxBytes: number | null;
  uptimeSeconds: number | null;
};

export type Project = {
  id: string;
  name: string;
  kind: "managed-compose" | "external-compose" | "standalone";
  state: "running" | "partial" | "stopped";
  operation?: string;
  health: string;
  cpuPercent: number | null;
  memoryBytes: number | null;
  networkRxBytes: number | null;
  networkTxBytes: number | null;
  uptimeSeconds: number | null;
  containers: Container[];
};

export type Inventory = {
  collectedAt: string;
  projects: Project[];
};

export type ContainerMetrics = Pick<Container, "cpuPercent" | "memoryBytes" | "networkRxBytes" | "networkTxBytes" | "uptimeSeconds"> & { collectedAt: string };

export function getMetrics(signal?: AbortSignal): Promise<Record<string, ContainerMetrics>> {
  return request<Record<string, ContainerMetrics>>("/api/metrics", { signal, priority: "low" });
}

export type ManagedSourceInput = {
  kind: "url" | "paste" | "upload";
  url?: string;
  filename?: string;
  yaml?: string;
};

export type ManagedSource = {
  kind: ManagedSourceInput["kind"];
  url?: string;
  filename?: string;
  yaml: string;
  variables: { name: string; required: boolean; default?: string }[];
  envFiles: { path: string; required: boolean }[];
  suggestedName?: string;
};

export type ManagedRequest = { name: string; source: ManagedSourceInput; variables: Record<string, string>; envFiles: Record<string, string>; mode: "new" | "copy" | "sync" | "adopt"; fingerprint?: string; projectDir?: string };
export type ManagedPreview = {
  name: string;
	projectDir: string;
  adoptionDir?: string;
  services: { name: string; image: string; ports: string[]; volumes: string[]; networks: string[] }[];
  volumes: string[];
  networks: string[];
  envFiles: string[];
  duplicates: string[];
  externalMatch: boolean;
  changes: string[];
  mode: ManagedRequest["mode"];
  fingerprint: string;
};
export type ImageIdentity = { service?: string; containerID?: string; imageID: string; platform?: string; startedAt?: string; previousContainerID?: string; outcome?: string; state?: string };

export type RecreatePreview = { fingerprint: string; items: { id: string; name: string; service?: string; image: string; imageID: string; running: boolean; preserved: string[]; environment: string[] }[]; order: string[] };
export function previewRecreate(id: string, operation: "update" | "recreate", csrfToken: string): Promise<RecreatePreview> {
  return request<RecreatePreview>("/api/recreate/preview", { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken }, body: JSON.stringify({ id, operation }) });
}
export function submitRecreate(id: string, operation: "update" | "recreate", fingerprint: string, csrfToken: string): Promise<ManagedJob> {
  return trackInventoryAction(id, operation, () => request<ManagedJob>("/api/recreate/submit", { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken }, body: JSON.stringify({ id, operation, fingerprint, confirm: true }) }));
}
export type ManagedJob = { id: string; projectName: string; targetID: string; domain: string; operation: string; status: "running" | "succeeded" | "failed"; stage: string; outcome?: string; scheduledFor?: number; error?: string; cleanupError?: string; rollback?: string; createdAt: number; startedAt?: number; completedAt?: number; updatedAt: number; deadlineAt?: number; workerID?: string; sourceImages?: ImageIdentity[]; targetImages?: ImageIdentity[] };

export type LifecycleAction = "start" | "stop" | "restart";

export type LifecycleResult = {
  action: LifecycleAction;
  succeeded: number;
  skipped: number;
  failed: number;
  queued: number;
  errors?: string[];
};

export type MaintenanceResult = {
  succeeded: number;
  failed: number;
  images?: string[];
  errors?: string[];
};

export type RemovalItem = {
  kind: string;
  id: string;
  name: string;
  action: "remove" | "keep";
  reason?: string;
};

export type RemovalPlan = { fingerprint: string; items: RemovalItem[] };
export type RemovalReport = { items: (RemovalItem & { status: "removed" | "retained" | "failed" })[] };

export type ContainerInspection = {
  id: string;
  ports: { containerPort: string; hostIP?: string; hostPort?: string }[];
  mounts: { type: string; source: string; destination: string; readOnly: boolean }[];
  networks: { name: string; ipv4?: string; ipv6?: string }[];
  environment: { name: string; value?: string }[];
};

export type SelfUpdateStatus = {
  automatic: boolean;
  intervalMinutes: number;
  status: "not_checked" | "checking" | "up_to_date" | "updating" | "update_failed";
  lastChecked?: string;
  currentBuildSHA?: string;
  error?: string;
};

export type ManagedSettings = { projectsBase: string };

type Credentials = {
  username: string;
  password: string;
};

const localBackendMessage =
  "The local API is not responding on 127.0.0.1:8080. Start Docker Desktop, then run docker compose -f compose.dev.yaml up -d --build from the project root and retry.";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try {
    response = await fetch(path, {
      credentials: "same-origin",
      cache: "no-store",
      ...init,
    });
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === "AbortError") throw cause;
    if (import.meta.env.DEV && path === "/api/bootstrap") throw new Error(localBackendMessage);
    throw new Error("Cannot reach NoX Yard. Check the connection and try again.");
  }

  if (response.status === 204) {
    return undefined as T;
  }

  const payload: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const message =
      payload &&
      typeof payload === "object" &&
      "error" in payload &&
      typeof payload.error === "string"
        ? payload.error
        : import.meta.env.DEV && path === "/api/bootstrap" && response.status >= 500
          ? localBackendMessage
          : "The request could not be completed.";
    throw new Error(message);
  }
  return payload as T;
}

export function getBootstrap(): Promise<Bootstrap> {
  return request<Bootstrap>("/api/bootstrap");
}

export function getProjects(signal?: AbortSignal): Promise<Inventory> {
  return request<Inventory>("/api/projects", { signal });
}

export function loadManagedSource(input: ManagedSourceInput, csrfToken: string): Promise<ManagedSource> {
  return request<ManagedSource>("/api/managed/source", {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken },
    body: JSON.stringify(input),
  });
}

export function getManagedSettings(): Promise<ManagedSettings> {
  return request<ManagedSettings>("/api/managed/settings");
}

export function saveManagedSettings(projectsBase: string, csrfToken: string): Promise<ManagedSettings> {
  return request<ManagedSettings>("/api/managed/settings", {
    method: "PUT",
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken },
    body: JSON.stringify({ projectsBase }),
  });
}

export function previewManaged(input: ManagedRequest, csrfToken: string): Promise<ManagedPreview> {
  return request<ManagedPreview>("/api/managed/preview", { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken }, body: JSON.stringify(input) });
}

export function deployManaged(input: ManagedRequest, csrfToken: string): Promise<ManagedJob> {
  return trackInventoryAction(`compose:${input.name}`, input.mode, () => request<ManagedJob>("/api/managed/deploy", { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken }, body: JSON.stringify(input) }));
}

export function getManagedJob(id: string): Promise<ManagedJob> {
  return request<ManagedJob>(`/api/managed/jobs/${encodeURIComponent(id)}`);
}

export function getJobHistory(target: string): Promise<ManagedJob[]> {
  return request<ManagedJob[]>(`/api/jobs?target=${encodeURIComponent(target)}`);
}
export function acknowledgeJobRecovery(job: ManagedJob, csrfToken: string): Promise<ManagedJob> {
  return request<ManagedJob>(`/api/jobs/${encodeURIComponent(job.id)}/recovery`, { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken }, body: JSON.stringify({ confirm: true, updatedAt: job.updatedAt }) });
}

export function runManagedOperation(name: string, operation: "start" | "stop" | "restart" | "pull" | "update" | "remove", removeVolumes: boolean, csrfToken: string, fingerprint?: string): Promise<ManagedJob> {
  return trackInventoryAction(`compose:${name}`, operation, () => request<ManagedJob>(`/api/managed/projects/${encodeURIComponent(name)}/operations`, { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken }, body: JSON.stringify({ operation, removeVolumes, fingerprint }) }));
}

export function getContainerInspection(id: string, signal?: AbortSignal): Promise<ContainerInspection> {
  return request<ContainerInspection>(`/api/containers/${encodeURIComponent(id)}`, { signal });
}

export function containerLogsURL(id: string): string {
  return `/api/containers/${encodeURIComponent(id)}/logs`;
}

export function runContainerAction(id: string, action: LifecycleAction, csrfToken: string): Promise<LifecycleResult> {
  return trackInventoryAction(`container:${id}`, action, () => request<LifecycleResult>(`/api/containers/${encodeURIComponent(id)}/actions`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken },
    body: JSON.stringify({ action }),
  }));
}

export function runProjectAction(id: string, action: LifecycleAction, csrfToken: string): Promise<LifecycleResult> {
  return trackInventoryAction(id, action, () => request<LifecycleResult>(`/api/projects/${encodeURIComponent(id)}/actions`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken },
    body: JSON.stringify({ action }),
  }));
}

export function pullContainerImage(id: string, csrfToken: string): Promise<MaintenanceResult> {
  return request<MaintenanceResult>(`/api/containers/${encodeURIComponent(id)}/pull`, {
    method: "POST",
    headers: { "X-CSRF-Token": csrfToken },
  });
}

export function pullProjectImages(id: string, csrfToken: string): Promise<MaintenanceResult> {
  return request<MaintenanceResult>(`/api/projects/${encodeURIComponent(id)}/pull`, {
    method: "POST",
    headers: { "X-CSRF-Token": csrfToken },
  });
}

export function previewRemoveContainer(id: string, removeVolumes = false): Promise<RemovalPlan> {
  return request<RemovalPlan>(`/api/containers/${encodeURIComponent(id)}/remove/preview?removeVolumes=${removeVolumes}`);
}

export function previewRemoveProject(id: string, removeVolumes = false): Promise<RemovalPlan> {
  return request<RemovalPlan>(`/api/projects/${encodeURIComponent(id)}/remove/preview?removeVolumes=${removeVolumes}`);
}

export function removeContainer(id: string, fingerprint: string, csrfToken: string, removeVolumes = false): Promise<RemovalReport> {
  return trackInventoryAction(`container:${id}`, "remove", () => request<RemovalReport>(`/api/containers/${encodeURIComponent(id)}/remove`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken },
    body: JSON.stringify({ confirm: true, fingerprint, removeVolumes }),
  }));
}

export function removeProject(id: string, fingerprint: string, csrfToken: string, removeVolumes = false): Promise<RemovalReport> {
  return trackInventoryAction(id, "remove", () => request<RemovalReport>(`/api/projects/${encodeURIComponent(id)}/remove`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken },
    body: JSON.stringify({ confirm: true, fingerprint, removeVolumes }),
  }));
}

export function revealContainerEnvironment(id: string, csrfToken: string, signal?: AbortSignal): Promise<ContainerInspection> {
  return request<ContainerInspection>(`/api/containers/${encodeURIComponent(id)}/environment`, {
    method: "POST",
    headers: { "X-CSRF-Token": csrfToken },
    signal,
  });
}

export function getSelfUpdateStatus(signal?: AbortSignal): Promise<SelfUpdateStatus> {
  return request<SelfUpdateStatus>("/api/self-update", { signal });
}

export function checkSelfUpdateNow(csrfToken: string): Promise<SelfUpdateStatus> {
  return request<SelfUpdateStatus>("/api/self-update/check", {
    method: "POST",
    headers: { "X-CSRF-Token": csrfToken },
  });
}

export function saveSelfUpdateSettings(
  settings: Pick<SelfUpdateStatus, "automatic" | "intervalMinutes">,
  csrfToken: string,
): Promise<SelfUpdateStatus> {
  return request<SelfUpdateStatus>("/api/self-update", {
    method: "PUT",
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken },
    body: JSON.stringify(settings),
  });
}

export function createAdministrator(input: Credentials): Promise<Bootstrap> {
  return request<Bootstrap>("/api/setup", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function signIn(input: Credentials): Promise<Bootstrap> {
  return request<Bootstrap>("/api/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function signOut(csrfToken: string): Promise<void> {
  return request<void>("/api/logout", {
    method: "POST",
    headers: { "X-CSRF-Token": csrfToken },
  });
}

export type ProjectScheduleStatus = { targetID: string; enabled: boolean; timezone: string; time: string; eligible: boolean; reason?: string; nextAt?: number; lastAt?: number; lastOutcome?: string; lastReason?: string; lastJobID?: string };
export function getProjectSchedule(target: string): Promise<ProjectScheduleStatus> {
  return request<ProjectScheduleStatus>(`/api/projects/${encodeURIComponent(target)}/schedule`);
}
export function setProjectSchedule(target: string, enabled: boolean, csrfToken: string): Promise<ProjectScheduleStatus> {
  return request<ProjectScheduleStatus>(`/api/projects/${encodeURIComponent(target)}/schedule`, { method: "PUT", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken }, body: JSON.stringify({ enabled }) });
}
