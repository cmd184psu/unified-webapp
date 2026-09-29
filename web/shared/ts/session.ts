// session.ts — a protected module's session, as the page sees it. When this
// module has a session (GET /api/auth/session answers), HamburgerMenu mounts
// it for every module, so no module needs its own copy:
//   - the sign-out control: a door-and-arrow icon just before the hamburger
//     trigger (grouped with it); it signs out of this module
//     (POST /api/auth/logout) and reloads, which lands on the login page;
//   - activity reporting: clicks, typing, touches and scrolling are real use,
//     reported (POST /api/auth/activity) at most once a minute so the
//     module's idle clock is renewed; background requests never renew it;
//   - idle sign-out: when the module's idle time runs out (the server says
//     how long is left), the page reloads onto the login page;
//   - and the same at once whenever one of the module's own requests is
//     answered 401 (its session ended some other way).
// An open module has no session, so none of this runs there.

import { showToast } from "./toast.js";

const SVG_NS = "http://www.w3.org/2000/svg";

/** The door-and-arrow glyph, assembled node by node. */
function doorGlyph(): SVGElement {
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("width", "1em");
  svg.setAttribute("height", "1em");
  svg.setAttribute("fill", "none");
  svg.setAttribute("stroke", "currentColor");
  svg.setAttribute("stroke-width", "2");
  svg.setAttribute("stroke-linecap", "round");
  svg.setAttribute("stroke-linejoin", "round");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  const door = document.createElementNS(SVG_NS, "path");
  door.setAttribute("d", "M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4");
  const head = document.createElementNS(SVG_NS, "polyline");
  head.setAttribute("points", "16 17 21 12 16 7");
  const shaft = document.createElementNS(SVG_NS, "line");
  shaft.setAttribute("x1", "21");
  shaft.setAttribute("y1", "12");
  shaft.setAttribute("x2", "9");
  shaft.setAttribute("y2", "12");
  svg.append(door, head, shaft);
  return svg;
}

/** The button itself; exported for tests. */
export function buildSignOutButton(): HTMLButtonElement {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = "ui-menu-trigger ui-signout";
  btn.title = "Sign out";
  btn.setAttribute("aria-label", "Sign out");
  btn.append(doorGlyph());
  btn.addEventListener("click", () => {
    btn.disabled = true;
    void fetch("/api/auth/logout", { method: "POST" })
      .catch(() => undefined)
      .then(() => window.location.reload());
  });
  return btn;
}

/** The server's answer about this module's session. */
type SessionState =
  | { kind: "signed-in"; idleSeconds: number }
  | { kind: "signed-out" } // 401: no session, or it ended
  | { kind: "unknown" }; // network trouble, or an open module (no auth routes)

/**
 * Asks the server (GET /api/auth/session, or POST /api/auth/activity to also
 * report use). An open module has no auth routes, so anything but a JSON
 * session answer (404, a SPA fallback page) is "unknown", never "signed-out".
 */
async function sessionState(method: "GET" | "POST"): Promise<SessionState> {
  if (typeof fetch !== "function") return { kind: "unknown" };
  const url = method === "GET" ? "/api/auth/session" : "/api/auth/activity";
  try {
    const res = await fetch(url, { method, headers: { Accept: "application/json" } });
    if (res.status === 401) return { kind: "signed-out" };
    if (!res.ok) return { kind: "unknown" };
    const body = (await res.json()) as { methods?: unknown; idleSeconds?: unknown };
    if (!Array.isArray(body.methods)) return { kind: "unknown" };
    return { kind: "signed-in", idleSeconds: typeof body.idleSeconds === "number" ? body.idleSeconds : 0 };
  } catch {
    return { kind: "unknown" };
  }
}

/** How often real use is reported, at most. Matches the server's renewal. */
const REPORT_EVERY_MS = 60_000;
/** Recheck at least this often, so a missed timer can't strand a page. */
const RECHECK_CAP_MS = 5 * 60_000;
const ACTIVITY_EVENTS = ["pointerdown", "keydown", "wheel", "touchstart"] as const;

/**
 * Wraps window.fetch to call `onUnauthorized` when a same-origin request is
 * answered 401 (the gate's "no valid session"). The auth routes themselves
 * are left to the session checks. Returns a function that unwraps it.
 */
