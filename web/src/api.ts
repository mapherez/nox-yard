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
  health: string;
  cpuPercent: number | null;
  memoryBytes: number | null;
  networkRxBytes: number | null;
  networkTxBytes: number | null;
  uptimeSeconds: number | null;
};

export type Project = {
  id: string;
  name: string;
  kind: "external-compose" | "standalone";
  state: "running" | "partial" | "stopped";
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

type Credentials = {
  username: string;
  password: string;
};

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

export function getContainerInspection(id: string, signal?: AbortSignal): Promise<ContainerInspection> {
  return request<ContainerInspection>(`/api/containers/${encodeURIComponent(id)}`, { signal });
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
