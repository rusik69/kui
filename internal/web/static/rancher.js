(() => {
	"use strict";

	const dialog = document.getElementById("rancher-dialog");
	if (!dialog) return;

	const form = dialog.querySelector("#rancher-form");
	const status = dialog.querySelector("#rancher-status");
	const removeButton = dialog.querySelector("[data-action=remove]");
	const urlInput = form.querySelector("[name=url]");
	const tokenInput = form.querySelector("[name=token]");
	const insecureInput = form.querySelector("[name=insecureSkipTLSVerify]");
	const kcDirInput = form.querySelector("[name=kubeconfigDir]");
	const serverList = document.getElementById("rancher-server-list");
	const serverListWrap = document.getElementById("rancher-servers");

	const csrfToken = () => document.querySelector('meta[name="kui-csrf-token"]')?.content || "";

	function announce(message, kind = "info") {
		status.textContent = message;
		status.dataset.state = kind;
	}

	function announceFromError(response) {
		response.text().then((text) => {
			const trimmed = text.trim().split("\n").pop();
			announce(trimmed || `Request failed (${response.status}).`, "failure");
		}).catch(() => announce(`Request failed (${response.status}).`, "failure"));
	}

	function describeServer(server) {
		const parts = [];
		if (server.clusters) parts.push(`${server.clusters} cluster${server.clusters === 1 ? "" : "s"}`);
		if (server.reachable === false) parts.push("unreachable");
		return parts.join(" · ");
	}

	// renderServers lists every connected server with its own disconnect
	// control. With one server the dialog looks like before (the legacy
	// top-level status still describes it), so the list only appears from
	// two servers on — plus any server currently in an error state.
	function renderServers(servers = []) {
		if (!serverList) return;
		serverList.replaceChildren();
		const showList = servers.length > 1 || servers.some(s => s.error || s.reachable === false);
		if (serverListWrap) serverListWrap.hidden = !showList;
		if (!showList) return;
		for (const server of servers) {
			const row = document.createElement("div");
			row.className = "rancher-server-row";
			const info = document.createElement("span");
			info.className = "rancher-server-info";
			info.textContent = `${server.url}${describeServer(server) ? " — " + describeServer(server) : ""}`;
			info.title = server.error || info.textContent;
			row.append(info);
			const disconnect = document.createElement("button");
			disconnect.type = "button";
			disconnect.textContent = "Disconnect";
			disconnect.setAttribute("aria-label", `Disconnect ${server.url}`);
			disconnect.addEventListener("click", async () => {
				disconnect.disabled = true;
				announce(`Disconnecting ${server.url}…`);
				try {
					const response = await fetch("/rancher", {
						method: "DELETE",
						headers: { "Content-Type": "application/json", "X-Kui-CSRF": csrfToken(), Accept: "application/json" },
						body: JSON.stringify({ disconnect: server.url }),
					});
					if (!response.ok) { disconnect.disabled = false; announceFromError(response); return; }
					announce(`Disconnected ${server.url}. Reloading…`, "success");
					setTimeout(() => window.location.reload(), 1200);
				} catch (_) {
					disconnect.disabled = false;
					announce("Disconnect failed.", "failure");
				}
			});
			row.append(disconnect);
			serverList.append(row);
		}
	}

	async function loadStatus() {
		try {
			const response = await fetch("/rancher", { headers: { Accept: "application/json" } });
			if (!response.ok) throw new Error(String(response.status));
			const data = await response.json();
			const servers = data.servers || [];
			urlInput.value = data.url || "";
			tokenInput.value = "";
			tokenInput.placeholder = data.hasToken ? "••••••••••••" : "";
			insecureInput.checked = false;
			kcDirInput.value = data.kubeconfigDir || "";
			removeButton.hidden = !data.configured;
			renderServers(servers);
			if (data.configured) {
				const reachable = servers.length ? servers.every(s => s.reachable !== false) : data.reachable !== false;
				const total = servers.length ? servers.reduce((sum, s) => sum + (s.clusters || 0), 0) : (data.clusters || 0);
				const parts = [`${servers.length || 1} server${(servers.length || 1) === 1 ? "" : "s"} connected`, `${total} cluster${total === 1 ? "" : "s"} imported`];
				if (!reachable) parts.push("some unreachable");
				parts.push(data.persist ? "saved to disk — restored on restart" : "from flags/env — not saved");
				announce(`Connected. ${parts.join(" · ")}.`, reachable ? "success" : "warning");
			} else {
				announce(data.kubeconfigDir ? "Kubeconfig directory set." : "Not connected. Enter a server URL and API key to import its clusters.");
			}
		} catch (_) {
			removeButton.hidden = true;
			renderServers([]);
			announce("Could not load the current settings.", "failure");
		}
	}

	form.addEventListener("submit", async (event) => {
		event.preventDefault();
		const payload = {
			kubeconfigDir: kcDirInput.value.trim(),
			url: urlInput.value.trim(),
			insecureSkipTLSVerify: insecureInput.checked,
		};
		const token = tokenInput.value.trim();
		if (token) payload.token = token;
		if (!payload.url && !payload.kubeconfigDir) {
			announce("Enter a server URL, an API key, or a kubeconfig directory.", "failure");
			return;
		}
		announce("Saving…");
		try {
			const response = await fetch("/rancher", {
				method: "PUT",
				headers: { "Content-Type": "application/json", "X-Kui-CSRF": csrfToken(), Accept: "application/json" },
				body: JSON.stringify(payload),
			});
			if (!response.ok) { announceFromError(response); return; }
			const data = await response.json();
			tokenInput.value = "";
			tokenInput.placeholder = data.hasToken ? "••••••••••••" : "";
			removeButton.hidden = false;
			const servers = data.servers || [];
			const total = servers.length ? servers.reduce((sum, s) => sum + (s.clusters || 0), 0) : (data.clusters || 0);
			const parts = [servers.length > 1 ? `${servers.length} servers connected` : (data.url ? `Rancher: ${data.url}` : "Kubeconfig dir updated")];
			if (total) parts.push(`${total} cluster${total === 1 ? "" : "s"}`);
			announce(`${parts.join(" · ")}. Saved — restored on restart. Reloading…`, "success");
			setTimeout(() => window.location.reload(), 1200);
		} catch (_) {
			announce("Saving failed. Check the URL, API key, and directory path.", "failure");
		}
	});

	removeButton.addEventListener("click", async () => {
		announce("Disconnecting…");
		try {
			const response = await fetch("/rancher", {
				method: "DELETE",
				headers: { "X-Kui-CSRF": csrfToken(), Accept: "application/json" },
			});
			if (!response.ok) { announceFromError(response); return; }
			removeButton.hidden = true;
			urlInput.value = "";
			tokenInput.value = "";
			tokenInput.placeholder = "";
			insecureInput.checked = false;
			renderServers([]);
			announce("Disconnected. Saved settings were removed. Reloading…", "success");
			setTimeout(() => window.location.reload(), 1200);
		} catch (_) {
			announce("Disconnect failed.", "failure");
		}
	});

	window.kuiRancher = {
		open() {
			loadStatus();
			dialog.showModal();
			kcDirInput.focus();
		},
	};
})();
