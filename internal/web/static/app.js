window.kui = (function () {
	function showTab(btn, panelId) {
		document.querySelectorAll(".detail-tab-panel").forEach((p) => (p.style.display = "none"));
		const target = document.getElementById(panelId);
		if (target) target.style.display = "block";
		document.querySelectorAll(".tab-row .tab-btn").forEach((b) => b.classList.remove("active"));
		btn.classList.add("active");
	}

	function wsURL(path) {
		const proto = location.protocol === "https:" ? "wss:" : "ws:";
		return proto + "//" + location.host + path;
	}

	function startLogs(base) {
		const pane = document.getElementById("logs-pane");
		if (!pane) return;
		pane.textContent = "";
		const container = document.getElementById("log-container");
		const qs = container && container.value ? "?container=" + encodeURIComponent(container.value) : "";
		const ws = new WebSocket(wsURL(base + qs));
		ws.onmessage = (ev) => {
			pane.textContent += ev.data;
			pane.scrollTop = pane.scrollHeight;
		};
		ws.onerror = () => {
			pane.textContent += "\n[log stream error]";
		};
	}

	function startShell(base) {
		const pane = document.getElementById("term-pane");
		if (!pane) return;
		pane.innerHTML = "";
		const containerSel = document.getElementById("shell-container");
		const qs = containerSel && containerSel.value ? "?container=" + encodeURIComponent(containerSel.value) : "";

		const term = new Terminal({ fontSize: 13, theme: { background: "#000000" } });
		const fit = new FitAddon.FitAddon();
		term.loadAddon(fit);
		term.open(pane);
		fit.fit();

		const ws = new WebSocket(wsURL(base + qs));
		ws.onopen = () => term.focus();
		ws.onmessage = (ev) => term.write(ev.data);
		ws.onclose = () => term.write("\r\n[connection closed]");
		term.onData((data) => {
			if (ws.readyState === WebSocket.OPEN) ws.send(data);
		});
	}

	function filterRows(value) {
		const needle = value.toLowerCase();
		document.querySelectorAll("#kui-table tbody tr").forEach((tr) => {
			tr.style.display = tr.textContent.toLowerCase().includes(needle) ? "" : "none";
		});
	}

	// ---- detail pane open/close (Foundation) ----

	function closeDetail() {
		const dp = document.getElementById("detail-pane");
		if (!dp) return;
		dp.innerHTML = "";
		dp.classList.remove("open");
		const splitter = document.getElementById("detail-splitter");
		if (splitter) splitter.style.display = "none";
		setBreadcrumbName("");
	}

	// ---- toasts (Foundation) ----

	function showToast(toast) {
		const container = document.getElementById("toast-container");
		if (!container || !toast) return;
		const el = document.createElement("div");
		el.className = "toast toast-" + (toast.level || "success");
		el.textContent = toast.message || "";
		container.appendChild(el);
		setTimeout(() => el.remove(), 4000);
	}

	// ---- breadcrumb (Phase 4) ----

	function setBreadcrumbName(name) {
		const el = document.getElementById("breadcrumb-name");
		if (!el) return;
		el.textContent = name ? " › " + name : "";
	}

	// Sidebar links swap only #content via htmx — the sidebar itself and the
	// breadcrumb live in the outer page shell and are never touched by that
	// swap, so without this their "active"/resource label would permanently
	// reflect whatever was true at the last full page load. Runs instantly
	// on click, independent of (and not blocked by) the actual hx-get.
	// applyActiveResource is the single source of truth for "which resource
	// is currently shown" — both the instant on-click path (below) and the
	// authoritative afterSwap path (wiring section) funnel through it, so
	// they can never disagree regardless of how #content got navigated
	// (sidebar click, command palette, or an "All resources" directory row).
	function applyActiveResource(resource, label) {
		document.querySelectorAll(".sidebar-link.active").forEach((l) => l.classList.remove("active"));
		const link = document.querySelector('.sidebar-link[data-resource="' + cssEscape(resource) + '"]');
		if (link) link.classList.add("active");
		const resourceEl = document.getElementById("breadcrumb-resource");
		if (resourceEl) resourceEl.textContent = label || resource;
		setBreadcrumbName("");
	}

	// Fires instantly on click, before the (possibly multi-second) network
	// round trip completes — see namespace-select's stale-resource bug this
	// was built to fix: without this, "what resource am I on" had no live
	// client-side source of truth at all, only a value baked into the page
	// at the last full load.
	function selectSidebarLink(el) {
		applyActiveResource(el.dataset.resource, el.dataset.label);
	}

	// Authoritative sync, driven by the actual request htmx just completed —
	// catches every path into #content, not just sidebar clicks (command
	// palette results and "All resources" directory rows navigate #content
	// directly with no onclick of their own).
	function syncActiveResourceFromPath(path) {
		if (!path) return;
		const parts = path.split("/");
		// ["", "c", ctxId, ns, resource]
		if (parts[1] !== "c" || parts.length < 5) return;
		const resource = parts[4];
		const link = document.querySelector('.sidebar-link[data-resource="' + cssEscape(resource) + '"]');
		applyActiveResource(resource, link ? link.dataset.label : resource);
	}

	function changeNamespace(ns) {
		const ctx = document.body.dataset.ctx;
		const activeLink = document.querySelector(".sidebar-link.active");
		const resource = activeLink ? activeLink.dataset.resource : "overview";
		location.href = "/c/" + ctx + "/" + ns + "/" + resource;
	}

	// ---- row selection / sort / filter reconciliation (Phases 2, 3, 7) ----

	function findListPane(root) {
		return (root || document).querySelector(".list-pane");
	}

	function parseSortToken(text) {
		const t = text.trim();
		const age = t.match(/^(\d+)(s|m|h|d)$/);
		if (age) {
			const n = parseInt(age[1], 10);
			const mult = { s: 1, m: 60, h: 3600, d: 86400 }[age[2]];
			return n * mult;
		}
		const frac = t.match(/^(\d+)\/(\d+)$/);
		if (frac) return parseInt(frac[1], 10);
		const num = t.match(/^-?\d+(\.\d+)?$/);
		if (num) return parseFloat(t);
		return t.toLowerCase();
	}

	function applySortAndSelection(lp) {
		if (!lp) return;
		const table = lp.querySelector("#kui-table");
		const tbody = table && table.querySelector("tbody");
		if (tbody) {
			const sortCol = lp.dataset.sortCol;
			if (sortCol !== undefined && sortCol !== "") {
				const idx = parseInt(sortCol, 10);
				const dir = lp.dataset.sortDir === "desc" ? -1 : 1;
				const rows = Array.from(tbody.querySelectorAll("tr")).filter((tr) => tr.cells.length > idx);
				rows.sort((a, b) => {
					const av = parseSortToken(a.cells[idx].textContent);
					const bv = parseSortToken(b.cells[idx].textContent);
					if (av < bv) return -1 * dir;
					if (av > bv) return 1 * dir;
					return 0;
				});
				rows.forEach((tr) => tbody.appendChild(tr));
			}

			if (lp.dataset.selectedRow) {
				tbody.querySelectorAll("tr.selected").forEach((tr) => tr.classList.remove("selected"));
				const match = tbody.querySelector('tr[data-id="' + cssEscape(lp.dataset.selectedRow) + '"]');
				if (match) match.classList.add("selected");
			}

			const checked = (lp.dataset.checkedRows || "").split(",").filter(Boolean);
			if (checked.length) {
				tbody.querySelectorAll(".row-select").forEach((cb) => {
					cb.checked = checked.includes(cb.dataset.id);
				});
			}
			updateBulkBar(lp);
		}

		const filterInput = document.getElementById("row-filter");
		if (filterInput && filterInput.value) filterRows(filterInput.value);
	}

	function cssEscape(s) {
		return window.CSS && CSS.escape ? CSS.escape(s) : s.replace(/[^a-zA-Z0-9_-]/g, "\\$&");
	}

	function onTableClick(e) {
		const th = e.target.closest("#kui-table thead th");
		if (th) {
			const lp = th.closest(".list-pane");
			if (!lp) return;
			const row = th.parentElement;
			const idx = Array.from(row.children).indexOf(th);
			const current = lp.dataset.sortCol === String(idx);
			lp.dataset.sortCol = String(idx);
			lp.dataset.sortDir = current && lp.dataset.sortDir === "asc" ? "desc" : "asc";
			row.querySelectorAll("th").forEach((h) => h.classList.remove("sort-asc", "sort-desc"));
			th.classList.add(lp.dataset.sortDir === "desc" ? "sort-desc" : "sort-asc");
			applySortAndSelection(lp);
			return;
		}

		const tr = e.target.closest("#kui-table tbody tr");
		if (tr) {
			const lp = tr.closest(".list-pane");
			if (lp && tr.dataset.id) {
				lp.querySelectorAll("tr.selected").forEach((x) => x.classList.remove("selected"));
				tr.classList.add("selected");
				lp.dataset.selectedRow = tr.dataset.id;
			}
			if (tr.dataset.name) setBreadcrumbName(tr.dataset.name);
		}
	}

	// ---- bulk select / delete (Phase 7) ----

	function updateBulkBar(lp) {
		const bar = document.getElementById("bulk-bar");
		if (!bar) return;
		const checked = (lp.dataset.checkedRows || "").split(",").filter(Boolean);
		if (checked.length) {
			bar.style.display = "flex";
			document.getElementById("bulk-count").textContent = checked.length + " selected";
		} else {
			bar.style.display = "none";
		}
	}

	function onTableChange(e) {
		const cb = e.target.closest(".row-select");
		if (cb) {
			const lp = cb.closest(".list-pane");
			if (!lp) return;
			const ids = new Set((lp.dataset.checkedRows || "").split(",").filter(Boolean));
			if (cb.checked) ids.add(cb.dataset.id);
			else ids.delete(cb.dataset.id);
			lp.dataset.checkedRows = Array.from(ids).join(",");
			updateBulkBar(lp);
			return;
		}
		const selectAll = e.target.closest("#select-all-rows");
		if (selectAll) {
			const lp = selectAll.closest(".list-pane");
			if (!lp) return;
			const visible = Array.from(lp.querySelectorAll(".row-select")).filter((c) => c.closest("tr").style.display !== "none");
			visible.forEach((c) => (c.checked = selectAll.checked));
			lp.dataset.checkedRows = selectAll.checked ? visible.map((c) => c.dataset.id).join(",") : "";
			updateBulkBar(lp);
		}
	}

	function clearSelection() {
		const lp = findListPane(document);
		if (!lp) return;
		lp.dataset.checkedRows = "";
		lp.querySelectorAll(".row-select").forEach((c) => (c.checked = false));
		const selectAll = lp.querySelector("#select-all-rows");
		if (selectAll) selectAll.checked = false;
		updateBulkBar(lp);
	}

	function bulkDelete() {
		const lp = findListPane(document);
		if (!lp) return;
		const ids = (lp.dataset.checkedRows || "").split(",").filter(Boolean);
		if (!ids.length) return;
		if (!confirm("Delete " + ids.length + " selected resources?")) return;
		htmx.ajax("POST", lp.dataset.bulkDeleteUrl, { values: { path: ids } });
		clearSelection();
	}

	// ---- per-row metrics patching (Phase 6) ----

	function applyMetrics(map) {
		if (!map) return;
		Object.keys(map).forEach((key) => {
			const m = map[key];
			document.querySelectorAll('#kui-table tr[data-metric-key="' + cssEscape(key) + '"]').forEach((tr) => {
				const cpu = tr.querySelector(".cell-cpu");
				const mem = tr.querySelector(".cell-mem");
				if (cpu) cpu.textContent = m.cpu;
				if (mem) mem.textContent = m.mem;
			});
		});
		const lp = findListPane(document);
		if (lp && (lp.dataset.sortCol || "") !== "") applySortAndSelection(lp);
	}

	function applyCounts(map) {
		if (!map) return;
		Object.keys(map).forEach((key) => {
			const entry = map[key];
			document.querySelectorAll('[data-count-for="' + cssEscape(key) + '"]').forEach((el) => {
				const badge = el.querySelector(".sidebar-count");
				if (badge) badge.textContent = entry.count;
				const dot = el.querySelector(".sidebar-count-dot");
				if (dot) dot.style.display = entry.failing ? "inline-block" : "none";
			});
		});
	}

	// ---- command palette (⌘K) ----

	function isPaletteOpen() {
		const overlay = document.getElementById("palette-overlay");
		return overlay && overlay.style.display !== "none";
	}

	function openPalette() {
		const overlay = document.getElementById("palette-overlay");
		const input = document.getElementById("palette-input");
		const results = document.getElementById("palette-results");
		if (!overlay || !input) return;
		overlay.style.display = "flex";
		input.value = "";
		if (results) results.innerHTML = "";
		input.focus();
	}

	function closePalette() {
		const overlay = document.getElementById("palette-overlay");
		if (overlay) overlay.style.display = "none";
	}

	function movePaletteSelection(key) {
		const items = Array.from(document.querySelectorAll("#palette-results .palette-result"));
		if (!items.length) return;
		let idx = items.findIndex((el) => el.classList.contains("active"));
		if (key === "Enter") {
			(items[idx] || items[0]).click();
			return;
		}
		if (idx === -1) idx = 0;
		else {
			items[idx].classList.remove("active");
			idx = key === "ArrowDown" ? Math.min(idx + 1, items.length - 1) : Math.max(idx - 1, 0);
		}
		items[idx].classList.add("active");
		items[idx].scrollIntoView({ block: "nearest" });
	}

	// ---- resizable detail pane (Phase 2) ----

	function initSplitter() {
		const splitter = document.getElementById("detail-splitter");
		if (!splitter) return;
		const saved = localStorage.getItem("kui.detailPaneWidth");
		if (saved) document.documentElement.style.setProperty("--detail-pane-w", saved + "px");

		let dragging = false;
		let startX = 0;
		let startWidth = 0;
		splitter.addEventListener("mousedown", (e) => {
			dragging = true;
			startX = e.clientX;
			startWidth = parseInt(getComputedStyle(document.documentElement).getPropertyValue("--detail-pane-w"), 10) || 420;
			e.preventDefault();
		});
		document.addEventListener("mousemove", (e) => {
			if (!dragging) return;
			const delta = startX - e.clientX;
			const newWidth = Math.max(280, Math.min(900, startWidth + delta));
			document.documentElement.style.setProperty("--detail-pane-w", newWidth + "px");
		});
		document.addEventListener("mouseup", () => {
			if (!dragging) return;
			dragging = false;
			const w = getComputedStyle(document.documentElement).getPropertyValue("--detail-pane-w").trim();
			localStorage.setItem("kui.detailPaneWidth", parseInt(w, 10));
		});
	}

	// ---- theme toggle ----

	function setThemeIcon(theme) {
		const icon = document.getElementById("theme-icon");
		if (icon) icon.setAttribute("href", theme === "light" ? "#icon-sun" : "#icon-moon");
	}

	function initTheme() {
		setThemeIcon(document.documentElement.getAttribute("data-theme") || "dark");
	}

	function toggleTheme() {
		const current = document.documentElement.getAttribute("data-theme") || "dark";
		const next = current === "dark" ? "light" : "dark";
		document.documentElement.setAttribute("data-theme", next);
		localStorage.setItem("kui.theme", next);
		setThemeIcon(next);
	}

	// ---- wiring ----

	document.addEventListener("DOMContentLoaded", initSplitter);
	document.addEventListener("DOMContentLoaded", initTheme);

	document.addEventListener("click", onTableClick);
	document.addEventListener("change", onTableChange);

	document.addEventListener("keydown", (e) => {
		if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
			e.preventDefault();
			openPalette();
		} else if (e.key === "/" && !["INPUT", "TEXTAREA"].includes(document.activeElement.tagName)) {
			e.preventDefault();
			const f = document.getElementById("row-filter");
			if (f) f.focus();
		} else if (e.key === "Escape") {
			if (isPaletteOpen()) closePalette();
			else closeDetail();
		} else if (isPaletteOpen() && ["ArrowDown", "ArrowUp", "Enter"].includes(e.key)) {
			movePaletteSelection(e.key);
			e.preventDefault();
		}
	});

	document.body.addEventListener("toast", (e) => showToast(e.detail.toast));

	// htmx only swaps 2xx responses by default — a failed fragment request
	// (e.g. cluster unreachable) would otherwise fail completely silently,
	// leaving whatever pane was loading stuck. Surface it as a toast instead.
	document.body.addEventListener("htmx:responseError", (e) => {
		const xhr = e.detail.xhr;
		const msg = (xhr && xhr.responseText ? xhr.responseText : "request failed").trim().slice(0, 300);
		showToast({ level: "error", message: msg });
	});

	document.body.addEventListener("htmx:beforeSwap", (e) => {
		if (e.detail.target && e.detail.target.classList && e.detail.target.classList.contains("list-pane")) {
			e.detail.target.dataset.scrollTop = e.detail.target.scrollTop;
		}
	});

	document.body.addEventListener("htmx:afterSwap", (e) => {
		const target = e.detail.target;
		if (!target) return;

		if (target.id === "detail-pane") {
			const hasContent = target.innerHTML.trim() !== "";
			target.classList.toggle("open", hasContent);
			const splitter = document.getElementById("detail-splitter");
			if (splitter) splitter.style.display = hasContent ? "block" : "none";
		}

		if (target.id === "content") {
			syncActiveResourceFromPath(e.detail.requestConfig && e.detail.requestConfig.path);
		}

		if (target.classList && target.classList.contains("list-pane")) {
			applySortAndSelection(target);
			if (target.dataset.scrollTop) target.scrollTop = parseInt(target.dataset.scrollTop, 10);
		}
	});

	return {
		showTab,
		startLogs,
		startShell,
		filterRows,
		closeDetail,
		setBreadcrumbName,
		selectSidebarLink,
		changeNamespace,
		clearSelection,
		bulkDelete,
		applyMetrics,
		applyCounts,
		openPalette,
		closePalette,
		toggleTheme,
	};
})();
