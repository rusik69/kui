import { test, expect, openTableFilters } from "./fixtures/application.mjs";

test.beforeEach(async ({ app, page }) => {
  await page.goto(app.url);
  await expect(page.locator("#content")).toBeVisible();
});

async function openResource(page, resource) {
  await page.locator(`.sidebar-link[data-resource="${resource}"]`).click();
  await expect(page.locator("#list-pane #kui-table")).toBeVisible();
  await openTableFilters(page);
}

async function openDetail(page, resource, name) {
  const row = page.locator(`#list-pane #kui-table tbody tr[data-name="${name}"]`);
  await expect(row).toBeVisible();
  await row.press("Enter");
  await expect(page.locator("#detail-pane .detail-header strong")).toHaveText(name);
}

test("detail metadata identity, disclosure state, and escaped label selectors are usable", async ({ page }) => {
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await expect(page.getByRole("button", { name: "Hide all secret values" })).toHaveCount(0);
  await expect(page.getByRole("region", { name: "Resource identity" })).toContainText("uid-dev-alpha");
  await expect(page.getByRole("link", { name: "Open canonical resource detail in new tab" })).toHaveAttribute("target", "_blank");
  const metadata = page.locator('.pill-row[data-metadata-kind="labels"] button[data-key="app"]');
  await expect(metadata).toHaveAttribute("data-value", "alpha");
  await metadata.click();
  await expect(page).toHaveURL(/labelSelector=app%3Dalpha/);
  await expect(page).toHaveURL(/\/dev\/pods/);
  await openDetail(page, "pods", "alpha");
  const annotation = page.locator('.pill-row[data-metadata-kind="annotations"] button[data-key="owner"]');
  await annotation.click();
  await expect(page).toHaveURL(/rowFilter=owner%3Dui-test/);
  await expect(page.locator("#list-pane #kui-table tbody tr[data-id]")).not.toHaveCount(0);
  await openTableFilters(page);
  // The annotation filter correctly hides alpha; clear it before opening that row again.
  await page.locator("#list-pane #row-filter").fill("");
  await openDetail(page, "pods", "alpha");
  const section = page.getByRole("button", { name: /Metadata/ });
  await section.click();
  await expect(section).toHaveAttribute("aria-expanded", "false");
  await page.getByRole("button", { name: "Expand all sections" }).click();
  await expect(section).toHaveAttribute("aria-expanded", "true");
});

test("YAML drafts are scoped and bounded; validation reports server parsing failures", async ({ page, app }) => {
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await page.locator("#tab-button-yaml").click();
  const editor = page.locator("#yaml-editor");
  const serverYAML = await editor.inputValue();
  await editor.fill(`${serverYAML}\n# local workbench draft`);
  await expect(page.locator("#yaml-dirty-status")).toContainText("Local draft saved");
  await page.reload();
  await page.goto(app.url);
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await page.locator("#tab-button-yaml").click();
  await expect(page.locator("#yaml-editor")).toHaveValue(/local workbench draft/);
  await page.locator("#yaml-editor").fill("kind: [unterminated\nmetadata:");
  await page.getByRole("button", { name: "Validate YAML" }).click();
  await expect(page.locator("#yaml-result")).toContainText(/validation failed/i);
  await page.locator("#yaml-editor").fill("x".repeat(262145));
  await expect(page.locator("#yaml-dirty-status")).toContainText("Draft exceeds the 256 KiB local limit; not saved");
  const draftValues = await page.evaluate(() => Object.entries(localStorage).filter(([key]) => key.startsWith("kui.yaml-draft:")));
  expect(draftValues.every(([, value]) => value.length < 262144)).toBe(true);
});

test("YAML workbench find/replace, gutter, wrap, indent, and revisions keep editor text stable", async ({ page }) => {
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await page.locator("#tab-button-yaml").click();
  const editor = page.locator("#yaml-editor");
  const original = await editor.inputValue();
  await editor.fill(`${original}\n# marker one\n`);
  await page.locator("#yaml-find").fill("marker one");
  await page.getByRole("button", { name: "Find next" }).click();
  await expect(editor).toHaveJSProperty("selectionStart", original.length + 3);
  await page.locator("#yaml-replace").fill("marker two");
  await page.getByRole("button", { name: "Replace all" }).click();
  await expect(page.locator("#yaml-dirty-status")).toContainText("Replaced 1 matches");
  const beforeWrap = await editor.inputValue();
  await page.locator("#yaml-wrap").check();
  await expect(editor).toHaveClass(/yaml-wrap/);
  await expect(editor).toHaveValue(beforeWrap);
  await page.locator("#yaml-wrap").uncheck();
  const lineCount = await editor.evaluate((element) => element.value.split("\n").length);
  await expect(page.locator("#yaml-line-numbers")).toContainText(String(lineCount));
  await editor.evaluate((element) => element.setSelectionRange(0, element.value.indexOf("\n")));
  await page.getByRole("button", { name: "Indent selection" }).click();
  await expect(editor).toHaveValue(/^  apiVersion:/);
  const revisions = page.locator("#yaml-revisions");
  await expect(revisions.locator("option")).toHaveCount(3);
  await revisions.selectOption("0");
  await page.getByRole("button", { name: "Restore revision" }).click();
  await expect(editor).toHaveValue(/marker two/);
});

