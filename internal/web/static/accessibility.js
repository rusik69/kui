(() => {
	"use strict";

	let dialog;
	let returnFocus;
	let lastDetailTrigger;
	let pendingGo;
	let pendingFocus;
	let goTimer;
	const announcedRequests = new WeakSet();

	function isTyping(target) {
		return Boolean(target?.closest?.("input, textarea, select, [contenteditable='true'], [role='textbox']"));
	}

	function appendCell(row, text, header) {
		const cell = document.createElement(header ? "th" : "td");
		cell.scope = header ? "col" : "";
		cell.textContent = text;
		row.appendChild(cell);
	}

	function ensureDialog() {
		if (dialog?.isConnected) return dialog;
		dialog = document.getElementById("shortcuts-dialog");
		if (dialog) return dialog;
		dialog = document.createElement("dialog");
		dialog.id = "shortcuts-dialog";
		dialog.className = "shortcuts-dialog";
		dialog.setAttribute("aria-labelledby", "shortcuts-title");
		const heading = document.createElement("div");
		heading.className = "shortcuts-dialog-heading";
		const title = document.createElement("h2");
		title.id = "shortcuts-title";
		title.textContent = "Keyboard shortcuts";
		const close = document.createElement("button");
		close.type = "button";
		close.className = "icon-btn";
		close.dataset.shortcutClose = "true";
		close.setAttribute("aria-label", "Close keyboard shortcuts");
		close.textContent = "×";
		heading.append(title, close);
		const description = document.createElement("p");
		description.textContent = "Navigate the cluster without leaving the keyboard.";
		const table = document.createElement("table");
		table.className = "shortcuts-table";
		const thead = document.createElement("thead");
		const header = document.createElement("tr");
		appendCell(header, "Shortcut", true);
		appendCell(header, "Action", true);
		thead.appendChild(header);
		const tbody = document.createElement("tbody");
		[["?", "Open this help"], ["g, then p", "Open Pods"], ["g, then n", "Open Nodes"], ["g, then d", "Open Deployments"], ["f, then t", "Focus the resource table"], ["f, then d", "Focus the detail pane"], ["f, then n", "Focus navigation"], ["Alt + ↑ / ↓", "Move between resource rows"], ["Enter", "Open the focused resource row"], ["Space on a row", "Toggle its selection"], ["Esc", "Close dialog or return from detail"]].forEach(([key, action]) => {
			const row = document.createElement("tr");
			appendCell(row, key, false);
			appendCell(row, action, false);
			tbody.appendChild(row);
		});
		table.append(thead, tbody);
		const footer = document.createElement("div");
		footer.className = "shortcuts-dialog-footer";
		const done = document.createElement("button");
		done.type = "button";
		done.className = "btn btn-primary";
		done.dataset.shortcutClose = "true";
		done.textContent = "Done";
		footer.appendChild(done);
		dialog.append(heading, description, table, footer);
		document.body.appendChild(dialog);
		dialog.addEventListener("close", () => {
			if (returnFocus?.isConnected) returnFocus.focus();
			returnFocus = null;
		});
		return dialog;
	}

	function ensureButton() {
		if (document.getElementById("shortcuts-button")) return;
		const button = document.createElement("button");
		button.type = "button";
		button.id = "shortcuts-button";
		button.className = "btn shortcuts-button";
		button.setAttribute("aria-haspopup", "dialog");
		button.setAttribute("aria-keyshortcuts", "?");
		button.textContent = "Shortcuts ?";
		const host = document.querySelector(".topbar .topbar-actions, .topbar, .app-header, .toolbar-actions") || document.querySelector(".app");
		if (!host) return;
		if (host.classList.contains("app")) button.classList.add("shortcuts-button-floating");
		host.appendChild(button);
	}

	function openDialog(trigger) {
		const element = ensureDialog();
		if (element.open) return;
		returnFocus = trigger || document.activeElement;
		element.showModal();
		element.querySelector("[data-shortcut-close]")?.focus();
	}

	function navigateResource(resource) {
		const links = Array.from(document.querySelectorAll(".sidebar a[href], nav a[href]"));
		const link = links.find((item) => {
			try { return new URL(item.href, location.href).pathname.replace(/\/$/, "").endsWith("/" + resource); }
			catch { return false; }
		});
		link?.click();
	}

	function focusAdjacentRow(current, direction) {
		const row = current.closest("tr, .resource-row, [role='row']");
		if (!row) return;
		const candidates = Array.from(document.querySelectorAll("tbody tr, .resource-row, [role='row']"))
			.filter((item) => !item.hidden && item.matches("[tabindex='0']"));
		const index = candidates.indexOf(row);
		candidates[index + direction]?.focus();
	}
	function focusLandmark(name) {
		const selectors = {
			t: "#list-pane #kui-table, #list-pane .resource-table",
			d: "#detail-pane button:not([disabled]), #detail-pane a[href], #detail, #detail-pane",
			n: ".sidebar a[href], nav a[href]",
		};
		const target = document.querySelector(selectors[name]);
		if (!target) return;
		if (!target.matches("a,button,input,select,textarea,[tabindex]")) target.tabIndex = -1;
		target.focus();
	}

	function announce(message, request) {
		if (request && announcedRequests.has(request)) return;
		if (request) announcedRequests.add(request);
		let live = document.getElementById("kui-live-region");
		if (!live) {
			live = document.createElement("div");
			live.id = "kui-live-region";
			live.className = "sr-only";
			live.setAttribute("role", "status");
			live.setAttribute("aria-live", "polite");
			document.body.appendChild(live);
		}
		live.textContent = "";
		requestAnimationFrame(() => { live.textContent = message; });
	}


	document.addEventListener("click", (event) => {
		const trigger = event.target.closest("#shortcuts-button");
		if (trigger) {
			event.preventDefault();
			openDialog(trigger);
			return;
		}
		if (event.target.closest("[data-shortcut-close]")) {
			dialog?.close();
			return;
		}
		const detailTrigger = event.target.closest("tr[hx-target='#detail-pane'], a[hx-target='#detail-pane'], a[hx-target='#detail']");
		if (detailTrigger && !event.target.closest("button,input")) lastDetailTrigger = detailTrigger;
	}, true);

	document.addEventListener("keydown", (event) => {
		if (event.defaultPrevented || event.isComposing) return;
		if (event.key === "Escape") {
			if (dialog?.open) {
				dialog.close();
				event.preventDefault();
				return;
			}
			if (document.querySelector("dialog[open]")) return;
			const detailPane = document.querySelector("#detail-pane, #detail");
			if (detailPane?.children.length && lastDetailTrigger?.isConnected) {
				const close = detailPane.querySelector("[data-close-detail], .detail-close, button[aria-label*='close' i]");
				if (close) {
					close.click();
					event.preventDefault();
				}
			}
			return;
		}
		if (document.querySelector("dialog[open]") || isTyping(event.target)) return;
		if (event.altKey && (event.key === "ArrowUp" || event.key === "ArrowDown")) {
			focusAdjacentRow(event.target, event.key === "ArrowUp" ? -1 : 1);
			event.preventDefault();
			return;
		}
		if (event.key === "Enter" && event.target.matches("tr[tabindex='0'][hx-get]")) {
			event.preventDefault();
			event.target.click();
			return;
		}
		if (event.key === " " && !event.altKey && !event.ctrlKey && !event.metaKey) {
			const row = event.target.closest("tr, [role='row']");
			const checkbox = row?.querySelector("input[type='checkbox']:not([disabled])");
			if (checkbox && event.target === row) {
				event.preventDefault();
				checkbox.click();
				const pane = row.closest("#list-pane") || document;
				const checkboxes = Array.from(pane.querySelectorAll("tbody tr:not([hidden]) input[type='checkbox']:not([disabled])"));
				announce(checkboxes.filter((item) => item.checked).length + " of " + checkboxes.length + " visible resources selected.");
				return;
			}
		}
		if (event.metaKey || event.ctrlKey || event.altKey) return;
		if (event.key === "?") {
			event.preventDefault();
			openDialog(event.target);
			return;
		}
		if (pendingFocus) {
			const key = event.key.toLowerCase();
			pendingFocus = false;
			clearTimeout(goTimer);
			if (["t", "d", "n"].includes(key)) {
				event.preventDefault();
				focusLandmark(key);
			}
			return;
		}
		if (pendingGo) {
			const key = event.key.toLowerCase();
			const destinations = { p: "pods", n: "nodes", d: "deployments" };
			pendingGo = false;
			clearTimeout(goTimer);
			if (destinations[key]) {
				event.preventDefault();
				navigateResource(destinations[key]);
			}
			return;
		}
		if (event.key.toLowerCase() === "g" || event.key.toLowerCase() === "f") {
			pendingFocus = event.key.toLowerCase() === "f";
			pendingGo = !pendingFocus;
			goTimer = setTimeout(() => { pendingGo = false; pendingFocus = false; }, 1100);
		}
	});

	function setLoading(target, loading) {
		if (!(target instanceof Element)) return;
		if (loading) {
			target.setAttribute("aria-busy", "true");
			if (target.id === "detail-pane") {
				if (!target.querySelector(".detail-loading-skeleton")) {
					const skeleton = document.createElement("div");
					skeleton.className = "detail-loading-skeleton";
					skeleton.setAttribute("aria-hidden", "true");
					for (const className of ["detail-skeleton-heading", "detail-skeleton-tabs", "detail-skeleton-line", "detail-skeleton-line", "detail-skeleton-line", "detail-skeleton-line"]) {
						const block = document.createElement("span");
						block.className = "skeleton " + className;
						skeleton.appendChild(block);
					}
					target.appendChild(skeleton);
				}
				return;
			}
			const body = target.querySelector(".resource-table tbody");
			if (!body || body.querySelector(".loading-skeleton-row")) return;
			const columns = Math.max(1, target.querySelectorAll(".resource-table thead th").length);
			for (let index = 0; index < 3; index++) {
				const row = document.createElement("tr");
				row.className = "loading-skeleton-row";
				row.setAttribute("aria-hidden", "true");
				const cell = document.createElement("td");
				cell.colSpan = columns;
				const line = document.createElement("span");
				line.className = "skeleton skeleton-line";
				cell.appendChild(line);
				row.appendChild(cell);
				body.appendChild(row);
			}
		} else {
			target.removeAttribute("aria-busy");
			target.querySelectorAll(".loading-skeleton-row, .detail-loading-skeleton").forEach((row) => row.remove());
		}
	}

	document.addEventListener("kui:detail-closed", () => {
		if (lastDetailTrigger?.isConnected) lastDetailTrigger.focus();
		lastDetailTrigger = null;
	});
	document.addEventListener("htmx:beforeRequest", (event) => {
		setLoading(event.detail?.target, true);
	});
	document.addEventListener("htmx:afterSwap", (event) => {
		const target = event.detail?.target;
		if (target?.id === "detail-pane" && !target.children.length && lastDetailTrigger?.isConnected) {
			lastDetailTrigger.focus();
			lastDetailTrigger = null;
		}
		if (target && (target.id === "list-pane" || target.matches?.(".list-pane"))) {
			const rows = target.querySelectorAll("tbody tr:not([hidden])");
			const count = Array.from(rows).filter((row) => row.querySelector("td:not(.empty-state), th")).length;
			announce("Resource view updated; " + count + " visible rows.", event.detail?.xhr);
		} else if (target?.id === "detail-pane" && target.children.length) {
			announce("Resource details opened.", event.detail?.xhr);
		} else if (target?.id === "palette-results") {
			announce("Search results updated.", event.detail?.xhr);
		} else if (target?.id === "batch-metadata-results") {
			announce("Batch metadata results updated.", event.detail?.xhr);
		} else if (target?.id === "comparison-panel") {
			announce("Resource comparison updated.", event.detail?.xhr);
		}
		ensureButton();
	});
	document.addEventListener("htmx:afterRequest", (event) => {
		const detail = event.detail;
		setLoading(detail?.target, false);
		if (detail && detail.success === false) {
			const status = detail.xhr?.status;
			announce(status ? "Request failed with status " + status + "." : "Request failed; check the connection and retry.", detail.xhr);
		}
	});
	document.addEventListener("DOMContentLoaded", () => {
		ensureButton();
		ensureDialog();
	});
})();
