(() => {
  "use strict";

  const SESSION_KEY = "kui.workspaces.v1";
  const SIDEBAR_KEY = "kui.sidebar.v1";
  const MAX_TABS = 12;
  const MAX_CLOSED = 5;
  let preferences = { collapsed: false, groups: {} };

  function safeRoute(value) {
    if (typeof value !== "string" || value.length > 2048) return null;
    try {
      const url = new URL(value, location.origin);
      if (url.origin !== location.origin) return null;
      const parts = url.pathname.split("/").filter(Boolean);
      if (parts.length < 2 || parts[0] !== "c" || parts.length > 6) return null;
      if (parts.some(part => !/^[A-Za-z0-9._~-]+$/.test(part))) return null;
      return url.pathname + url.search + url.hash;
    } catch {
      return null;
    }
  }

  function currentRoute() {
    return safeRoute(location.pathname + location.search + location.hash);
  }

  function loadSession() {
    try {
      const saved = JSON.parse(localStorage.getItem(SESSION_KEY));
      if (saved?.version !== 1 || !Array.isArray(saved.tabs) || saved.tabs.length > MAX_TABS) return null;
      const tabs = saved.tabs.map(tab => {
        const route = safeRoute(tab?.route);
        if (!route || typeof tab.id !== "string" || typeof tab.title !== "string") return null;
        return { id: tab.id.slice(0, 48), title: tab.title.slice(0, 80), route, pinned: tab.pinned === true };
      });
      const valid = tabs.map((tab, index) => ({ tab, index })).filter(item => item.tab);
      if (!valid.length) return null;
      const wanted = Number.isInteger(saved.active) ? saved.active : 0;
      const active = Math.max(0, valid.findIndex(item => item.index === wanted));
      const closed = Array.isArray(saved.closed) ? saved.closed.slice(0, MAX_CLOSED).flatMap(tab => {
        const route = safeRoute(tab?.route);
        return route && typeof tab.title === "string"
          ? [{ id: typeof tab.id === "string" ? tab.id.slice(0, 48) : crypto.randomUUID(), title: tab.title.slice(0, 80), route, pinned: false }]
          : [];
      }) : [];
      return { tabs: valid.map(item => item.tab), active, closed };
    } catch {
      return null;
    }
  }

  const route = currentRoute();
  const state = loadSession() || {
    tabs: [{ id: crypto.randomUUID(), title: document.title.replace(/^kui\s*[·•]\s*/, "") || "Workspace", route, pinned: false }],
    active: 0,
    closed: [],
  };
  if (route) state.tabs[state.active].route = route;
  let pendingSaveClose = null;

  function persist() {
    try {
      localStorage.setItem(SESSION_KEY, JSON.stringify({
        version: 1,
        tabs: state.tabs.slice(0, MAX_TABS),
        active: state.active,
        closed: state.closed.slice(0, MAX_CLOSED),
      }));
    } catch { /* Storage can be disabled or full; the in-memory session remains usable. */ }
  }

  function makeButton(name, text, action) {
    const button = document.createElement("button");
    button.type = "button";
    button.textContent = text;
    button.setAttribute("aria-label", name);
    button.addEventListener("click", action);
    return button;
  }

  function render() {
    let strip = document.querySelector(".workspace-strip");
    if (!strip) {
      strip = document.createElement("nav");
      strip.className = "workspace-strip";
      strip.setAttribute("aria-label", "Workspaces");
      document.querySelector(".topbar")?.after(strip);
    }
    if (!strip) return;
    strip.replaceChildren();
    const list = document.createElement("div");
    list.className = "workspace-tabs";
    list.setAttribute("role", "tablist");
    list.setAttribute("aria-label", "Workspace tabs");
    state.tabs.forEach((tab, index) => list.append(makeTab(tab, index, list)));
    strip.append(list);
    strip.append(makeButton("New workspace tab", "+", () => createTab(currentRoute())));
    strip.append(makeButton("Duplicate workspace", "Duplicate", () => createTab(state.tabs[state.active].route)));
    strip.append(makeButton("Close other workspaces", "Close others", closeOthers));
    strip.append(makeButton("Close unpinned workspaces", "Close unpinned", closeUnpinned));
    strip.append(makeButton("Reopen closed workspace", "Reopen closed", reopen));
    const scroll = makeButton("Scroll workspace tabs right", "›", () => list.scrollBy({ left: 160, behavior: "smooth" }));
    scroll.className = "workspace-scroll";
    strip.append(scroll);
    const announcement = document.createElement("span");
    announcement.id = "workspace-announcement";
    announcement.className = "sr-only";
    announcement.setAttribute("aria-live", "polite");
    strip.append(announcement);
    const saveStatus = document.createElement("span");
    saveStatus.id = "workspace-save-status";
    saveStatus.setAttribute("role", "status");
    saveStatus.setAttribute("aria-live", "polite");
    strip.append(saveStatus);
    persist();
  }

  function makeTab(tab, index, list) {
    const wrapper = document.createElement("div");
    wrapper.className = "workspace-tab";
    wrapper.draggable = true;
    const select = document.createElement("button");
    select.type = "button";
    select.setAttribute("role", "tab");
    select.className = "workspace-tab-select";
    select.textContent = `${tab.pinned ? "Pinned: " : ""}${tab.title}`;
    select.setAttribute("aria-selected", String(index === state.active));
    select.setAttribute("aria-label", `${tab.title}${tab.pinned ? ", pinned" : ""}, tab ${index + 1} of ${state.tabs.length}`);
    select.tabIndex = index === state.active ? 0 : -1;
    select.addEventListener("click", () => switchTab(index));
    select.addEventListener("keydown", event => {
      if (!["ArrowRight", "ArrowLeft", "Home", "End"].includes(event.key)) return;
      event.preventDefault();
      const next = event.key === "Home" ? 0 : event.key === "End" ? state.tabs.length - 1
        : (index + (event.key === "ArrowRight" ? 1 : -1) + state.tabs.length) % state.tabs.length;
      list.querySelectorAll('[role="tab"]')[next]?.focus();
    });
    wrapper.append(select);
    wrapper.append(makeButton("Rename tab", "Rename", () => renameTab(index)));
    wrapper.append(makeButton(tab.pinned ? "Unpin tab" : "Pin tab", tab.pinned ? "Unpin" : "Pin", () => togglePin(index)));
    wrapper.append(makeButton("Close tab", "Close", () => closeTab(index)));
    wrapper.addEventListener("dragstart", event => event.dataTransfer.setData("text/plain", String(index)));
    wrapper.addEventListener("dragover", event => event.preventDefault());
    wrapper.addEventListener("drop", event => {
      event.preventDefault();
      reorder(Number(event.dataTransfer.getData("text/plain")), index);
    });
    wrapper.addEventListener("keydown", event => {
      if (!event.altKey || !["ArrowLeft", "ArrowRight"].includes(event.key)) return;
      event.preventDefault();
      reorder(index, Math.max(0, Math.min(state.tabs.length - 1, index + (event.key === "ArrowLeft" ? -1 : 1))));
      list.querySelectorAll('[role="tab"]')[state.active]?.focus();
    });
    return wrapper;
  }

  function switchTab(index) {
    if (index === state.active || !confirmDraftDecision(() => switchTab(index))) return;
    const activeRoute = currentRoute();
    if (activeRoute) state.tabs[state.active].route = activeRoute;
    state.active = index;
    persist();
    location.assign(state.tabs[index].route);
  }

  function createTab(route) {
    const safe = safeRoute(route);
    if (!safe || state.tabs.length >= MAX_TABS) return;
    state.tabs[state.active].route = currentRoute() || state.tabs[state.active].route;
    const parts = safe.split(/[?#]/, 1)[0].split("/").filter(Boolean).map(part => decodeURIComponent(part));
    const title = parts.slice(2).join(" · ") || "Cluster overview";
    state.tabs.splice(state.active + 1, 0, {
      id: crypto.randomUUID(), title,
      route: safe, pinned: false,
    });
    state.active++;
    render();
  }

  function renameTab(index) {
    const title = prompt("Workspace name", state.tabs[index].title);
    if (title?.trim()) {
      state.tabs[index].title = title.trim().slice(0, 80);
      render();
    }
  }

  function togglePin(index) {
    state.tabs[index].pinned = !state.tabs[index].pinned;
    render();
  }

  function reorder(from, to) {
    if (from === to || from < 0 || to < 0 || from >= state.tabs.length || to >= state.tabs.length) return;
    const activeId = state.tabs[state.active].id;
    const [tab] = state.tabs.splice(from, 1);
    state.tabs.splice(to, 0, tab);
    state.active = state.tabs.findIndex(item => item.id === activeId);
    render();
    document.getElementById("workspace-announcement").textContent = `${tab.title} moved to position ${to + 1} of ${state.tabs.length}.`;
  }

  function hasDirtyYAML() {
    const editor = document.getElementById("yaml-editor");
    const original = editor?.form?.querySelector('input[name="original"]');
    return !!original && editor.value !== original.value;
  }

  function confirmDraftDecision(onSaved = () => {}) {
    if (!hasDirtyYAML()) return true;
    const decision = prompt("Unsaved YAML draft: enter save, discard, or cancel.", "cancel");
    if (decision === "discard") {
      if (typeof window.kui?.discardYAMLDraft !== "function" || !window.kui.discardYAMLDraft(true)) {
        document.getElementById("workspace-save-status").textContent = "The local YAML draft could not be discarded; the workspace remains open.";
        return false;
      }
      return true;
    }
    if (decision === "save") {
      const editor = document.getElementById("yaml-editor");
      const submit = editor?.form?.querySelector('button[type="submit"]');
      if (submit) {
        pendingSaveClose = onSaved;
        document.getElementById("workspace-save-status").textContent = "Review the YAML, then apply it to save and close this workspace.";
        submit.click();
      }
    }
    return false;
  }

  function closeTab(index) {
    const tab = state.tabs[index];
    if (tab.pinned) return;
    const wasActive = index === state.active;
    if (wasActive && !confirmDraftDecision(() => removeTab(index, wasActive))) return;
    removeTab(index, wasActive);
  }

  function removeTab(index, wasActive) {
    const [removed] = state.tabs.splice(index, 1);
    state.closed.unshift(removed);
    state.closed = state.closed.slice(0, MAX_CLOSED);
    if (!state.tabs.length) {
      const fallback = `/c/${encodeURIComponent(document.body.dataset.ctx || "default")}/_all/overview`;
      state.tabs.push({ id: crypto.randomUUID(), title: "Workspace", route: safeRoute(fallback) || fallback, pinned: false });
      state.active = 0;
    } else if (index < state.active) state.active--;
    else if (wasActive) state.active = Math.min(index, state.tabs.length - 1);
    render();
    if (wasActive) location.assign(state.tabs[state.active].route);
  }

  function closeOthers() {
    if (!confirmDraftDecision(closeOthers)) return;
    const activeId = state.tabs[state.active].id;
    state.closed.unshift(...state.tabs.filter(tab => tab.id !== activeId && !tab.pinned));
    state.tabs = state.tabs.filter(tab => tab.id === activeId || tab.pinned).slice(0, MAX_TABS);
    state.active = state.tabs.findIndex(tab => tab.id === activeId);
    state.closed = state.closed.slice(0, MAX_CLOSED);
    render();
  }

  function closeUnpinned() {
    if (!confirmDraftDecision(closeUnpinned)) return;
    const activeId = state.tabs[state.active].id;
    const activeRoute = currentRoute() || state.tabs[state.active].route;
    state.closed.unshift(...state.tabs.filter(tab => !tab.pinned));
    state.tabs = state.tabs.filter(tab => tab.pinned);
    state.closed = state.closed.slice(0, MAX_CLOSED);
    if (!state.tabs.length) {
      state.tabs.push({ id: crypto.randomUUID(), title: "Workspace", route: activeRoute, pinned: false });
      state.active = 0;
    } else {
      state.active = Math.max(0, state.tabs.findIndex(tab => tab.id === activeId));
      if (state.active >= state.tabs.length) state.active = 0;
    }
    render();
    if (!state.tabs.some(tab => tab.id === activeId)) location.assign(state.tabs[state.active].route);
  }

  function reopen() {
    const tab = state.closed[0];
    if (!tab || state.tabs.length >= MAX_TABS || !confirmDraftDecision(reopen)) return;
    state.closed.shift();
    state.tabs.splice(state.active + 1, 0, { ...tab, id: crypto.randomUUID() });
    state.active++;
    render();
    location.assign(tab.route);
  }

  function updateRoute() {
    const route = currentRoute();
    if (route && route !== state.tabs[state.active].route) {
      state.tabs[state.active].route = route;
      persist();
    }
    if (pendingSaveClose && !hasDirtyYAML()) {
      const finish = pendingSaveClose;
      pendingSaveClose = null;
      document.getElementById("workspace-save-status").textContent = "YAML saved; closing workspace.";
      finish();
    }
  }

  function setupSidebar() {
    const sidebar = document.querySelector(".sidebar");
    if (!sidebar) return;
    try {
      const stored = JSON.parse(localStorage.getItem(SIDEBAR_KEY));
      if (stored && typeof stored === "object" && !Array.isArray(stored)) preferences = stored;
    } catch { /* Ignore corrupt preferences. */ }
    preferences.collapsed = preferences.collapsed === true;
    preferences.groups = preferences.groups && typeof preferences.groups === "object" && !Array.isArray(preferences.groups)
      ? preferences.groups : {};
    const save = () => { try { localStorage.setItem(SIDEBAR_KEY, JSON.stringify(preferences)); } catch {} };
    sidebar.classList.toggle("collapsed", preferences.collapsed);
    const collapse = sidebar.querySelector(".sidebar-collapse");
    collapse?.setAttribute("aria-expanded", String(!preferences.collapsed));
    collapse?.setAttribute("aria-label", preferences.collapsed ? "Expand sidebar" : "Collapse sidebar");
    collapse?.addEventListener("click", () => {
      preferences.collapsed = !sidebar.classList.contains("collapsed");
      sidebar.classList.toggle("collapsed", preferences.collapsed);
      collapse.setAttribute("aria-expanded", String(!preferences.collapsed));
      collapse.setAttribute("aria-label", preferences.collapsed ? "Expand sidebar" : "Collapse sidebar");
      save();
    });
    sidebar.querySelectorAll("[data-sidebar-group]").forEach(group => {
      const key = group.dataset.sidebarGroup;
      const toggle = group.querySelector(".sidebar-section button");
      const update = collapsed => {
        group.classList.toggle("group-collapsed", collapsed);
        toggle?.setAttribute("aria-expanded", String(!collapsed));
        toggle?.setAttribute("aria-label", `${collapsed ? "Expand" : "Collapse"} ${group.querySelector(".sidebar-section span")?.textContent} group`);
      };
      update(preferences.groups?.[key] === true);
      toggle?.addEventListener("click", () => {
        const collapsed = !group.classList.contains("group-collapsed");
        update(collapsed);
        preferences.groups = { ...preferences.groups, [key]: collapsed };
        save();
      });
    });
    const filter = sidebar.querySelector("#sidebar-filter");
    const clear = sidebar.querySelector("#sidebar-filter-clear");
    const empty = sidebar.querySelector("#sidebar-filter-empty");
    const apply = () => {
      const needle = filter.value.trim().toLocaleLowerCase();
      let visible = 0;
      sidebar.querySelectorAll(".sidebar-link[data-resource]").forEach(link => {
        link.hidden = !!needle && !(link.dataset.label || link.textContent).toLocaleLowerCase().includes(needle);
        if (!link.hidden) visible++;
      });
      sidebar.querySelectorAll(".sidebar-group").forEach(group => {
        const matches = !!needle && group.querySelectorAll('.sidebar-link:not([hidden])').length > 0;
        const collapsed = !needle
          ? preferences.groups[group.dataset.sidebarGroup] === true
          : matches ? false : group.classList.contains("group-collapsed");
        group.classList.toggle("group-collapsed", collapsed);
        const toggle = group.querySelector(".sidebar-section button");
        toggle?.setAttribute("aria-expanded", String(!collapsed));
        toggle?.setAttribute("aria-label", `${collapsed ? "Expand" : "Collapse"} ${group.querySelector(".sidebar-section span")?.textContent} group`);
      });
      empty.hidden = visible > 0;
      clear.hidden = !filter.value;
    };
    filter?.addEventListener("input", apply);
    clear?.addEventListener("click", () => { filter.value = ""; apply(); filter.focus(); });
  }

  function isYAMLRequest(detail) {
    const target = detail?.target;
    const element = detail?.elt;
    return target?.id === "yaml-result" || element?.closest?.("#tab-yaml, #yaml-editor") ||
      element?.id === "yaml-editor";
  }

  function cancelPendingSave(message) {
    pendingSaveClose = null;
    const status = document.getElementById("workspace-save-status");
    if (status) status.textContent = message;
  }

  function handleSettle(event) {
    const result = document.getElementById("yaml-result");
    if (pendingSaveClose && isYAMLRequest(event.detail) && result?.classList.contains("yaml-error")) {
      cancelPendingSave("YAML review or apply failed; the workspace remains open.");
      return;
    }
    updateRoute();
  }

  function init() {
    render();
    setupSidebar();
    window.addEventListener("popstate", updateRoute);
    document.addEventListener("htmx:pushedIntoHistory", updateRoute, true);
    document.addEventListener("htmx:afterSettle", handleSettle, true);
    document.body.addEventListener("htmx:afterRequest", event => {
      if (pendingSaveClose && event.detail?.successful === false && isYAMLRequest(event.detail)) {
        cancelPendingSave("YAML save failed; the workspace remains open.");
        return;
      }
      updateRoute();
    });
    document.body.addEventListener("htmx:responseError", event => {
      if (pendingSaveClose && isYAMLRequest(event.detail)) {
        cancelPendingSave("YAML save failed; the workspace remains open.");
      }
    });
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init, { once: true });
  else init();
})();
