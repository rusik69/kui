(() => {
  "use strict";

  const dialog = document.getElementById("create-dialog");
  if (!dialog) return;

  const editor = dialog.querySelector("#create-editor");
  const status = dialog.querySelector("#create-status");
  const kindLabel = dialog.querySelector("#create-dialog-kind");
  const namespaceSelect = dialog.querySelector("#create-namespace");
  const validateButton = dialog.querySelector("#create-validate");
  const submitButton = dialog.querySelector("#create-submit");
  const lineNumbers = dialog.querySelector("#create-line-numbers");

  const csrfToken = () => document.querySelector('meta[name="kui-csrf-token"]')?.content || "";

  let createURL = "";
  let busy = false;

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

  function refreshLineNumbers() {
    if (!lineNumbers) return;
    const count = editor.value.split("\n").length;
    const previous = lineNumbers.childElementCount;
    if (previous === count) return;
    const fragment = document.createDocumentFragment();
    for (let i = 1; i <= count; i++) {
      const div = document.createElement("div");
      div.textContent = i;
      fragment.append(div);
    }
    lineNumbers.replaceChildren(fragment);
  }

  // Namespaces come from the page shell's selector when present (it already
  // carries the full list); fall back to a free-text field via optgroup when
  // it does not.
  function fillNamespaces() {
    const pageSelect = document.getElementById("namespace-select");
    namespaceSelect.replaceChildren();
    if (!pageSelect) {
      const option = document.createElement("option");
      option.value = "";
      option.textContent = "(from manifest)";
      namespaceSelect.append(option);
      return;
    }
    const custom = document.createElement("option");
    custom.value = "";
    custom.textContent = "(from manifest)";
    namespaceSelect.append(custom);
    let sawCurrent = false;
    for (const option of pageSelect.options) {
      if (option.value === "_all") continue;
      const clone = document.createElement("option");
      clone.value = option.value;
      clone.textContent = option.value;
      if (option.selected) { clone.selected = true; sawCurrent = true; }
      namespaceSelect.append(clone);
    }
    if (!sawCurrent && pageSelect.value && pageSelect.value !== "_all") {
      const fallback = document.createElement("option");
      fallback.value = pageSelect.value;
      fallback.textContent = pageSelect.value;
      fallback.selected = true;
      namespaceSelect.prepend(fallback);
    }
  }

  function applyNamespaceSelection() {
    if (!namespaceSelect.value) return;
    const lines = editor.value.split("\n");
    const namespaceIndex = lines.findIndex((line) => /^ {2}namespace:/.test(line));
    if (namespaceIndex >= 0) {
      lines[namespaceIndex] = `  namespace: ${namespaceSelect.value}`;
    } else {
      const nameIndex = lines.findIndex((line) => /^ {2}name:/.test(line));
      lines.splice(nameIndex >= 0 ? nameIndex + 1 : 3, 0, `  namespace: ${namespaceSelect.value}`);
    }
    editor.value = lines.join("\n");
    refreshLineNumbers();
  }

  async function fetchScaffold(url) {
    announce("Loading starter manifest…");
    try {
      const response = await fetch(url, { headers: { Accept: "text/plain" } });
      if (!response.ok) { announceFromError(response); return false; }
      editor.value = await response.text();
      refreshLineNumbers();
      fillNamespaces();
      applyNamespaceSelection();
      announce("Starter manifest loaded. Edit it, then validate or create.");
      return true;
    } catch (_) {
      announce("Could not load the starter manifest.", "failure");
      return false;
    }
  }

  async function post(query) {
    if (busy) return;
    busy = true;
    submitButton.disabled = true;
    validateButton.disabled = true;
    try {
      const response = await fetch(createURL + query, {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded", "X-Kui-CSRF": csrfToken() },
        body: new URLSearchParams({ yaml: editor.value }),
      });
      if (!response.ok) { announceFromError(response); return; }
      const result = await response.text();
      announce(response.headers.get("HX-Trigger")?.includes("created") ? "Created." : "Server-side validation passed.", "success");
      if (!query) {
        status.insertAdjacentHTML("beforeend", " ");
        status.append(...new DOMParser().parseFromString(result, "text/html").body.childNodes);
        setTimeout(() => window.location.reload(), 900);
      }
    } catch (_) {
      announce("Request failed. Check the manifest and cluster access.", "failure");
    } finally {
      busy = false;
      submitButton.disabled = false;
      validateButton.disabled = false;
    }
  }

  submitButton.addEventListener("click", () => post(""));
  validateButton.addEventListener("click", () => post("?validate=1"));
  namespaceSelect.addEventListener("change", applyNamespaceSelection);
  editor.addEventListener("input", refreshLineNumbers);
  editor.addEventListener("keydown", (event) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
      event.preventDefault();
      post("");
    }
  });

  window.kuiCreate = {
    open(url, kind) {
      createURL = url;
      kindLabel.textContent = kind || "resource";
      status.textContent = "";
      status.dataset.state = "";
      editor.value = "";
      if (typeof dialog.open === "boolean" && dialog.open) dialog.close();
      dialog.showModal();
      if (fetchScaffold(url)) editor.focus();
    },
  };
})();
