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

	async function loadStatus() {
		try {
			const response = await fetch("/rancher", { headers: { Accept: "application/json" } });
			if (!response.ok) throw new Error(String(response.status));
			const data = await response.json();
			urlInput.value = data.url || "";
			tokenInput.value = "";
			tokenInput.placeholder = data.hasToken ? "••••••••••••" : "";
			insecureInput.checked = false;
			removeButton.hidden = !data.configured;
			if (data.configured) {
				const parts = [];
				if (data.clusters) parts.push(`${data.clusters} cluster${data.clusters === 1 ? "" : "s"} imported`);
				if (data.reachable === false) parts.push("unreachable");
				parts.push(data.persist ? "saved to disk — restored on restart" : "from flags/env — not saved");
				announce(parts.length ? `Connected. ${parts.join(" · ")}.` : "Connected.", data.reachable === false ? "warning" : "success");
			} else {
				announce("Not connected. Enter a server URL and API key to import its clusters.");
			}
		} catch (_) {
			removeButton.hidden = true;
			announce("Could not load the current Rancher settings.", "failure");
		}
	}

	form.addEventListener("submit", async (event) => {
		event.preventDefault();
		const url = urlInput.value.trim();
		if (!url) { announce("A server URL is required.", "failure"); return; }
		const payload = { url, insecureSkipTLSVerify: insecureInput.checked };
		const token = tokenInput.value.trim();
		if (token) payload.token = token;
		announce("Connecting…");
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
			announce(`Connected. ${data.clusters ?? 0} cluster${data.clusters === 1 ? "" : "s"} available as rancher/<name> contexts. Settings saved — they will be restored on restart. Reloading…`, "success");
			setTimeout(() => window.location.reload(), 1200);
		} catch (_) {
			announce("Connection failed. Check the URL, API key, and network.", "failure");
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
			urlInput.focus();
		},
	};
})();
