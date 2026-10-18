(() => {
	"use strict";

	const sections = ["health", "capacity", "objects", "attention", "activity", "warnings", "event-groups"];
	const optionalSections = sections.slice(1);
	const tiles = ["nodes", "running", "pending", "failed", "namespaces", "warnings"];
	const installed = new WeakSet();
	let refreshPending = false;

	function scopeKey(root) {
		return "kui.overview.v1:" + encodeURIComponent(root.dataset.context || "") + ":" + encodeURIComponent(root.dataset.namespace || "_all");
	}

	function defaults() {
		return { hidden: [], pinned: [], order: sections.slice(), severity: "all" };
	}

	function normalizePrefs(raw) {
		const value = raw && typeof raw === "object" ? raw : {};
		const order = Array.isArray(value.order) ? [...new Set(value.order.filter((item) => sections.includes(item)))] : [];
		sections.forEach((item) => { if (!order.includes(item)) order.push(item); });
		return {
			hidden: Array.isArray(value.hidden) ? [...new Set(value.hidden.filter((item) => optionalSections.includes(item)))] : [],
			pinned: Array.isArray(value.pinned) ? [...new Set(value.pinned.filter((item) => tiles.includes(item)))] : [],
			order,
			severity: ["all", "critical", "warning"].includes(value.severity) ? value.severity : "all",
		};
	}

	function loadPrefs(root) {
		try {
			return normalizePrefs(JSON.parse(localStorage.getItem(scopeKey(root)) || "null"));
		} catch {
			return defaults();
		}
	}

	function savePrefs(root, prefs) {
		try {
			localStorage.setItem(scopeKey(root), JSON.stringify(prefs));
		} catch {
			// The dashboard remains interactive if storage is disabled or full.
		}
	}

	function announce(root, message) {
		const status = root.querySelector("#overview-announcer");
		if (status) status.textContent = message;
	}

	function applySections(root, prefs) {
		const panel = root.querySelector("#overview-settings-panel");
		root.querySelectorAll("[data-overview-section]").forEach((section) => {
			const name = section.dataset.overviewSection;
			section.hidden = prefs.hidden.includes(name);
			section.draggable = Boolean(panel && !panel.hidden);
			section.setAttribute("aria-grabbed", "false");
		});
		if (panel) {
			panel.querySelectorAll("[data-toggle-section]").forEach((input) => {
				input.checked = !prefs.hidden.includes(input.dataset.toggleSection);
			});
		}
		const byName = new Map(Array.from(root.querySelectorAll(":scope > .overview-section"), (section) => [section.dataset.overviewSection, section]));
		prefs.order.forEach((name) => {
			const section = byName.get(name);
			if (section) root.appendChild(section);
		});
	}

	function ensureSectionActions(root) {
		root.querySelectorAll(".overview-section").forEach((section) => {
			const title = section.querySelector(":scope > h2.section-title, :scope > .overview-section-heading > h2.section-title");
			if (!title || title.closest(".overview-section-heading")?.querySelector(".overview-section-actions")) return;
			let heading = title.parentElement;
			if (!heading.classList.contains("overview-section-heading")) {
				heading = document.createElement("div");
				heading.className = "overview-section-heading";
				title.before(heading);
				heading.appendChild(title);
			}
			const actions = document.createElement("div");
			actions.className = "overview-section-actions";
			for (const [direction, text] of [["up", "Move section up"], ["down", "Move section down"]]) {
				const button = document.createElement("button");
				button.type = "button";
				button.className = "icon-btn overview-section-move";
				button.dataset.sectionMove = direction;
				button.setAttribute("aria-label", text + ": " + section.getAttribute("aria-label"));
				button.textContent = direction === "up" ? "↑" : "↓";
				actions.appendChild(button);
			}
			heading.appendChild(actions);
		});
	}

	function applyTiles(root, prefs) {
		const grid = root.querySelector(".tile-grid");
		if (!grid) return;
		const shells = Array.from(grid.querySelectorAll("[data-overview-tile]"));
		shells.sort((a, b) => Number(prefs.pinned.includes(b.dataset.overviewTile)) - Number(prefs.pinned.includes(a.dataset.overviewTile)));
		shells.forEach((shell) => grid.appendChild(shell));
		shells.forEach((shell) => {
			const button = shell.querySelector("[data-pin-tile]");
			if (!button) return;
			const pinned = prefs.pinned.includes(button.dataset.pinTile);
			button.setAttribute("aria-pressed", String(pinned));
			button.textContent = pinned ? "Pinned" : "Pin";
			button.setAttribute("aria-label", (pinned ? "Unpin " : "Pin ") + button.dataset.pinTile + " tile");
		});
	}

	function applySeverity(root, prefs) {
		const select = root.querySelector("#attention-severity");
		if (select) select.value = prefs.severity;
		const rows = Array.from(root.querySelectorAll("#overview [data-severity]"));
		rows.forEach((row) => {
			row.hidden = prefs.severity !== "all" && row.dataset.severity !== prefs.severity;
		});
		const empty = root.querySelector("#attention-filter-empty");
		if (empty) empty.hidden = prefs.severity === "all" || rows.length === 0 || rows.some((row) => !row.hidden);
	}

	function render(root) {
		const prefs = loadPrefs(root);
		ensureSectionActions(root);
		applySections(root, prefs);
		root.querySelectorAll("[data-section-move]").forEach((button) => {
			const orderIndex = prefs.order.indexOf(button.closest(".overview-section")?.dataset.overviewSection);
			button.disabled = button.dataset.sectionMove === "up" ? orderIndex <= 0 : orderIndex < 0 || orderIndex >= prefs.order.length - 1;
		});
		applyTiles(root, prefs);
		applySeverity(root, prefs);
		if (!root.querySelector("#overview-announcer")) {
			const status = document.createElement("span");
			status.id = "overview-announcer";
			status.className = "sr-only";
			status.setAttribute("role", "status");
			status.setAttribute("aria-live", "polite");
			root.appendChild(status);
		}
	}

	function initialize(root) {
		if (!root || installed.has(root)) return;
		installed.add(root);
		render(root);
	}

	function reorder(root, prefs, name, direction) {
		const index = prefs.order.indexOf(name);
		const next = index + (direction === "up" ? -1 : 1);
		if (index < 0 || next < 0 || next >= prefs.order.length) return;
		[prefs.order[index], prefs.order[next]] = [prefs.order[next], prefs.order[index]];
		savePrefs(root, prefs);
		render(root);
		const section = root.querySelector('[data-overview-section="' + name + '"]');
		section?.querySelector('[data-section-move="' + direction + '"]')?.focus();
		announce(root, (section?.getAttribute("aria-label") || name) + " moved " + direction + ".");
	}

	function toggleSettings(root, button) {
		const panel = root.querySelector("#overview-settings-panel");
		if (!panel) return;
		panel.hidden = !panel.hidden;
		button.setAttribute("aria-expanded", String(!panel.hidden));
		root.querySelectorAll("[data-overview-section]").forEach((section) => {
			section.draggable = !panel.hidden;
		});
		if (!panel.hidden) panel.querySelector("input,button")?.focus();
	}

	document.addEventListener("click", (event) => {
		const root = event.target.closest("#overview");
		if (!root) return;
		const prefs = loadPrefs(root);
		const settings = event.target.closest("#overview-settings-button");
		if (settings) return toggleSettings(root, settings);
		const reset = event.target.closest("#overview-settings-reset");
		if (reset) {
			try { localStorage.removeItem(scopeKey(root)); } catch { /* no-op */ }
			render(root);
			announce(root, "Overview layout reset.");
			root.querySelector("#overview-settings-button")?.focus();
			return;
		}
		const pin = event.target.closest("[data-pin-tile]");
		if (pin) {
			const tile = pin.dataset.pinTile;
			prefs.pinned = prefs.pinned.includes(tile) ? prefs.pinned.filter((item) => item !== tile) : [...prefs.pinned, tile];
			savePrefs(root, prefs);
			render(root);
			announce(root, tile + (prefs.pinned.includes(tile) ? " tile pinned." : " tile unpinned."));
			root.querySelector('[data-pin-tile="' + tile + '"]')?.focus();
			return;
		}
		const move = event.target.closest("[data-section-move]");
		if (move) return reorder(root, prefs, move.closest(".overview-section")?.dataset.overviewSection, move.dataset.sectionMove);
		const refresh = event.target.closest("#overview-refresh");
		if (refresh) refreshOverview(refresh);
	});

	document.addEventListener("change", (event) => {
		const root = event.target.closest("#overview");
		if (!root) return;
		const prefs = loadPrefs(root);
		const sectionToggle = event.target.closest("[data-toggle-section]");
		if (sectionToggle) {
			const section = sectionToggle.dataset.toggleSection;
			prefs.hidden = sectionToggle.checked ? prefs.hidden.filter((item) => item !== section) : [...prefs.hidden, section];
			savePrefs(root, prefs);
			render(root);
			announce(root, section + (sectionToggle.checked ? " section shown." : " section hidden."));
			return;
		}
		const severity = event.target.closest("#attention-severity");
		if (severity) {
			prefs.severity = severity.value;
			savePrefs(root, prefs);
			applySeverity(root, prefs);
			announce(root, severity.value === "all" ? "Showing all attention items." : "Showing " + severity.value + " attention items.");
		}
	});

	document.addEventListener("dragstart", (event) => {
		const section = event.target.closest("#overview .overview-section");
		const root = event.target.closest("#overview");
		if (!section || !root || root.querySelector("#overview-settings-panel")?.hidden) return event.preventDefault();
		section.setAttribute("aria-grabbed", "true");
		event.dataTransfer?.setData("text/plain", section.dataset.overviewSection);
	});
	document.addEventListener("dragover", (event) => {
		if (event.target.closest("#overview .overview-section") && !event.target.closest("#overview .overview-section").closest("#overview")?.querySelector("#overview-settings-panel")?.hidden) event.preventDefault();
	});
	document.addEventListener("drop", (event) => {
		const target = event.target.closest("#overview .overview-section");
		const root = target?.closest("#overview");
		if (!target || !root) return;
		event.preventDefault();
		const source = event.dataTransfer?.getData("text/plain");
		const prefs = loadPrefs(root);
		const from = prefs.order.indexOf(source);
		const to = prefs.order.indexOf(target.dataset.overviewSection);
		if (from < 0 || to < 0 || from === to) return;
		prefs.order.splice(from, 1);
		prefs.order.splice(to, 0, source);
		savePrefs(root, prefs);
		render(root);
		announce(root, "Dashboard section order updated.");
	});
	document.addEventListener("dragend", (event) => {
		event.target.closest("#overview .overview-section")?.setAttribute("aria-grabbed", "false");
	});

	async function refreshOverview(button) {
		if (refreshPending || !window.htmx) return;
		const list = document.querySelector("#list-pane");
		const url = list?.dataset.listUrl;
		if (!url) return;
		refreshPending = true;
		const status = button.closest("#overview")?.querySelector("#overview-refresh-status");
		if (status) status.textContent = "Refreshing overview data.";
		button.disabled = true;
		button.setAttribute("aria-busy", "true");
		const release = () => {
			refreshPending = false;
			button.disabled = false;
			button.removeAttribute("aria-busy");
		};
		try {
			await window.htmx.ajax("GET", url, { target: list, swap: "innerHTML" });
		} catch {
			const message = "Could not refresh the overview. Existing values are unchanged; retry when cluster access is available.";
			if (status?.isConnected) status.textContent = message;
		} finally {
			release();
		}
	}

	document.addEventListener("htmx:afterRequest", (event) => {
		const detail = event.detail;
		if (detail?.target?.id !== "list-pane" || detail.success !== false) return;
		const message = "Could not refresh the overview. Existing values are unchanged; retry when cluster access is available.";
		const status = document.querySelector("#overview-refresh-status");
		if (status) status.textContent = message;
		document.body.dispatchEvent(new CustomEvent("toast", {
			detail: { toast: { level: "error", message: "Could not refresh the overview." } },
		}));
	});
	document.addEventListener("htmx:afterSwap", (event) => {
		const target = event.detail?.target;
		if (!target) return;
		initialize(target.matches?.("#overview") ? target : target.querySelector?.("#overview"));
		if (target.matches?.(".list-pane") || target.id === "list-pane") initialize(target.querySelector("#overview"));
	});
	document.addEventListener("DOMContentLoaded", () => initialize(document.querySelector("#overview")));
	window.addEventListener("storage", () => {
		const root = document.querySelector("#overview");
		if (root) render(root);
	});
})();
