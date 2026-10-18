(() => {
  "use strict";

  const VIEW_FORMAT = "kui-resource-views";
  const VIEW_VERSION = 1;
  const MAX_VIEW_BYTES = 128 * 1024;
  const MAX_VIEWS = 40;
  const MAX_SELECTOR_LENGTH = 2048;
  const MIN_COLUMN_WIDTH = 80;
  const MAX_COLUMN_WIDTH = 1200;
  const NAME_COLUMN = "Name";
  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  const clone = (value) => JSON.parse(JSON.stringify(value));
  const liveState = { scope: null, table: null, defaultLayout: [], layoutDraft: [], layoutBeforeEdit: null, layoutOpen: false, activeView: "", comparison: [], comparisonData: [], comparisonSequence: 0, comparisonFetchedAt: 0, comparisonScope: null, pendingViewState: null, requestRollback: null, comparisonController: null };
  let resizeState = null;

  function scopeFor(table) {
    const route = location.pathname.split("/").filter(Boolean);
    const context = document.body.dataset.ctx || route[1] || "";
    const namespace = $("#namespace-select")?.value || route[2] || "_all";
    const resourceKind = route[3] || "unknown";
    const kind = $(".toolbar[data-resource-version] strong")?.textContent.trim() || resourceKind;
    return { context, namespace, resourceKind, kind };
  }

  function layoutKey(scope) {
    return `kui.table-layout.v1:${encodeURIComponent(scope.context)}:${encodeURIComponent(scope.resourceKind)}`;
  }

  function viewsKey(scope) {
    return `kui.resource-views.v1:${encodeURIComponent(scope.context)}:${encodeURIComponent(scope.namespace)}:${encodeURIComponent(scope.resourceKind)}`;
  }

  function encodeColumn(label, occurrence) {
    return `c:${label}:${occurrence}`;
  }

  function tableColumns(table) {
    const row = $("thead tr", table);
    if (!row) return [];
    const occurrences = new Map();
    return Array.from(row.cells).map((cell) => {
      if (cell.classList.contains("select-col")) {
        cell.dataset.kuiColumn = "__select";
        return { id: "__select", label: "Selection", locked: true, visible: true, width: null };
      }
      const label = (cell.dataset.kuiLabel || cell.textContent).trim();
      cell.dataset.kuiLabel = label;
      const occurrence = occurrences.get(label) || 0;
      occurrences.set(label, occurrence + 1);
      const id = cell.dataset.kuiColumn || encodeColumn(label, occurrence);
      cell.dataset.kuiColumn = id;
      return { id, label, locked: label.toLowerCase() === NAME_COLUMN.toLowerCase(), visible: true, width: null };
    }).filter((column) => column.id !== "__select");
  }

  function assignCellColumns(table) {
    const headers = Array.from($("thead tr", table)?.cells || []);
    headers.forEach((th) => {
      if (th.classList.contains("select-col")) th.dataset.kuiColumn = "__select";
    });
    const allRows = $$("tbody tr", table);
    allRows.forEach((row) => {
      if (row.cells.length !== headers.length) return;
      Array.from(row.cells).forEach((cell, index) => {
        const header = headers[index];
        cell.dataset.kuiColumn = header.classList.contains("select-col") ? "__select" : header.dataset.kuiColumn;
      });
    });
  }

  function defaultLayout(table) {
    assignCellColumns(table);
    const columns = tableColumns(table);
    if (columns.length && !columns.some((column) => column.locked)) columns[0].locked = true;
    return columns.map((column) => ({ id: column.id, label: column.label, locked: column.locked, visible: true, width: null }));
  }

  function normalizeLayout(raw, defaults) {
    const current = new Map(defaults.map((column) => [column.id, column]));
    const normalized = [];
    const seen = new Set();
    if (Array.isArray(raw)) {
      raw.forEach((entry) => {
        if (!entry || typeof entry.id !== "string" || seen.has(entry.id) || !current.has(entry.id)) return;
        const base = current.get(entry.id);
        const width = Number.isFinite(entry.width) && entry.width >= MIN_COLUMN_WIDTH && entry.width <= MAX_COLUMN_WIDTH ? Math.round(entry.width) : null;
        normalized.push({ id: base.id, label: base.label, locked: base.locked, visible: base.locked || entry.visible !== false, width });
        seen.add(entry.id);
      });
    }
    defaults.forEach((column) => {
      if (!seen.has(column.id)) normalized.push({ ...column, visible: true, width: null });
    });
    return normalized;
  }

  function readLayout(scope, defaults) {
    try {
      const stored = localStorage.getItem(layoutKey(scope));
      if (!stored) return clone(defaults);
      const parsed = JSON.parse(stored);
      if (!Array.isArray(parsed) || parsed.length > 200) return clone(defaults);
      return normalizeLayout(parsed, defaults);
    } catch {
      announce("Saved column layout is unavailable; using the default layout.", true);
      return clone(defaults);
    }
  }

  function persistLayout() {
    if (!liveState.scope) return;
    try {
      localStorage.setItem(layoutKey(liveState.scope), JSON.stringify(liveState.layoutDraft));
      liveState.layoutBeforeEdit = clone(liveState.layoutDraft);
      updateLayoutStatus("Layout saved for this context and resource kind.");
    } catch {
      updateLayoutStatus("Could not save this layout in local storage.", true);
    }
  }

  function currentSelectHeader(table) {
    return $("thead .select-col", table);
  }

  function applyLayout(layout, table = liveState.table) {
    if (!table?.isConnected) return;
    assignCellColumns(table);
    const headers = $("thead tr", table);
    if (!headers) return;
    const byID = new Map(Array.from(headers.cells).map((cell) => [cell.dataset.kuiColumn, cell]));
    const select = currentSelectHeader(table);
    if (select) headers.appendChild(select);
    layout.forEach((column) => {
      const cell = byID.get(column.id);
      if (cell) headers.appendChild(cell);
    });
    const orderedHeaderIDs = Array.from(headers.cells).map((cell) => cell.dataset.kuiColumn);
    const pane = table.closest(".list-pane");
    if (pane) {
      const sortedHeader = pane.dataset.sortKey && Array.from(headers.cells).find((cell) => cell.textContent.trim() === pane.dataset.sortKey);
      if (sortedHeader) pane.dataset.sortCol = String(Array.from(sortedHeader.parentElement.children).indexOf(sortedHeader));
      else delete pane.dataset.sortCol;
    }
    $$("tbody tr", table).forEach((row) => {
      if (row.cells.length !== orderedHeaderIDs.length) return;
      const rowByID = new Map(Array.from(row.cells).map((cell) => [cell.dataset.kuiColumn, cell]));
      orderedHeaderIDs.forEach((id) => {
        const cell = rowByID.get(id);
        if (cell) row.appendChild(cell);
      });
    });
    layout.forEach((column) => {
      const visible = column.locked || column.visible;
      const width = column.width ? `${column.width}px` : "";
      $$(`[data-kui-column="${CSS.escape(column.id)}"]`, table).forEach((cell) => {
        cell.hidden = !visible;
        cell.style.width = width;
        cell.style.minWidth = column.width ? `${MIN_COLUMN_WIDTH}px` : "";
      });
    });
    if (select) {
      select.hidden = false;
      select.style.width = "";
      $$('.select-col[data-kui-column="__select"]', table).forEach((cell) => { cell.hidden = false; });
    }
    $$('tbody tr:not([data-id]) td[colspan]', table).forEach((cell) => { cell.colSpan = orderedHeaderIDs.length; });
    installResizeHandles(table, layout);
    updateColumnCount(layout);
  }

  function installResizeHandles(table, layout) {
    const layoutByID = new Map(layout.map((column) => [column.id, column]));
    $$("thead th:not(.select-col)", table).forEach((header) => {
      if ($(".kui-column-resize", header)) return;
      const column = layoutByID.get(header.dataset.kuiColumn);
      if (!column) return;
      const handle = document.createElement("button");
      handle.type = "button";
      handle.className = "kui-column-resize";
      handle.dataset.column = column.id;
      handle.setAttribute("aria-label", `Resize ${column.label} column`);
      handle.title = `Resize ${column.label} column`;
      handle.setAttribute("aria-keyshortcuts", "ArrowLeft ArrowRight");
      header.appendChild(handle);
    });
  }

  function renderWorkbench(table) {
    const pane = table.closest(".list-pane");
    if (!pane) return;
    let root = $(".kui-workbench", pane);
    if (!root) {
      root = createWorkbench();
      const wrap = table.closest(".resource-table-wrap");
      (wrap || table).before(root);
    }
    const previousScope = liveState.scope;
    const previousLayout = liveState.layoutDraft;
    liveState.scope = scopeFor(table);
    const comparisonScopeChanged = liveState.comparisonScope && ["context", "namespace", "resourceKind"].some((key) => liveState.comparisonScope[key] !== liveState.scope[key]);
    if (comparisonScopeChanged) {
      liveState.comparisonController?.abort();
      liveState.comparisonSequence++;
      liveState.comparison = [];
      liveState.comparisonData = [];
      liveState.comparisonFetchedAt = 0;
      liveState.comparisonScope = null;
    }
    liveState.table = table;
    liveState.defaultLayout = defaultLayout(table);
    liveState.layoutDraft = previousScope && layoutKey(previousScope) === layoutKey(liveState.scope)
      ? normalizeLayout(previousLayout, liveState.defaultLayout)
      : readLayout(liveState.scope, liveState.defaultLayout);
    if (liveState.pendingViewState) {
      liveState.layoutDraft = normalizeLayout(liveState.pendingViewState.layout, liveState.defaultLayout);
      liveState.pendingViewState = null;
    }
    applyLayout(liveState.layoutDraft, table);
    renderColumnChooser(root);
    renderViewControls(root);
    syncRowFilterMirror();
    updateViewDirtyLabel();
    const layoutPanel = $("#table-view-panel", root);
    layoutPanel.hidden = !liveState.layoutOpen;
    $("#table-view-button", root).setAttribute("aria-expanded", String(liveState.layoutOpen));
    const comparison = ensureComparisonPanel();
    if (comparisonScopeChanged) {
      comparison.hidden = true;
      setComparisonStatus("Comparison cleared because the resource scope changed.");
    }
    if (!liveState.comparison.length) renderComparisonMembers(comparison);
    $("#compare-selected", root).setAttribute("aria-expanded", String(!comparison.hidden));
  }

  function createWorkbench() {
    const root = document.createElement("section");
    root.className = "kui-workbench";
    root.setAttribute("aria-label", "Table layout, views, and comparison");
    root.innerHTML = `<div class="kui-workbench-bar">
      <button type="button" class="btn" id="table-view-button" aria-expanded="false" aria-controls="table-view-panel">Columns and views</button>
      <label class="kui-saved-view-label" for="saved-view-select">Saved view</label>
      <select id="saved-view-select" aria-label="Saved resource view"><option value="">Default view</option></select>
      <span class="kui-view-state" id="kui-view-state" role="status" aria-live="polite"></span>
      <button type="button" class="btn" id="compare-selected" aria-expanded="false" aria-controls="comparison-panel">Compare selected</button>
    </div>
    <section id="table-view-panel" class="kui-view-panel" aria-labelledby="table-view-button" hidden>
      <div class="kui-panel-heading"><h2>Table layout and saved views</h2><button type="button" class="btn" data-action="layout-cancel">Cancel preview</button></div>
      <p class="kui-view-help">Column changes preview immediately. The resource name or primary identifier stays visible so each item remains identifiable.</p>
      <p class="kui-column-count" id="kui-column-count" aria-live="polite"></p>
      <ol class="kui-column-list" aria-label="Table columns"></ol>
      <div class="kui-actions">
        <button type="button" class="btn" data-action="layout-reset">Reset layout</button>
        <button type="button" class="btn btn-primary" data-action="layout-save">Save layout</button>
      </div>
      <hr>
      <div class="kui-view-actions" aria-label="Saved view actions">
        <button type="button" class="btn" data-action="view-create">Create</button>
        <button type="button" class="btn" data-action="view-update">Update</button>
        <button type="button" class="btn" data-action="view-rename">Rename</button>
        <button type="button" class="btn" data-action="view-duplicate">Duplicate</button>
        <button type="button" class="btn btn-danger" data-action="view-delete">Delete</button>
        <button type="button" class="btn" data-action="view-export">Export JSON</button>
        <button type="button" class="btn" data-action="view-import">Import JSON</button>
        <input type="file" class="kui-import-file" accept="application/json,.json" aria-label="Choose a saved views JSON file" hidden>
        <button type="button" class="btn" data-action="view-default">Return to default view</button>
      </div>
      <p class="kui-view-message" role="status" aria-live="polite"></p>
    </section></div>`;
    return root;
  }

  function layoutLabel(layout) {
    const visible = layout.filter((column) => column.visible || column.locked).length;
    const total = layout.length;
    return `${visible} of ${total} columns visible`;
  }

  function updateColumnCount(layout = liveState.layoutDraft) {
    const count = document.getElementById("kui-column-count");
    if (count) count.textContent = layoutLabel(layout);
  }

  function updateLayoutStatus(text, error = false) {
    const node = $(".kui-view-message", $(".kui-workbench"));
    if (node) {
      node.textContent = text;
      node.classList.toggle("is-error", error);
    }
  }

  function announce(message, error = false) {
    const status = $("#kui-view-state");
    if (status) {
      status.textContent = message;
      status.classList.toggle("is-error", error);
    }
  }

  function renderColumnChooser(root) {
    const list = $(".kui-column-list", root);
    if (!list) return;
    list.replaceChildren();
    liveState.layoutDraft.forEach((column, index) => {
      const item = document.createElement("li");
      item.draggable = true;
      item.dataset.column = column.id;
      item.setAttribute("aria-label", `${column.label}, position ${index + 1} of ${liveState.layoutDraft.length}`);
      const title = document.createElement("span");
      title.className = "kui-column-title";
      title.textContent = column.label;
      const visibleLabel = document.createElement("label");
      visibleLabel.className = "kui-column-visible";
      const visible = document.createElement("input");
      visible.type = "checkbox";
      visible.checked = column.locked || column.visible;
      visible.disabled = column.locked;
      visible.dataset.action = "column-visible";
      visible.dataset.column = column.id;
      visible.setAttribute("aria-label", column.locked ? `${column.label} column is always visible` : `Show ${column.label} column`);
      visibleLabel.append(visible, document.createTextNode(" Visible"));
      if (column.locked) visibleLabel.title = `The ${column.label} column cannot be hidden because it identifies each resource.`;
      const widthLabel = document.createElement("label");
      widthLabel.className = "kui-column-width";
      widthLabel.append(document.createTextNode("Width"));
      const width = document.createElement("input");
      width.type = "range";
      width.min = String(MIN_COLUMN_WIDTH);
      width.max = String(MAX_COLUMN_WIDTH);
      width.step = "10";
      width.value = String(column.width || Math.max(MIN_COLUMN_WIDTH, Math.round($( `thead [data-kui-column="${CSS.escape(column.id)}"]`, liveState.table)?.getBoundingClientRect().width || 160)));
      width.dataset.action = "column-width";
      width.dataset.column = column.id;
      width.setAttribute("aria-label", `${column.label} column width`);
      widthLabel.append(width);
      const widthValue = document.createElement("output");
      widthValue.textContent = column.width ? `${column.width}px` : "Auto";
      widthValue.dataset.widthFor = column.id;
      const up = document.createElement("button");
      up.type = "button";
      up.className = "btn kui-reorder";
      up.dataset.action = "column-up";
      up.dataset.column = column.id;
      up.textContent = "Move up";
      up.setAttribute("aria-label", `Move ${column.label} earlier`);
      up.disabled = index === 0;
      const down = document.createElement("button");
      down.type = "button";
      down.className = "btn kui-reorder";
      down.dataset.action = "column-down";
      down.dataset.column = column.id;
      down.textContent = "Move down";
      down.setAttribute("aria-label", `Move ${column.label} later`);
      down.disabled = index === liveState.layoutDraft.length - 1;
      item.append(title, visibleLabel, widthLabel, widthValue, up, down);
      list.appendChild(item);
    });
    updateColumnCount();
  }

  function getScopedViews(scope = liveState.scope) {
    if (!scope) return { views: [], error: "" };
    try {
      const raw = localStorage.getItem(viewsKey(scope));
      if (!raw) return { views: [], error: "" };
      if (new Blob([raw]).size > MAX_VIEW_BYTES) return { views: [], error: "Saved views exceed the storage limit and cannot be loaded." };
      const parsed = JSON.parse(raw);
      if (!exactKeys(parsed, ["version", "views"]) || parsed.version !== VIEW_VERSION || !Array.isArray(parsed.views) || parsed.views.length > MAX_VIEWS) {
        return { views: [], error: "Saved views have an unsupported or invalid schema." };
      }
      const views = [];
      for (const view of parsed.views) {
        const checked = validateView(view, scope, false);
        if (!checked.ok) return { views: [], error: "A saved view is invalid; it was not applied." };
        views.push(checked.view);
      }
      return { views, error: "" };
    } catch {
      return { views: [], error: "Saved views could not be read from local storage." };
    }
  }

  function saveViews(views) {
    const payload = JSON.stringify({ version: VIEW_VERSION, views });
    if (views.length > MAX_VIEWS || !views.every((view) => validateView(view, liveState.scope, false).ok) || new Blob([payload]).size > MAX_VIEW_BYTES) {
      updateLayoutStatus("Saved views exceed the local storage limit.", true);
      return false;
    }
    try {
      localStorage.setItem(viewsKey(liveState.scope), payload);
      return true;
    } catch {
      updateLayoutStatus("Could not save views in local storage.", true);
      return false;
    }
  }

  function exactKeys(value, keys) {
    return value && typeof value === "object" && !Array.isArray(value) && Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key));
  }

  function safeLabelSelector(value) {
    if (typeof value !== "string" || value.length > MAX_SELECTOR_LENGTH || /[\u0000-\u001f\u007f\\;{}\[\]"'<>]/.test(value)) return false;
    if (!value.trim()) return true;
    const chunks = [];
    let part = "";
    let depth = 0;
    for (const char of value) {
      if (char === "(" && ++depth > 1) return false;
      if (char === ")" && --depth < 0) return false;
      if (char === "," && depth === 0) { chunks.push(part.trim()); part = ""; }
      else part += char;
    }
    if (depth !== 0) return false;
    chunks.push(part.trim());
    const key = "[A-Za-z0-9_.\\/-]+";
    const val = "[A-Za-z0-9_.\\/-]*";
    return chunks.every((chunk) => new RegExp(`^(?:!?${key}|${key}\\s*(?:=|==|!=)\\s*${val}|${key}\\s+(?:in|notin)\\s*\\(\\s*${val}(?:\\s*,\\s*${val})*\\s*\\))$`).test(chunk));
  }

  function safeFieldSelector(value) {
    if (typeof value !== "string" || value.length > MAX_SELECTOR_LENGTH || /[\u0000-\u001f\u007f\\;{}\[\]"'<>]/.test(value)) return false;
    if (!value.trim()) return true;
    const key = "[A-Za-z0-9_.\\/-]+";
    return value.split(",").every((part) => new RegExp(`^\\s*${key}\\s*(?:=|==|!=)\\s*[^,]*\\s*$`).test(part));
  }

  function validateView(view, scope, imported = true) {
    if (!exactKeys(view, ["id", "name", "scope", "state"]) || typeof view.id !== "string" || !/^[a-zA-Z0-9_-]{1,80}$/.test(view.id) || typeof view.name !== "string" || !view.name.trim() || view.name.length > 100) return { ok: false };
    if (!exactKeys(view.scope, ["context", "namespace", "resourceKind"]) || view.scope.context !== scope.context || view.scope.namespace !== scope.namespace || view.scope.resourceKind !== scope.resourceKind) return { ok: false };
    const state = view.state;
    if (!exactKeys(state, ["selectors", "filters", "sort", "pageSize", "density", "layout"]) || !exactKeys(state.selectors, ["label", "field"]) || !exactKeys(state.filters, ["text", "column"]) || !exactKeys(state.sort, ["key", "direction"])) return { ok: false };
    if (!safeLabelSelector(state.selectors.label) || !safeFieldSelector(state.selectors.field)) return { ok: false };
    if (typeof state.filters.text !== "string" || state.filters.text.length > 512 || typeof state.filters.column !== "string" || state.filters.column.length > 100) return { ok: false };
    if (state.filters.column !== "" && !liveState.defaultLayout.some((column) => column.label === state.filters.column)) return { ok: false };
    if (state.sort.key !== "" && !liveState.defaultLayout.some((column) => column.label === state.sort.key)) return { ok: false };
    if (!(["asc", "desc"].includes(state.sort.direction)) || !Number.isInteger(state.pageSize) || state.pageSize < 1 || state.pageSize > 1000 || !["default", "compact", "comfortable"].includes(state.density)) return { ok: false };
    if (!Array.isArray(state.layout) || state.layout.length > 200) return { ok: false };
    const columns = [];
    const seen = new Set();
    for (const column of state.layout) {
      if (!exactKeys(column, ["id", "visible", "width"]) || typeof column.id !== "string" || seen.has(column.id) || typeof column.visible !== "boolean" || (column.width !== null && (!Number.isInteger(column.width) || column.width < MIN_COLUMN_WIDTH || column.width > MAX_COLUMN_WIDTH))) return { ok: false };
      const base = liveState.defaultLayout.find((candidate) => candidate.id === column.id);
      if (!base) return { ok: false };
      if (base.locked && !column.visible) return { ok: false };
      columns.push({ id: base.id, label: base.label, locked: base.locked, visible: base.locked || column.visible, width: column.width });
      seen.add(column.id);
    }
    const normalizedLayout = normalizeLayout(columns, liveState.defaultLayout);
    const safe = { id: view.id, name: view.name.trim(), scope: { context: scope.context, namespace: scope.namespace, resourceKind: scope.resourceKind }, state: { selectors: { label: state.selectors.label, field: state.selectors.field }, filters: { text: state.filters.text, column: state.filters.column }, sort: { key: state.sort.key, direction: state.sort.direction }, pageSize: state.pageSize, density: state.density, layout: normalizedLayout.map(({ id, visible, width }) => ({ id, visible, width })) } };
    if (imported && !safeLabelSelector(safe.state.selectors.label)) return { ok: false };
    return { ok: true, view: safe };
  }

  function currentState() {
    const table = liveState.table;
    const pane = table?.closest(".list-pane");
    const localFilter = $("#row-filter", pane) || $("#row-filter");
    const label = $("#label-selector", pane)?.value || "";
    const field = $("#field-selector", pane)?.value || "";
    const pageSize = Number.parseInt($("#page-size", pane)?.value || "200", 10);
    return {
      selectors: { label: label.trim(), field: field.trim() },
      filters: { text: localFilter?.value || "", column: $("#column-filter", pane)?.value || "" },
      sort: { key: pane?.dataset.sortKey || "", direction: pane?.dataset.sortDir === "desc" ? "desc" : "asc" },
      pageSize: Number.isInteger(pageSize) && pageSize > 0 ? pageSize : 200,
      density: document.body.dataset.rowDensity || "default",
      layout: liveState.layoutDraft.map(({ id, visible, width }) => ({ id, visible, width }))
    };
  }

  function currentViewList() {
    return getScopedViews().views;
  }

  function selectedView() {
    const id = $("#saved-view-select")?.value || "";
    return id ? currentViewList().find((view) => view.id === id) : null;
  }

  function renderViewControls(root) {
    const select = $("#saved-view-select", root);
    const previous = select?.value || liveState.activeView;
    if (!select) return;
    const result = getScopedViews();
    select.replaceChildren(new Option("Default view", ""));
    result.views.forEach((view) => select.add(new Option(view.name, view.id)));
    select.value = result.views.some((view) => view.id === previous) ? previous : "";
    liveState.activeView = select.value;
    if (result.error) updateLayoutStatus(result.error, true);
    syncViewActionAvailability(root);
  }

  function syncViewActionAvailability(root = $(".kui-workbench")) {
    const hasView = !!$("#saved-view-select", root)?.value;
    ["view-update", "view-rename", "view-duplicate", "view-delete"].forEach((action) => {
      const button = $(`[data-action="${action}"]`, root);
      if (button) button.disabled = !hasView;
    });
  }

  function updateViewDirtyLabel() {
    const select = $("#saved-view-select");
    const state = $("#kui-view-state");
    if (!select || !state) return;
    const view = selectedView();
    const current = JSON.stringify(currentState());
    const saved = view ? JSON.stringify(view.state) : "";
    state.textContent = view ? (current === saved ? `Active view: ${view.name}` : `Active view: ${view.name} · unsaved changes`) : "Default view";
    if (!view && isNonDefaultState(currentState())) state.textContent = "Default view · unsaved changes";
    syncViewActionAvailability();
  }

  function isNonDefaultState(state) {
    return !!(state.selectors.label || state.selectors.field || state.filters.text || state.filters.column || state.sort.key || state.pageSize !== 200 || state.density !== "default" || state.layout.some((column, index) => !column.visible || column.width || column.id !== liveState.defaultLayout[index]?.id));
  }

  function syncRowFilterMirror(source) {
    const pane = liveState.table?.closest(".list-pane");
    const inTable = $("#row-filter", pane);
    const topbar = $(".topbar #row-filter");
    if (!inTable || !topbar) return;
    if (source === inTable) topbar.value = inTable.value;
    else if (source === topbar) inTable.value = topbar.value;
    else if (document.activeElement === inTable) topbar.value = inTable.value;
    else inTable.value = topbar.value;
  }

  function setLayout(layout, preview = true) {
    liveState.layoutDraft = normalizeLayout(layout, liveState.defaultLayout);
    applyLayout(liveState.layoutDraft);
    renderColumnChooser($(".kui-workbench"));
    if (preview) updateLayoutStatus("Layout preview updated. Save it to keep these columns, or cancel to revert.");
    updateViewDirtyLabel();
  }

  function reorderColumn(id, direction) {
    const from = liveState.layoutDraft.findIndex((column) => column.id === id);
    const to = from + direction;
    if (from < 0 || to < 0 || to >= liveState.layoutDraft.length) return;
    const next = clone(liveState.layoutDraft);
    [next[from], next[to]] = [next[to], next[from]];
    setLayout(next);
    const moved = $(`.kui-column-list [data-column="${CSS.escape(id)}"]`);
    moved?.querySelector(".kui-reorder:not(:disabled)")?.focus();
    updateLayoutStatus(`${next[to].label} moved to position ${to + 1} of ${next.length}.`);
  }

  function viewPrompt(message, initial = "") {
    const value = window.prompt(message, initial);
    return value === null ? null : value.trim();
  }

  function makeID() {
    if (window.crypto?.randomUUID) return crypto.randomUUID().replaceAll("-", "");
    return `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`;
  }

  function saveCurrentAsView(name, id = makeID()) {
    const views = currentViewList();
    if (!name || name.length > 100) { updateLayoutStatus("Enter a view name up to 100 characters.", true); return; }
    if (views.some((view) => view.id !== id && view.name.toLocaleLowerCase() === name.toLocaleLowerCase())) { updateLayoutStatus("A view with that name already exists in this scope.", true); return; }
    const view = { id, name, scope: { context: liveState.scope.context, namespace: liveState.scope.namespace, resourceKind: liveState.scope.resourceKind }, state: currentState() };
    const checked = validateView(view, liveState.scope);
    if (!checked.ok) { updateLayoutStatus("The current table state cannot be saved as a view.", true); return; }
    const index = views.findIndex((candidate) => candidate.id === id);
    if (index >= 0) views[index] = checked.view;
    else views.push(checked.view);
    if (saveViews(views)) {
      liveState.activeView = id;
      renderViewControls($(".kui-workbench"));
      $("#saved-view-select").value = id;
      updateViewDirtyLabel();
      updateLayoutStatus(`Saved view “${name}”.`);
    }
  }

  function selectorValuesSafe(state) {
    return safeLabelSelector(state.selectors.label) && safeFieldSelector(state.selectors.field);
  }

  function requestViewState(state, viewID = "") {
    const pane = liveState.table?.closest(".list-pane");
    if (!pane || !window.htmx) return;
    if (!selectorValuesSafe(state)) { updateLayoutStatus("View selectors are malformed or unsafe; nothing was applied.", true); return; }
    liveState.requestRollback = {
      state: currentState(),
      viewID: liveState.activeView,
      url: location.pathname + location.search,
      listUrl: pane.dataset.listUrl || location.href,
      pageHistory: pane.dataset.pageHistory || "[]"
    };
    const label = $("#label-selector", pane);
    const field = $("#field-selector", pane);
    const page = $("#page-size", pane);
    if (label) label.value = state.selectors.label;
    if (field) field.value = state.selectors.field;
    if (page) page.value = String(state.pageSize);
    const filter = $("#row-filter", pane) || $(".topbar #row-filter");
    if (filter) filter.value = state.filters.text;
    const topFilter = $(".topbar #row-filter");
    if (topFilter) topFilter.value = state.filters.text;
    const column = $("#column-filter", pane);
    if (column) column.value = Array.from(column.options).some((option) => option.value === state.filters.column) ? state.filters.column : "";
    pane.dataset.sortKey = liveState.defaultLayout.some((item) => item.label === state.sort.key) ? state.sort.key : "";
    liveState.pendingViewState = state;
    setLayout(state.layout, false);
    document.body.dataset.rowDensity = state.density;
    const densityControl = $("#row-density", pane);
    if (densityControl) densityControl.value = state.density;
    const baseURL = new URL(pane.dataset.listUrl || location.href, location.origin);
    ["frame", "continue", "labelSelector", "fieldSelector", "limit", "rowFilter", "columnFilter", "sortColumn", "sortDir"].forEach((key) => baseURL.searchParams.delete(key));
    if (state.selectors.label) baseURL.searchParams.set("labelSelector", state.selectors.label);
    if (state.selectors.field) baseURL.searchParams.set("fieldSelector", state.selectors.field);
    baseURL.searchParams.set("limit", String(state.pageSize));
    if (state.filters.text) baseURL.searchParams.set("rowFilter", state.filters.text);
    if (state.filters.column && column?.value) baseURL.searchParams.set("columnFilter", column.value);
    if (pane.dataset.sortKey) {
      baseURL.searchParams.set("sortColumn", pane.dataset.sortKey);
      baseURL.searchParams.set("sortDir", state.sort.direction);
    }
    const pageURL = new URL(baseURL);
    baseURL.searchParams.set("frame", "table");
    pane.dataset.pageHistory = "[]";
    pane.dataset.listUrl = baseURL.toString();
    pane.setAttribute("hx-get", baseURL.toString());
    history.replaceState(history.state, "", pageURL.pathname + pageURL.search);
    liveState.activeView = viewID;
    window.htmx.ajax("GET", baseURL.toString(), { target: pane, swap: "innerHTML" });
    updateViewDirtyLabel();
    announce(viewID ? "Saved view applied; refreshing current resource details." : "Returned to the default view.");
  }
  function restoreRequestRollback() {
    const rollback = liveState.requestRollback;
    const pane = liveState.table?.closest(".list-pane");
    if (!rollback || !pane) return;
    const state = rollback.state;
    const label = $("#label-selector", pane);
    const field = $("#field-selector", pane);
    const page = $("#page-size", pane);
    if (label) label.value = state.selectors.label;
    if (field) field.value = state.selectors.field;
    if (page) page.value = String(state.pageSize);
    const filter = $("#row-filter", pane);
    if (filter) filter.value = state.filters.text;
    const topFilter = $(".topbar #row-filter");
    if (topFilter) topFilter.value = state.filters.text;
    const column = $("#column-filter", pane);
    if (column) column.value = Array.from(column.options).some((option) => option.value === state.filters.column) ? state.filters.column : "";
    pane.dataset.sortKey = state.sort.key;
    pane.dataset.listUrl = rollback.listUrl;
    pane.dataset.pageHistory = rollback.pageHistory;
    pane.setAttribute("hx-get", rollback.listUrl);
    document.body.dataset.rowDensity = state.density;
    const densityControl = $("#row-density", pane);
    if (densityControl) densityControl.value = state.density;
    liveState.pendingViewState = null;
    liveState.activeView = rollback.viewID;
    liveState.layoutDraft = normalizeLayout(state.layout, liveState.defaultLayout);
    applyLayout(liveState.layoutDraft);
    renderColumnChooser();
    if (typeof window.kui?.filterRows === "function") window.kui.filterRows(state.filters.text);
    history.replaceState(history.state, "", rollback.url);
    const viewSelect = $("#saved-view-select");
    if (viewSelect) viewSelect.value = rollback.viewID;
    syncRowFilterMirror();
    updateViewDirtyLabel();
    liveState.requestRollback = null;
  }

  function closeLayoutPanel(cancel) {
    const panel = $("#table-view-panel");
    const button = $("#table-view-button");
    if (cancel && liveState.layoutBeforeEdit) setLayout(liveState.layoutBeforeEdit, false);
    liveState.layoutOpen = false;
    if (panel) panel.hidden = true;
    if (button) { button.setAttribute("aria-expanded", "false"); button.focus(); }
  }


  function selectedTableRows() {
    const table = liveState.table;
    const selected = new Map();
    const addSelected = (id, name, row) => {
      const url = safeDetailURL(id);
      if (!url) return;
      const parts = url.pathname.split("/").filter(Boolean);
      const label = liveState.scope.resourceKind === "all"
        ? `${parts[3]} · ${parts[2]}/${name}`
        : liveState.scope.namespace === "_all" ? `${parts[2]}/${name}` : name;
      selected.set(id, { name: label, row });
    };
    $$(".row-select:checked", table).forEach((checkbox) => {
      const row = checkbox.closest("tr[data-id]");
      if (row) addSelected(row.dataset.id, row.dataset.name || row.dataset.id, row);
    });
    const selection = history.state || {};
    if (selection.kuiSelectionScope === location.pathname && Array.isArray(selection.kuiSelectedRows)) {
      selection.kuiSelectedRows.forEach((entry) => {
        if (Array.isArray(entry) && typeof entry[0] === "string" && typeof entry[1] === "string") addSelected(entry[0], entry[1], null);
      });
    }
    return Array.from(selected, ([id, item]) => ({ id, ...item }));
  }

  function safeDetailURL(raw) {
    try {
      const url = new URL(raw, location.origin);
      if (url.origin !== location.origin || !liveState.scope || url.search || url.hash) return null;
      const parts = url.pathname.split("/").filter(Boolean);
      if (parts.length !== 5 || parts[0] !== "c" || parts[1] !== liveState.scope.context) return null;
      if (liveState.scope.namespace !== "_all" && parts[2] !== liveState.scope.namespace) return null;
      if (liveState.scope.resourceKind !== "all" && parts[3] !== liveState.scope.resourceKind) return null;
      return url;
    } catch { return null; }
  }

  function ensureComparisonPanel() {
    let panel = document.getElementById("comparison-panel");
    if (panel) return panel;
    panel = document.createElement("section");
    panel.id = "comparison-panel";
    panel.className = "kui-comparison-panel";
    panel.hidden = true;
    panel.tabIndex = -1;
    panel.setAttribute("role", "region");
    panel.setAttribute("aria-labelledby", "kui-comparison-title");
    panel.innerHTML = `<div class="kui-comparison-heading"><div><h2 id="kui-comparison-title">Resource comparison</h2><p class="kui-comparison-status" role="status" aria-live="polite"></p></div><div class="kui-actions">
      <label><input type="checkbox" data-action="comparison-changed"> Changed fields only</label>
      <button type="button" class="btn" data-action="comparison-refresh">Refresh</button>
      <button type="button" class="btn" data-action="comparison-copy-text">Copy text</button>
      <button type="button" class="btn" data-action="comparison-copy-json">Copy JSON</button>
      <button type="button" class="btn" data-action="comparison-export-csv">Export CSV</button>
      <button type="button" class="btn" data-action="comparison-clear">Clear</button>
      <button type="button" class="btn" data-action="comparison-close">Close</button>
    </div></div><div class="kui-comparison-members"></div><div class="kui-comparison-results" aria-live="polite"></div>`;
    document.body.appendChild(panel);
    return panel;
  }

  async function fetchComparisonMember(member, signal) {
    const url = safeDetailURL(member.id);
    if (!url) throw new Error("Resource detail URL is invalid or outside this application.");
    const response = await fetch(url.pathname + url.search, { credentials: "same-origin", headers: { Accept: "text/html" }, signal });
    if (!response.ok) throw new Error(`Detail request failed (${response.status}).`);
    const html = await response.text();
    if (html.length > 2 * 1024 * 1024) throw new Error("Detail fragment is too large to compare safely.");
    const parsed = new DOMParser().parseFromString(html, "text/html");
    const details = parsed.querySelector(".detail-header");
    if (!details || !parsed.querySelector("#tab-details")) throw new Error("The server returned an unexpected detail fragment.");
    return { ...member, fields: extractResourceFields(parsed, member) };
  }

  function cleanText(node) {
    const copy = node.cloneNode(true);
    copy.querySelectorAll("button,script,style,.copy-action").forEach((child) => child.remove());
    return copy.textContent.trim();
  }

  function addValue(fields, path, value) {
    if (sensitivePath(path)) return;
    fields[path] = String(value ?? "");
  }

  function sensitivePath(path) {
    if (path.startsWith("containers.")) return false;
    const segments = path.toLowerCase().split(/[.[\]]+/).filter(Boolean).filter((segment) => !segment.startsWith("name="));
    return segments.some((segment) =>
      segment === "data" || segment === "stringdata" || segment === "env" || segment === "envfrom" ||
      /password|passwd|token|secret|credential|authorization|privatekey|clientkey|last-applied-configuration|managedfields/.test(segment)
    );
  }

  function extractResourceFields(doc, member) {
    const fields = Object.create(null);
    const header = $(".detail-header", doc);
    const identity = header?.querySelector("strong")?.textContent.trim() || member.name;
    const namespace = header?.querySelector(".text-dim")?.textContent.split("·")[1]?.trim() || "";
    addValue(fields, "metadata.name", identity);
    addValue(fields, "metadata.namespace", namespace);
    const detailsPanel = $("#tab-details", doc);
    if (detailsPanel) {
      const terms = $$(".kv-grid dt", detailsPanel);
      terms.forEach((term) => {
        const dd = term.nextElementSibling;
        if (!dd) return;
        const name = term.textContent.trim().toLowerCase();
        if (name === "labels" || name === "annotations") {
          $$(".pill", dd).forEach((pill) => {
            const text = pill.textContent;
            const equals = text.indexOf("=");
            const key = equals < 0 ? text.trim() : text.slice(0, equals).trim();
            const value = equals < 0 ? "" : text.slice(equals + 1).trim();
            addValue(fields, `metadata.${name}.${key}`, value);
          });
        } else if (name !== "age") {
          addValue(fields, `metadata.${name}`, cleanText(dd));
        }
      });
      const sectionTitles = $$(".section-title", detailsPanel);
      sectionTitles.forEach((title) => {
        if (title.textContent.trim().toLowerCase() !== "containers") return;
        const table = title.nextElementSibling?.matches("table") ? title.nextElementSibling : null;
        if (!table) return;
        const headerCells = $$("thead th", table).map((cell) => cell.textContent.trim().toLowerCase());
        $$("tbody tr", table).forEach((row) => {
          const values = Array.from(row.cells).map((cell) => cleanText(cell));
          const index = headerCells.indexOf("name");
          const name = values[index >= 0 ? index : 0] || "(unnamed)";
          headerCells.forEach((headerName, i) => {
            const key = ({ image: "image", ready: "readiness", restarts: "restarts", state: "state" })[headerName];
            if (key) addValue(fields, `containers.${name}.${key}`, values[i] || "");
          });
        });
      });
      const yaml = $("#yaml-editor", doc)?.value || "";
      flattenYamlSpec(yaml).forEach(([path, value]) => addValue(fields, path, value));
    }
    return fields;
  }

  function flattenYamlSpec(text) {
    const output = [];
    const stack = [];
    const arrayIndices = new Map();
    let rootIndent = null;
    let specIndent = -1;
    let inSpec = false;
    let emptySpec = false;
    const pathFor = (segments) => "spec" + segments.slice(1).map((part) => part.startsWith("[") ? part : `.${part}`).join("");
    for (const rawLine of text.split(/\r?\n/)) {
      if (!rawLine.trim() || /^\s*#/.test(rawLine) || /^\s*---/.test(rawLine)) continue;
      const indent = rawLine.match(/^\s*/)[0].replace(/\t/g, "  ").length;
      const line = rawLine.trim();
      if (rootIndent === null) rootIndent = indent;
      if (indent === rootIndent) {
        stack.length = 0;
        const rootPair = splitYamlPair(line);
        inSpec = !!rootPair && rootPair[0] === "spec" && rootPair[1] === "";
        specIndent = inSpec ? indent : -1;
        if (inSpec) {
          emptySpec = true;
          stack.push({ indent, key: "spec", kind: "map" });
        } else if (rootPair?.[0] === "spec") output.push(["spec", parseYamlScalar(rootPair[1])]);
        continue;
      }
      if (!inSpec || indent <= specIndent) continue;
      const sequence = line === "-" || line.startsWith("- ");
      if (sequence) {
        while (stack.length && (stack[stack.length - 1].indent > indent ||
          (stack[stack.length - 1].indent === indent && stack[stack.length - 1].kind === "item"))) stack.pop();
        const parent = stack.map((item) => item.key);
        const listKey = parent.join(".");
        const index = arrayIndices.get(listKey) || 0;
        arrayIndices.set(listKey, index + 1);
        const itemKey = `[${index}]`;
        const itemPath = [...parent, itemKey];
        const valueText = line === "-" ? "" : line.slice(2).trim();
        const pair = splitYamlPair(valueText);
        if (pair) {
          const [key, value] = pair;
          if (!/^[A-Za-z0-9_.\/-]+$/.test(key)) continue;
          if (value === "" || value === "|" || value === ">") {
            stack.push({ indent, key: itemKey, kind: "item" });
            stack.push({ indent: indent + 1, key, kind: "map" });
          } else {
            output.push([pathFor([...itemPath, key]), parseYamlScalar(value)]);
            stack.push({ indent, key: itemKey, kind: "item" });
          }
        } else {
          if (valueText) output.push([pathFor(itemPath), parseYamlScalar(valueText)]);
          stack.push({ indent, key: itemKey, kind: "item" });
        }
        continue;
      }
      while (stack.length && stack[stack.length - 1].indent >= indent) stack.pop();
      const pair = splitYamlPair(line);
      if (!pair) continue;
      const [key, value] = pair;
      if (!/^[A-Za-z0-9_.\/-]+$/.test(key)) continue;
      const path = [...stack.map((item) => item.key), key];
      if (value === "" || value === "|" || value === ">") stack.push({ indent, key, kind: "map" });
      else output.push([pathFor(path), parseYamlScalar(value)]);
    }
    if (emptySpec && !output.some(([path]) => path.startsWith("spec.") || path.startsWith("spec["))) output.push(["spec", "{}"]);
    const identities = new Map();
    output.forEach(([path, value]) => {
      const match = path.match(/^(.*\[\d+\])\.name$/);
      if (match) identities.set(match[1], String(value));
    });
    return output.map(([path, value]) => {
      let stablePath = path;
      Array.from(path.matchAll(/\[\d+\]/g)).reverse().forEach((match) => {
        const arrayItem = path.slice(0, match.index + match[0].length);
        const identity = identities.get(arrayItem);
        if (identity !== undefined) stablePath = stablePath.slice(0, match.index) + `[name=${encodeURIComponent(identity)}]` + stablePath.slice(match.index + match[0].length);
      });
      return [stablePath, value];
    });
  }

  function splitYamlPair(line) {
    let quote = "";
    let depth = 0;
    for (let i = 0; i < line.length; i++) {
      const char = line[i];
      if (quote) { if (char === quote && line[i - 1] !== "\\") quote = ""; continue; }
      if (char === "'" || char === '"') { quote = char; continue; }
      if (char === "[" || char === "{") depth++;
      if (char === "]" || char === "}") depth--;
      if (char === ":" && depth === 0 && (i + 1 === line.length || /\s/.test(line[i + 1]))) {
        return [line.slice(0, i).trim(), line.slice(i + 1).trim()];
      }
    }
    return null;
  }

  function parseYamlScalar(value) {
    const valueText = value.trim();
    if (!valueText) return "";
    if ((valueText.startsWith('"') && valueText.endsWith('"')) || (valueText.startsWith("'") && valueText.endsWith("'"))) return valueText.slice(1, -1);
    const trimmed = valueText.replace(/\s+#.*$/, "").trim();
    if (!trimmed) return "";
    if (["null", "~"].includes(trimmed.toLowerCase())) return "null";
    if (/^(?:true|false)$/i.test(trimmed)) return trimmed.toLowerCase();
    return trimmed;
  }

  async function loadComparison(members = liveState.comparison) {
    const panel = ensureComparisonPanel();
    const sequence = ++liveState.comparisonSequence;
    const controller = new AbortController();
    if (liveState.comparisonController) liveState.comparisonController.abort();
    liveState.comparisonController = controller;
    liveState.comparison = members.slice(0, 4);
    liveState.comparisonData = [];
    renderComparisonMembers(panel);
    setComparisonStatus("Fetching current resource details…");
    panel.querySelector(".kui-comparison-results").replaceChildren();
    const loaded = await Promise.all(members.slice(0, 4).map(async (member) => {
      try { return await fetchComparisonMember(member, controller.signal); }
      catch (error) { return { ...member, error: error.name === "AbortError" ? "Request cancelled." : error.message }; }
    }));
    if (sequence !== liveState.comparisonSequence) return;
    liveState.comparisonData = loaded;
    liveState.comparisonFetchedAt = Date.now();
    const successes = loaded.filter((entry) => !entry.error).length;
    setComparisonStatus(loaded.some((entry) => entry.error) ? `${successes} of ${loaded.length} details loaded. Failed or stale members are marked; refresh to retry.` : `Current details fetched ${new Date(liveState.comparisonFetchedAt).toLocaleTimeString()}.`);
    renderComparisonMembers(panel);
    renderComparisonRows();
  }

  function setComparisonStatus(message, error = false) {
    const node = $(".kui-comparison-status", document.getElementById("comparison-panel"));
    if (node) { node.textContent = message; node.classList.toggle("is-error", error); }
  }

  function renderComparisonMembers(panel = ensureComparisonPanel()) {
    const host = $(".kui-comparison-members", panel);
    const active = document.activeElement;
    const focusToken = host.contains(active) ? {
      action: active.dataset.action,
      index: active.dataset.index,
      replaceIndex: active.dataset.replaceIndex
    } : null;
    host.replaceChildren();
    if (!liveState.comparison.length) {
      const empty = document.createElement("p");
      empty.className = "kui-comparison-empty";
      empty.textContent = "Select 2–4 resource rows, then choose Compare selected. Comparison fetches live details and does not change table selection.";
      host.appendChild(empty);
      if (focusToken) $("#compare-selected")?.focus();
      return;
    }
    const selected = selectedTableRows();
    liveState.comparison.forEach((member, index) => {
      const card = document.createElement("article");
      card.className = "kui-comparison-member";
      const title = document.createElement("strong");
      title.textContent = member.name;
      const url = safeDetailURL(member.id);
      const path = document.createElement("span");
      path.className = "kui-member-path";
      path.textContent = url?.pathname || "Invalid resource URL";
      const detail = liveState.comparisonData.find((item) => item.id === member.id);
      const state = document.createElement("span");
      state.className = detail?.error ? "kui-member-error" : "kui-member-ok";
      state.textContent = detail?.error || (detail ? "Current details loaded" : "Waiting for current details");
      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "btn";
      remove.dataset.action = "comparison-remove";
      remove.dataset.index = String(index);
      remove.textContent = "Remove";
      remove.setAttribute("aria-label", `Remove ${member.name} from comparison`);
      const replacements = selected.filter((candidate) => !liveState.comparison.some((existing) => existing.id === candidate.id));
      const replaceSelect = document.createElement("select");
      replaceSelect.dataset.replaceIndex = String(index);
      replaceSelect.setAttribute("aria-label", `Choose selected resource to replace ${member.name}`);
      replaceSelect.add(new Option("Replace with selected…", ""));
      replacements.forEach((candidate) => replaceSelect.add(new Option(candidate.name, candidate.id)));
      const replace = document.createElement("button");
      replace.type = "button";
      replace.className = "btn";
      replace.dataset.action = "comparison-replace";
      replace.dataset.index = String(index);
      replace.textContent = "Replace";
      replace.disabled = !replacements.length;
      card.append(title, path, state, replaceSelect, replace, remove);
      host.appendChild(card);
    });
    const add = document.createElement("div");
    add.className = "kui-comparison-add";
    const select = document.createElement("select");
    select.dataset.action = "comparison-add-select";
    select.setAttribute("aria-label", "Add selected resource to comparison");
    select.add(new Option("Add a selected resource…", ""));
    selected.filter((candidate) => !liveState.comparison.some((existing) => existing.id === candidate.id)).forEach((candidate) => select.add(new Option(candidate.name, candidate.id)));
    const button = document.createElement("button");
    button.type = "button";
    button.className = "btn";
    button.dataset.action = "comparison-add";
    button.textContent = "Add member";
    button.disabled = liveState.comparison.length >= 4 || !select.options.length || select.options.length === 1;
    add.append(select, button);
    host.appendChild(add);
    if (focusToken) {
      const focused = focusToken.replaceIndex !== undefined
        ? $(`[data-replace-index="${focusToken.replaceIndex}"]`, host)
        : focusToken.action
          ? $$(`[data-action="${CSS.escape(focusToken.action)}"]`, host).find((item) => item.dataset.index === focusToken.index)
          : null;
      if (focused) focused.focus({ preventScroll: true });
      else $("[data-action='comparison-add-select']", host)?.focus({ preventScroll: true });
    }
  }

  function buildDifferences() {
    const loaded = liveState.comparisonData.filter((entry) => !entry.error);
    const paths = new Set();
    loaded.forEach((entry) => Object.keys(entry.fields).forEach((path) => paths.add(path)));
    const rows = [];
    Array.from(paths).sort((a, b) => a.localeCompare(b)).forEach((path) => {
      const values = liveState.comparisonData.map((entry) => ({
        id: entry.id,
        name: entry.name,
        error: entry.error || "",
        present: !entry.error && Object.hasOwn(entry.fields, path),
        value: entry.error || !Object.hasOwn(entry.fields, path) ? null : entry.fields[path]
      }));
      const compared = values.filter((value) => !value.error);
      const first = compared[0];
      const changed = compared.some((value) => value.present !== first?.present || (value.present && value.value !== first.value));
      rows.push({ path, category: path.split(".")[0], changed, values });
    });
    return rows;
  }

  function renderComparisonRows() {
    const panel = document.getElementById("comparison-panel");
    if (!panel) return;
    const result = $(".kui-comparison-results", panel);
    result.replaceChildren();
    const failed = liveState.comparisonData.filter((entry) => entry.error);
    if (liveState.comparison.length < 2) {
      const note = document.createElement("p");
      note.className = "kui-comparison-empty";
      note.textContent = liveState.comparison.length ? "At least two resources are required. Add another currently selected resource or clear the comparison." : "Select two to four table rows to start a comparison.";
      result.appendChild(note);
      return;
    }
    if (liveState.comparisonData.length < liveState.comparison.length) {
      const pending = document.createElement("p");
      pending.textContent = "Waiting for detail data.";
      result.appendChild(pending);
      return;
    }
    if (failed.length) {
      const warning = document.createElement("p");
      warning.className = "kui-comparison-warning";
      warning.textContent = `Comparison is partial: ${failed.map((entry) => `${entry.name}: ${entry.error}`).join("; ")}`;
      result.appendChild(warning);
    }
    const rows = buildDifferences();
    const changedOnly = $("[data-action='comparison-changed']", panel)?.checked;
    const visible = changedOnly ? rows.filter((row) => row.changed) : rows;
    if (!visible.length) {
      const empty = document.createElement("p");
      empty.className = "kui-comparison-empty";
      empty.textContent = changedOnly ? "No changed fields were found among the loaded details." : "No comparable metadata, spec, or container fields were found.";
      result.appendChild(empty);
      return;
    }
    const wrapper = document.createElement("div");
    wrapper.className = "kui-comparison-table-wrap";
    const table = document.createElement("table");
    table.className = "kui-comparison-table";
    const thead = document.createElement("thead");
    const head = document.createElement("tr");
    const pathHeader = document.createElement("th");
    pathHeader.scope = "col";
    pathHeader.textContent = "Field path";
    head.appendChild(pathHeader);
    liveState.comparison.forEach((member) => {
      const th = document.createElement("th");
      th.scope = "col";
      th.textContent = member.name;
      head.appendChild(th);
    });
    thead.appendChild(head);
    table.appendChild(thead);
    const body = document.createElement("tbody");
    visible.forEach((row) => {
      const tr = document.createElement("tr");
      tr.classList.toggle("is-changed", row.changed);
      const pathCell = document.createElement("th");
      pathCell.scope = "row";
      pathCell.textContent = row.path;
      tr.appendChild(pathCell);
      row.values.forEach((value) => {
        const td = document.createElement("td");
        td.classList.toggle("is-changed", row.changed);
        if (value.error) td.textContent = "Unavailable";
        else if (!value.present) td.textContent = "Absent";
        else if (value.value === "") td.textContent = "(empty)";
        else {
          const details = document.createElement("details");
          const summary = document.createElement("summary");
          summary.textContent = String(value.value).replace(/\s+/g, " ").slice(0, 120) || "(empty)";
          const full = document.createElement("pre");
          full.textContent = value.value;
          details.append(summary, full);
          td.appendChild(details);
        }
        tr.appendChild(td);
      });
      body.appendChild(tr);
    });
    table.appendChild(body);
    wrapper.appendChild(table);
    result.appendChild(wrapper);
  }

  function comparisonExportData() {
    return {
      format: "kui-resource-comparison",
      version: 1,
      fetchedAt: liveState.comparisonFetchedAt ? new Date(liveState.comparisonFetchedAt).toISOString() : null,
      resources: liveState.comparisonData.map((entry) => ({ name: entry.name, path: safeDetailURL(entry.id)?.pathname || "", error: entry.error || null })),
      differences: buildDifferences()
    };
  }

  function csvCell(value) {
    const text = String(value);
    const safe = /^[\s]*[=+\-@]/.test(text) ? `'${text}` : text;
    return `"${safe.replaceAll('"', '""')}"`;
  }

  function comparisonCSV() {
    const rows = buildDifferences();
    const header = ["Field path", ...liveState.comparison.map((member) => member.name)];
    return [header, ...rows.map((row) => [row.path, ...row.values.map((value) => value.error ? "Unavailable" : !value.present ? "Absent" : value.value === "" ? "(empty)" : value.value)])].map((row) => row.map(csvCell).join(",")).join("\r\n");
  }

  async function copyText(text) {
    try {
      if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(text);
      else {
        const area = document.createElement("textarea");
        area.value = text;
        area.setAttribute("readonly", "");
        area.className = "kui-clipboard-fallback";
        document.body.appendChild(area);
        let copied = false;
        try {
          area.select();
          copied = document.execCommand("copy");
        } finally {
          area.remove();
        }
        if (!copied) throw new Error("Clipboard permission was denied.");
      }
      setComparisonStatus("Comparison copied to clipboard.");
    } catch (error) { setComparisonStatus(`Could not copy comparison: ${error.message}`, true); }
  }

  function downloadFile(filename, content, type) {
    const blob = new Blob([content], { type });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = filename;
    anchor.hidden = true;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  function startComparisonFromSelection() {
    const selected = selectedTableRows();
    if (selected.length < 2 || selected.length > 4) {
      announce(`Select 2–4 resource rows to compare; ${selected.length} are selected.`, true);
      return;
    }
    const unique = [];
    selected.forEach((member) => { if (!unique.some((item) => item.id === member.id)) unique.push(member); });
    liveState.comparison = unique;
    liveState.comparisonScope = { context: liveState.scope.context, namespace: liveState.scope.namespace, resourceKind: liveState.scope.resourceKind };
    const panel = ensureComparisonPanel();
    panel.hidden = false;
    $("#compare-selected")?.setAttribute("aria-expanded", "true");
    $("[data-action='comparison-changed']", panel)?.focus();
    loadComparison(unique);
  }

  function markComparisonStale() {
    if (!liveState.comparison.length || !liveState.comparisonFetchedAt) return;
    setComparisonStatus("The resource table updated after this comparison was fetched. Refresh for current details.");
  }

  function readImportView(raw, index) {
    if (!exactKeys(raw, ["id", "name", "scope", "state"])) throw new Error(`View ${index + 1} has an invalid schema.`);
    const checked = validateView(raw, liveState.scope, true);
    if (!checked.ok) throw new Error(`View ${index + 1} has an invalid, unsafe, or out-of-scope value.`);
    return checked.view;
  }

  function importViewsText(text) {
    if (new Blob([text]).size > MAX_VIEW_BYTES) throw new Error("Import is larger than 128 KiB.");
    const documentData = JSON.parse(text);
    if (!exactKeys(documentData, ["format", "version", "scope", "views"]) || documentData.format !== VIEW_FORMAT || documentData.version !== VIEW_VERSION || !Array.isArray(documentData.views) || documentData.views.length > MAX_VIEWS) throw new Error("Unsupported export format or schema.");
    if (!exactKeys(documentData.scope, ["context", "namespace", "resourceKind"]) || documentData.scope.context !== liveState.scope.context || documentData.scope.namespace !== liveState.scope.namespace || documentData.scope.resourceKind !== liveState.scope.resourceKind) throw new Error("This file belongs to a different context, namespace, or resource kind.");
    const imported = documentData.views.map(readImportView);
    const views = currentViewList();
    for (const incoming of imported) {
      let candidate = incoming;
      if (views.some((view) => view.name.toLocaleLowerCase() === candidate.name.toLocaleLowerCase())) {
        const baseName = candidate.name.slice(0, 80);
        let suffix = 2;
        let name = `${baseName} (imported)`;
        while (views.some((view) => view.name.toLocaleLowerCase() === name.toLocaleLowerCase())) name = `${baseName} (imported ${suffix++})`;
        candidate = { ...candidate, id: makeID(), name };
      } else if (views.some((view) => view.id === candidate.id)) candidate = { ...candidate, id: makeID() };
      views.push(candidate);
    }
    if (!saveViews(views)) throw new Error("Imported views could not be saved locally.");
    renderViewControls($(".kui-workbench"));
    updateLayoutStatus(`Imported ${imported.length} view${imported.length === 1 ? "" : "s"} for this scope.`);
  }

  function compareAction(action, target) {
    const panel = document.getElementById("comparison-panel");
    if (action === "comparison-close") {
      if (panel) panel.hidden = true;
      $("#compare-selected")?.setAttribute("aria-expanded", "false");
      $("#compare-selected")?.focus();
      return;
    }
    if (action === "comparison-clear") {
      liveState.comparisonController?.abort();
      liveState.comparisonSequence++;
      liveState.comparison = [];
      liveState.comparisonData = [];
      liveState.comparisonFetchedAt = 0;
      liveState.comparisonScope = null;
      renderComparisonMembers();
      panel?.querySelector(".kui-comparison-results")?.replaceChildren();
      setComparisonStatus("Comparison cleared. Table selection was left unchanged.");
      return;
    }
    if (action === "comparison-refresh") { if (liveState.comparison.length) loadComparison(); return; }
    if (action === "comparison-changed") { renderComparisonRows(); return; }
    if (action === "comparison-copy-text") {
      const data = comparisonExportData();
      copyText([`Resource comparison (${data.resources.map((item) => item.name).join(" vs ")})`, ...data.differences.map((row) => `${row.path}: ${row.values.map((value) => value.error ? "Unavailable" : value.present ? value.value === "" ? "(empty)" : value.value : "Absent").join(" | ")}`)].join("\n"));
      return;
    }
    if (action === "comparison-copy-json") { copyText(JSON.stringify(comparisonExportData(), null, 2)); return; }
    if (action === "comparison-export-csv") { downloadFile("resource-comparison.csv", comparisonCSV(), "text/csv;charset=utf-8"); setComparisonStatus("Comparison exported as escaped CSV."); return; }
    if (action === "comparison-remove") {
      const index = Number(target.dataset.index);
      liveState.comparisonController?.abort();
      liveState.comparison = liveState.comparison.filter((_, i) => i !== index);
      liveState.comparisonData = liveState.comparisonData.filter((entry) => liveState.comparison.some((member) => member.id === entry.id));
      if (!liveState.comparison.length) liveState.comparisonScope = null;
      liveState.comparisonSequence++;
      renderComparisonMembers();
      renderComparisonRows();
      setComparisonStatus(liveState.comparison.length ? "Member removed; remaining details are unchanged. Refresh for latest data." : "Comparison cleared. Table selection was left unchanged.");
      return;
    }
    if (action === "comparison-replace") {
      const index = Number(target.dataset.index);
      const id = $(`[data-replace-index="${index}"]`, panel)?.value;
      const member = selectedTableRows().find((candidate) => candidate.id === id);
      if (!member || !Number.isInteger(index) || index < 0 || index >= liveState.comparison.length) return;
      const next = liveState.comparison.slice();
      next[index] = member;
      if (new Set(next.map((entry) => entry.id)).size !== next.length) { setComparisonStatus("A resource cannot appear twice in the comparison.", true); return; }
      loadComparison(next);
      return;
    }
    if (action === "comparison-add") {
      const id = $(`[data-action="comparison-add-select"]`, panel)?.value;
      const member = selectedTableRows().find((candidate) => candidate.id === id);
      if (!member || liveState.comparison.length >= 4 || liveState.comparison.some((entry) => entry.id === id)) return;
      loadComparison([...liveState.comparison, member]);
    }
  }

  function workbenchAction(action, target) {
    const root = $(".kui-workbench");
    if (action === "layout-save") { persistLayout(); return; }
    if (action === "layout-cancel") { closeLayoutPanel(true); return; }
    if (action === "layout-reset") { setLayout(liveState.defaultLayout); updateLayoutStatus("Default columns previewed. Save to persist, or cancel to keep the prior layout."); return; }
    if (action === "column-up") { reorderColumn(target.dataset.column, -1); return; }
    if (action === "column-down") { reorderColumn(target.dataset.column, 1); return; }
    if (action === "view-create") {
      const name = viewPrompt("Name this saved resource view:");
      if (name) saveCurrentAsView(name);
      return;
    }
    if (action === "view-update") {
      const view = selectedView();
      if (view) saveCurrentAsView(view.name, view.id);
      return;
    }
    if (action === "view-rename") {
      const view = selectedView();
      if (!view) return;
      const name = viewPrompt("Rename saved view:", view.name);
      if (!name) return;
      const views = currentViewList();
      if (views.some((candidate) => candidate.id !== view.id && candidate.name.toLocaleLowerCase() === name.toLocaleLowerCase())) { updateLayoutStatus("A view with that name already exists in this scope.", true); return; }
      const next = views.map((candidate) => candidate.id === view.id ? { ...candidate, name } : candidate);
      if (saveViews(next)) { renderViewControls(root); $("#saved-view-select", root).value = view.id; updateViewDirtyLabel(); updateLayoutStatus(`Renamed view to “${name}”.`); }
      return;
    }
    if (action === "view-duplicate") {
      const view = selectedView();
      if (!view) return;
      const name = viewPrompt("Name the duplicate saved view:", `${view.name} copy`);
      if (!name) return;
      const views = currentViewList();
      if (views.some((candidate) => candidate.name.toLocaleLowerCase() === name.toLocaleLowerCase())) { updateLayoutStatus("A view with that name already exists in this scope.", true); return; }
      const duplicate = validateView({ ...view, id: makeID(), name }, liveState.scope);
      if (!duplicate.ok) { updateLayoutStatus("The duplicate could not be validated.", true); return; }
      if (saveViews([...views, duplicate.view])) {
        renderViewControls(root);
        $("#saved-view-select", root).value = duplicate.view.id;
        liveState.activeView = duplicate.view.id;
        updateViewDirtyLabel();
        updateLayoutStatus(`Duplicated “${view.name}” as “${name}”.`);
      }
      return;
    }
    if (action === "view-delete") {
      const view = selectedView();
      if (!view || !window.confirm(`Delete the saved view “${view.name}”?`)) return;
      if (saveViews(currentViewList().filter((candidate) => candidate.id !== view.id))) {
        liveState.activeView = "";
        renderViewControls(root);
        requestViewState(defaultState(), "");
        updateLayoutStatus(`Deleted “${view.name}” and returned to the default view.`);
      }
      return;
    }
    if (action === "view-default") { requestViewState(defaultState(), ""); return; }
    if (action === "view-export") {
      const stored = getScopedViews();
      if (stored.error) { updateLayoutStatus(stored.error, true); return; }
      const payload = { format: VIEW_FORMAT, version: VIEW_VERSION, scope: { context: liveState.scope.context, namespace: liveState.scope.namespace, resourceKind: liveState.scope.resourceKind }, views: stored.views };
      const text = JSON.stringify(payload, null, 2);
      if (new Blob([text]).size > MAX_VIEW_BYTES) { updateLayoutStatus("Export exceeds the 128 KiB limit.", true); return; }
      downloadFile("kui-resource-views.json", text, "application/json;charset=utf-8");
      updateLayoutStatus(`Exported ${payload.views.length} scoped view${payload.views.length === 1 ? "" : "s"}.`);
      return;
    }
    if (action === "view-import") { $(".kui-import-file", root)?.click(); return; }
    if (action === "table-view-toggle") {
      const panel = $("#table-view-panel", root);
      liveState.layoutOpen = panel.hidden;
      panel.hidden = !liveState.layoutOpen;
      $("#table-view-button", root).setAttribute("aria-expanded", String(liveState.layoutOpen));
      if (liveState.layoutOpen) {
        liveState.layoutBeforeEdit = clone(liveState.layoutDraft);
        $(".kui-column-list", root)?.querySelector("input:not(:disabled),button:not(:disabled)")?.focus();
      }
      return;
    }
  }

  function defaultState() {
    return { selectors: { label: "", field: "" }, filters: { text: "", column: "" }, sort: { key: "", direction: "asc" }, pageSize: Number.parseInt($("#page-size")?.dataset.defaultValue || "200", 10) || 200, density: "default", layout: liveState.defaultLayout.map(({ id }) => ({ id, visible: true, width: null })) };
  }

  function handleDocumentEvent(event) {
    const target = event.target;
    if (!(target instanceof Element)) return;
    if (event.type === "input" || event.type === "change") {
      if (target.matches("#row-filter")) {
        syncRowFilterMirror(target);
        updateViewDirtyLabel();
      }
      if (target.matches("#label-selector, #field-selector, #page-size, #column-filter, #row-density")) updateViewDirtyLabel();
      if (target.dataset.action === "column-visible") {
        const column = liveState.layoutDraft.find((item) => item.id === target.dataset.column);
        if (column && !column.locked) { column.visible = target.checked; setLayout(liveState.layoutDraft); }
      }
      if (target.dataset.action === "column-width") {
        const column = liveState.layoutDraft.find((item) => item.id === target.dataset.column);
        if (column) {
          column.width = Math.max(MIN_COLUMN_WIDTH, Math.min(MAX_COLUMN_WIDTH, Number(target.value)));
          applyLayout(liveState.layoutDraft);
          const output = $(`[data-width-for="${CSS.escape(column.id)}"]`);
          if (output) output.textContent = `${column.width}px`;
          updateViewDirtyLabel();
        }
      }
      if (target.id === "saved-view-select") {
        syncViewActionAvailability();
        const view = selectedView();
        liveState.activeView = view?.id || "";
        if (view) requestViewState(view.state, view.id);
        else requestViewState(defaultState(), "");
      }
      if (target.matches("[data-action='comparison-changed']")) renderComparisonRows();
    }
    if (event.type === "click") {
      const actionTarget = target.closest("[data-action]");
      const action = actionTarget?.dataset.action;
      if (target.closest("#table-view-button")) { workbenchAction("table-view-toggle", target); return; }
      if (actionTarget && actionTarget.closest(".kui-workbench")) { workbenchAction(action, actionTarget); return; }
      if (actionTarget && actionTarget.closest("#comparison-panel")) { compareAction(action, actionTarget); return; }
      if (target.closest("#compare-selected")) { startComparisonFromSelection(); return; }
      if (target.matches(".kui-column-resize")) event.stopPropagation();
      setTimeout(updateViewDirtyLabel, 0);
    }
    if (event.type === "change" && target.matches(".kui-import-file")) {
      const file = target.files?.[0];
      target.value = "";
      if (!file) return;
      if (file.size > MAX_VIEW_BYTES) { updateLayoutStatus("Import is larger than 128 KiB.", true); return; }
      file.text().then(importViewsText).catch((error) => updateLayoutStatus(`Import failed: ${error.message}`, true));
    }
    if (event.type === "dragstart" && target.matches(".kui-column-list li")) {
      event.dataTransfer?.setData("text/plain", target.dataset.column);
      if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
      target.classList.add("is-dragging");
    }
    if (event.type === "dragend" && target.matches(".kui-column-list li")) target.classList.remove("is-dragging");
    if (event.type === "dragover" && target.closest(".kui-column-list li")) event.preventDefault();
    if (event.type === "drop") {
      const item = target.closest(".kui-column-list li");
      const fromID = event.dataTransfer?.getData("text/plain");
      if (!item || !fromID) return;
      event.preventDefault();
      const from = liveState.layoutDraft.findIndex((column) => column.id === fromID);
      const to = liveState.layoutDraft.findIndex((column) => column.id === item.dataset.column);
      if (from < 0 || to < 0 || from === to) return;
      const next = clone(liveState.layoutDraft);
      const [moved] = next.splice(from, 1);
      next.splice(to, 0, moved);
      setLayout(next);
      updateLayoutStatus(`${moved.label} moved to position ${to + 1} of ${next.length}.`);
    }
    if (event.type === "keydown") {
      if (target.matches(".kui-column-resize") && ["ArrowLeft", "ArrowRight"].includes(event.key)) {
        event.preventDefault();
        const step = event.shiftKey ? 32 : 10;
        resizeColumn(target.dataset.column, event.key === "ArrowRight" ? step : -step);
      }
      const columnItem = target.closest(".kui-column-list li");
      if (columnItem && event.altKey && ["ArrowUp", "ArrowDown"].includes(event.key)) {
        event.preventDefault();
        reorderColumn(columnItem.dataset.column, event.key === "ArrowUp" ? -1 : 1);
      }
      if (event.key === "Escape" && !$("#table-view-panel")?.hidden) closeLayoutPanel(true);
      if (event.key === "Escape" && !$("#comparison-panel")?.hidden) {
        $("#comparison-panel").hidden = true;
        $("#compare-selected")?.setAttribute("aria-expanded", "false");
        $("#compare-selected")?.focus();
      }
    }
  }

  function resizeColumn(id, delta) {
    const column = liveState.layoutDraft.find((item) => item.id === id);
    if (!column) return;
    const header = $(`thead [data-kui-column="${CSS.escape(id)}"]`, liveState.table);
    const current = column.width || Math.round(header?.getBoundingClientRect().width || 160);
    column.width = Math.min(MAX_COLUMN_WIDTH, Math.max(MIN_COLUMN_WIDTH, current + delta));
    applyLayout(liveState.layoutDraft);
    renderColumnChooser($(".kui-workbench"));
    updateViewDirtyLabel();
    updateLayoutStatus(`${column.label} width ${column.width} pixels.`);
  }

  function pointerResizeStart(event) {
    const handle = event.target.closest?.(".kui-column-resize");
    if (!handle || !liveState.table) return;
    event.preventDefault();
    event.stopPropagation();
    const column = liveState.layoutDraft.find((item) => item.id === handle.dataset.column);
    const header = $(`thead [data-kui-column="${CSS.escape(handle.dataset.column)}"]`, liveState.table);
    if (!column || !header) return;
    resizeState = { id: column.id, x: event.clientX, width: column.width || Math.round(header.getBoundingClientRect().width || 160), pointer: event.pointerId };
    handle.setPointerCapture?.(event.pointerId);
  }

  function pointerResizeMove(event) {
    if (!resizeState || event.pointerId !== resizeState.pointer) return;
    const column = liveState.layoutDraft.find((item) => item.id === resizeState.id);
    if (!column) return;
    column.width = Math.min(MAX_COLUMN_WIDTH, Math.max(MIN_COLUMN_WIDTH, Math.round(resizeState.width + event.clientX - resizeState.x)));
    applyLayout(liveState.layoutDraft);
    updateViewDirtyLabel();
  }

  function pointerResizeEnd(event) {
    if (!resizeState || event.pointerId !== resizeState.pointer) return;
    const column = liveState.layoutDraft.find((item) => item.id === resizeState.id);
    if (column) {
      renderColumnChooser($(".kui-workbench"));
      updateLayoutStatus(`${column.label} width ${column.width} pixels. Save layout to keep it.`);
    }
    resizeState = null;
  }

  function init() {
    document.addEventListener("htmx:afterSwap", (event) => {
      const target = event.detail?.target;
      if (target?.classList?.contains("list-pane")) {
        const table = $("#kui-table", target);
        if (table) {
          renderWorkbench(table);
          if (liveState.comparison.length) {
            markComparisonStale();
            renderComparisonMembers();
          }
        }
        liveState.requestRollback = null;
      }
    });
    const handleListRequestError = (event) => {
      const target = event.detail?.target || liveState.table?.closest(".list-pane");
      if (target?.classList?.contains("list-pane")) {
        const applyingView = !!liveState.requestRollback;
        restoreRequestRollback();
        updateLayoutStatus("The resource table could not be refreshed. The previous table state remains available.", true);
        announce(applyingView ? "The saved view was not fully applied because the table request failed." : "The table request failed; the current rows remain available.", true);
      }
    };
    ["htmx:responseError", "htmx:sendError", "htmx:timeout"].forEach((type) => document.addEventListener(type, handleListRequestError));
    document.addEventListener("click", (event) => {
      if (event.target instanceof Element && event.target.closest(".kui-column-resize")) {
        event.preventDefault();
        event.stopPropagation();
      }
    }, true);
    ["click", "input", "change", "keydown", "dragstart", "dragend", "dragover", "drop"].forEach((type) => document.addEventListener(type, handleDocumentEvent));
    document.addEventListener("pointerdown", pointerResizeStart, true);
    document.addEventListener("pointermove", pointerResizeMove, true);
    document.addEventListener("pointerup", pointerResizeEnd, true);
    document.addEventListener("pointercancel", pointerResizeEnd, true);
    const table = $("#kui-table");
    if (table?.closest(".list-pane")) renderWorkbench(table);
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init, { once: true });
  else init();
})();
