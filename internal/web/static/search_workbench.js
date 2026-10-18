(() => {
  if (window.kuiSearchWorkbenchInstalled) return;
  window.kuiSearchWorkbenchInstalled = true;

  const storageKey = "kui.searchDestinations.v1";
  const limit = 20;
  let shortcutOpener = null;
  const palette = () => document.getElementById("palette-results");
  const workbench = () => palette()?.querySelector(".search-workbench");
  const readEntries = (key) => {
    try {
      const parsed = JSON.parse(localStorage.getItem(key) || "[]");
      return Array.isArray(parsed) ? parsed.filter((item) => item && typeof item.url === "string" && item.url.startsWith("/c/") && typeof item.label === "string" && item.label.length <= 200 && typeof item.context === "string" && typeof item.namespace === "string").slice(0, limit * 2) : [];
    } catch { return []; }
  };
  const save = (entry, collection) => {
    let items = readEntries(storageKey).filter((item) => item.collection !== collection || item.url !== entry.url);
    items.unshift({ ...entry, collection });
    items = items.filter((item, index, all) => item.collection !== collection || all.findIndex((candidate) => candidate.collection === collection && candidate.url === item.url) === index);
    const favorites = items.filter((item) => item.collection === "favorite").slice(0, limit);
    const recents = items.filter((item) => item.collection === "recent").slice(0, limit);
    try { localStorage.setItem(storageKey, JSON.stringify([...favorites, ...recents])); } catch { /* Storage is optional. */ }
  };
  const scoped = (collection) => {
    const box = workbench();
    const context = box?.dataset.searchContext || location.pathname.split("/")[2] || "";
    const namespace = box?.dataset.searchNamespace || "_all";
    return readEntries(storageKey).filter((item) => item.collection === collection && item.context === context && item.namespace === namespace);
  };
  const persistURL = (url) => {
    const box = workbench();
    if (!box) return url;
    const next = new URL(url, location.origin);
    next.searchParams.set("searchReturn", "1");
    next.searchParams.set("q", box.dataset.searchQuery || "");
    next.searchParams.set("ns", box.dataset.searchNamespace || "_all");
    const kindFilter = box.dataset.kindFilter || "";
    if (kindFilter) next.searchParams.set("kindFilter", kindFilter);
    return next.pathname + next.search;
  };
  const decorate = () => {
    const root = palette();
    if (!root) return;
    const box = workbench();
    root.querySelectorAll("[data-search-result]").forEach((item) => {
      const target = persistURL(item.dataset.recentUrl || "");
      item.dataset.recentUrl = target;
      item.dataset.recentLabel = item.dataset.recentLabel || item.textContent.trim();
      item.dataset.recentFullNav = item.dataset.fullNav === "true" ? "true" : "false";
      if (item.hasAttribute("hx-get")) {
        item.setAttribute("hx-get", target);
        item.setAttribute("hx-push-url", target);
      }
      const fav = scoped("favorite").some((entry) => entry.url === target);
      if (!item.querySelector("[data-search-pin]")) {
        const pin = document.createElement("button");
        pin.type = "button";
        pin.className = "search-pin";
        pin.dataset.searchPin = "";
        pin.setAttribute("aria-label", fav ? "Unpin favorite" : "Pin favorite");
        pin.textContent = fav ? "★" : "☆";
        item.appendChild(pin);
      }
    });
    const select = root.querySelector("[data-search-kind-filter]");
    if (select) {
      const kinds = new Set();
      Array.from(select.options).forEach((option) => {
        if (option.value && kinds.has(option.value)) option.remove();
        else if (option.value) kinds.add(option.value);
      });
      const chosen = box?.dataset.kindFilter || "";
      if (chosen) select.value = chosen;
      filterKinds(select.value);
    }
    const modifier = root.querySelector("[data-search-modifier]");
    if (modifier) modifier.textContent = /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘K" : "Ctrl+K";
  };
  const filterKinds = (kind) => {
    const root = palette();
    if (!root) return;
    root.querySelectorAll("[data-search-kind]").forEach((item) => { item.hidden = Boolean(kind && item.dataset.searchKind !== kind); });
  };
  const updateQuery = (remove) => {
    const box = workbench();
    if (!box) return;
    const query = box.dataset.searchQuery || "";
    const escaped = remove.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const next = query.replace(new RegExp("(?:^|\\s+)" + escaped + ":(?:\"(?:[^\"\\\\]|\\\\.)*\"|[^\\s]+)", "i"), "").trim();
    const url = new URL(location.href);
    url.searchParams.set("q", next);
    history.replaceState(history.state, "", url.pathname + url.search);
    const input = document.getElementById("palette-input");
    if (input) { input.value = next; input.dispatchEvent(new Event("input", { bubbles: true })); }
  };
  const renderCollections = () => {
    const root = palette();
    if (!root || workbench()) return;
    root.replaceChildren();
    for (const [collection, label] of [["favorite", "Favorites"], ["recent", "Recent"]]) {
      const entries = scoped(collection);
      if (!entries.length) continue;
      const group = document.createElement("div"); group.className = "palette-group"; group.setAttribute("role", "group"); group.setAttribute("aria-label", label);
      const title = document.createElement("div"); title.className = "section-title"; title.textContent = label; group.appendChild(title);
      entries.forEach((entry, index) => {
        const row = document.createElement("div"); row.className = "palette-result" + (index === 0 && !root.querySelector(".palette-result.active") ? " active" : ""); row.setAttribute("role", "option"); row.setAttribute("aria-selected", String(row.classList.contains("active"))); row.dataset.destination = entry.url; row.textContent = entry.label;
        for (const [action, text, aria] of [["open", "Open", "Open destination"], ["pin", collection === "favorite" ? "★" : "☆", collection === "favorite" ? "Unpin favorite" : "Pin favorite"], ["remove", "×", "Remove recent destination"], ["up", "↑", "Move favorite up"], ["down", "↓", "Move favorite down"]]) {
          if ((collection === "favorite" && action === "remove") || (collection === "recent" && ["up", "down"].includes(action))) continue;
          const button = document.createElement("button"); button.type = "button"; button.dataset.destinationAction = action; button.dataset.destinationURL = entry.url; button.dataset.destinationCollection = collection; button.setAttribute("aria-label", aria); button.textContent = text; row.appendChild(button);
        }
        group.appendChild(row);
      });
      root.appendChild(group);
    }
    if (!root.childElementCount) { const empty = document.createElement("div"); empty.className = "empty-state"; empty.setAttribute("role", "status"); empty.textContent = "No favorites or recent destinations yet. Search and open a result to add one."; root.appendChild(empty); }
  };
  const openDestination = (url) => { location.href = url; };
  document.addEventListener("click", (event) => {
    const pin = event.target.closest("[data-search-pin]");
    if (pin) {
      event.preventDefault(); event.stopPropagation();
      const item = pin.closest("[data-search-result]"); if (!item) return;
      const url = item.dataset.recentUrl; const label = item.dataset.recentLabel;
      const entry = { url, label, context: workbench()?.dataset.searchContext || "", namespace: workbench()?.dataset.searchNamespace || "_all", fullNav: item.dataset.recentFullNav === "true" };
      const existing = readEntries(storageKey);
      if (existing.some((candidate) => candidate.collection === "favorite" && candidate.url === url)) {
        try { localStorage.setItem(storageKey, JSON.stringify(existing.filter((candidate) => !(candidate.collection === "favorite" && candidate.url === url)))); } catch {}
      } else save(entry, "favorite");
      pin.textContent = pin.textContent === "★" ? "☆" : "★"; pin.setAttribute("aria-label", pin.textContent === "★" ? "Unpin favorite" : "Pin favorite");
      return;
    }
    const action = event.target.closest("[data-destination-action]");
    if (action) {
      event.preventDefault(); event.stopPropagation();
      const key = action.dataset.destinationURL; const collection = action.dataset.destinationCollection; const entries = readEntries(storageKey);
      if (action.dataset.destinationAction === "open") { openDestination(key); return; }
      if (action.dataset.destinationAction === "pin") {
        const target = entries.find((item) => item.url === key && item.collection === collection);
        if (collection === "favorite") {
          try { localStorage.setItem(storageKey, JSON.stringify(entries.filter((item) => !(item.url === key && item.collection === "favorite")))); } catch {}
        } else if (target) save(target, "favorite");
      } else if (action.dataset.destinationAction === "remove") {
        try { localStorage.setItem(storageKey, JSON.stringify(entries.filter((item) => !(item.url === key && item.collection === collection)))); } catch {}
      } else {
        const favorites = entries.filter((item) => item.collection === "favorite"); const index = favorites.findIndex((item) => item.url === key); const delta = action.dataset.destinationAction === "up" ? -1 : 1; const other = index + delta;
        if (other >= 0 && other < favorites.length) { [favorites[index], favorites[other]] = [favorites[other], favorites[index]]; const rest = entries.filter((item) => item.collection !== "favorite"); try { localStorage.setItem(storageKey, JSON.stringify([...favorites.slice(0, limit), ...rest.filter((item) => item.collection === "recent").slice(0, limit)])); } catch {} }
      }
      renderCollections(); return;
    }
    const shortcut = event.target.closest("[data-search-shortcuts]");
    if (shortcut) {
      event.preventDefault();
      const existing = document.querySelector(".search-shortcut-help");
      if (existing?.open) { existing.querySelector("[data-search-help-close]")?.focus(); return; }
      const dialog = existing || document.createElement("dialog");
      shortcutOpener = shortcut;
      dialog.className = "search-shortcut-help";
      dialog.setAttribute("aria-labelledby", "search-shortcuts-title");
      const title = document.createElement("h2"); title.id = "search-shortcuts-title"; title.textContent = "Search keyboard shortcuts";
      const text = document.createElement("p"); text.textContent = "Use ↑ and ↓ to move, Enter to open, Escape to close, and " + (/Mac|iPhone|iPad/.test(navigator.platform) ? "⌘K" : "Ctrl+K") + " to open search.";
      const close = document.createElement("button"); close.type = "button"; close.textContent = "Close"; close.dataset.searchHelpClose = "";
      dialog.replaceChildren(title, text, close);
      if (!dialog.isConnected) document.body.appendChild(dialog);
      dialog.showModal();
      close.focus();
      return;
    }
    if (event.target.closest("[data-search-help-close]")) {
      const dialog = event.target.closest(".search-shortcut-help");
      if (dialog?.open) dialog.close();
      if (shortcutOpener?.isConnected) shortcutOpener.focus();
      shortcutOpener = null;
      return;
    }
    const result = event.target.closest("[data-search-result]");
    if (result && !event.defaultPrevented) {
      const entry = { url: result.dataset.recentUrl, label: result.dataset.recentLabel, context: workbench()?.dataset.searchContext || "", namespace: workbench()?.dataset.searchNamespace || "_all", fullNav: result.dataset.recentFullNav === "true" };
      save(entry, "recent");
      if (entry.fullNav) openDestination(entry.url);
      else {
        const overlay = document.getElementById("palette-overlay");
        if (overlay) { overlay.style.display = "none"; overlay.setAttribute("aria-hidden", "true"); }
        document.querySelectorAll(".app, #statusbar").forEach((element) => { element.inert = false; });
      }
    }
  });
  document.addEventListener("change", (event) => {
    if (!event.target.matches("[data-search-kind-filter]")) return;
    const box = workbench(); if (!box) return;
    box.dataset.kindFilter = event.target.value; filterKinds(event.target.value);
    const url = new URL(location.href); if (event.target.value) url.searchParams.set("kindFilter", event.target.value); else url.searchParams.delete("kindFilter");
    history.replaceState(history.state, "", url.pathname + url.search);
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      const dialog = document.querySelector(".search-shortcut-help");
      if (dialog?.open) {
        event.preventDefault();
        dialog.close();
        if (shortcutOpener?.isConnected) shortcutOpener.focus();
        shortcutOpener = null;
      }
    }
  });
  document.addEventListener("htmx:afterSwap", (event) => { if (event.detail?.target?.id === "palette-results") decorate(); });
  document.addEventListener("htmx:afterSettle", (event) => { if (event.detail?.target?.id === "palette-results") decorate(); });
  document.addEventListener("focusin", (event) => { if (event.target.id === "palette-input" && palette() && !workbench()) renderCollections(); });
  decorate();
})();
