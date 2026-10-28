import { useEffect } from "react";

const serverConnectedEvent = "yard:server-connected";

export function notifyServerConnected() {
  window.dispatchEvent(new Event(serverConnectedEvent));
}

function interfaceRevision(page: Document): string | null {
  const scripts = Array.from(page.querySelectorAll<HTMLScriptElement>('script[type="module"][src]'), script => script.getAttribute("src"));
  if (!scripts.length) return null;
  const styles = Array.from(page.querySelectorAll<HTMLLinkElement>('link[rel="stylesheet"][href]'), link => link.getAttribute("href"));
  const version = page.querySelector<HTMLMetaElement>('meta[name="yard-version"]')?.content ?? "";
  return JSON.stringify({ version, scripts, styles });
}

// Data refreshes cannot replace the JavaScript already running in an open tab.
// Compare with fresh HTML, then navigate only when no editor is in use.
export function useInterfaceUpdates() {
  useEffect(() => {
    if (!import.meta.env.PROD) return;
    const loaded = interfaceRevision(document);
    if (!loaded) return;
    let active = true;
    let pending = false;
    let reloading = false;
    let request: AbortController | null = null;
    const editing = () => Boolean(document.querySelector('dialog[open], form:focus-within'));

    async function check() {
      if (!active || reloading || request || document.visibilityState === "hidden") return;
      const controller = new AbortController();
      request = controller;
      const timeout = window.setTimeout(() => controller.abort(), 8000);
      try {
        const response = await fetch(window.location.href, { cache: "no-store", credentials: "same-origin", signal: controller.signal });
        if (!response.ok || !response.headers.get("Content-Type")?.includes("text/html")) return;
        const next = interfaceRevision(new DOMParser().parseFromString(await response.text(), "text/html"));
        if (!active || !next) return;
        pending = next !== loaded;
        if (pending && !editing()) {
          reloading = true;
          window.location.reload();
        }
      } catch { /* Keep the current interface usable during server restarts. */ }
      finally {
        window.clearTimeout(timeout);
        if (request === controller) request = null;
      }
    }

    const checkPending = () => { if (pending && !editing()) void check(); };
    const checkNow = () => { void check(); };
    const observer = new MutationObserver(checkPending);
    observer.observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ["open"] });
    const interval = window.setInterval(checkNow, 15_000);
    document.addEventListener("visibilitychange", checkNow);
    document.addEventListener("focusout", checkPending);
    window.addEventListener("focus", checkNow);
    window.addEventListener("online", checkNow);
    window.addEventListener("pageshow", checkNow);
    window.addEventListener(serverConnectedEvent, checkNow);
    void check();
    return () => {
      active = false;
      request?.abort();
      observer.disconnect();
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", checkNow);
      document.removeEventListener("focusout", checkPending);
      window.removeEventListener("focus", checkNow);
      window.removeEventListener("online", checkNow);
      window.removeEventListener("pageshow", checkNow);
      window.removeEventListener(serverConnectedEvent, checkNow);
    };
  }, []);
}
