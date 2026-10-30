(() => {
  "use strict";
  // The notification center is a singleton that survives #overview
  // replacement. Overview fragments are re-rendered by htmx (load trigger and
  // every SSE message), so a per-render build would drop in-flight ingests,
  // briefly leave no sseMessage listener attached, and register duplicates.
  // Instead the center is built once and moved into each new #overview.
  const allowed = ["Normal", "Warning", "Error"];
  const limit = 100;
  const safe = value => String(value || "").replace(/[\u0000-\u001f\u007f]/g, "").slice(0, 500);

  const currentRoot = () => document.querySelector("#overview");
  const scopeKey = root => `kui.notifications.v1:${encodeURIComponent(root?.dataset.context || "")}:${encodeURIComponent(root?.dataset.namespace || "_all")}`;
  const validItem = (root, item) => item && typeof item === "object" &&
    typeof item.id === "string" && item.id.length > 0 && item.id.length <= 200 &&
    item.context === root.dataset.context && typeof item.namespace === "string" && item.namespace.length <= 500 && (root.dataset.namespace === "_all" || item.namespace === root.dataset.namespace) &&
    typeof item.message === "string" && item.message.length <= 500 &&
    typeof item.reason === "string" && item.reason.length <= 500 &&
    typeof item.kind === "string" && item.kind.length <= 500 &&
    typeof item.name === "string" && item.name.length <= 500 &&
    allowed.includes(item.severity) && typeof item.read === "boolean" &&
    typeof item.timestamp === "string" && Number.isFinite(Date.parse(item.timestamp)) &&
    (item.url === "" || (typeof item.url === "string" && item.url.startsWith(`/c/${encodeURIComponent(root.dataset.context)}/`)));

  function loadState(root) {
    const key = scopeKey(root);
    try {
      const raw = JSON.parse(localStorage.getItem(key) || "{}");
      const severities = Array.isArray(raw.severities) ? raw.severities.filter(x => allowed.includes(x)) : [];
      return {
        key,
        items: Array.isArray(raw.items) ? raw.items.filter(item => validItem(root, item)).slice(0, limit) : [],
        severities: severities.length ? severities : allowed,
        quiet: raw.quiet === true,
      };
    } catch {
      return { key, items: [], severities: allowed, quiet: false };
    }
  }

  let state = loadState(currentRoot());
  let scope = scopeKey(currentRoot());

  const persist = () => { try { localStorage.setItem(scope, JSON.stringify(state)); } catch {} };

  const host = document.createElement("details");
  host.className = "notification-center";
  host.setAttribute("aria-label", "Notification center");
  host.innerHTML = '<summary class="notification-summary"><h2>Notifications</h2><span id="notification-unread" aria-live="polite"></span><span class="notification-disclosure-hint" aria-hidden="true"></span></summary><div class="notification-content"><div class="notification-controls"><label><input type="checkbox" id="notification-quiet"> Quiet mode</label><label>Severity <select id="notification-severity"><option value="all">All</option><option>Normal</option><option>Warning</option><option>Error</option></select></label><label>Context <input id="notification-context" type="search" aria-label="Filter notifications by context"></label><label>Namespace <input id="notification-namespace" type="search" aria-label="Filter notifications by namespace"></label><label>Kind <input id="notification-kind" type="search" aria-label="Filter notifications by resource kind"></label><button type="button" id="notification-clear">Clear all</button></div><p id="notification-state" role="status"></p><ul id="notification-list"></ul></div>';
  const list = host.querySelector("#notification-list"), unread = host.querySelector("#notification-unread");

  function render() {
    list.replaceChildren();
    const context = host.querySelector("#notification-context").value.toLowerCase(), ns = host.querySelector("#notification-namespace").value.toLowerCase(), kind = host.querySelector("#notification-kind").value.toLowerCase(), severity = host.querySelector("#notification-severity").value;
    const items = state.items.filter(x => (!context || x.context.toLowerCase().includes(context)) && (!ns || x.namespace.toLowerCase().includes(ns)) && (!kind || x.kind.toLowerCase().includes(kind)) && (severity === "all" || x.severity === severity));
    unread.textContent = `${state.items.filter(x => !x.read).length} unread`;
    for (const item of items) {
      const li = document.createElement("li"), title = document.createElement("strong"), message = document.createElement("span"), read = document.createElement("button"), clear = document.createElement("button");
      li.dataset.notificationId = item.id; li.dataset.severity = item.severity; title.textContent = `${item.severity} · ${item.reason}`; message.textContent = item.message; read.type = clear.type = "button"; read.textContent = item.read ? "Mark unread" : "Mark read"; clear.textContent = "Clear";
      if (item.url) { const link = document.createElement("a"); link.href = item.url; link.textContent = `${item.kind}/${item.name}`; li.append(link); }
      li.append(title, message, read, clear); read.addEventListener("click", () => { item.read = !item.read; persist(); render(); }); clear.addEventListener("click", () => { state.items = state.items.filter(x => x.id !== item.id); persist(); render(); }); list.append(li);
    }
  }

  function attach() {
    const root = currentRoot();
    if (!root || root.contains(host)) return;
    // Context/namespace changes navigate to a new page, so an attached scope
    // never changes mid-page; reload state only when the scope differs.
    if (scopeKey(root) !== scope) {
      scope = scopeKey(root);
      state = loadState(root);
      host.querySelector("#notification-quiet").checked = state.quiet;
      const severitySelect = host.querySelector("#notification-severity");
      severitySelect.value = state.severities.length === 1 ? state.severities[0] : "all";
    }
    root.prepend(host);
    render();
    // The overview fragment is re-rendered several times right after page
    // load; an ingest started before a swap was discarded with its detached
    // host. Refresh from the server on every (re)attach so the newest
    // #overview always ends up with ingested events.
    ingest();
  }

  host.querySelector("#notification-quiet").checked = state.quiet;
  const severitySelect = host.querySelector("#notification-severity");
  severitySelect.value = state.severities.length === 1 ? state.severities[0] : "all";
  host.querySelector("#notification-quiet").addEventListener("change", e => { state.quiet = e.target.checked; persist(); });
  severitySelect.addEventListener("change", e => { state.severities = e.target.value === "all" ? allowed : [e.target.value]; persist(); render(); });
  host.querySelector("#notification-clear").addEventListener("click", () => {
    if (!window.confirm("Clear all notifications? This does not change cluster state.")) return;
    state.items = [];
    persist();
    render();
  });
  host.querySelector("#notification-context").addEventListener("input", render);
  host.querySelector("#notification-namespace").addEventListener("input", render);
  host.querySelector("#notification-kind").addEventListener("input", render);

  async function ingest() {
    const root = currentRoot();
    if (!root || !root.isConnected || !root.contains(host)) return;
    const base = `/c/${encodeURIComponent(root.dataset.context)}/${encodeURIComponent(root.dataset.namespace)}/overview?frame=event-records`;
    try {
      const response = await fetch(base, { credentials: "same-origin" });
      if (!response.ok) throw new Error("unavailable");
      const events = await response.json();
      // The host may have been detached and re-attached by an overview swap
      // while the fetch was in flight; the data is still valid because it is
      // keyed by context/namespace, so continue rendering into the host.
      if (!currentRoot()?.contains(host)) return;
      host.querySelector("#notification-state").textContent = response.headers.get("X-Kui-Partial") === "true" ? "Event sample is bounded and partial; it may omit newer records. Open the Events view for complete history." : "Recent notifications from live event data.";
      let added = 0;
      for (const event of events) {
        if (!allowed.includes(event.type) || !state.severities.includes(event.type) || !event.uid || !event.resourceVersion) continue;
        const id = `${safe(root.dataset.context)}|${safe(root.dataset.namespace)}|${safe(event.uid)}|${safe(event.resourceVersion)}`;
        if (state.items.some(x => x.id === id)) continue;
        const url = typeof event.url === "string" && event.url.startsWith(`/c/${encodeURIComponent(root.dataset.context)}/`) ? event.url : "";
        state.items.unshift({ id, timestamp: new Date().toISOString(), context: safe(root.dataset.context), namespace: safe(event.namespace || root.dataset.namespace), kind: safe(event.kind), name: safe(event.name), severity: event.type, reason: safe(event.reason), message: safe(event.message), read: false, url });
        added++;
      }
      state.items = state.items.slice(0, limit); persist(); render();
      if (added && !state.quiet) document.body.dispatchEvent(new CustomEvent("toast", { detail: { toast: { level: "info", message: `${added} new Kubernetes ${added === 1 ? "event" : "events"} added to notifications.` } } }));
    } catch {
      const stateLine = host.querySelector("#notification-state");
      if (stateLine) stateLine.textContent = "Event history partially unavailable; existing notifications retained.";
    }
  }

  document.body.addEventListener("htmx:sseMessage", e => {
    const root = currentRoot();
    if (!root || !root.contains(host)) return;
    if (e.detail?.type === "watch-error") {
      host.querySelector("#notification-state").textContent = "Watch error: live notification history may be partial.";
      const timestamp = new Date().toISOString();
      const message = safe(typeof e.detail.data === "string" && e.detail.data ? e.detail.data : "Live updates stopped after the API server rejected the resource watch.");
      state.items.unshift({ id: `watch-error|${safe(root.dataset.context)}|${safe(root.dataset.namespace)}|${timestamp}`, timestamp, context: safe(root.dataset.context), namespace: safe(root.dataset.namespace), kind: "Watch", name: "watch", severity: "Error", reason: "Watch error", message, read: false, url: "" });
      state.items = state.items.slice(0, limit); persist(); render();
      if (!state.quiet) document.body.dispatchEvent(new CustomEvent("toast", { detail: { toast: { level: "error", message: "A Kubernetes watch error was added to notifications." } } }));
      return;
    }
    if (e.detail?.type === "message") ingest();
  });

  attach();
  ingest();
  if (!window.kuiNotificationObserver) {
    window.kuiNotificationObserver = new MutationObserver(attach);
    window.kuiNotificationObserver.observe(document.documentElement, { childList: true, subtree: true });
  }
  window.kuiNotificationCenter = { attach };
})();
