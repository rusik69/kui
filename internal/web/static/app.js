window.kui = (function () {
	function showTab(btn, panelId) {
		document.querySelectorAll(".detail-tab-panel").forEach((panel) => {
			const selected = panel.id === panelId;
			panel.hidden = !selected;
			panel.style.display = selected ? "block" : "none";
		});
		document.querySelectorAll(".tab-row .tab-btn").forEach((tab) => {
			const selected = tab === btn;
			tab.classList.toggle("active", selected);
			tab.setAttribute("aria-selected", String(selected));
			tab.tabIndex = selected ? 0 : -1;
		});
	}

	function wsURL(path) {
		const proto = location.protocol === "https:" ? "wss:" : "ws:";
		return proto + "//" + location.host + path;
	}

	let logSocket = null;
	let logBuffer = "";
	let logAttempt = false;
	let logBase = "";
	let logPaused = false;
	let logRetryTimer = null;
	let logRetryAttempt = 0;
	let logTrimmed = false;
	let logBufferBaseLine = 0;
	let logTotalLines = 0;
	let logFilterInvalid = false;
	let terminalSession = null;
	let shellAttempt = false;
	let shellBase = "";
	let watchFailed = false;
	let secretTimers = new WeakMap();
	let selectedNames = new Map();
	let batchFailures = [];
	let activeSplitter = null;
	let activeDetailPane = null;
	let detailReturnFocus = null;
	let splitterListenersInstalled = false;
	let splitterWidthLoaded = false;
	let splitterDragType = null;
	let splitterPointerId = null;
	let splitterStartX = 0;
	let splitterStartWidth = 0;
	const maxLogLines = 5000;
	const maxLogBytes = 2 * 1024 * 1024;

	function setStreamStatus(id, text) {
		const status = document.getElementById(id);
		if (status) status.textContent = text;
	}

	function scheduleLogReconnect() {
		if (logRetryTimer !== null || !logBase) return;
		logRetryAttempt += 1;
		const delay = Math.min(30000, 1000 * Math.pow(2, logRetryAttempt - 1));
		const cancel = document.getElementById("log-retry-cancel");
		if (cancel) cancel.hidden = false;
		setStreamStatus("log-status", "Reconnect attempt " + logRetryAttempt + " in " + (delay / 1000) + "s");
		logRetryTimer = window.setTimeout(() => {
			logRetryTimer = null;
			if (cancel) cancel.hidden = true;
			startLogs(logBase);
		}, delay);
	}

	function cancelLogReconnect() {
		clearTimeout(logRetryTimer);
		logRetryTimer = null;
		logRetryAttempt = 0;
		const cancel = document.getElementById("log-retry-cancel");
		if (cancel) cancel.hidden = true;
		setStreamStatus("log-status", "Reconnect canceled");
	}

	function closeLogStream() {
		clearTimeout(logRetryTimer);
		logRetryTimer = null;
		const retryCancel = document.getElementById("log-retry-cancel");
		if (retryCancel) retryCancel.hidden = true;
		if (logSocket) {
			logSocket.onopen = null;
			logSocket.onmessage = null;
			logSocket.onerror = null;
			logSocket.onclose = null;
			logSocket.close();
			logSocket = null;
		}
		logAttempt = false;
		setStreamStatus("log-status", "Disconnected");
		const reconnect = document.getElementById("log-reconnect");
		if (reconnect) reconnect.disabled = !logBase;
		const pause = document.getElementById("log-pause");
		if (pause) {
			pause.disabled = true;
			pause.textContent = "Pause";
			pause.setAttribute("aria-pressed", "false");
		}
	}

	function filteredLogText() {
		const input = document.getElementById("log-filter");
		const filter = input?.value || "";
		if (!filter) { logFilterInvalid = false; return logBuffer; }
		const sensitive = !!document.getElementById("log-case-sensitive")?.checked;
		const regex = !!document.getElementById("logs-regex")?.checked;
		if (regex) {
			try {
				const expression = new RegExp(filter, sensitive ? "" : "i");
				logFilterInvalid = false;
				return logBuffer.split("\n").filter((line) => expression.test(line)).join("\n");
			} catch {
				logFilterInvalid = true;
				return "";
			}
		}
		logFilterInvalid = false;
		const query = sensitive ? filter : filter.toLowerCase();
		return logBuffer.split("\n").filter((line) =>
			(sensitive ? line : line.toLowerCase()).includes(query)).join("\n");
	}

	function renderLogBuffer() {
		const pane = document.getElementById("logs-pane");
		if (!pane) return;
		const previousTop = pane.scrollTop;
		pane.textContent = filteredLogText();
		pane.style.whiteSpace = document.getElementById("logs-wrap")?.checked ? "pre-wrap" : "pre";
		if (document.getElementById("log-follow")?.checked && !logPaused) pane.scrollTop = pane.scrollHeight;
		else pane.scrollTop = previousTop;
		const lines = logBuffer.split("\n");
		const status = document.getElementById("log-buffer-status");
		if (status) status.textContent = logFilterInvalid ? "Invalid regular expression" :
			lines.filter((line, index) => line || index < lines.length - 1).length +
			" buffered lines" + (logTrimmed ? " · older lines trimmed" : "");
	}

	function logSettingsKey() {
		const pane = document.getElementById("detail-pane");
		return "kui.logs:" + (pane?.querySelector(".detail-header")?.dataset.podIdentity || location.pathname);
	}

	function restoreLogSettings() {
		const container = document.getElementById("log-container");
		const tail = document.getElementById("log-tail-lines");
		if (!container || !tail) return;
		try {
			const saved = JSON.parse(localStorage.getItem(logSettingsKey()) || "{}");
			if (saved && Array.from(container.options).some((option) => option.value === saved.container)) {
				container.value = saved.container;
			} else if (saved && saved.container) {
				if (container.options.length) container.selectedIndex = 0;
				saved.container = container.value;
				localStorage.setItem(logSettingsKey(), JSON.stringify(saved));
			}
			const count = Number.parseInt(saved.tail, 10);
			if (Number.isInteger(count) && count >= 1 && count <= maxLogLines) tail.value = String(count);
			const preset = document.getElementById("log-tail-preset");
			if (preset && Array.from(preset.options).some((option) => option.value === String(count))) preset.value = String(count);
			["logs-previous", "logs-timestamps", "logs-wrap"].forEach((id) => {
				const el = document.getElementById(id);
				if (el && typeof saved[id] === "boolean") el.checked = saved[id];
			});
			const since = document.getElementById("logs-since");
			if (since && Array.from(since.options).some((option) => option.value === saved.since)) since.value = saved.since;
		} catch { /* Storage is optional. */ }
	}

	function persistLogSettings() {
		try {
			localStorage.setItem(logSettingsKey(), JSON.stringify({
				container: document.getElementById("log-container")?.value || "",
				tail: document.getElementById("log-tail-lines")?.value || "500",
				"logs-previous": !!document.getElementById("logs-previous")?.checked,
				"logs-timestamps": !!document.getElementById("logs-timestamps")?.checked,
				"logs-wrap": !!document.getElementById("logs-wrap")?.checked,
				since: document.getElementById("logs-since")?.value || "",
			}));
		} catch { /* Storage is optional. */ }
	}

	function startLogs(base) {
		if (logAttempt) return;
		disposeTerminal();
		closeLogStream();
		const pane = document.getElementById("logs-pane");
		if (!pane) return;
		logBuffer = "";
		logBufferBaseLine = logTotalLines;
		logTrimmed = false;
		logFailed = false;
		logPaused = false;
		renderLogBuffer();
		const params = new URLSearchParams();
		const containerSelect = document.getElementById("log-container");
		const container = containerSelect?.value || "";
		const tailInput = document.getElementById("log-tail-lines");
		const parsedTail = Number.parseInt(tailInput?.value || "500", 10);
		const tailLines = Number.isFinite(parsedTail) ? Math.max(1, Math.min(parsedTail, maxLogLines)) : 500;
		if (tailInput) tailInput.value = String(tailLines);
		if (container) params.set("container", container);
		params.set("tailLines", String(tailLines));
		const previous = !!document.getElementById("logs-previous")?.checked;
		const timestamps = !!document.getElementById("logs-timestamps")?.checked;
		const since = document.getElementById("logs-since")?.value || "";
		if (previous) params.set("previous", "true");
		if (timestamps) params.set("timestamps", "true");
		if (since) params.set("since", since);
		const mode = document.getElementById("log-mode");
		if (mode) mode.textContent = (previous ? "Previous instance" : "Current instance") + (timestamps ? " · timestamps" : "") + (since ? " · last " + (since / 3600) + "h" : "");
		persistLogSettings();
		logAttempt = true;
		const ws = new WebSocket(wsURL(base + "?" + params.toString()));
		logSocket = ws;
		const pause = document.getElementById("log-pause");
		if (pause) pause.disabled = true;
		const reconnect = document.getElementById("log-reconnect");
		if (reconnect) reconnect.disabled = true;
		ws.onopen = () => {
			if (logSocket !== ws) return;
			// Keep the attempt active for the lifetime of the socket to prevent duplicate connects.
			if (pause) pause.disabled = false;
			const reconnect = document.getElementById("log-reconnect");
			if (reconnect) reconnect.disabled = true;
			logRetryAttempt = 0;
			setStreamStatus("log-status", "Connected · " + (container || "default container"));
		};
		ws.onmessage = (ev) => {
			if (logSocket !== ws) return;
			logBuffer += ev.data;
			let lines = logBuffer.split("\n");
			let bytes = new TextEncoder().encode(logBuffer).length;
			while (lines.length && (lines.length > maxLogLines || bytes > maxLogBytes)) {
				if (lines.length === 1) lines = [];
				else lines.shift();
				logBufferBaseLine += 1;
				logTrimmed = true;
				bytes = new TextEncoder().encode(lines.join("\n")).length;
			}
			logBuffer = lines.join("\n");
			logTotalLines = logBufferBaseLine + lines.filter((line, index) => line || index < lines.length - 1).length;
			const status = document.getElementById("log-buffer-status");
			if (status && logPaused) status.textContent =
				(lines.filter((line, index) => line || index < lines.length - 1).length) +
				" buffered lines" + (logTrimmed ? " · older lines trimmed" : "");
			if (!logPaused) renderLogBuffer();
		};
		ws.onerror = () => {
			if (logSocket !== ws) return;
			logAttempt = false;
			logFailed = true;
			const reconnect = document.getElementById("log-reconnect");
			if (reconnect) reconnect.disabled = false;
			const pause = document.getElementById("log-pause");
			if (pause) pause.disabled = true;
			setStreamStatus("log-status", "Connection error — reconnect available");
			scheduleLogReconnect();
		};
		ws.onclose = () => {
			if (logSocket === ws) {
				logSocket = null;
				logAttempt = false;
				const reconnect = document.getElementById("log-reconnect");
				if (reconnect) reconnect.disabled = false;
				setStreamStatus("log-status", logFailed ? "Connection error — reconnect available" : "Disconnected — reconnect available");
				scheduleLogReconnect();
				if (pause) pause.disabled = true;
			}
		};
	}

	function downloadLogs(filtered = false) {
		const pod = document.querySelector("#detail-pane .detail-header strong")?.textContent || "pod";
		const container = document.getElementById("log-container")?.value || "default";
		const safe = (value) => value.replace(/[^a-zA-Z0-9._-]+/g, "_").replace(/^[-_.]+|[-_.]+$/g, "") || "unknown";
		const content = filtered ? filteredLogText() : logBuffer;
		const mode = filtered ? "filtered" : "raw";
		const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
		const url = URL.createObjectURL(blob);
		const link = document.createElement("a");
		link.href = url;
		link.download = safe(pod) + "-" + safe(container) + "-" + mode + (logTrimmed ? "-partial" : "") + ".log";
		link.hidden = true;
		document.body.appendChild(link);
		link.click();
		link.remove();
		window.setTimeout(() => URL.revokeObjectURL(url), 0);
	}

	function copyFilteredLogs() { copyText(filteredLogText()); }
	function downloadFilteredLogs() { downloadLogs(true); }

	function jumpToLatest() {
		const pane = document.getElementById("logs-pane");
		if (pane) pane.scrollTop = pane.scrollHeight;
	}

	function toggleFollow(enabled) {
		if (enabled) jumpToLatest();
	}

	function toggleLogs() {
		if (!logSocket) return;
		logPaused = !logPaused;
		const button = document.getElementById("log-pause");
		if (button) {
			button.textContent = logPaused ? "Resume" : "Pause";
			button.setAttribute("aria-pressed", String(logPaused));
		}
		if (logPaused) setStreamStatus("log-status", "Paused");
		else {
			setStreamStatus("log-status", "Connected");
			renderLogBuffer();
		}
	}

	function clearLogs() {
		logBuffer = "";
		logBufferBaseLine = logTotalLines;
		logTrimmed = false;
		renderLogBuffer();
	}

	function filterLogs() {
		renderLogBuffer();
	}

	function disposeTerminal() {
		const session = terminalSession;
		if (!session) return;
		if (session.observer) session.observer.disconnect();
		window.removeEventListener("resize", session.resize);
		session.ws.onopen = null;
		session.ws.onmessage = null;
		session.ws.onerror = null;
		session.ws.onclose = null;
		session.ws.close();
		terminalSession = null;
		window.kuiTerminalSession = null;
		shellAttempt = false;
		setStreamStatus("shell-status", "Disconnected");
		const reconnect = document.getElementById("shell-reconnect");
		if (reconnect) reconnect.disabled = !shellBase;
	}

	function disposeStreams() {
		closeLogStream();
		disposeTerminal();
	}

	function startShell(base) {
		if (shellAttempt) return;
		shellBase = base;
		closeLogStream();
		disposeTerminal();
		const pane = document.getElementById("term-pane");
		if (!pane) return;
		shellAttempt = true;
		setStreamStatus("shell-status", "Connecting…");
		pane.innerHTML = "";
		const container = document.getElementById("shell-container")?.value || "";
		const params = new URLSearchParams();
		if (container) params.set("container", container);

		const terminalColors = getComputedStyle(document.documentElement);
		const term = new Terminal({
			fontSize: (() => { const saved = Number.parseInt(localStorage.getItem("kui.terminal-font-size") || "13", 10); return Number.isInteger(saved) ? Math.max(8, Math.min(28, saved)) : 13; })(),
			theme: {
				background: terminalColors.getPropertyValue("--surface-code").trim(),
				foreground: terminalColors.getPropertyValue("--text-code").trim(),
			},
		});
		const fit = new FitAddon.FitAddon();
		term.loadAddon(fit);
		term.open(pane);
		const resize = () => fit.fit();
		const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(resize);
		if (observer) observer.observe(pane);
		window.addEventListener("resize", resize);
		requestAnimationFrame(resize);
		let ws;
		try {
			ws = new WebSocket(wsURL(base + (params.size ? "?" + params.toString() : "")));
		} catch {
			if (observer) observer.disconnect();
			window.removeEventListener("resize", resize);
			term.dispose();
			shellAttempt = false;
			setStreamStatus("shell-status", "Connection failed — reconnect available");
			const reconnect = document.getElementById("shell-reconnect");
			if (reconnect) reconnect.disabled = false;
			return;
		}
		const session = { term, ws, observer, resize };
		terminalSession = session;
		window.kuiTerminalSession = session;
		const reconnect = document.getElementById("shell-reconnect");
		if (reconnect) reconnect.disabled = true;
		ws.onopen = () => {
			if (terminalSession !== session) return;
			setStreamStatus("shell-status", "Connected · " + (container || "default container"));
			term.focus();
		};
		ws.onmessage = (ev) => {
			if (terminalSession === session) term.write(ev.data);
		};
		ws.onerror = () => {
			if (terminalSession !== session) return;
			shellAttempt = false;
			setStreamStatus("shell-status", "Connection error — reconnect available");
			const retry = document.getElementById("shell-reconnect");
			if (retry) retry.disabled = false;
			term.write("\r\n[connection error]");
		};
		ws.onclose = () => {
			if (terminalSession === session) {
				shellAttempt = false;
				setStreamStatus("shell-status", "Disconnected — reconnect available");
				const retry = document.getElementById("shell-reconnect");
				if (retry) retry.disabled = false;
			}
			if (terminalSession === session) term.write("\r\n[connection closed]");
		};
		term.onData((data) => {
			if (ws.readyState === WebSocket.OPEN) ws.send(data);
		});
	}
	function reconnectShell() {
		if (!shellAttempt && shellBase) startShell(shellBase);
	}
	function openDebugShell() {
		const namespace = document.getElementById("namespace-select");
		if (!namespace || !namespace.value || namespace.value === "_all") {
			showToast({ level: "error", message: "Choose a namespace before starting a debug shell." });
			if (namespace) namespace.focus();
			return;
		}
		const pane = document.getElementById("detail-pane");
		if (!pane || !window.htmx) {
			showToast({ level: "error", message: "The debug shell interface is unavailable." });
			return;
		}
		const splitter = document.getElementById("detail-splitter");
		pane.classList.add("open");
		if (splitter) splitter.style.display = "block";
		const loading = document.createElement("div");
		loading.className = "loading-state";
		loading.setAttribute("role", "status");
		loading.textContent = "Starting temporary shell…";
		pane.replaceChildren(loading);
		const context = document.body.dataset.ctx;
		const path = "/c/" + encodeURIComponent(context) + "/" + encodeURIComponent(namespace.value) + "/debug-shell";
		window.htmx.ajax("POST", path, { target: "#detail-pane", swap: "innerHTML" });
	}
	function openNodeShell(nodeName) {
		const namespace = document.getElementById("namespace-select");
		if (!namespace || !namespace.value || namespace.value === "_all") {
			showToast({ level: "error", message: "Choose a namespace before starting a node shell." });
			if (namespace) namespace.focus();
			return;
		}
		const pane = document.getElementById("detail-pane");
		if (!pane || !window.htmx) {
			showToast({ level: "error", message: "The node shell interface is unavailable." });
			return;
		}
		const context = document.body.dataset.ctx;
		const path = "/c/" + encodeURIComponent(context) + "/" + encodeURIComponent(namespace.value) +
			"/nodes/" + encodeURIComponent(nodeName) + "/debug-shell";
		showConfirmModal(
			"Open a shell on node " + nodeName + " in namespace " + namespace.value +
				"? It joins host network/PID/IPC namespaces and mounts the host root read-only at /host.",
			() => {
				pane.classList.add("open");
				const splitter = document.getElementById("detail-splitter");
				if (splitter) splitter.style.display = "block";
				const loading = document.createElement("div");
				loading.className = "loading-state";
				loading.setAttribute("role", "status");
				loading.textContent = "Starting node shell…";
				pane.replaceChildren(loading);
				window.htmx.ajax("POST", path, { target: "#detail-pane", swap: "innerHTML" });
			},
		);
	}

	function copyTerminalSelection() {
		if (!terminalSession) return;
		const selection = terminalSession.term.getSelection();
		if (selection) copyText(selection);
	}

	function clearTerminal() {
		if (terminalSession) terminalSession.term.write("\x1b[2J\x1b[3J\x1b[H");
	}

	function filterRows(value) {
		const needle = (value || "").toLowerCase();
		const selectedColumn = document.getElementById("column-filter")?.value || "";
		document.querySelectorAll("#kui-table").forEach((table) => {
			const headers = Array.from(table.querySelectorAll("thead th"));
			const columnIndex = selectedColumn ? headers.findIndex((th) => th.textContent.trim() === selectedColumn) : -1;
			const rows = Array.from(table.querySelectorAll("tbody tr[data-id]"));
			let visible = 0;
			rows.forEach((tr) => {
				const content = columnIndex >= 0 && tr.cells[columnIndex] ? tr.cells[columnIndex].textContent : tr.textContent;
				const matches = content.toLowerCase().includes(needle);
				tr.style.display = matches ? "" : "none";
				if (matches) visible++;
			});
			const empty = table.querySelector("[data-filter-empty]");
			if (empty) empty.style.display = needle && rows.length > 0 && visible === 0 ? "" : "none";
		});
		const lp = findListPane(document);
		if (lp) updateBulkBar(lp);
		syncClientListStateURL();
	}

	function filterColumn() {
		filterRows(document.getElementById("row-filter")?.value || "");
	}

	function resetTable() {
		const lp = findListPane(document);
		if (!lp) return;
		const label = document.getElementById("label-selector");
		const field = document.getElementById("field-selector");
		const size = document.getElementById("page-size");
		if (label) label.value = "";
		if (field) field.value = "";
		if (size) size.value = size.dataset.defaultValue || "100";
		lp.dataset.sortKey = "";
		lp.dataset.sortDir = "asc";
		const rowFilter = document.getElementById("row-filter");
		if (rowFilter) rowFilter.value = "";
		const col = document.getElementById("column-filter");
		if (col) col.value = "";
		lp.dataset.pageHistory = "[]";
		const url = new URL(lp.dataset.listUrl || location.href, location.origin);
		if (size) url.searchParams.set("limit", size.value);
		url.searchParams.delete("frame");
		history.replaceState(history.state, "", url.pathname + url.search);
		clearSelection();
		loadResourcePage(url.toString());
	}

	function clearRowFilter() {
		const input = document.getElementById("row-filter");
		if (!input) return;
		input.value = "";
		filterRows("");
		input.focus();
	}

	function selectedRowIDs(lp) {
		return new Set(selectedNames.keys());
	}

	function syncSelectAll(lp, selected) {
		const selectAll = lp.querySelector("#select-all-rows");
		if (!selectAll) return;
		const visible = Array.from(lp.querySelectorAll(".row-select")).filter((cb) => cb.closest("tr").style.display !== "none");
		const selectedVisible = visible.filter((cb) => selected.has(cb.dataset.id)).length;
		selectAll.checked = visible.length > 0 && selectedVisible === visible.length;
		selectAll.indeterminate = selectedVisible > 0 && selectedVisible < visible.length;
		selectAll.disabled = visible.length === 0;
	}
	function persistSelectedRows() {
		const state = Object.assign({}, history.state || {});
		state.kuiSelectionScope = location.pathname;
		state.kuiSelectedRows = Array.from(selectedNames.entries());
		history.replaceState(state, "", location.pathname + location.search);
	}

	function restoreSelectedRows(state) {
		selectedNames.clear();
		const entries = state && state.kuiSelectionScope === location.pathname && Array.isArray(state.kuiSelectedRows)
			? state.kuiSelectedRows : [];
		entries.forEach((entry) => {
			if (Array.isArray(entry) && typeof entry[0] === "string" && typeof entry[1] === "string") {
				selectedNames.set(entry[0], entry[1]);
			}
		});
		const lp = findListPane(document);
		if (lp) setSelectedRows(lp, selectedRowIDs(lp));
		else persistSelectedRows();
	}

	function setSelectedRows(lp, selected) {
		lp.dataset.checkedRows = Array.from(selected).join(",");
		lp.querySelectorAll(".row-select").forEach((cb) => {
			cb.checked = selected.has(cb.dataset.id);
		});
		syncSelectAll(lp, selected);
		persistSelectedRows();
		updateBulkBar(lp);
	}

	function copySelectedNames() {
		const lp = findListPane(document);
		if (!lp) return;
		const ordered = [];
		const copied = new Set();
		lp.querySelectorAll("tbody tr[data-id]").forEach((row) => {
			if (selectedNames.has(row.dataset.id) && row.style.display !== "none") {
				ordered.push(selectedNames.get(row.dataset.id));
				copied.add(row.dataset.id);
			}
		});
		selectedNames.forEach((name, id) => {
			if (!copied.has(id)) ordered.push(name);
		});
		if (ordered.length) copyText(ordered.join("\n"));
	}

	function selectAllPage() {
		const lp = findListPane(document);
		if (!lp) return;
		const selected = selectedRowIDs(lp);
		lp.querySelectorAll(".row-select").forEach((cb) => {
			selected.add(cb.dataset.id);
			selectedNames.set(cb.dataset.id, cb.closest("tr")?.dataset.name || "");
		});
		setSelectedRows(lp, selected);
	}

	function exportCSV() {
		const table = findListPane(document)?.querySelector("#kui-table");
		const rows = Array.from(table?.querySelectorAll("tbody tr[data-id]") || [])
			.filter((row) => row.style.display !== "none");
		if (!rows.length) {
			showToast({ level: "error", message: "CSV export is empty: no visible rows." });
			return;
		}
		const headers = Array.from(table.querySelectorAll("thead th")).filter((th) => !th.classList.contains("select-col"));
		const escape = (value) => '"' + value.replace(/"/g, '""') + '"';
		const lines = [headers.map((th) => escape(th.textContent.trim())).join(",")];
		rows.forEach((row) => {
			const cells = Array.from(row.cells).filter((cell) => !cell.classList.contains("select-col"));
			lines.push(cells.map((cell) => escape(cell.textContent)).join(","));
		});
		const blob = new Blob(["\ufeff" + lines.join("\r\n")], { type: "text/csv;charset=utf-8" });
		const url = URL.createObjectURL(blob);
		const link = document.createElement("a");
		link.href = url;
		link.download = "resources.csv";
		link.hidden = true;
		document.body.appendChild(link);
		link.click();
		link.remove();
		window.setTimeout(() => URL.revokeObjectURL(url), 0);
	}

	// ---- detail pane open/close (Foundation) ----

	function clearDetailPane() {
		const dp = document.getElementById("detail-pane");
		if (!dp) return;
		hideAllSecrets();
		disposeStreams();
		logBuffer = "";
		logBufferBaseLine = logTotalLines;
		logTrimmed = false;
		logFailed = false;
		logBase = "";
		shellBase = "";
		relatedNodeHTML = "";
		relatedNodeRowID = "";
		dp.replaceChildren();
		dp.classList.remove("open");
		dp.removeAttribute("aria-busy");
		const splitter = document.getElementById("detail-splitter");
		if (splitter) splitter.style.display = "none";
		setBreadcrumbName("");
		dp.dispatchEvent(new CustomEvent("kui:detail-closed", { bubbles: true }));
		const returnFocus = detailReturnFocus;
		detailReturnFocus = null;
		const focusTarget = returnFocus?.isConnected ? returnFocus :
			returnFocus?.dataset.id && Array.from(document.querySelectorAll("#kui-table tbody tr[data-id]"))
				.find((row) => row.dataset.id === returnFocus.dataset.id);
		if (focusTarget) focusTarget.focus();
	}

	function closeDetail() {
		if (hasUnsavedYAMLChanges()) {
			showConfirmModal("Discard unsaved YAML changes?", clearDetailPane);
			return;
		}
		clearDetailPane();
	}

	function updateYAMLDirtyState(editor) {
		const original = editor.form && editor.form.querySelector('input[name="original"]');
		const dirty = !!original && editor.value !== original.value;
		const panel = editor.closest("#tab-yaml") || document;
		const status = panel.querySelector("#yaml-dirty-status");
		const review = editor.form && editor.form.querySelector('button[type="submit"]');
		if (status) {
			status.textContent = dirty ? "Unsaved changes" : "Saved";
			status.classList.toggle("yaml-dirty", dirty);
		}
		if (review) review.disabled = !dirty;
	}

	function markYAMLEditorSaved() {
		const editor = document.getElementById("yaml-editor");
		if (!editor || !editor.form) return;
		const original = editor.form.querySelector('input[name="original"]');
		if (original) original.value = editor.value;
		updateYAMLDirtyState(editor);
	}

	function syncYAMLDirtyState() {
		const editor = document.getElementById("yaml-editor");
		if (editor) updateYAMLDirtyState(editor);
	}

	function hasUnsavedYAMLChanges() {
		const editor = document.getElementById("yaml-editor");
		const original = editor && editor.form && editor.form.querySelector('input[name="original"]');
		return !!original && editor.value !== original.value;
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

	function copyTextFallback(value) {
		const field = document.createElement("textarea");
		const previousFocus = document.activeElement;
		field.value = value;
		field.readOnly = true;
		field.tabIndex = -1;
		field.style.position = "fixed";
		field.style.opacity = "0";
		field.style.left = "-1000px";
		document.body.appendChild(field);
		field.select();
		let copied = false;
		try {
			copied = document.execCommand("copy");
		} finally {
			field.remove();
			if (previousFocus && typeof previousFocus.focus === "function") previousFocus.focus();
		}
		return copied;
	}

	async function copyText(value) {
		let copied = false;
		if (navigator.clipboard && typeof navigator.clipboard.writeText === "function") {
			try {
				await navigator.clipboard.writeText(value);
				copied = true;
			} catch {
				// Fall back for browsers that deny the async Clipboard API.
			}
		}
		if (!copied) {
			try {
				copied = copyTextFallback(value);
			} catch {
				copied = false;
			}
		}
		showToast(copied
			? { level: "success", message: "Copied to clipboard" }
			: { level: "error", message: "Could not copy to clipboard" });
	}

	function downloadYAML(filename) {
		const editor = document.getElementById("yaml-editor");
		if (!editor) return;
		const blob = new Blob([editor.value], { type: "application/yaml;charset=utf-8" });
		const url = URL.createObjectURL(blob);
		const link = document.createElement("a");
		link.href = url;
		link.download = filename && !/[\\/]/.test(filename) ? filename : "resource.yaml";
		link.hidden = true;
		document.body.appendChild(link);
		link.click();
		link.remove();
		window.setTimeout(() => URL.revokeObjectURL(url), 0);
	}

	async function downloadSelectedYAMLBundle() {
		const button = document.getElementById("bulk-yaml-export");
		const paths = Array.from(selectedNames.keys()).sort();
		if (!paths.length) return;
		const originalText = button?.textContent || "Download selected YAML";
		if (button) {
			button.disabled = true;
			button.setAttribute("aria-busy", "true");
			button.textContent = "Preparing YAML…";
		}
		try {
			const csrfToken = document.querySelector('meta[name="kui-csrf-token"]')?.content;
			if (!csrfToken) throw new Error("CSRF token is unavailable.");
			const context = document.body?.dataset.ctx;
			if (!context) throw new Error("The active context is unavailable.");
			const response = await fetch("/c/" + encodeURIComponent(context) + "/bulk-yaml", {
				method: "POST",
				headers: { "Content-Type": "application/json", "X-Kui-CSRF": csrfToken },
				body: JSON.stringify({ paths }),
			});
			if (!response.ok) throw new Error((await response.text()).trim() || `HTTP ${response.status}`);
			const blob = await response.blob();
			if (!blob.size) throw new Error("The server returned an empty YAML bundle.");
			const url = URL.createObjectURL(blob);
			const link = document.createElement("a");
			link.href = url;
			link.download = "kui-selected-resources.yaml";
			link.hidden = true;
			document.body.appendChild(link);
			link.click();
			link.remove();
			window.setTimeout(() => URL.revokeObjectURL(url), 1000);
			showToast({ level: "success", message: `Downloaded ${paths.length} selected resource${paths.length === 1 ? "" : "s"} as YAML.` });
		} catch (error) {
			showToast({ level: "error", message: `YAML export failed: ${error.message}` });
		} finally {
			if (button) {
				button.disabled = false;
				button.removeAttribute("aria-busy");
				button.textContent = originalText;
			}
		}
	}

	// ---- breadcrumb (Phase 4) ----

	function setBreadcrumbName(name) {
		const el = document.getElementById("breadcrumb-name");
		if (!el) return;
		el.textContent = name ? " › " + name : "";
		el.title = name || "";
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
		document.querySelectorAll(".sidebar-link.active").forEach((link) => {
			link.classList.remove("active");
			link.removeAttribute("aria-current");
		});
		const link = document.querySelector('.sidebar-link[data-resource="' + cssEscape(resource) + '"]');
		if (link) {
			link.classList.add("active");
			link.setAttribute("aria-current", "page");
		}
		const resourceEl = document.getElementById("breadcrumb-resource");
		if (resourceEl) {
			resourceEl.textContent = label || resource;
			resourceEl.title = label || resource;
		}
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
		const parts = new URL(path, location.origin).pathname.split("/");
		// ["", "c", ctxId, ns, resource]
		if (parts[1] !== "c" || parts.length < 5) return;
		const resource = parts[4];
		const link = document.querySelector('.sidebar-link[data-resource="' + cssEscape(resource) + '"]');
		applyActiveResource(resource, link ? link.dataset.label : resource);
	}

	function filterNamespaceOptions(value) {
		const select = document.getElementById("namespace-select");
		if (!select) return;
		const needle = (value || "").trim().toLowerCase();
		Array.from(select.options).forEach((option) => {
			option.hidden = option.value !== "_all" && !option.selected &&
				!option.textContent.toLowerCase().includes(needle);
		});
	}
	function changeNamespace(ns) {
		const ctx = document.body.dataset.ctx;
		const activeLink = document.querySelector(".sidebar-link.active");
		const resource = activeLink ? activeLink.dataset.resource : "overview";
		syncClientListStateURL();
		const current = new URL(location.href);
		const target = new URL("/c/" + ctx + "/" + ns + "/" + resource, location.origin);
		["labelSelector", "fieldSelector", "limit", "rowFilter", "sortColumn", "sortDir", "columnFilter"].forEach((key) => {
			if (current.searchParams.has(key)) target.searchParams.set(key, current.searchParams.get(key));
		});
		location.href = target.pathname + target.search;
	}

	function syncClientListStateURL() {
		const url = new URL(location.href);
		const filter = document.getElementById("row-filter")?.value || "";
		const column = document.getElementById("column-filter")?.value || "";
		if (filter) url.searchParams.set("rowFilter", filter);
		else url.searchParams.delete("rowFilter");
		if (column) url.searchParams.set("columnFilter", column);
		else url.searchParams.delete("columnFilter");
		const lp = findListPane(document);
		if (lp?.dataset.sortKey) {
			url.searchParams.set("sortColumn", lp.dataset.sortKey);
			url.searchParams.set("sortDir", lp.dataset.sortDir || "asc");
		} else {
			url.searchParams.delete("sortColumn");
			url.searchParams.delete("sortDir");
		}
		history.replaceState(history.state, "", url.pathname + url.search);
	}

	function restoreClientListState() {
		const params = new URLSearchParams(location.search);
		const filterInput = document.getElementById("row-filter");
		const colInput = document.getElementById("column-filter");
		// A swap triggered right after a keystroke must not revert the filter
		// the user (or a test) is actively editing; the input event will sync
		// the URL via table_workbench's syncExplorerURL.
		const editingFilter = filterInput && (document.activeElement === filterInput || document.activeElement === colInput);
		if (filterInput && !editingFilter) filterInput.value = params.get("rowFilter") || "";
		if (colInput && !editingFilter) colInput.value = params.get("columnFilter") || "";
		const lp = findListPane(document);
		if (!lp) return;
		lp.dataset.sortKey = params.get("sortColumn") || "";
		lp.dataset.sortDir = params.get("sortDir") === "desc" ? "desc" : "asc";
		const labels = document.getElementById("label-selector");
		const fields = document.getElementById("field-selector");
		const size = document.getElementById("page-size");
		if (labels) labels.value = params.get("labelSelector") || labels.value;
		if (fields) fields.value = params.get("fieldSelector") || fields.value;
		if (size && params.has("limit")) size.value = params.get("limit");
	}

	function applyResourceSelectors() {
		const lp = findListPane(document);
		const labelInput = document.getElementById("label-selector");
		const fieldInput = document.getElementById("field-selector");
		if (!lp || !labelInput || !fieldInput || !window.htmx) return;
		const listURL = new URL(lp.dataset.listUrl || location.href, location.origin);
		copyClientListState(new URL(location.href), listURL);
		listURL.searchParams.delete("frame");
		listURL.searchParams.delete("labelSelector");
		listURL.searchParams.delete("fieldSelector");
		listURL.searchParams.delete("continue");
		if (labelInput.value.trim()) listURL.searchParams.set("labelSelector", labelInput.value.trim());
		if (fieldInput.value.trim()) listURL.searchParams.set("fieldSelector", fieldInput.value.trim());
		lp.dataset.pageHistory = "[]";
		const pageURL = new URL(listURL);
		listURL.searchParams.set("frame", "table");
		lp.dataset.listUrl = listURL.toString();
		lp.setAttribute("hx-get", listURL.toString());
		history.replaceState(history.state, "", pageURL.pathname + pageURL.search);
		window.htmx.ajax("GET", listURL.toString(), { target: lp, swap: "innerHTML" });
	}

	function loadResourcePage(rawURL) {
		const lp = findListPane(document);
		if (!lp || !window.htmx) return;
		const listURL = new URL(rawURL, location.origin);
		copyClientListState(new URL(location.href), listURL);
		const pageURL = new URL(listURL);
		pageURL.searchParams.delete("frame");
		lp.dataset.listUrl = listURL.toString();
		lp.setAttribute("hx-get", listURL.toString());
		history.replaceState(history.state, "", pageURL.pathname + pageURL.search);
		window.htmx.ajax("GET", listURL.toString(), { target: lp, swap: "innerHTML" });
	}

	function readPageHistory(lp) {
		try {
			return JSON.parse(lp.dataset.pageHistory || "[]");
		} catch {
			return [];
		}
	}

	function nextResourcePage(rawURL) {
		const lp = findListPane(document);
		if (!lp) return;
		const current = new URL(lp.dataset.listUrl || location.href, location.origin);
		copyClientListState(new URL(location.href), current);
		current.searchParams.delete("frame");
		const pages = readPageHistory(lp);
		pages.push(current.pathname + current.search);
		lp.dataset.pageHistory = JSON.stringify(pages);
		loadResourcePage(rawURL);
	}

	function previousResourcePage() {
		const lp = findListPane(document);
		if (!lp) return;
		const pages = readPageHistory(lp);
		const previous = pages.pop();
		if (!previous) return;
		lp.dataset.pageHistory = JSON.stringify(pages);
		loadResourcePage(previous);
	}

	function changeResourcePageSize(value) {
		const lp = findListPane(document);
		const limit = Number.parseInt(value, 10);
		if (!lp || !Number.isInteger(limit) || limit < 1 || limit > 1000) return;
		const listURL = new URL(lp.dataset.listUrl || location.href, location.origin);
		listURL.searchParams.delete("frame");
		listURL.searchParams.delete("continue");
		listURL.searchParams.set("limit", String(limit));
		lp.dataset.pageHistory = "[]";
		const pageURL = new URL(listURL);
		history.replaceState(history.state, "", pageURL.pathname + pageURL.search);
		loadResourcePage(listURL.toString());
	}

	function syncPagination(lp) {
		const previous = lp.querySelector("#previous-page");
		if (previous) previous.disabled = readPageHistory(lp).length === 0;
	}

	function copyClientListState(source, target) {
		["rowFilter", "sortColumn", "sortDir", "columnFilter"].forEach((key) => {
			if (source.searchParams.has(key)) target.searchParams.set(key, source.searchParams.get(key));
			else target.searchParams.delete(key);
		});
	}

	function setRowDensity(value) {
		if (!["default", "compact", "comfortable"].includes(value)) return;
		document.body.dataset.rowDensity = value;
		try {
			localStorage.setItem("kui-row-density", value);
		} catch {
			// Keep the current-session choice even when storage is unavailable.
		}
		syncRowDensityControl();
	}

	function restoreRowDensity() {
		let density = "default";
		try {
			const saved = localStorage.getItem("kui-row-density");
			if (["default", "compact", "comfortable"].includes(saved)) density = saved;
		} catch {
			// The default density remains usable without persistent storage.
		}
		document.body.dataset.rowDensity = density;
		syncRowDensityControl();
	}

	function syncRowDensityControl() {
		const control = document.getElementById("row-density");
		if (control) control.value = document.body.dataset.rowDensity || "default";
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
		if (table) {
			const headers = Array.from(table.querySelectorAll("thead th[aria-sort]"));
			let sortedHeader = null;
			if (lp.dataset.sortKey) {
				const index = headers.findIndex((th) => th.textContent.trim() === lp.dataset.sortKey);
				if (index >= 0) {
					sortedHeader = headers[index];
					lp.dataset.sortCol = String(Array.from(sortedHeader.parentElement.children).indexOf(sortedHeader));
				} else {
					delete lp.dataset.sortCol;
					delete lp.dataset.sortKey;
					syncClientListStateURL();
				}
			}
			headers.forEach((th) => {
				th.classList.remove("sort-asc", "sort-desc");
				th.setAttribute("aria-sort", "none");
			});
			if (sortedHeader) {
				const descending = lp.dataset.sortDir === "desc";
				sortedHeader.classList.add(descending ? "sort-desc" : "sort-asc");
				sortedHeader.setAttribute("aria-sort", descending ? "descending" : "ascending");
			}
		}
		const tbody = table && table.querySelector("tbody");
		if (tbody) {
			const sortCol = lp.dataset.sortCol;
			if (sortCol !== undefined && sortCol !== "") {
				const idx = parseInt(sortCol, 10);
				const dir = lp.dataset.sortDir === "desc" ? -1 : 1;
				const rows = Array.from(tbody.querySelectorAll("tr[data-id]")).filter((tr) => tr.cells.length > idx);
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
				tbody.querySelectorAll("tr.selected").forEach((tr) => {
					tr.classList.remove("selected");
					tr.setAttribute("aria-selected", "false");
				});
				const match = tbody.querySelector('tr[data-id="' + cssEscape(lp.dataset.selectedRow) + '"]');
				if (match) {
					match.classList.add("selected");
					match.setAttribute("aria-selected", "true");
				}
			}

			const checked = selectedRowIDs(lp);
			tbody.querySelectorAll(".row-select").forEach((cb) => {
				cb.checked = checked.has(cb.dataset.id);
			});
			syncSelectAll(lp, checked);
			updateBulkBar(lp);
		}

		const filterInput = document.getElementById("row-filter");
		if (filterInput && filterInput.value) filterRows(filterInput.value);
		syncPagination(lp);
		syncRowDensityControl();
	}

	function cssEscape(s) {
		return window.CSS && CSS.escape ? CSS.escape(s) : s.replace(/[^a-zA-Z0-9_-]/g, "\\$&");
	}

	function sortTableColumn(th) {
		if (!th.textContent.trim()) return;
		const lp = th.closest(".list-pane");
		if (!lp) return;
		const row = th.parentElement;
		const idx = Array.from(row.children).indexOf(th);
		const current = lp.dataset.sortCol === String(idx);
		lp.dataset.sortCol = String(idx);
		lp.dataset.sortKey = th.textContent.trim();
		lp.dataset.sortDir = current && lp.dataset.sortDir === "asc" ? "desc" : "asc";
		syncClientListStateURL();
		applySortAndSelection(lp);
	}

	function onTableKeydown(e) {
		const tab = e.target.closest(".tab-row [role='tab']");
		if (tab && ["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) {
			const tabs = Array.from(tab.parentElement.querySelectorAll("[role='tab']"));
			const current = tabs.indexOf(tab);
			const nextIndex = e.key === "Home" ? 0 : e.key === "End" ? tabs.length - 1 :
				(current + (e.key === "ArrowRight" ? 1 : tabs.length - 1)) % tabs.length;
			const next = tabs[nextIndex];
			showTab(next, next.getAttribute("aria-controls"));
			next.focus();
			e.preventDefault();
			return;
		}
		if (e.key === "Enter") {
			const row = e.target.closest("#kui-table tbody tr[data-id]");
			if (row && !e.target.closest("input,button,a,select,textarea")) {
				e.preventDefault();
				selectTableRow(row);
				const request = {
					source: row,
					target: row.getAttribute("hx-target"),
					swap: row.getAttribute("hx-swap") || "innerHTML",
				};
				const pushURL = row.getAttribute("hx-push-url");
				if (pushURL) request.push = pushURL;
				window.htmx.ajax("GET", row.getAttribute("hx-get"), request);
				return;
			}
		}
		if (e.key !== "Enter" && e.key !== " ") return;
		const th = e.target.closest("#kui-table thead th[tabindex='0']");
		if (!th) return;
		e.preventDefault();
		sortTableColumn(th);
	}

	function selectTableRow(row) {
		detailReturnFocus = row;
		const lp = row.closest(".list-pane");
		if (lp) {
			lp.querySelectorAll("tr.selected").forEach((selected) => {
				selected.classList.remove("selected");
				selected.setAttribute("aria-selected", "false");
			});
			row.classList.add("selected");
			row.setAttribute("aria-selected", "true");
			lp.dataset.selectedRow = row.dataset.id;
		}
		if (row.dataset.name) setBreadcrumbName(row.dataset.name);
	}

	function onTableClick(e) {
		const th = e.target.closest("#kui-table thead th");
		if (th) {
			sortTableColumn(th);
			return;
		}
		const row = e.target.closest("#kui-table tbody tr[data-id]");
		if (row) selectTableRow(row);
	}

	// ---- bulk select / delete (Phase 7) ----

	function updateBulkBar(lp) {
		const bar = document.getElementById("bulk-bar");
		if (!bar) return;
		const checked = Array.from(selectedNames.keys());
		const visible = Array.from(lp.querySelectorAll(".row-select")).filter((cb) => cb.closest("tr").style.display !== "none");
		const visibleSelected = visible.filter((cb) => selectedNames.has(cb.dataset.id)).length;
		if (checked.length) {
			bar.style.display = "flex";
			const count = document.getElementById("bulk-count");
			if (count) count.textContent = checked.length + " selected across pages · " + visibleSelected + " of " + visible.length + " visible selected";
			if (!document.getElementById("batch-metadata-button")) {
				const button = document.createElement("button");
				button.type = "button";
				button.className = "btn";
				button.id = "batch-metadata-button";
				button.textContent = "Edit labels / annotations";
				button.addEventListener("click", showBatchMetadataForm);
				bar.appendChild(button);
			}
			if (!document.getElementById("batch-metadata-form")) createBatchMetadataForm(bar);
			if (batchFailures.length && !document.getElementById("batch-metadata-retry")) {
				const retry = document.createElement("button");
				retry.type = "button";
				retry.id = "batch-metadata-retry";
				retry.className = "btn";
				retry.textContent = "Retry failed metadata updates";
				retry.addEventListener("click", retryBatchMetadata);
				bar.appendChild(retry);
			}
		} else bar.style.display = "none";
	}

	function onTableChange(e) {
		const cb = e.target.closest(".row-select");
		if (cb) {
			const lp = cb.closest(".list-pane");
			if (!lp) return;
			if (cb.checked) selectedNames.set(cb.dataset.id, cb.closest("tr")?.dataset.name || "");
			else selectedNames.delete(cb.dataset.id);
			setSelectedRows(lp, selectedRowIDs(lp));
			return;
		}
		const selectAll = e.target.closest("#select-all-rows");
		if (selectAll) {
			const lp = selectAll.closest(".list-pane");
			if (!lp) return;
			const selected = selectedRowIDs(lp);
			const visible = Array.from(lp.querySelectorAll(".row-select")).filter((c) => c.closest("tr").style.display !== "none");
			visible.forEach((cb) => {
				if (selectAll.checked) {
					selected.add(cb.dataset.id);
					selectedNames.set(cb.dataset.id, cb.closest("tr")?.dataset.name || "");
				} else {
					selected.delete(cb.dataset.id);
					selectedNames.delete(cb.dataset.id);
				}
			});
			setSelectedRows(lp, selected);
			return;
		}
		if (e.target.id === "log-tail-preset") {
			const tail = document.getElementById("log-tail-lines");
			if (tail && e.target.value) tail.value = e.target.value;
		}
		if (e.target.id === "column-filter") filterColumn();
		if (e.target.id === "log-tail-lines") document.getElementById("log-tail-preset").value = "";
		if (e.target.id === "log-container" || e.target.id === "log-tail-lines" || e.target.id === "log-tail-preset" ||
			["logs-previous", "logs-timestamps", "logs-since", "logs-wrap"].includes(e.target.id)) {
			persistLogSettings();
			if (e.target.id === "logs-wrap") renderLogBuffer();
		}
	}

	function clearSelection() {
		selectedNames.clear();
		const lp = findListPane(document);
		if (lp) setSelectedRows(lp, new Set());
		else persistSelectedRows();
	}

	function bulkDelete() {
		const lp = findListPane(document);
		if (!lp) return;
		const ids = Array.from(selectedNames.keys());
		if (!ids.length) return;
		if (!confirm("Delete " + ids.length + " selected resources?")) return;
		htmx.ajax("POST", lp.dataset.bulkDeleteUrl, { values: { path: ids } });
		clearSelection();
	}

	let batchPending = null;
	let batchPreviewFailures = [];
	function createBatchMetadataForm(bar) {
		const form = document.createElement("form");
		form.id = "batch-metadata-form";
		form.hidden = true;
		const select = (id, labelText, options) => {
			const label = document.createElement("label");
			label.textContent = labelText + " ";
			const el = document.createElement("select");
			el.id = id;
			options.forEach(([value, text]) => { const option = document.createElement("option"); option.value = value; option.textContent = text; el.appendChild(option); });
			label.appendChild(el); form.appendChild(label);
		};
		const text = (id, labelText) => {
			const label = document.createElement("label"); label.textContent = labelText + " ";
			const el = id === "batch-metadata-value" ? document.createElement("textarea") : document.createElement("input");
			el.id = id; if (el.tagName === "INPUT") el.type = "text"; label.appendChild(el); form.appendChild(label);
		};
		select("batch-metadata-kind", "Metadata", [["labels", "Label"], ["annotations", "Annotation"]]);
		select("batch-metadata-action", "Action", [["add", "Add"], ["update", "Update"], ["remove", "Remove"]]);
		text("batch-metadata-key", "Key"); text("batch-metadata-value", "Value");
		const preview = document.createElement("button"); preview.type = "submit"; preview.className = "btn"; preview.textContent = "Preview exact patches"; form.appendChild(preview);
		const submit = document.createElement("button"); submit.type = "button"; submit.id = "batch-metadata-submit"; submit.className = "btn btn-primary"; submit.textContent = "Confirm and apply"; submit.hidden = true; submit.addEventListener("click", submitBatchMetadata); form.appendChild(submit);
		const output = document.createElement("pre"); output.id = "batch-metadata-results"; output.setAttribute("aria-live", "polite"); form.appendChild(output);
		form.addEventListener("submit", previewBatchMetadata);
		bar.appendChild(form);
	}

	function showBatchMetadataForm() {
		const form = document.getElementById("batch-metadata-form");
		if (!form) return;
		form.hidden = !form.hidden;
		if (!form.hidden) document.getElementById("batch-metadata-key")?.focus();
	}

	function batchMetadataURL() {
		const context = location.pathname.split("/")[2];
		return "/c/" + encodeURIComponent(context || "") + "/bulk-metadata";
	}

	function batchMetadataPayload(paths, preview) {
		return {paths, kind: document.getElementById("batch-metadata-kind").value,
			action: document.getElementById("batch-metadata-action").value,
			key: document.getElementById("batch-metadata-key").value,
			value: document.getElementById("batch-metadata-value").value, preview};
	}

	async function sendBatchMetadata(payload) {
		const token = document.querySelector('meta[name="kui-csrf-token"]')?.content || "";
		const response = await fetch(batchMetadataURL(), {method: "POST", credentials: "same-origin",
			headers: {"Content-Type": "application/json", "X-Kui-CSRF": token}, body: JSON.stringify(payload)});
		const data = await response.json().catch(() => ({}));
		if (!response.ok) throw new Error(data.error || "Metadata request failed (" + response.status + ")");
		return data;
	}

	async function previewBatchMetadata(event) {
		event.preventDefault();
		const paths = Array.from(selectedNames.keys());
		if (!paths.length) return;
		const payload = batchMetadataPayload(paths, true);
		const output = document.getElementById("batch-metadata-results");
		const submit = document.getElementById("batch-metadata-submit");
		if (output) output.textContent = "Preparing exact per-resource patches…";
		try {
			const result = await sendBatchMetadata(payload);
			const outcomes = result.outcomes || [];
			const ready = outcomes.filter((item) => item.success);
			batchPreviewFailures = outcomes.filter((item) => !item.success);
			batchPending = {...payload, paths: ready.map((item) => item.path), preview: false};
			if (output) output.textContent = outcomes.map((item) =>
				(item.success ? "READY " : "FAILED ") + item.name + " (" + item.path + "): " +
				(item.patch ? JSON.stringify(item.patch) : item.error)).join("\n");
			if (submit) submit.hidden = !ready.length;
		} catch (error) {
			if (output) output.textContent = error.message;
			if (submit) submit.hidden = true;
		}
	}

	async function submitBatchMetadata() {
		if (!batchPending) return;
		const names = batchPending.paths.map((path) => selectedNames.get(path) || path);
		if (!confirm("Apply " + batchPending.action + " " + batchPending.kind + " " + batchPending.key +
			" to " + names.length + " selected resources?\n\n" + names.join("\n"))) return;
		try {
			const result = await sendBatchMetadata(batchPending);
			const outcomes = [...batchPreviewFailures, ...(result.outcomes || [])];
			batchFailures = (result.outcomes || []).filter((item) => !item.success).map((item) => item.path);
			renderBatchMetadataOutcomes(outcomes);
			const listPane = document.querySelector(".list-pane");
			if (listPane) updateBulkBar(listPane);
			const retry = document.getElementById("batch-metadata-retry");
			if (retry) retry.hidden = !batchFailures.length;
		} catch (error) {
			const output = document.getElementById("batch-metadata-results");
			if (output) output.textContent = "Request failed; no full success reported. " + error.message;
		}
	}

	function renderBatchMetadataOutcomes(outcomes) {
		const output = document.getElementById("batch-metadata-results");
		if (output) output.textContent = outcomes.map((item) =>
			(item.success ? "SUCCESS " : "FAILED ") + item.name + ": " + (item.error || "updated")).join("\n");
	}

	async function retryBatchMetadata() {
		if (!batchPending || !batchFailures.length) return;
		const retryPaths = batchFailures.slice();
		try {
			const result = await sendBatchMetadata({...batchPending, paths: retryPaths, preview: false});
			batchFailures = (result.outcomes || []).filter((item) => !item.success).map((item) => item.path);
			renderBatchMetadataOutcomes(result.outcomes || []);
			const button = document.getElementById("batch-metadata-retry");
			if (button) button.hidden = !batchFailures.length;
		} catch (error) {
			const output = document.getElementById("batch-metadata-results");
			if (output) output.textContent = "Retry failed; prior successes were not replayed. " + error.message;
		}
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

	let palettePreviousFocus = null;
	const recentPaletteKey = "kui.recentResources";

	function isPaletteOpen() {
		const overlay = document.getElementById("palette-overlay");
		return overlay && overlay.style.display !== "none";
	}
	function isConfirmOpen() {
		const overlay = document.getElementById("confirm-overlay");
		return overlay && overlay.style.display !== "none";
	}

	function setBackgroundInert(inert) {
		[".app", "#statusbar"].forEach((selector) => {
			const element = document.querySelector(selector);
			if (element) element.inert = inert;
		});
	}

	let confirmPreviousFocus = null;

	function syncPaletteOptions() {
		const items = Array.from(document.querySelectorAll("#palette-results .palette-result"));
		const input = document.getElementById("palette-input");
		if (items.length && !items.some((item) => item.classList.contains("active"))) items[0].classList.add("active");
		let active = null;
		items.forEach((item, index) => {
			item.id = "palette-option-" + index;
			item.setAttribute("role", "option");
			item.setAttribute("aria-selected", String(item.classList.contains("active")));
			item.tabIndex = -1;
			if (item.classList.contains("active")) active = item;
		});
		if (input) {
			if (active) input.setAttribute("aria-activedescendant", active.id);
			else input.removeAttribute("aria-activedescendant");
		}
	}

	function recentPaletteItems() {
		try {
			const items = JSON.parse(localStorage.getItem(recentPaletteKey) || "[]");
			return Array.isArray(items) ? items.filter((item) =>
				item && typeof item.url === "string" && item.url.startsWith("/c/") &&
				typeof item.label === "string" && item.label.length <= 200
			).slice(0, 8) : [];
		} catch {
			return [];
		}
	}

	function recordRecent(url, label, fullNav) {
		if (typeof url !== "string" || !url.startsWith("/c/") ||
			typeof label !== "string" || label.length > 200) return;
		const items = recentPaletteItems().filter((item) => item.url !== url);
		items.unshift({ url, label, fullNav: Boolean(fullNav) });
		try {
			localStorage.setItem(recentPaletteKey, JSON.stringify(items.slice(0, 8)));
		} catch {
			// Recent navigation is optional when storage is unavailable.
		}
	}

	function renderRecentPalette() {
		const results = document.getElementById("palette-results");
		if (!results) return;
		const recent = recentPaletteItems();
		results.replaceChildren();
		if (!recent.length) {
			results.innerHTML = '<div class="empty-state" role="status">Type to search…</div>';
			return;
		}
		const group = document.createElement("div");
		group.className = "palette-group";
		group.setAttribute("role", "group");
		group.setAttribute("aria-label", "Recent");
		const title = document.createElement("div");
		title.className = "section-title";
		title.textContent = "Recent";
		group.appendChild(title);
		recent.forEach((item, index) => {
			const entry = document.createElement("div");
			entry.className = "palette-result" + (index === 0 ? " active" : "");
			entry.textContent = item.label;
			entry.dataset.recentUrl = item.url;
			entry.dataset.recentLabel = item.label;
			entry.setAttribute("role", "option");
			entry.id = "palette-option-" + index;
			entry.setAttribute("aria-selected", String(index === 0));
			entry.tabIndex = -1;
			if (item.fullNav) {
				entry.addEventListener("click", () => {
					recordRecent(item.url, item.label, true);
					closePalette();
					location.href = item.url;
				});
			} else {
				entry.setAttribute("hx-get", item.url);
				entry.setAttribute("hx-target", "#content");
				entry.setAttribute("hx-swap", "innerHTML");
				entry.setAttribute("hx-push-url", item.url);
				entry.addEventListener("click", () => {
					recordRecent(item.url, item.label, false);
					closePalette();
				});
			}
			group.appendChild(entry);
		});
		results.appendChild(group);
		if (window.htmx) htmx.process(results);
		syncPaletteOptions();
	}

	function openPalette() {
		const overlay = document.getElementById("palette-overlay");
		const input = document.getElementById("palette-input");
		if (!overlay || !input) return;
		if (isPaletteOpen()) {
			input.focus();
			return;
		}
		palettePreviousFocus = document.activeElement;
		setBackgroundInert(true);
		overlay.style.display = "flex";
		overlay.setAttribute("aria-hidden", "false");
		input.setAttribute("aria-expanded", "true");
		input.value = "";
		renderRecentPalette();
		input.focus();
	}

	function closePalette() {
		const overlay = document.getElementById("palette-overlay");
		const input = document.getElementById("palette-input");
		if (overlay) {
			overlay.style.display = "none";
			overlay.setAttribute("aria-hidden", "true");
		}
		if (input) {
			input.setAttribute("aria-expanded", "false");
			input.removeAttribute("aria-activedescendant");
		}
		setBackgroundInert(false);
		if (palettePreviousFocus && palettePreviousFocus.isConnected) palettePreviousFocus.focus();
		palettePreviousFocus = null;
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
		syncPaletteOptions();
		items[idx].scrollIntoView({ block: "nearest" });
	}

	// Replaces native hx-confirm's unstyled OS dialog.
	function showConfirmModal(message, onConfirm) {
		const overlay = document.getElementById("confirm-overlay");
		const dialog = overlay && overlay.querySelector(".confirm-modal");
		const messageEl = document.getElementById("confirm-message");
		const okBtn = document.getElementById("confirm-ok");
		const cancelBtn = document.getElementById("confirm-cancel");
		if (!overlay || !dialog || !messageEl || !okBtn || !cancelBtn) {
			onConfirm();
			return;
		}
		confirmPreviousFocus = document.activeElement;
		messageEl.textContent = message;
		overlay.style.display = "flex";
		overlay.setAttribute("aria-hidden", "false");
		setBackgroundInert(true);
		cancelBtn.focus();

		const cleanup = () => {
			overlay.style.display = "none";
			overlay.setAttribute("aria-hidden", "true");
			setBackgroundInert(false);
			okBtn.removeEventListener("click", onOk);
			cancelBtn.removeEventListener("click", onCancel);
			if (confirmPreviousFocus && confirmPreviousFocus.isConnected) confirmPreviousFocus.focus();
			confirmPreviousFocus = null;
		};
		const onOk = () => { cleanup(); onConfirm(); };
		const onCancel = () => cleanup();
		okBtn.addEventListener("click", onOk);
		cancelBtn.addEventListener("click", onCancel);
	}

	// ---- resizable detail pane (Phase 2) ----

	function initSplitter() {
		const splitter = document.getElementById("detail-splitter");
		const pane = document.getElementById("detail-pane");
		if (!splitter || !pane) {
			activeSplitter = null;
			activeDetailPane = null;
			return;
		}
		if (activeSplitter === splitter && activeDetailPane === pane) return;
		activeSplitter = splitter;
		activeDetailPane = pane;

		const minWidth = 280;
		const defaultWidth = 420;
		const currentWidth = () =>
			parseInt(getComputedStyle(document.documentElement).getPropertyValue("--detail-pane-w"), 10) || defaultWidth;
		const maxWidth = () => {
			if (innerWidth <= 720) return Math.max(minWidth, Math.min(900, innerWidth * 0.9));
			const sidebar = document.querySelector(".sidebar");
			const sidebarWidth = sidebar ? sidebar.getBoundingClientRect().width : 0;
			return Math.max(minWidth, Math.min(900, innerWidth - sidebarWidth - minWidth - (activeSplitter?.offsetWidth || 12)));
		};
		const applyWidth = (width, save) => {
			const maximum = maxWidth();
			const next = Math.max(minWidth, Math.min(maximum, width));
			document.documentElement.style.setProperty("--detail-pane-w", next + "px");
			if (activeSplitter) {
				activeSplitter.setAttribute("aria-valuemin", String(minWidth));
				activeSplitter.setAttribute("aria-valuemax", String(maximum));
				activeSplitter.setAttribute("aria-valuenow", String(next));
			}
			if (save) {
				try {
					localStorage.setItem("kui.detailPaneWidth", String(next));
				} catch {
					// Resizing remains usable when persistent storage is unavailable.
				}
			}
			return next;
		};
		const isDividerTarget = (event) => {
			if (!activeSplitter || !activeDetailPane) return false;
			if (activeSplitter.contains(event.target)) return true;
			if (!activeDetailPane.classList.contains("open")) return false;
			const rect = activeDetailPane.getBoundingClientRect();
			const hitWidth = Math.max(18, activeSplitter.getBoundingClientRect().width);
			return event.clientX >= rect.left - hitWidth && event.clientX <= rect.left + hitWidth &&
				event.clientY >= rect.top && event.clientY <= rect.bottom;
		};
		const beginDrag = (event, type) => {
			if (event.button !== 0 || !isDividerTarget(event) || splitterDragType) return;
			splitterDragType = type;
			splitterPointerId = type === "pointer" ? event.pointerId : null;
			splitterStartX = event.clientX;
			splitterStartWidth = currentWidth();
			document.documentElement.classList.add("kui-resizing");
			if (type === "pointer") {
				try {
					activeSplitter.setPointerCapture(event.pointerId);
				} catch {
					// Document listeners keep the drag active if capture is unavailable.
				}
			}
			event.preventDefault();
			event.stopPropagation();
		};
		const moveDrag = (clientX) => applyWidth(splitterStartWidth + splitterStartX - clientX, false);
		const endDrag = (type, event) => {
			if (splitterDragType !== type || (type === "pointer" && event.pointerId !== splitterPointerId)) return;
			splitterDragType = null;
			splitterPointerId = null;
			document.documentElement.classList.remove("kui-resizing");
			applyWidth(currentWidth(), true);
		};

		if (!splitterWidthLoaded) {
			let savedWidth = defaultWidth;
			try {
				const stored = Number.parseInt(localStorage.getItem("kui.detailPaneWidth"), 10);
				if (Number.isFinite(stored)) savedWidth = stored;
			} catch {
				// The default width remains usable when persistent storage is unavailable.
			}
			splitterWidthLoaded = true;
			applyWidth(savedWidth, false);
		} else {
			applyWidth(currentWidth(), false);
		}

		if (!splitterListenersInstalled) {
			document.addEventListener("pointerdown", (event) => beginDrag(event, "pointer"), true);
			document.addEventListener("mousedown", (event) => beginDrag(event, "mouse"), true);
			document.addEventListener("pointermove", (event) => {
				if (splitterDragType === "pointer" && event.pointerId === splitterPointerId) moveDrag(event.clientX);
			});
			document.addEventListener("pointerup", (event) => endDrag("pointer", event));
			document.addEventListener("pointercancel", (event) => endDrag("pointer", event));
			document.addEventListener("mousemove", (event) => {
				if (splitterDragType === "mouse") moveDrag(event.clientX);
			});
			document.addEventListener("mouseup", (event) => endDrag("mouse", event));
			window.addEventListener("resize", () => applyWidth(currentWidth(), false));
			splitterListenersInstalled = true;
		}

		splitter.addEventListener("keydown", (event) => {
			if (event.target !== activeSplitter) return;
			const current = currentWidth();
			if (event.key === "ArrowLeft") applyWidth(current + 20, true);
			else if (event.key === "ArrowRight") applyWidth(current - 20, true);
			else if (event.key === "Home") applyWidth(minWidth, true);
			else if (event.key === "End") applyWidth(maxWidth(), true);
			else return;
			event.preventDefault();
		});
	}

	// ---- theme toggle ----

	function setThemeIcon(theme) {
		const icon = document.getElementById("theme-icon");
		if (icon) icon.setAttribute("href", theme === "light" ? "#icon-sun" : "#icon-moon");
		const button = document.getElementById("theme-toggle");
		if (button) button.setAttribute("aria-pressed", String(theme === "light"));
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

	// ---- secret value reveal ----

	// Secret values are already present in the page (as base64, in a
	// data-b64 attribute) — decoding happens entirely client-side, no
	// server round trip, no plaintext ever sent beyond what was already
	// fetched for this object.
	function hideSecretValue(valueEl) {
		if (!valueEl) return;
		const row = valueEl.closest(".secret-row");
		valueEl.textContent = "••••••••";
		valueEl.classList.add("masked");
		const button = row?.querySelector("button");
		if (button) button.textContent = "Show";
		clearTimeout(secretTimers.get(valueEl));
		secretTimers.delete(valueEl);
	}

	function hideAllSecrets() {
		document.querySelectorAll("#detail-pane .secret-value").forEach(hideSecretValue);
	}

	function toggleSecretValue(btn) {
		const valueEl = btn.previousElementSibling;
		if (!valueEl) return;
		if (valueEl.classList.contains("masked")) {
			let decoded;
			try {
				const bytes = Uint8Array.from(atob(valueEl.dataset.b64), (c) => c.charCodeAt(0));
				decoded = new TextDecoder("utf-8", { fatal: false }).decode(bytes);
			} catch {
				decoded = "(unable to decode)";
			}
			valueEl.textContent = decoded;
			valueEl.classList.remove("masked");
			btn.textContent = "Hide";
			secretTimers.set(valueEl, window.setTimeout(() => hideSecretValue(valueEl), 30000));
		} else hideSecretValue(valueEl);
	}

	function searchDetail(value) {
		const pane = document.getElementById("tab-details");
		if (!pane) return;
		const query = (value || "").toLowerCase();
		pane.querySelectorAll(".kv-grid > dt, .kv-grid > dd, .resource-table tbody tr, .secret-row").forEach((el) => {
			el.style.display = !query || el.textContent.toLowerCase().includes(query) ? "" : "none";
		});
	}

	function filterEvents(value) {
		const query = (value || "").toLowerCase();
		document.querySelectorAll("#tab-events tbody tr").forEach((row) => {
			row.style.display = !query || row.textContent.toLowerCase().includes(query) ? "" : "none";
		});
	}

	function sortHistory(th) {
		const table = th.closest("table");
		const index = Array.from(th.parentElement.children).indexOf(th);
		const descending = th.getAttribute("aria-sort") === "ascending";
		table.querySelectorAll("th").forEach((header) => header.setAttribute("aria-sort", "none"));
		th.setAttribute("aria-sort", descending ? "descending" : "ascending");
		const tbody = table.tBodies[0];
		Array.from(tbody.rows).filter((row) => row.cells.length > index).sort((a, b) => {
			const av = parseSortToken(index === 0 ? a.cells[index].textContent.replace(/\s+\(current\)$/, "") : a.cells[index].textContent);
			const bv = parseSortToken(index === 0 ? b.cells[index].textContent.replace(/\s+\(current\)$/, "") : b.cells[index].textContent);
			return (av < bv ? -1 : av > bv ? 1 : 0) * (descending ? -1 : 1);
		}).forEach((row) => tbody.appendChild(row));
	}
	function reconnectLogs() {
		if (!logAttempt && logBase) startLogs(logBase);
	}

	let workloadStatusTimer = null;
	async function requestWorkload(url, body) {
		const status = document.getElementById("deployment-operation-status");
		const token = document.querySelector('meta[name="kui-csrf-token"]')?.content || "";
		try {
			const response = await fetch(url, {method: "POST", credentials: "same-origin",
				headers: {"Content-Type": "application/json", "X-Kui-CSRF": token}, body: JSON.stringify(body)});
			const text = await response.text();
			if (!response.ok) throw new Error(text || "Operation failed (" + response.status + ")");
			if (status) status.textContent = "Operation accepted.";
			refreshDeploymentStatus();
		} catch (error) {
			if (status) status.textContent = "Operation failed: " + error.message;
		}
	}

	async function refreshDeploymentStatus() {
		const pane = document.querySelector(".deployment-workbench");
		if (!pane) return;
		try {
			const response = await fetch(pane.dataset.statusUrl, {credentials: "same-origin"});
			if (!response.ok) throw new Error("Deployment status unavailable (" + response.status + ")");
			const state = await response.json();
			const summary = document.getElementById("deployment-replicas-status");
			if (summary) summary.textContent = "Desired " + state.desired + " · Current " + state.current + " · Updated " + state.updated + " · Available " + state.available + (state.paused ? " · Paused" : "");
			const desired = document.getElementById("deployment-replicas");
			if (desired && document.activeElement !== desired) desired.value = state.desired;
			const conditions = document.getElementById("deployment-conditions");
			if (conditions) conditions.textContent = (state.conditions || []).map((item) =>
				item.type + ": " + item.status + (item.reason ? " (" + item.reason + ")" : "") + (item.message ? " — " + item.message : "")).join("\n");
			const revisionSelect = document.getElementById("deployment-revision");
			if (revisionSelect) {
				const selected = revisionSelect.value;
				revisionSelect.replaceChildren(new Option("Choose an available revision", ""));
				(state.revisions || []).forEach((revision) => revisionSelect.add(new Option("Revision " + revision, String(revision))));
				if (Array.from(revisionSelect.options).some((option) => option.value === selected)) revisionSelect.value = selected;
			}
			if (state.historyError && conditions) conditions.textContent += "\nRollback history unavailable: " + state.historyError;
		} catch (error) {
			const status = document.getElementById("deployment-operation-status");
			if (status && !status.textContent.startsWith("Operation failed:")) status.textContent = error.message;
		}
	}

	function setupDeploymentWorkbench() {
		clearInterval(workloadStatusTimer);
		workloadStatusTimer = null;
		if (!document.querySelector(".deployment-workbench")) return;
		refreshDeploymentStatus();
		workloadStatusTimer = window.setInterval(refreshDeploymentStatus, 5000);
	}

	document.addEventListener("click", (event) => {
		const button = event.target.closest("[data-workload-action]");
		if (!button) return;
		const pane = button.closest(".deployment-workbench");
		if (!pane) return;
		const action = button.dataset.workloadAction;
		const body = {action};
		if (action === "rollback") {
			const revision = Number(document.getElementById("deployment-revision")?.value);
			if (!Number.isInteger(revision) || revision < 1) {
				document.getElementById("deployment-operation-status").textContent = "Choose a positive available revision.";
				return;
			}
			body.revision = revision;
		}
		if ((action === "pause" || action === "rollback") && !confirm(action === "pause" ? "Pause this Deployment rollout?" : "Roll back to revision " + body.revision + "?")) return;
		requestWorkload(pane.dataset.actionUrl, body);
	});
	document.addEventListener("submit", (event) => {
		if (event.target.id !== "deployment-scale-form") return;
		event.preventDefault();
		const pane = event.target.closest(".deployment-workbench");
		const value = document.getElementById("deployment-replicas").value;
		if (!/^(0|[1-9][0-9]*)$/.test(value) || Number(value) > 100000) {
			document.getElementById("deployment-operation-status").textContent = "Replicas must be an integer from 0 to 100000.";
			return;
		}
		requestWorkload(pane.dataset.scaleUrl, {replicas: Number(value)});
	});

	function samePort(checkbox) {
		const form = checkbox.closest("form");
		const local = form?.querySelector('[name="localPort"]');
		const remote = form?.querySelector('[name="remotePort"]');
		if (!local || !remote) return;
		if (checkbox.checked) local.value = remote.value;
	}

	function decorateCopyValues(root) {
		root.querySelectorAll("#tab-details .resource-table tbody tr").forEach((row) => {
			Array.from(row.cells).forEach((cell) => {
				if (cell.querySelector(".copy-action") || cell.classList.contains("empty-state")) return;
				const value = cell.textContent;
				const button = document.createElement("button");
				button.type = "button";
				button.className = "copy-action";
				button.setAttribute("aria-label", "Copy " + value);
				button.title = "Copy value";
				button.dataset.copyText = value;
				button.addEventListener("click", (event) => {
					event.stopPropagation();
					copyText(button.dataset.copyText);
				});
				cell.appendChild(button);
			});
		});
		root.querySelectorAll("#tab-details .pill").forEach((pill) => {
			if (pill.querySelector(".copy-action")) return;
			const value = pill.textContent;
			const button = document.createElement("button");
			button.type = "button";
			button.className = "copy-action";
			button.title = "Copy value";
			button.setAttribute("aria-label", "Copy " + value);
			button.dataset.copyText = value;
			button.addEventListener("click", (event) => {
				event.stopPropagation();
				copyText(button.dataset.copyText);
			});
			pill.appendChild(button);
		});
		root.querySelectorAll("#tab-details .kv-grid dd").forEach((valueEl) => {
			if (valueEl.querySelector(".copy-action, .pill-row")) return;
			const value = valueEl.innerText;
			const button = document.createElement("button");
			button.type = "button";
			button.className = "copy-action";
			button.title = "Copy value";
			button.setAttribute("aria-label", "Copy value");
			button.dataset.copyText = value;
			button.addEventListener("click", (event) => {
				event.stopPropagation();
				copyText(button.dataset.copyText);
			});
			valueEl.appendChild(button);
		});
	}

	let relatedNodeRowID = "";
	let relatedNodeHTML = "";

	function returnToRelatedNode() {
		const pane = document.getElementById("detail-pane");
		if (!pane || !relatedNodeHTML) return;
		disposeStreams();
		pane.innerHTML = relatedNodeHTML;
		relatedNodeHTML = "";
		pane.classList.add("open");
		decorateCopyValues(pane);
		const row = Array.from(pane.querySelectorAll(".related-pod-row")).find((item) => item.dataset.id === relatedNodeRowID);
		if (row) row.focus();
		relatedNodeRowID = "";
	}

	function setupDetailPane() {
		const pane = document.getElementById("detail-pane");
		if (!pane) return;
		const header = pane.querySelector(".detail-header");
		if (!header) return;
		decorateCopyValues(pane);
		const logButton = pane.querySelector("#tab-logs .stream-controls button[onclick*='startLogs']");
		if (logButton) {
			const match = logButton.getAttribute("onclick")?.match(/startLogs\('([^']+)'\)/);
			if (match) logBase = match[1];
			restoreLogSettings();
		}
		const shell = pane.querySelector("#shell-container");
		if (shell && shell.selectedIndex < 0 && shell.options.length) shell.selectedIndex = 0;
		setupDeploymentWorkbench();
		if (relatedNodeHTML) {
			const back = document.createElement("button");
			back.type = "button";
			back.className = "btn";
			back.textContent = "Back to node";
			back.addEventListener("click", returnToRelatedNode);
			header.querySelector(".detail-header-row")?.appendChild(back);
		}
	}

	// ---- wiring ----

	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", initSplitter, { once: true });
	} else {
		initSplitter();
	}
	document.addEventListener("DOMContentLoaded", initTheme);

	document.addEventListener("click", onTableClick);
	document.addEventListener("click", (e) => {
		const related = e.target.closest(".related-pod-row");
		if (related) {
			relatedNodeHTML = document.getElementById("detail-pane")?.innerHTML || "";
			relatedNodeRowID = related.dataset.id || "";
		}
	}, true);
	document.addEventListener("change", onTableChange);
	window.addEventListener("beforeunload", (e) => {
		if (!hasUnsavedYAMLChanges()) return;
		e.preventDefault();
		e.returnValue = "";
	});

	document.addEventListener("keydown", onTableKeydown);
	document.addEventListener("keydown", (e) => {
		const historyHeader = e.target.closest(".history-table th[role='button']");
		if (historyHeader && ["Enter", " "].includes(e.key)) {
			e.preventDefault();
			sortHistory(historyHeader);
		}
	});
	document.addEventListener("keydown", (e) => {
		if (isConfirmOpen()) {
			if (e.key === "Escape") {
				e.preventDefault();
				document.getElementById("confirm-cancel")?.click();
			} else if (e.key === "Tab") {
				e.preventDefault();
				const cancel = document.getElementById("confirm-cancel");
				const confirm = document.getElementById("confirm-ok");
				if (e.shiftKey) {
					(document.activeElement === cancel ? confirm : cancel)?.focus();
				} else {
					(document.activeElement === confirm ? cancel : confirm)?.focus();
				}
			}
			return;
		}
		if (e.target.closest("dialog[open]")) return;
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
		} else if (isPaletteOpen() && e.key === "Tab") {
			e.preventDefault();
			document.getElementById("palette-input")?.focus();
		} else if (isPaletteOpen() && ["ArrowDown", "ArrowUp", "Enter"].includes(e.key)) {
			movePaletteSelection(e.key);
			e.preventDefault();
		}
	});

	document.body.addEventListener("toast", (e) => showToast(e.detail.toast));

	document.body.addEventListener("htmx:configRequest", (e) => {
		const token = document.querySelector('meta[name="kui-csrf-token"]')?.content;
		if (token) e.detail.headers["X-Kui-CSRF"] = token;
	});
	document.body.addEventListener("htmx:beforeRequest", (e) => {
		if (e.detail.target && e.detail.target.id === "palette-results") {
			e.detail.target.innerHTML = '<div class="loading-state" role="status">Searching…</div>';
		}
	});

	function setWatchState(state) {
		const statusbar = document.getElementById("statusbar");
		const label = document.getElementById("watch-state-label");
		if (statusbar) statusbar.dataset.watchState = state;
		if (!label) return;
		label.textContent = state === "connected" ? "Live updates connected" :
			state === "error" ? "Live watch failed; reconnecting" : "Live updates reconnecting";
	}

	document.body.addEventListener("htmx:sseOpen", () => {
		watchFailed = false;
		setWatchState("connected");
	});
	document.body.addEventListener("htmx:sseError", () => setWatchState(watchFailed ? "error" : "reconnecting"));
	document.body.addEventListener("htmx:sseMessage", (e) => {
		if (e.detail && e.detail.type === "watch-error") {
			watchFailed = true;
			setWatchState("error");
		}
	});

	// Intercepts every hx-confirm-bearing element globally — no per-button
	// wiring needed for the two existing call sites (Delete/Evict) or any
	// future one.
	document.body.addEventListener("htmx:confirm", (e) => {
		const trigger = e.detail.elt;
		const selector = trigger && trigger.getAttribute("hx-target");
		const destination = e.detail.target || (selector ? document.querySelector(selector) : null);
		const replacesDetail = destination && (destination.id === "detail-pane" || destination.id === "content");
		let question = e.detail.question || "";
		if (hasUnsavedYAMLChanges() && replacesDetail) {
			question = question
				? question + "\nThis will discard unsaved YAML changes."
				: "Discard unsaved YAML changes?";
		}
		if (!question) return;
		e.preventDefault();
		showConfirmModal(question, () => e.detail.issueRequest(true));
	});

	// A failed fragment request must leave a visible error in its target and
	// a toast, rather than leaving the detail pane loading indefinitely.
	document.body.addEventListener("htmx:responseError", (e) => {
		const xhr = e.detail.xhr;
		const msg = (xhr && xhr.responseText ? xhr.responseText : "request failed").trim().slice(0, 300);
		const target = e.detail.target;
		const requestElement = e.detail.elt;
		const yamlTarget = (target && target.id === "yaml-result") ||
			(requestElement && requestElement.closest && requestElement.closest("#tab-yaml"));
		if (yamlTarget) {
			const result = document.getElementById("yaml-result");
			if (result) {
				result.textContent = msg;
				result.classList.add("yaml-error");
				result.setAttribute("role", "alert");
			}
		}
		if (target && target.id === "palette-results") {
			target.textContent = "Search failed: " + msg;
			target.setAttribute("role", "alert");
		}
		if (target && target.id === "detail-pane") {
			const error = document.createElement("div");
			error.className = "error-state";
			error.setAttribute("role", "alert");
			error.textContent = msg || "request failed";
			target.replaceChildren(error);
			target.classList.add("open");
			const splitter = document.getElementById("detail-splitter");
			if (splitter) splitter.style.display = "block";
		}
		showToast({ level: "error", message: msg });
	});

	document.body.addEventListener("htmx:beforeSwap", (e) => {
		if (e.detail.target && e.detail.target.id === "detail-pane") {
			hideAllSecrets();
			disposeStreams();
		}
		if (e.detail.target && e.detail.target.classList && e.detail.target.classList.contains("list-pane")) {
			e.detail.target.dataset.scrollTop = e.detail.target.scrollTop;
		}
	});

	document.body.addEventListener("htmx:afterSwap", (e) => {
		const target = e.detail.target;
		if (!target) return;
		if (target.id === "palette-results") syncPaletteOptions();
		if (target.id === "cluster-status") {
			target.dataset.clusterState = /unreachable/i.test(target.textContent) ? "unreachable" : "reachable";
		}

		if (target.id === "detail-pane") {
			const hasContent = target.innerHTML.trim() !== "";
			target.classList.toggle("open", hasContent);
			const splitter = document.getElementById("detail-splitter");
			if (splitter) splitter.style.display = hasContent ? "block" : "none";
			setupDetailPane();
			if (!target.querySelector("#tab-logs")) {
				logBuffer = "";
				logBufferBaseLine = logTotalLines;
				logTrimmed = false;
				logBase = "";
				shellBase = "";
			}
			const terminal = target.querySelector("#term-pane[data-exec-url]");
			if (terminal) startShell(terminal.dataset.execUrl);
		}

		if (target.id === "yaml-result" && target.textContent.toLowerCase().includes("applied")) {
			if (!window.kui.markYAMLApplied?.()) markYAMLEditorSaved();
		}

		if (target.id === "content") {
			initSplitter();
			syncActiveResourceFromPath(e.detail.requestConfig && e.detail.requestConfig.path);
		}

		if (target.classList && target.classList.contains("list-pane")) {
			const resourceVersion = target.querySelector(".toolbar")?.dataset.resourceVersion;
			const source = document.getElementById("watch-source");
			const connect = source?.getAttribute("sse-connect");
			if (resourceVersion && connect) {
				const watchURL = new URL(connect, location.origin);
				watchURL.searchParams.set("resourceVersion", resourceVersion);
				for (const [key, id] of [["labelSelector", "label-selector"], ["fieldSelector", "field-selector"]]) {
					const value = target.querySelector("#" + id)?.value.trim() || "";
					if (value) watchURL.searchParams.set(key, value);
					else watchURL.searchParams.delete(key);
				}
				const nextConnect = watchURL.pathname + watchURL.search;
				if (connect !== nextConnect) {
					// Close the old source before the SSE extension processes this URL again.
					window.htmx.trigger(source, "htmx:beforeCleanupElement");
					source.setAttribute("sse-connect", nextConnect);
					window.htmx.trigger(source, "htmx:afterProcessNode");
				}
			}
			restoreClientListState();
			applySortAndSelection(target);
			if (target.dataset.scrollTop) target.scrollTop = parseInt(target.dataset.scrollTop, 10);
		}
	});
	window.addEventListener("popstate", (event) => restoreSelectedRows(event.state));
	restoreSelectedRows(history.state);
	restoreClientListState();
	restoreRowDensity();
	return {
		showTab,
		startLogs,
		startShell,
		openDebugShell,
		openNodeShell,
		toggleLogs,
		clearLogs,
		filterLogs,
		copyTerminalSelection,
		clearTerminal,
		reconnectLogs,
		cancelLogReconnect,
		reconnectShell,
		downloadLogs,
		copyFilteredLogs,
		downloadFilteredLogs,
		jumpToLatest,
		toggleFollow,
		searchDetail,
		filterEvents,
		sortHistory,
		hideAllSecrets,
		samePort,
		resetTable,
		copySelectedNames,
		exportCSV,
		selectAllPage,
		filterRows,
		closeDetail,
		clearRowFilter,
		setBreadcrumbName,
		selectSidebarLink,
		changeNamespace,
		filterNamespaceOptions,
		clearSelection,
		bulkDelete,
		applyMetrics,
		applyCounts,
		openPalette,
		closePalette,
		recordRecent,
		toggleTheme,
		applyResourceSelectors,
		toggleSecretValue,
		nextResourcePage,
		previousResourcePage,
		changeResourcePageSize,
		setRowDensity,
		copyText,
		syncYAMLDirtyState,
		getLogBuffer: () => logBuffer,
		getLogBufferInfo: () => ({ text: logBuffer, baseLine: logBufferBaseLine, totalLines: logTotalLines }),
		downloadYAML,
		downloadSelectedYAMLBundle,
	};
})();
