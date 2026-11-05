(() => {
  "use strict";

  let session = null;
  let pane = null;
  let status = null;
  let reconnectButton = null;
  let wsURLBase = "";

  function setStatus(text) {
    if (status) status.textContent = text;
  }

  function terminalFontSize() {
    const saved = Number.parseInt(localStorage.getItem("kui.terminal-font-size") || "13", 10);
    return Number.isInteger(saved) ? Math.max(8, Math.min(28, saved)) : 13;
  }

  function connect() {
    if (session) return;
    const terminalColors = getComputedStyle(document.documentElement);
    const term = new Terminal({
      fontSize: terminalFontSize(),
      cursorBlink: true,
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

    setStatus("Connecting…");
    let ws;
    try {
      ws = new WebSocket(wsURL(wsURLBase));
    } catch (_) {
      fail(term, observer, resize, "Connection failed — reconnect available");
      return;
    }
    session = { term, ws, observer, resize };
    if (reconnectButton) reconnectButton.hidden = true;

    ws.onopen = () => {
      setStatus("Connected · KUBECONFIG scoped to this context");
      term.focus();
    };
    ws.onmessage = (ev) => {
      if (session && session.ws === ws) {
        term.write(ev.data);
        requestAnimationFrame(resize);
      }
    };
    ws.onerror = () => {
      fail(term, observer, resize, "Connection error — reconnect available");
    };
    ws.onclose = () => {
      if (session && session.ws === ws) {
        term.write("\r\n\x1b[2m[shell ended — Reconnect starts a new one]\x1b[0m\r\n");
        finish("Disconnected — Reconnect starts a new shell");
      }
    };
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(data);
    });
  }

  function fail(term, observer, resize, message) {
    term.write("\r\n\x1b[31m[" + message + "]\x1b[0m\r\n");
    finish(message, observer, resize);
  }

  function finish(message, observer, resize) {
    if (session) {
      try { session.ws.close(); } catch (_) { /* already closed */ }
      if (session.observer) session.observer.disconnect();
      window.removeEventListener("resize", session.resize);
      session = null;
    }
    if (observer) observer.disconnect();
    if (resize) window.removeEventListener("resize", resize);
    setStatus(message);
    if (reconnectButton) reconnectButton.hidden = false;
  }

  function bind() {
    const panel = document.querySelector(".terminal-panel");
    if (!panel || panel.dataset.bound === "1") return;
    panel.dataset.bound = "1";
    pane = panel.querySelector("#kubectl-term-pane");
    status = panel.querySelector("#terminal-status");
    reconnectButton = panel.querySelector("#terminal-reconnect");
    wsURLBase = panel.dataset.terminalWs || "";
    reconnectButton?.addEventListener("click", () => { session = null; connect(); });
    connect();
  }

  // The panel lives inside #content, which htmx swaps; bind after each swap
  // and on first load.
  document.addEventListener("htmx:afterSettle", (event) => {
    if (event.detail?.target?.id === "content" || event.detail?.target?.querySelector?.(".terminal-panel")) {
      bind();
    }
  }, true);
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", bind, { once: true });
  else bind();
})();
