(() => {
  if (window.kuiSearchWorkbenchInstalled) return;
  window.kuiSearchWorkbenchInstalled = true;

  const storageKey = "kui.searchDestinations.v1";
  const limit = 20;
  const historyKey = "kui.searchQueryHistory.v1";
  const savedKey = "kui.savedSearchQueries.v1";
  const libraryScope = () => {
    const path = location.pathname.split("/");
    const namespace = new URL(location.href).searchParams.get("ns") || document.querySelector(".search-workbench")?.dataset.searchNamespace || (path[3] ? decodeURIComponent(path[3]) : "_all");
    return { context: decodeURIComponent(path[2] || ""), namespace };
  };
  const rawLibrary = (key) => { try { const data = JSON.parse(localStorage.getItem(key) || "[]"); return Array.isArray(data) ? data : []; } catch { return []; } };
  const scopedLibrary = (key) => {
    const scope = libraryScope();
    return rawLibrary(key).filter((item) => item && item.context === scope.context && item.namespace === scope.namespace && typeof item.query === "string" && item.query.length <= 500 && typeof item.name === "string").slice(0, limit);
  };
  const replaceScoped = (key, entries) => {
    const scope = libraryScope();
    const other = rawLibrary(key).filter((item) => item && (item.context !== scope.context || item.namespace !== scope.namespace));
    try { localStorage.setItem(key, JSON.stringify([...entries.slice(0, limit), ...other].slice(0, limit * 20))); } catch {}
  };
  const saveHistory = (query) => {
    query = query.trim();
    if (!query || query.length > 500) return;
    const scope = libraryScope();
    let entries = scopedLibrary(historyKey).filter((entry) => entry.query !== query);
    entries.unshift({ query, name: query, ...scope, at: Date.now() });
    replaceScoped(historyKey, entries);
    decorate();
  };
  const saveQuery = (name, query) => {
    const scope = libraryScope();
    const entries = scopedLibrary(savedKey);
    if (entries.some((entry) => entry.name === name)) return false;
    entries.unshift({ name, query, ...scope });
    replaceScoped(savedKey, entries);
    return true;
  };
  const refreshLibrary = () => decorate();
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
    const queryList = root.querySelector("[data-query-library]");
    if (queryList) {
      queryList.replaceChildren();
      for (const entry of [...scopedLibrary(savedKey), ...scopedLibrary(historyKey).slice(0, 8)]) {
        const button = document.createElement("button"); button.type = "button"; button.dataset.restoreQuery = entry.query; button.textContent = entry.name; queryList.appendChild(button);
        if (scopedLibrary(savedKey).some((saved) => saved.name === entry.name && saved.query === entry.query)) { const rename = document.createElement("button"); rename.type = "button"; rename.dataset.renameQuery = entry.name; rename.textContent = "Rename"; rename.setAttribute("aria-label", `Rename ${entry.name}`); queryList.appendChild(rename); const remove = document.createElement("button"); remove.type = "button"; remove.dataset.deleteQuery = entry.name; remove.textContent = "Delete"; remove.setAttribute("aria-label", `Delete ${entry.name}`); queryList.appendChild(remove); }
      }
    }
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
    const suggestions = document.createElement("div");
    suggestions.className = "search-suggestions";
    suggestions.setAttribute("role", "group");
    suggestions.setAttribute("aria-label", "Search qualifier suggestions");
    for (const query of ["kind:Pod", "status:Running", "label:app=web", "field:metadata.name=web", "name:=web"]) {
      const button = document.createElement("button"); button.type = "button"; button.dataset.suggestQuery = query; button.textContent = query; suggestions.appendChild(button);
    }
    root.appendChild(suggestions);
    const library = document.createElement("section");
    library.className = "search-query-library";
    library.setAttribute("aria-label", "Search query library");
    const title = document.createElement("strong"); title.textContent = "Saved queries"; library.appendChild(title);
    scopedLibrary(savedKey).forEach((entry) => {
      const row = document.createElement("div"); row.className = "search-query-entry";
      const restore = document.createElement("button"); restore.type = "button"; restore.dataset.restoreQuery = entry.query; restore.textContent = entry.name; row.appendChild(restore);
      const rename = document.createElement("button"); rename.type = "button"; rename.dataset.renameQuery = entry.name; rename.dataset.query = entry.query; rename.textContent = "Rename"; rename.setAttribute("aria-label", `Rename ${entry.name}`); row.appendChild(rename);
      const remove = document.createElement("button"); remove.type = "button"; remove.dataset.deleteQuery = entry.name; remove.textContent = "Delete"; remove.setAttribute("aria-label", `Delete ${entry.name}`); row.appendChild(remove);
      library.appendChild(row);
    });
    const historyTitle = document.createElement("strong"); historyTitle.textContent = "Recent queries"; library.appendChild(historyTitle);
    scopedLibrary(historyKey).slice(0, 8).forEach((entry) => { const button = document.createElement("button"); button.type = "button"; button.className = "search-query-history"; button.dataset.restoreQuery = entry.query; button.textContent = entry.query; library.appendChild(button); });
    const saveForm = document.createElement("form"); saveForm.dataset.saveQueryForm = "";
    const name = document.createElement("input"); name.name = "name"; name.maxLength = 80; name.placeholder = "Name this query"; name.setAttribute("aria-label", "Name this query");
    const saveButton = document.createElement("button"); saveButton.type = "submit"; saveButton.textContent = "Save current query"; saveForm.append(name, saveButton); library.appendChild(saveForm);
    const exportButton = document.createElement("button"); exportButton.type = "button"; exportButton.dataset.exportQueries = ""; exportButton.textContent = "Export saved queries"; library.appendChild(exportButton);
    const importLabel = document.createElement("label"); importLabel.textContent = "Import saved queries";
    const importFile = document.createElement("input"); importFile.type = "file"; importFile.accept = "application/json,.json"; importFile.dataset.importQueries = ""; importLabel.appendChild(importFile); library.appendChild(importLabel);
    root.appendChild(library);
    if (!root.childElementCount) { const empty = document.createElement("div"); empty.className = "empty-state"; empty.setAttribute("role", "status"); empty.textContent = "No favorites or recent destinations yet. Search and open a result to add one."; root.appendChild(empty); }
  };
  const openDestination = (url) => { location.href = url; };
  document.addEventListener("click", (event) => {
    const restoreQuery = event.target.closest("[data-restore-query]");
    if (restoreQuery) { event.preventDefault(); const input = document.getElementById("palette-input"); if (input) { input.value = restoreQuery.dataset.restoreQuery; input.dispatchEvent(new Event("input", {bubbles:true})); input.focus(); } return; }
    const saveForm = event.target.closest("[data-save-query-form]");
    if (saveForm) { event.preventDefault(); const input = saveForm.elements.namedItem("name"); input.setCustomValidity(""); const query = document.getElementById("palette-input")?.value.trim() || ""; const name = input.value.trim(); if (!name || !query || name.length > 80 || query.length > 500) { input.setCustomValidity("Enter a name (up to 80 characters) and a search query (up to 500 characters)."); input.reportValidity(); return; } if (!saveQuery(name, query)) { const status = document.getElementById("search-query-library-status"); if (status) status.textContent = `A saved query named “${name}” already exists in this scope.`; return; } saveForm.reset(); refreshLibrary(); return; }
    const renameQuery = event.target.closest("[data-rename-query]");
    if (renameQuery) { const name = window.prompt("Rename saved query", renameQuery.dataset.renameQuery)?.trim(); if (name) { const entries = scopedLibrary(savedKey); const target = entries.find((entry) => entry.name === renameQuery.dataset.renameQuery); if (entries.some((entry) => entry.name === name && entry !== target)) { const status = document.getElementById("search-query-library-status"); if (status) status.textContent = `A saved query named “${name}” already exists in this scope.`; } else if (target) replaceScoped(savedKey, entries.map((entry) => entry === target ? {...entry, name} : entry)); refreshLibrary(); } return; }
    const deleteQuery = event.target.closest("[data-delete-query]");
    if (deleteQuery) { const entries = scopedLibrary(savedKey); const deleted = entries.find((entry) => entry.name === deleteQuery.dataset.deleteQuery); if (deleted) { replaceScoped(savedKey, entries.filter((entry) => entry !== deleted)); refreshLibrary(); const button = document.createElement("button"); button.textContent = `Undo deleting ${deleted.name}`; button.dataset.undoQuery = ""; button.dataset.queryEntry = JSON.stringify(deleted); palette()?.append(button); } return; }
    const undo = event.target.closest("[data-undo-query]");
    if (undo) { try { const entry = JSON.parse(undo.dataset.queryEntry); saveQuery(entry.name, entry.query); undo.remove(); refreshLibrary(); } catch {} return; }
    if (event.target.closest("[data-export-queries]")) { const content = JSON.stringify({version:1, queries:scopedLibrary(savedKey).map(({name,query,context,namespace})=>({name,query,context,namespace}))}, null, 2); const link=document.createElement("a"); link.href=URL.createObjectURL(new Blob([content],{type:"application/json"})); link.download="kui-saved-queries.json"; link.click(); URL.revokeObjectURL(link.href); return; }
    const suggestion = event.target.closest("[data-suggest-query]");
    if (suggestion) { const input = document.getElementById("palette-input"); if (input) { input.value = suggestion.dataset.suggestQuery; input.dispatchEvent(new Event("input", {bubbles:true})); input.focus(); } return; }
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
  document.addEventListener("change", async (event) => {
    if (!event.target.matches("[data-import-queries]") || !event.target.files?.[0]) return;
    const file = event.target.files[0], status = document.getElementById("search-query-library-status");
    try {
      if (file.size > 32768) throw new Error("Import file exceeds 32 KB.");
      const data = JSON.parse(await file.text()), scope = libraryScope();
      if (!data || Object.keys(data).sort().join(",") !== "queries,version" || data.version !== 1 || !Array.isArray(data.queries) || data.queries.length > limit) throw new Error("Unsupported query file version or schema.");
      const seen = new Set(), incoming = data.queries.map((entry) => {
        if (!entry || Object.keys(entry).sort().join(",") !== "context,name,namespace,query" || typeof entry.name !== "string" || !entry.name.trim() || entry.name.length > 80 || typeof entry.query !== "string" || !entry.query.trim() || entry.query.length > 500 || entry.context !== scope.context || entry.namespace !== scope.namespace) throw new Error("Invalid query entry or scope mismatch.");
        if (seen.has(entry.name)) throw new Error("Duplicate query name.");
        seen.add(entry.name); return {name:entry.name.trim(), query:entry.query, ...scope};
      });
      if (scopedLibrary(savedKey).some((entry) => seen.has(entry.name))) throw new Error("A query with an imported name already exists in this scope.");
      replaceScoped(savedKey, [...incoming, ...scopedLibrary(savedKey)]);
      refreshLibrary(); const live = document.getElementById("search-query-library-status"); if (live) live.textContent = `Imported ${incoming.length} saved queries.`;
    } catch (error) { if (status) status.textContent = `Import failed: ${error.message}`; }
    event.target.value = "";
  });
  document.addEventListener("input", (event) => {
    if (event.target.id !== "palette-input") return;
    const value = event.target.value;
    clearTimeout(event.target._queryHistoryTimer);
    event.target._queryHistoryTimer = setTimeout(() => { saveHistory(value); }, 400);
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