test("logs bookmark notes and highlight presets stay scoped without storing log content", async ({ page }) => {
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await page.locator("#tab-button-logs").click();
  await page.getByRole("button", { name: "Connect logs" }).click();
  await expect(page.locator("#logs-pane")).toContainText("alpha started");
  await expect(page.locator("#log-line-counts")).toContainText(/total · .* matching · .* bookmarked/);
  await page.locator("#log-bookmark-note").fill("check startup");
  await page.getByRole("button", { name: "Bookmark current line" }).click();
  await expect(page.locator("#log-bookmarks")).toContainText("check startup");
  await page.locator(".log-highlights summary").click();
  await page.locator("#log-highlight-patterns").fill("alpha started\nready");
  await page.getByRole("button", { name: "Apply highlights" }).click();
  await expect(page.locator("#logs-pane mark.log-highlight").first()).toContainText("alpha started");
  const saved = await page.evaluate(() => Object.entries(localStorage).filter(([key]) => key.startsWith("kui.log-workbench:")));
  expect(saved.every(([, value]) => {
    const state = JSON.parse(value);
    return state.bookmarks.every((bookmark) => Number.isInteger(bookmark.line) && !Object.hasOwn(bookmark, "text"));
  })).toBe(true);
  await page.locator("#log-highlight-import").setInputFiles({ name: "bad.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify({ version: 9, patterns: ["x"] })) });
  await expect(page.locator("#log-status")).toContainText("Invalid highlight preset schema");
  await page.locator("#log-highlight-import").setInputFiles({ name: "large.json", mimeType: "application/json", buffer: Buffer.alloc(16 * 1024 + 1, "x") });
  await expect(page.locator("#log-status")).toContainText("exceeds 16 KiB");
});

test("log bookmarks stay anchored to the original line as the stream grows and filters change", async ({ page, app }) => {
  app.api.controls.logText = "first stable line\nsecond stable line\n";
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await page.locator("#tab-button-logs").click();
  await page.getByRole("button", { name: "Connect logs" }).click();
  await expect(page.locator("#logs-pane")).toContainText("second stable line");
  await page.locator("#log-bookmark-note").fill("stable anchor");
  await page.getByRole("button", { name: "Bookmark current line" }).click();
  await expect(page.locator("#log-bookmarks")).toContainText("Line 2");
  await page.evaluate(() => document.querySelector("#logs-pane").scrollTop = 0);
  app.api.controls.appendLog("third appended line\n");
  await expect(page.locator("#logs-pane")).toContainText("third appended line");
  await expect(page.locator("#log-bookmarks")).toContainText("Line 2");
  await page.locator("#log-filter").fill("second stable line");
  await page.locator("#log-bookmarks button").click();
  await expect(page.locator("#logs-pane")).toContainText("second stable line");
  const saved = await page.evaluate(() => Object.values(localStorage).filter((value) => value.includes("stable anchor")).map((value) => JSON.parse(value)));
  expect(saved[0].bookmarks[0].line).toBe(1);
});

test("successful YAML apply clears its local draft and revisions", async ({ page }) => {
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await page.locator("#tab-button-yaml").click();
  const editor = page.locator("#yaml-editor");
  await editor.fill(`${await editor.inputValue()}\n# apply marker`);
  await page.getByRole("button", { name: "Review changes" }).click();
  await expect(page.locator("#yaml-diff-pane")).toContainText("Server-side dry-run passed");
  await page.getByRole("button", { name: "Confirm apply" }).click();
  await expect(page.locator("#yaml-result")).toContainText("applied");
  const drafts = await page.evaluate(() => Object.keys(localStorage).filter((key) => key.startsWith("kui.yaml-draft:")));
  expect(drafts).toHaveLength(0);
  await expect(page.locator("#yaml-revisions option")).toHaveCount(1);
});

test("terminal zoom is bounded and persistent; fullscreen exits and clear protects active output", async ({ page, app }) => {
  await page.addInitScript(() => {
    const NativeWebSocket = window.WebSocket;
    class TerminalSocket {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSING = 2;
      static CLOSED = 3;

      constructor(address, protocols) {
        const url = new URL(address, location.href);
        if (!/\/c\/dWktdGVzdA\/dev\/pods\/alpha\/exec$/.test(url.pathname)) {
          return new NativeWebSocket(address, protocols);
        }
        this.readyState = TerminalSocket.CONNECTING;
        queueMicrotask(() => {
          this.readyState = TerminalSocket.OPEN;
          this.onopen?.({ target: this });
          this.onmessage?.({ data: "terminal-output-marker", target: this });
        });
      }

      send() {}
      close() { this.readyState = TerminalSocket.CLOSED; this.onclose?.({ target: this }); }
    }
    window.WebSocket = TerminalSocket;
  });
  await page.reload();
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await page.locator("#tab-button-shell").click();
  await page.getByRole("button", { name: "Connect", exact: true }).click();
  await expect(page.locator("#shell-status")).toContainText("Connected");
  await expect(page.locator("#term-pane")).toContainText("terminal-output-marker");

  const zoomIn = page.getByRole("button", { name: "Increase terminal font size" });
  const zoomOut = page.getByRole("button", { name: "Decrease terminal font size" });
  await zoomIn.click();
  expect(await page.evaluate(() => window.kuiTerminalSession.term.options.fontSize)).toBe(14);
  await page.keyboard.press("Shift+Equal");
  expect(await page.evaluate(() => window.kuiTerminalSession.term.options.fontSize)).toBe(15);
  await page.evaluate(() => {
    const button = document.querySelector('button[aria-label="Increase terminal font size"]');
    for (let i = 0; i < 30; i++) button.click();
  });
  expect(await page.evaluate(() => window.kuiTerminalSession.term.options.fontSize)).toBe(28);
  await page.evaluate(() => {
    const button = document.querySelector('button[aria-label="Decrease terminal font size"]');
    for (let i = 0; i < 30; i++) button.click();
  });
  expect(await page.evaluate(() => window.kuiTerminalSession.term.options.fontSize)).toBe(8);
  expect(await page.evaluate(() => localStorage.getItem("kui.terminal-font-size"))).toBe("8");

  const originalPosition = await page.locator("#term-pane").evaluate((pane) => getComputedStyle(pane).position);
  const fullscreen = page.getByRole("button", { name: "Full screen terminal" });
  await fullscreen.click();
  await expect(page.locator("#term-pane")).toHaveClass(/terminal-fullscreen/);
  await expect.poll(() => page.locator("#term-pane").evaluate((pane) => getComputedStyle(pane).position)).toBe("fixed");
  await page.keyboard.press("Escape");
  await expect(page.locator("#term-pane")).not.toHaveClass(/terminal-fullscreen/);
  await expect.poll(() => page.locator("#term-pane").evaluate((pane) => getComputedStyle(pane).position)).toBe(originalPosition);

  page.once("dialog", async (dialog) => {
    expect(dialog.message()).toContain("terminal session is active");
    await dialog.dismiss();
  });
  await page.getByRole("button", { name: "Clear terminal" }).click();
  await expect(page.locator("#term-pane")).toContainText("terminal-output-marker");
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Clear terminal" }).click();
  await expect(page.locator("#term-pane")).not.toContainText("terminal-output-marker");

  await page.reload();
  await page.goto(app.url);
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await page.locator("#tab-button-shell").click();
  await page.getByRole("button", { name: "Connect", exact: true }).click();
  await expect(page.locator("#shell-status")).toContainText("Connected");
  expect(await page.evaluate(() => window.kuiTerminalSession.term.options.fontSize)).toBe(8);
});

test("port-forward panel exposes its real session listing scope", async ({ page }) => {
  await openResource(page, "pods");
  const requestPromise = page.waitForRequest((req) => req.url().includes("/portforward?"));
  await openDetail(page, "pods", "alpha");
  await page.locator("#tab-button-forward").click();
  await expect(page.locator(".active-forwards")).toContainText("Active port forwards");
  await expect(page.locator(".active-forwards [role=status]")).toContainText("No active port-forward sessions");
  const request = await requestPromise;
  expect(request.url()).toContain("context=");
  expect(request.url()).toContain("namespace=dev");
  expect(request.url()).toContain("pod=alpha");
});
test("pausing logs buffers new records until resume", async ({ page, app }) => {
  app.api.controls.logText = "initial log line\n";
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await page.locator("#tab-button-logs").click();
  await page.getByRole("button", { name: "Connect logs" }).click();
  await expect(page.locator("#logs-pane")).toContainText("initial log line");
  await page.getByRole("button", { name: "Pause" }).click();
  await expect(page.getByRole("button", { name: "Resume" })).toHaveAttribute("aria-pressed", "true");
  app.api.controls.appendLog("buffered while paused\n");
  await expect.poll(() => page.evaluate(() => window.kui.getLogBuffer())).toContain("buffered while paused");
  await expect(page.locator("#logs-pane")).not.toContainText("buffered while paused");
  await page.getByRole("button", { name: "Resume" }).click();
  await expect(page.locator("#logs-pane")).toContainText("buffered while paused");
});
