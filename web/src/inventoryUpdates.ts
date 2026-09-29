export type InventoryAction = { id: number; target: string; operation: string; finished: boolean };
const listeners = new Set<(action: InventoryAction) => void>();
let nextID = 0;

export function subscribeInventoryActions(listener: (action: InventoryAction) => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

// Acknowledgements are not completion for managed jobs; their ongoing state
// comes from the server's inventory and Docker events.
export async function trackInventoryAction<T>(target: string, operation: string, run: () => Promise<T>): Promise<T> {
  const action = { id: ++nextID, target, operation, finished: false };
  for (const listener of listeners) listener(action);
  try { return await run(); }
  finally { for (const listener of listeners) listener({ ...action, finished: true }); }
}

export function operationState(operation: string): string {
  return ({ start: "starting", stop: "stopping", restart: "restarting", remove: "removing", new: "starting", copy: "starting", update: "updating", sync: "updating" } as Record<string, string>)[operation] || "";
}
