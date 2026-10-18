(() => {
	"use strict";

	const STORAGE_KEY = "kui.appearance.v1";
	const MAX_IMPORT_BYTES = 8192;
	const DEFAULTS = Object.freeze({ theme: "system", accent: "#3b82f6", fontScale: 1, spacing: "comfortable" });
	const themes = new Set(["system", "light", "dark", "high-contrast"]);
	const scales = new Set([0.9, 1, 1.1, 1.2]);
	const spacings = new Set(["compact", "comfortable"]);
	const systemDark = window.matchMedia?.("(prefers-color-scheme: dark)");
	let settings;
	let dialog;
	let status;

	function validAccent(value) {
		if (typeof value !== "string" || !/^#[\da-f]{6}$/i.test(value)) return false;
		const channels = [1, 3, 5].map((index) => parseInt(value.slice(index, index + 2), 16) / 255).map((channel) => channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4);
		const luminance = 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
		return (luminance + 0.05) / 0.05 >= 3 && 1.05 / (luminance + 0.05) >= 3;
	}

	function validate(value) {
		if (!value || typeof value !== "object" || Array.isArray(value) || value.version !== 1) return null;
		const keys = Object.keys(value);
		if (keys.some((key) => !["version", "theme", "accent", "fontScale", "spacing"].includes(key))) return null;
		if (!themes.has(value.theme) || !validAccent(value.accent) || !scales.has(value.fontScale) || !spacings.has(value.spacing)) return null;
		return { theme: value.theme, accent: value.accent.toLowerCase(), fontScale: value.fontScale, spacing: value.spacing };
	}

	function load() {
		try {
			const raw = localStorage.getItem(STORAGE_KEY);
			if (raw && new TextEncoder().encode(raw).length <= MAX_IMPORT_BYTES) {
				const parsed = JSON.parse(raw);
				if (parsed.version === 1) return validate(parsed) || { ...DEFAULTS };
			}
			if (!raw) {
				const legacyTheme = localStorage.getItem("kui.theme");
				if (legacyTheme === "light" || legacyTheme === "dark") {
					const migrated = { ...DEFAULTS, theme: legacyTheme };
					try { localStorage.setItem(STORAGE_KEY, JSON.stringify({ version: 1, ...migrated })); } catch (_) { /* The legacy setting remains available. */ }
					return migrated;
				}
			}
		} catch (_) { /* Storage may be unavailable; defaults remain usable. */ }
		return { ...DEFAULTS };
	}

	function effectiveTheme() {
		if (settings.theme === "system") return systemDark?.matches ? "dark" : "light";
		return settings.theme === "high-contrast" ? "high-contrast" : settings.theme;
	}

	function apply(persist = true) {
		const root = document.documentElement;
		const theme = effectiveTheme();
		root.dataset.theme = theme;
		root.dataset.appearanceMode = settings.theme;
		root.dataset.contrast = settings.theme === "high-contrast" ? "high" : "normal";
		root.dataset.spacing = settings.spacing;
		root.style.setProperty("--accent", theme === "high-contrast" ? "#ff0" : settings.accent);
		root.style.setProperty("--appearance-font-scale", String(settings.fontScale));
		root.style.setProperty("--appearance-spacing-scale", settings.spacing === "compact" ? "0.82" : "1");
		try { localStorage.setItem("kui.theme", theme); } catch (_) { /* The current page still reflects the preference. */ }
		const themeIcon = document.querySelector("#theme-icon use");
		if (themeIcon) themeIcon.setAttribute("href", theme === "light" ? "#icon-sun" : "#icon-moon");
		const themeButton = document.getElementById("theme-toggle");
		if (themeButton) {
			themeButton.setAttribute("aria-pressed", String(theme === "light"));
			themeButton.title = theme === "light" ? "Switch to dark theme" : "Switch to light theme";
			themeButton.dataset.tooltip = themeButton.title;
		}
		let stored = true;
		if (persist) {
			try { localStorage.setItem(STORAGE_KEY, JSON.stringify({ version: 1, ...settings })); }
			catch (_) { stored = false; }
		}
		updateControls();
		return stored;
	}

	function applyAndAnnounce(message) {
		const stored = apply();
		announce(stored ? message : "Appearance applied for this session; browser storage is unavailable.", stored ? "success" : "warning");
	}

	function announce(message, kind = "success") {
		if (!status) return;
		status.dataset.state = kind;
		status.textContent = message;
	}

	function updateControls() {
		if (!dialog) return;
		dialog.querySelector("[name=theme]").value = settings.theme;
		dialog.querySelector("[name=accent]").value = settings.accent;
		dialog.querySelector("[name=fontScale]").value = String(settings.fontScale);
		dialog.querySelector("[name=spacing]").value = settings.spacing;
	}

	function ensureDialog() {
		if (dialog) return;
		const button = document.createElement("button");
		button.type = "button";
		button.id = "appearance-button";
		button.className = "appearance-launcher";
		button.textContent = "Appearance";
		button.setAttribute("aria-label", "Appearance settings");
		button.dataset.tooltip = "Appearance settings";
		button.title = "Appearance settings";
		(document.querySelector(".sidebar-header") || document.body).append(button);

		dialog = document.createElement("dialog");
		dialog.className = "appearance-dialog";
		dialog.setAttribute("aria-labelledby", "appearance-title");
		dialog.innerHTML = `<form method="dialog" class="appearance-heading"><h2 id="appearance-title">Appearance preferences</h2><button type="submit" aria-label="Close appearance settings" data-tooltip="Close">×</button></form>
			<div class="appearance-fields">
				<label>Theme<select name="theme"><option value="system">System</option><option value="light">Light</option><option value="dark">Dark</option><option value="high-contrast">High contrast</option></select></label>
				<label>Accent color<input name="accent" type="color" aria-describedby="appearance-accent-help"><span id="appearance-accent-help">Choose a color with sufficient contrast for focus indicators.</span></label>
				<label>Font size<select name="fontScale"><option value="0.9">Smaller</option><option value="1">Default</option><option value="1.1">Larger</option><option value="1.2">Largest</option></select></label>
				<label>Global spacing<select name="spacing"><option value="comfortable">Comfortable</option><option value="compact">Compact</option></select></label>
			</div>
			<div class="appearance-actions"><button type="button" data-action="export">Export</button><button type="button" data-action="import">Import</button><input class="appearance-file" type="file" accept="application/json,.json" aria-label="Import appearance preferences"><button type="button" data-action="reset">Reset appearance</button></div>
			<p class="appearance-status" role="status" aria-live="polite"></p>`;
		document.body.append(dialog);
		status = dialog.querySelector(".appearance-status");
		button.addEventListener("click", () => { updateControls(); dialog.showModal(); dialog.querySelector("[name=theme]").focus(); });
		dialog.addEventListener("change", (event) => {
			const field = event.target;
			if (!["theme", "accent", "fontScale", "spacing"].includes(field.name)) return;
			const candidate = { ...settings, [field.name]: field.name === "fontScale" ? Number(field.value) : field.value };
			if (field.name === "accent" && !validAccent(candidate.accent)) {
				field.value = settings.accent;
				announce("That accent color does not meet the contrast requirement.", "failure");
				return;
			}
			settings = candidate;
			applyAndAnnounce("Appearance preference saved.");
		});
		dialog.addEventListener("click", async (event) => {
			const action = event.target.closest("[data-action]")?.dataset.action;
			if (action === "export") {
				try {
					const blob = new Blob([JSON.stringify({ version: 1, ...settings }, null, 2)], { type: "application/json" });
					const link = document.createElement("a");
					link.href = URL.createObjectURL(blob);
					link.download = "kui-appearance-v1.json";
					link.click();
					setTimeout(() => URL.revokeObjectURL(link.href), 0);
					announce("Appearance preferences exported.");
				} catch (_) { announce("Appearance export failed.", "failure"); }
			} else if (action === "import") dialog.querySelector(".appearance-file").click();
			else if (action === "reset") {
				if (!window.confirm("Reset all appearance preferences to their defaults? This cannot be undone.")) return;
				settings = { ...DEFAULTS };
				applyAndAnnounce("Appearance preferences reset.");
			}
		});
		dialog.querySelector(".appearance-file").addEventListener("change", async (event) => {
			const file = event.target.files?.[0];
			event.target.value = "";
			if (!file) return;
			if (file.size > MAX_IMPORT_BYTES) { announce("Import failed: file exceeds the 8 KB limit.", "failure"); return; }
			try {
				const parsed = JSON.parse(await file.text());
				const validated = validate(parsed);
				if (!validated) throw new Error("invalid preferences");
				settings = validated;
				applyAndAnnounce("Appearance preferences imported.");
			} catch (_) { announce("Import failed: invalid or unsupported appearance preferences. Nothing was changed.", "failure"); }
		});
	}

	function installTooltips(root = document) {
		root.querySelectorAll?.("button[aria-label], a[aria-label]").forEach((control) => {
			const iconOnly = control.matches(".icon-btn, .copy-action") || !control.textContent.trim();
			if (!iconOnly) return;
			const label = control.getAttribute("title") || control.getAttribute("aria-label");
			if (label) {
				control.dataset.tooltip = label;
				if (!control.hasAttribute("title")) control.title = label;
			}
		});
	}

	function installThemeToggle() {
		installTooltips();
		const observer = new MutationObserver((records) => {
			for (const record of records) {
				if (record.type === "childList") installTooltips(record.target);
				else installTooltips(record.target.parentElement || document);
			}
		});
		observer.observe(document.documentElement, { subtree: true, childList: true, attributes: true, attributeFilter: ["aria-label", "title"] });
		document.addEventListener("click", (event) => {
			const button = event.target.closest?.("#theme-toggle");
			if (!button) return;
			event.preventDefault();
			event.stopImmediatePropagation();
			settings.theme = effectiveTheme() === "dark" ? "light" : "dark";
			applyAndAnnounce("Appearance preference saved.");
		}, true);
	}

	installThemeToggle();
	settings = load();
	apply(false);
	if (systemDark?.addEventListener) systemDark.addEventListener("change", () => { if (settings.theme === "system") apply(false); });
	document.addEventListener("DOMContentLoaded", ensureDialog, { once: true });
	if (document.readyState !== "loading") ensureDialog();
	document.addEventListener("pointerup", (event) => {
		if (event.pointerType !== "touch") return;
		const target = event.target.closest?.("[data-tooltip]");
		if (!target) return;
		target.dataset.tooltipOpen = "true";
		setTimeout(() => { delete target.dataset.tooltipOpen; }, 1800);
	});
})();
