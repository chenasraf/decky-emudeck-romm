// Browser-side bridge for @decky/api. Aliased via vite.playground.config.ts.
//
// callable(name) → POST /api/<name> on the playground bridge server.
// addEventListener(name) → subscribe to the WS /events stream that mirrors
// the backend's `decky.emit()` calls.
//
// The bridge listens at :5175. Vite proxies /api and /events from :5174.
// Run the backend with `pnpm playground:server` in a separate terminal.

type AnyFn = (...args: unknown[]) => unknown;

const listeners = new Map<string, Set<AnyFn>>();
let ws: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

function ensureWs(): void {
  if (typeof window === "undefined") return;
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return;
  const proto = location.protocol === "https:" ? "wss" : "ws";
  ws = new WebSocket(`${proto}://${location.host}/events`);
  ws.addEventListener("message", (evt) => {
    try {
      const { event, args } = JSON.parse(evt.data);
      const set = listeners.get(event);
      if (!set) return;
      for (const cb of set) (cb as AnyFn)(...(args ?? []));
    } catch (e) {
      console.warn("[playground] malformed ws message", e);
    }
  });
  ws.addEventListener("close", () => {
    ws = null;
    if (reconnectTimer) clearTimeout(reconnectTimer);
    reconnectTimer = setTimeout(ensureWs, 1500);
  });
  ws.addEventListener("error", () => {
    // close handler will trigger reconnect
  });
}

ensureWs();

export function callable<T>(name: string): T {
  const fn = async (...args: unknown[]) => {
    let res: Response;
    try {
      res = await fetch(`/api/${name}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ args }),
      });
    } catch (e) {
      console.error(`[playground] callable("${name}") fetch failed — is the server running?`, e);
      throw e;
    }
    if (!res.ok) {
      const text = await res.text().catch(() => "");
      console.error(`[playground] callable("${name}") HTTP ${res.status}`, text);
      throw new Error(`HTTP ${res.status}`);
    }
    const data = await res.json();
    if (data.error) {
      console.error(`[playground] callable("${name}") backend error:`, data.error);
      throw new Error(data.error);
    }
    return data.result;
  };
  return fn as unknown as T;
}

export function addEventListener(name: string, cb: AnyFn): void {
  if (!listeners.has(name)) listeners.set(name, new Set());
  listeners.get(name)!.add(cb);
  ensureWs();
}

export function removeEventListener(name: string, cb: AnyFn): void {
  listeners.get(name)?.delete(cb);
}

export const toaster = {
  toast: (opts: { title?: string; body?: string }) => {
    console.info("[playground] toast", opts);
    // Lightweight visual: top-right transient banner
    if (typeof document === "undefined") return;
    const el = document.createElement("div");
    el.textContent = `${opts.title ?? ""}${opts.body ? " — " + opts.body : ""}`;
    Object.assign(el.style, {
      position: "fixed",
      top: "16px",
      right: "16px",
      background: "#1e3346",
      color: "#c7d5e0",
      padding: "10px 14px",
      border: "1px solid #2a3548",
      borderRadius: "4px",
      zIndex: "9999",
      fontSize: "13px",
      maxWidth: "320px",
      boxShadow: "0 4px 12px rgba(0,0,0,0.4)",
    } as CSSStyleDeclaration);
    document.body.appendChild(el);
    setTimeout(() => el.remove(), 4000);
  },
};

export function definePlugin<T>(fn: T): T {
  return fn;
}

// Patches/routerHook are no-ops in the playground — there's no Steam React
// tree to patch and no Decky router to hook.
export const routerHook = {
  addPatch: () => ({ unpatch: () => {} }),
  removePatch: () => {},
};