function watchFetch(onUnauthorized: () => void): () => void {
  if (typeof window.fetch !== "function") return () => undefined;
  const original = window.fetch;
  const wrapped: typeof window.fetch = async (input, init) => {
    const res = await original.call(window, input, init);
    if (res.status === 401) {
      const raw = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
      const url = new URL(raw, window.location.href);
      if (url.origin === window.location.origin && !url.pathname.startsWith("/api/auth/")) onUnauthorized();
    }
    return res;
  };
  window.fetch = wrapped;
  return () => {
    if (window.fetch === wrapped) window.fetch = original;
  };
}

/**
 * Keeps this module's idle clock honest: reports real use at most once a
 * minute, schedules a check for when the idle time should run out, and
 * reloads onto the login page once the server says the session is over.
 * Returns a function that stops watching.
 */
function watchIdle(initialIdleSeconds: number): () => void {
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let lastReport = Date.now();
  let pendingUse = false;

  const signedOut = (): void => {
    if (stopped) return;
    stop();
    showToast("Your session has ended. Taking you to sign in…", "notice");
    // A moment to read the notice; the reload lands on the login page.
    setTimeout(() => window.location.reload(), 1500);
  };

  // Any of the module's own requests answered 401 means the session is over
  // (idle, a PIN change, a sign-out elsewhere, a server restart): go to the
  // login page now rather than leave the page failing request by request.
  const unwatchFetch = watchFetch(() => signedOut());

  const schedule = (idleSeconds: number): void => {
    if (stopped) return;
    clearTimeout(timer);
    const due = Math.min(Math.max(idleSeconds * 1000 + 1000, 5_000), RECHECK_CAP_MS);
    timer = setTimeout(() => void check(), due);
  };

  const apply = (state: SessionState): void => {
    if (state.kind === "signed-out") signedOut();
    else if (state.kind === "signed-in") schedule(state.idleSeconds);
    else schedule(60); // unknown: try again shortly
  };

  // Due: report pending use (which renews) or just ask.
  const check = async (): Promise<void> => {
    const report = pendingUse;
    pendingUse = false;
    if (report) lastReport = Date.now();
    apply(await sessionState(report ? "POST" : "GET"));
  };

  const onUse = (): void => {
    if (Date.now() - lastReport >= REPORT_EVERY_MS) {
      lastReport = Date.now();
      pendingUse = false;
      void sessionState("POST").then(apply);
    } else {
      pendingUse = true;
    }
  };

  const onVisible = (): void => {
    if (document.visibilityState === "visible") void check();
  };

  for (const type of ACTIVITY_EVENTS) window.addEventListener(type, onUse, { capture: true, passive: true });
  document.addEventListener("visibilitychange", onVisible);
  schedule(initialIdleSeconds);

  function stop(): void {
    stopped = true;
    unwatchFetch();
    clearTimeout(timer);
    for (const type of ACTIVITY_EVENTS) window.removeEventListener(type, onUse, { capture: true });
    document.removeEventListener("visibilitychange", onVisible);
  }
  return stop;
}

/**
 * Checks for a session and, if there is one, starts the idle watch and puts
 * the sign-out button just
 * before `trigger`, the two wrapped in one `.ui-menu-actions` group so the
 * module's top bar lays them out as a single item (a space-between bar would
 * otherwise push the icon off toward the middle). Waits for the trigger to
 * reach the page if the module hasn't mounted it yet. Returns a function that
 * undoes it (or cancels the pending mount).
 */
export function mountSignOut(trigger: HTMLElement): () => void {
  let canceled = false;
  let group: HTMLElement | null = null;
  let observer: MutationObserver | null = null;

  const place = (): boolean => {
    if (canceled || !trigger.isConnected) return false;
    group = document.createElement("span");
    group.className = "ui-menu-actions";
    trigger.before(group);
    group.append(buildSignOutButton(), trigger);
    return true;
  };

  let stopWatching: (() => void) | null = null;

  void sessionState("GET").then((state) => {
    if (state.kind !== "signed-in" || canceled) return;
    stopWatching = watchIdle(state.idleSeconds);
    if (place() || typeof MutationObserver !== "function") return;
    observer = new MutationObserver(() => {
      if (place()) observer?.disconnect();
    });
    observer.observe(document.body, { childList: true, subtree: true });
  });

  return () => {
    canceled = true;
    observer?.disconnect();
    stopWatching?.();
    if (group) {
      group.before(trigger);
      group.remove();
    }
  };
}
