import { readFile } from "node:fs/promises";
import { test, expect } from "./fixtures/application.mjs";

async function openResource(page, resource) {
  await page.locator(`.sidebar-link[data-resource="${resource}"]`).click();
  await expect(page.locator("#list-pane #kui-table")).toBeVisible();
}

async function openDetail(page, resource, name) {
  const row = page.locator(`#list-pane #kui-table tbody tr[data-name="${name}"]`);
  await expect(row).toBeVisible();
  await row.focus();
  const detailPath = await row.getAttribute("data-id");
  if (!detailPath) throw new Error(`${resource} row ${name} has no detail path`);
  const requests = [];
  const onRequest = (request) => {
    const url = new URL(request.url());
    if (url.origin === new URL(page.url()).origin && url.pathname.startsWith("/c/")) {
      requests.push(`${request.method()} ${url.pathname}`);
    }
  };
  page.on("request", onRequest);
  const responsePromise = page.waitForResponse(
    (response) => new URL(response.url()).pathname === detailPath,
    { timeout: 10_000 },
  );
  await row.press("Enter");
  let response;
  try {
    response = await responsePromise;
  } catch (error) {
    throw new Error(`Opening ${resource} ${name} timed out at ${page.url()}; requests: ${requests.join(", ")}; ${error.message}`);
  } finally {
    page.off("request", onRequest);
  }
  if (!response.ok()) {
    throw new Error(`Opening ${resource} ${name} returned ${response.status()}: ${await response.text()}`);
  }
  await expect(page.locator("#detail-pane .detail-header strong")).toHaveText(name);
  return row;
}

async function lastMutation(api, pathFragment) {
  return api.requests.filter((request) => request.method !== "GET" && request.path.includes(pathFragment)).at(-1);
}

test.beforeEach(async ({ app, page }) => {
  await page.goto(app.url);
  await expect(page.locator("#content")).toContainText("Cluster Overview");
});

test("switching context discards pending detail and search responses", async ({ page }) => {
  await openResource(page, "pods");
  let startDetail;
  let startSearch;
  const detailStarted = new Promise((resolve) => { startDetail = resolve; });
  const searchStarted = new Promise((resolve) => { startSearch = resolve; });
  await page.route("**/c/**/pods/alpha", async (route) => {
    startDetail();
    await new Promise((resolve) => setTimeout(resolve, 500));
    await route.continue().catch(() => {});
  });
  await page.route("**/c/**/search?*", async (route) => {
    startSearch();
    await new Promise((resolve) => setTimeout(resolve, 500));
    await route.continue().catch(() => {});
  });

  await page.locator('#kui-table tbody tr[data-name="alpha"]').click();
  await detailStarted;
  await page.getByRole("button", { name: "Open search" }).click();
  await page.locator("#palette-input").fill("kind:Pod");
  await searchStarted;

  const contextSelect = page.locator("#context-select");
  const alternateContext = await contextSelect.locator("option").filter({ hasText: "ui-test-alt" }).getAttribute("value");
  expect(alternateContext).toBeTruthy();
  await contextSelect.selectOption(alternateContext);
  await expect(page).toHaveURL(new RegExp(`/c/${alternateContext}$`));
  await expect(page.locator("#content")).toContainText("Cluster Overview");
  await expect(page.locator("#detail-pane .detail-header")).toHaveCount(0);

  await new Promise((resolve) => setTimeout(resolve, 600));
  await expect(page).toHaveURL(new RegExp(`/c/${alternateContext}$`));
  await expect(page.locator("#content")).toContainText("Cluster Overview");
});

test("palette searches live objects and navigates with the keyboard", async ({ page }) => {
  await page.keyboard.press(process.platform === "darwin" ? "Meta+k" : "Control+k");
  const input = page.locator("#palette-input");
  await expect(input).toBeFocused();
  await expect(page.locator("#palette-overlay")).toHaveAttribute("aria-hidden", "false");
  await input.fill("kind:Pod");
  const options = page.locator("#palette-results [role=option]");
  await expect(options).toHaveCount(5);
  await expect(page.locator(".search-count")).toHaveText("Showing up to 5 of 8 matching objects.");
  const initial = await page.locator("#palette-results .palette-result.active").getAttribute("data-recent-label");
  await page.keyboard.press("ArrowDown");
  const active = page.locator("#palette-results .palette-result.active");
  await expect(active).toHaveCount(1);
  expect(await active.getAttribute("data-recent-label")).not.toBe(initial);
  await expect(active).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Enter");
  await expect(page.locator("#palette-overlay")).toHaveAttribute("aria-hidden", "true");
  await expect(page.locator("#list-pane #kui-table")).toBeVisible();
  await expect(page.locator("#breadcrumb-resource")).toHaveText("Pod");
});

test("resource table filtering, sorting, and selection update visible results", async ({ page }) => {
  await openResource(page, "pods");
  const rows = page.locator("#list-pane #kui-table tbody tr[data-name]");
  await expect(rows).toHaveCount(8);

  const visibleRows = page.locator("#list-pane #kui-table tbody tr[data-name]:visible");
  await page.locator("#list-pane #row-filter").fill("bravo");
  await expect(visibleRows).toHaveCount(1);
  await expect(visibleRows.first()).toHaveAttribute("data-name", "bravo");
  await expect(page.locator("#list-pane [data-filter-empty]")).toBeHidden();
  await page.locator("#list-pane #row-filter").fill("not-present");
  await expect(page.locator("#list-pane [data-filter-empty]")).toBeVisible();
  await page.locator("#list-pane #row-filter").fill("");
  await expect(rows).toHaveCount(8);

  const nameHeader = page.locator("#kui-table thead th").filter({ hasText: /^Name$/ });
  await nameHeader.click();
  await nameHeader.click();
  await expect(nameHeader).toHaveAttribute("aria-sort", "descending");
  await expect(rows.first()).toHaveAttribute("data-name", "registry-unavailable");

  await page.locator("#list-pane #kui-table tbody tr[data-name='alpha'] .row-select").check();
  await expect(page.locator("#bulk-bar")).toBeVisible();
  await expect(page.locator("#bulk-count")).toContainText("1");
  await expect(page.locator("#list-pane #kui-table tbody tr[data-name='alpha'] .row-select")).toBeChecked();
});

test("Enter on an all-resources kind navigates to that resource table", async ({ page }) => {
  await openResource(page, "all");
  const row = page.locator('#list-pane #kui-table tbody tr[data-name="Pod"]');
  await expect(row).toBeVisible();
  const resourcePath = await row.getAttribute("data-id");
  await row.focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(new RegExp(`${resourcePath}$`));
  await expect(page.locator("#list-pane #kui-table tbody tr[data-name='alpha']")).toBeVisible();
});

test("keyboard activation of an in-row copy button does not open its row detail", async ({ page }) => {
  await openResource(page, "pods");
  const row = page.locator('#list-pane #kui-table tbody tr[data-name="alpha"]');
  const copy = row.getByRole("button", { name: "Copy resource name" });
  await copy.focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("#detail-pane .detail-header")).toHaveCount(0);
});

test("detail tabs are keyboard-operable and resizing survives HTMX swaps", async ({ page }) => {
  await openResource(page, "pods");
  const alpha = await openDetail(page, "pods", "alpha");
  const splitter = page.locator("#detail-splitter");
  await expect(splitter).toBeVisible();
  await splitter.focus();
  const initialWidth = Number(await splitter.getAttribute("aria-valuenow"));
  await page.keyboard.press("ArrowLeft");
  await expect.poll(async () => Number(await splitter.getAttribute("aria-valuenow"))).toBeGreaterThan(initialWidth);

  const detailsTab = page.locator("#tab-button-details");
  await detailsTab.focus();
  await detailsTab.press("ArrowRight");
  const yamlTab = page.locator("#tab-button-yaml");
  await expect(yamlTab).toBeFocused();
  await expect(yamlTab).toHaveAttribute("aria-selected", "true");
  await expect(page.locator("#tab-yaml")).toBeVisible();
  await yamlTab.press("ArrowRight");
  await expect(page.locator("#tab-button-events")).toBeFocused();
  await expect(page.locator("#tab-events")).toBeVisible();

  await page.getByRole("button", { name: "Close details" }).click();
  await expect(alpha).toBeFocused();
  const bravo = await openDetail(page, "pods", "bravo");
  await expect(splitter).toBeVisible();
  await splitter.focus();
  const secondWidth = Number(await splitter.getAttribute("aria-valuenow"));
  await page.keyboard.press("ArrowLeft");
  await expect.poll(async () => Number(await splitter.getAttribute("aria-valuenow"))).toBeGreaterThan(secondWidth);
  await expect(bravo).toBeVisible();
});

test("theme changes are visible and persist across a reload", async ({ page }) => {
  const root = page.locator("html");
  const toggle = page.getByRole("button", { name: "Toggle color theme" });
  await expect(root).toHaveAttribute("data-theme", "dark");
  await toggle.click();
  await expect(root).toHaveAttribute("data-theme", "light");
  await expect(toggle).toHaveAttribute("aria-pressed", "true");
  await page.reload();
  await expect(root).toHaveAttribute("data-theme", "light");
  await page.locator("#theme-toggle").click();
  await expect(root).toHaveAttribute("data-theme", "dark");
});

test("pod log workbench options reach the API and invalid regex reports safely", async ({ app, page }) => {
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await page.locator("#tab-button-logs").click();
  await page.locator("#logs-previous").check();
  await page.locator("#logs-timestamps").check();
  await page.locator("#logs-since").selectOption("3600");
  await page.locator("#logs-wrap").check();
  await page.locator("#logs-regex").check();
  const query = page.locator("#log-filter");
  await query.fill("^second deterministic");
  await page.getByRole("button", { name: "Connect logs" }).click();
  const logs = page.locator("#logs-pane");
  await expect(logs).toContainText("second deterministic log line");
  await expect(logs).toHaveCSS("white-space", "pre-wrap");
  await page.locator("#logs-wrap").uncheck();
  await expect(logs).toHaveCSS("white-space", "pre");
  await page.locator("#logs-wrap").check();
  await expect(logs).not.toContainText("alpha started");
  await expect(page.locator("#log-mode")).toContainText("Previous instance");
  await expect(page.locator("#log-mode")).toContainText("timestamps");
  const logged = app.api.requests.filter((request) => request.logOptions).at(-1)?.logOptions;
  expect(logged).toMatchObject({ previous: "true", timestamps: "true", sinceSeconds: "3600" });

  await query.fill("[");
  await expect(page.locator("#log-buffer-status")).toContainText("Invalid regular expression");
});

test("log tail presets, filtering, copying, and downloads preserve consumer-visible content", async ({ app, page }) => {
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await page.locator("#tab-button-logs").click();
  await page.locator("#log-container").selectOption("sidecar");
  await page.locator("#log-tail-preset").selectOption("1000");
  await expect(page.locator("#log-tail-lines")).toHaveValue("1000");
  await page.locator("#log-tail-lines").fill("37");
  await page.locator("#log-tail-lines").press("Tab");
  await expect(page.locator("#log-tail-preset")).toHaveValue("");
  await page.locator("#logs-regex").check();
  await page.locator("#log-filter").fill("^second deterministic");
  // Playwright WebKit cannot grant clipboard permissions; Chromium asserts the copied payloads.
  const canReadClipboard = page.context().browser()?.browserType().name() !== "webkit";
  if (canReadClipboard) {
    await page.context().grantPermissions(["clipboard-read", "clipboard-write"], { origin: app.url });
  }
  await page.getByRole("button", { name: "Connect logs" }).click();
  const logs = page.locator("#logs-pane");
  await expect(logs).toContainText("second deterministic log line");
  await expect(logs).not.toContainText("alpha started");
  await expect.poll(() => app.api.requests.filter((request) => request.logOptions).at(-1)?.logOptions).toMatchObject({
    container: "sidecar",
    tailLines: "37",
  });

  if (canReadClipboard) {
    await page.getByRole("button", { name: "Copy filtered" }).click();
    await expect(page.locator("#toast-container")).toContainText("Copied to clipboard");
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe("second deterministic log line");
  }

  const rawPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download raw" }).click();
  const raw = await rawPromise;
  expect(raw.suggestedFilename()).toBe("alpha-sidecar-raw.log");
  const rawContent = await readFile(await raw.path(), "utf8");
  expect(rawContent).toContain("alpha started");
  expect(rawContent).toContain("second deterministic log line");

  const filteredPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download filtered" }).click();
  const filtered = await filteredPromise;
  expect(filtered.suggestedFilename()).toBe("alpha-sidecar-filtered.log");
  expect(await readFile(await filtered.path(), "utf8")).toBe("second deterministic log line");
});


test("canceling a failed log reconnect prevents another connection attempt", async ({ app, page }) => {
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  app.api.controls.failNextLog = true;
  await page.locator("#tab-button-logs").click();
  await page.getByRole("button", { name: "Connect logs" }).click();
  const cancel = page.locator("#log-retry-cancel");
  await expect(cancel).toBeVisible();
  await cancel.click();
  await expect(page.locator("#log-status")).toHaveText("Reconnect canceled");
  const failedConnections = app.api.requests.filter((request) => request.logOptions).length;
  await page.waitForTimeout(1200);
  expect(app.api.requests.filter((request) => request.logOptions)).toHaveLength(failedConnections);
});

test("log settings expose tail, previous-instance, timestamp, and time-range options to the API", async ({ app, page }) => {
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await page.locator("#tab-button-logs").click();
  await page.locator("#logs-previous").check();
  await page.locator("#logs-timestamps").check();
  await page.locator("#logs-since").selectOption("3600");
  await page.locator("#logs-wrap").check();
  await page.locator("#log-tail-preset").selectOption("5000");
  await page.getByRole("button", { name: "Connect logs" }).click();
  await expect(page.locator("#logs-pane")).toContainText("alpha started");
  await expect.poll(() => app.api.requests.filter((request) => request.logOptions).at(-1)?.logOptions).toMatchObject({
    previous: "true",
    timestamps: "true",
    sinceSeconds: "3600",
    tailLines: "5000",
  });
  await expect(page.locator("#logs-previous")).toBeChecked();
  await expect(page.locator("#logs-wrap")).toBeChecked();
});

test("deployment scaling validates input, records success, and preserves conflicts", async ({ app, page }) => {
  await openResource(page, "deployments");
  await openDetail(page, "deployments", "api-gateway");
  await page.locator("#tab-button-history").click();
  const historyRows = page.locator("#tab-history tbody");
  await expect(historyRows).toContainText("api-gateway-v2");
  await expect(historyRows).toContainText("example.invalid/ui-test:2");
  await page.locator("#tab-button-details").click();
  const form = page.locator("#deployment-scale-form");
  const replicas = page.locator("#deployment-replicas");
  await expect(form).toBeVisible();
  await expect(replicas).toHaveValue("3");

  const mutationsBefore = app.api.requests.filter((request) => request.method !== "GET").length;
  await replicas.fill("-1");
  await form.locator("button[type=submit]").click();
  expect(await replicas.evaluate((input) => input.checkValidity())).toBe(false);
  expect(app.api.requests.filter((request) => request.method !== "GET")).toHaveLength(mutationsBefore);

  await replicas.fill("100001");
  await form.locator("button[type=submit]").click();
  expect(await replicas.evaluate((input) => input.checkValidity())).toBe(false);
  expect(app.api.requests.filter((request) => request.method !== "GET")).toHaveLength(mutationsBefore);

  for (const desired of [0, 4, 100000]) {
    await replicas.fill(String(desired));
    await form.locator("button[type=submit]").click();
    await expect.poll(() => app.api.objects.deployments.find((item) => item.metadata.name === "api-gateway").spec.replicas).toBe(desired);
    await expect(page.locator("#deployment-operation-status")).toContainText("Operation accepted.");
  }
  await expect(page.locator("#detail-pane .detail-header strong")).toHaveText("api-gateway");

  await page.getByRole("button", { name: "Close details" }).click();
  await openDetail(page, "deployments", "conflict-deployment");
  await page.locator("#deployment-replicas").fill("2");
  await page.locator("#deployment-scale-form button[type=submit]").click();
  await expect.poll(async () => lastMutation(app.api, "/deployments/conflict-deployment")).toBeTruthy();
  await expect(page.locator("#deployment-operation-status")).toContainText(/conflict|current resource version/i);
  await expect(page.locator("#detail-pane .detail-header strong")).toHaveText("conflict-deployment");

  await page.getByRole("button", { name: "Close details" }).click();
  await openDetail(page, "deployments", "invalid-scale");
  await page.locator("#deployment-replicas").fill("5");
  await page.locator("#deployment-scale-form button[type=submit]").click();
  await expect(page.locator("#deployment-operation-status")).toContainText(/fixture rejects scaling/i);
});

test("workload restart updates the Deployment pod-template annotation", async ({ app, page }) => {
  await openResource(page, "deployments");
  await openDetail(page, "deployments", "api-gateway");
  const restart = page.locator('[data-workload-action="restart"]');
  await expect(restart).toBeVisible();
  await restart.click();
  const deployment = () => app.api.objects.deployments.find((item) => item.metadata.name === "api-gateway");
  await expect.poll(() => deployment().spec.template.metadata.annotations?.["kubectl.kubernetes.io/restartedAt"]).toBeTruthy();
  const restartedAt = deployment().spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"];
  expect(restartedAt).toMatch(/^\d{4}-\d{2}-\d{2}T/);
  expect(app.api.objects.deployments.find((item) => item.metadata.name === "api-gateway").spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"])
    .toBe(restartedAt);
  await expect(page.locator("#deployment-operation-status")).toHaveText("Operation accepted.");
});

test("deployment pause, resume, and selected-revision rollback change live state", async ({ app, page }) => {
  await openResource(page, "deployments");
  await openDetail(page, "deployments", "api-gateway");
  const revisions = page.locator("#deployment-revision");
  await expect(revisions).toContainText("Revision 1");
  await expect(revisions).toContainText("Revision 2");

  const rollback = page.locator('[data-workload-action="rollback"]');
  const writesBeforeEmptyRollback = app.api.requests.filter((request) => request.method !== "GET").length;
  await rollback.click();
  await expect(page.locator("#deployment-operation-status")).toContainText("Choose a positive available revision");
  expect(app.api.requests.filter((request) => request.method !== "GET")).toHaveLength(writesBeforeEmptyRollback);

  await revisions.selectOption("1");
  page.once("dialog", (dialog) => dialog.accept());
  await rollback.click();
  await expect.poll(() => app.api.objects.deployments.find((item) => item.metadata.name === "api-gateway").spec.template.spec.containers[0].image).toBe("example.invalid/ui-test:1");
  expect(app.api.objects.deployments.find((item) => item.metadata.name === "api-gateway").spec.template.spec.containers[0].image)
    .toBe("example.invalid/ui-test:1");
  await expect(page.locator("#deployment-operation-status")).toHaveText("Operation accepted.");

  page.once("dialog", (dialog) => dialog.accept());
  await page.locator('[data-workload-action="pause"]').click();
  await expect.poll(() => app.api.objects.deployments.find((item) => item.metadata.name === "api-gateway").spec.paused).toBe(true);
  await expect(page.locator("#deployment-replicas-status")).toContainText("Paused");
  await page.locator('[data-workload-action="resume"]').click();
  await expect.poll(() => app.api.objects.deployments.find((item) => item.metadata.name === "api-gateway").spec.paused).toBe(false);
  await expect(page.locator("#deployment-replicas-status")).not.toContainText("Paused");
});

test("overview exposes live fixture health and stays usable at a narrow viewport", async ({ page }) => {
  await expect(page.locator("#content .stat-tile")).toHaveCount(6);
  await expect(page.locator("#content")).toContainText("Warning");
  await page.setViewportSize({ width: 320, height: 760 });
  await expect(page.getByRole("navigation", { name: "Resource navigation" })).toBeVisible();
  await expect(page.getByRole("main")).toBeVisible();
  await expect(page.getByRole("link", { name: "Skip to main content" })).toBeAttached();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("link", { name: "Skip to main content" })).toBeFocused();
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.locator("#theme-toggle").click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
test("mobile detail opens as an overlay without shrinking the table or overflowing the viewport", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 });
  await openResource(page, "pods");
  await openDetail(page, "pods", "alpha");
  await expect(page.locator("#detail-pane")).not.toHaveAttribute("aria-busy", "true");
  const geometry = await page.evaluate(() => {
    const pane = document.getElementById("detail-pane").getBoundingClientRect();
    const list = document.getElementById("list-pane").getBoundingClientRect();
    return {
      position: getComputedStyle(document.getElementById("detail-pane")).position,
      paneLeft: pane.left,
      paneRight: pane.right,
      listWidth: list.width,
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: innerWidth,
      resizeHandleContent: getComputedStyle(document.getElementById("detail-pane"), "::before").content,
    };
  });
  expect(geometry.position).toBe("absolute");
  expect(geometry.paneLeft).toBeGreaterThanOrEqual(0);
  expect(geometry.paneRight).toBeLessThanOrEqual(geometry.viewportWidth);
  expect(geometry.listWidth).toBeGreaterThanOrEqual(geometry.viewportWidth - 1);
  expect(geometry.documentWidth).toBeLessThanOrEqual(geometry.viewportWidth);
  expect(geometry.resizeHandleContent).toBe("none");
});

test("table workbench saves a filter and column layout, then compares distinct metadata", async ({ page }) => {
  await openResource(page, "pods");
  await page.locator("#list-pane #row-filter").fill("alpha");
  await page.locator("#table-view-button").click();
  const panel = page.locator("#table-view-panel");
  await expect(panel).toBeVisible();
  const columnControls = panel.locator('[data-action="column-visible"]');
  await expect(columnControls).not.toHaveCount(0);
  await expect(columnControls.first()).toBeDisabled();

  const optionalColumn = columnControls.nth(1);
  const columnName = await optionalColumn.evaluate((input) =>
    input.closest("li")?.querySelector(".kui-column-title")?.textContent?.trim());
  expect(columnName).toBeTruthy();
  const escapedColumn = columnName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const columnHeader = page.locator("#kui-table thead th").filter({ hasText: new RegExp(`^${escapedColumn}$`) });
  const wasVisible = await columnHeader.isVisible();
  await optionalColumn.setChecked(!wasVisible);
  if (wasVisible) await expect(columnHeader).toBeHidden();
  else await expect(columnHeader).toBeVisible();

  page.once("dialog", (dialog) => dialog.accept("Regression view"));
  await panel.locator('[data-action="view-create"]').click();
  const savedViews = page.locator("#saved-view-select");
  await expect(savedViews).toContainText("Regression view");
  await page.locator("#list-pane #row-filter").fill("bravo");
  await savedViews.selectOption({ label: "Regression view" });
  const visibleRows = page.locator("#list-pane #kui-table tbody tr[data-name]:visible");
  await expect(visibleRows).toHaveCount(1);
  await expect(visibleRows.first()).toHaveAttribute("data-name", "alpha");

  await page.locator("#list-pane #row-filter").fill("");
  await page.locator("#list-pane #kui-table tbody tr[data-name='alpha'] .row-select").check();
  await page.locator("#list-pane #kui-table tbody tr[data-name='bravo'] .row-select").check();
  const compare = page.locator("#compare-selected");
  await expect(compare).toBeEnabled();
  await compare.click();
  const comparison = page.locator("#comparison-panel");
  await expect(comparison).toBeVisible();
  await expect(comparison).toContainText("frontend");
  await expect(comparison).toContainText("backend");
  await expect(comparison).toContainText(/tier/i);
});

test("saved views duplicate, rename, update, export, import, reject other scopes, and delete safely", async ({ page }) => {
  await openResource(page, "pods");
  const panel = page.locator("#table-view-panel");
  await page.locator("#table-view-button").click();
  await page.locator("#list-pane #row-filter").fill("alpha");
  page.once("dialog", (dialog) => dialog.accept("Primary view"));
  await panel.locator('[data-action="view-create"]').click();
  const views = page.locator("#saved-view-select");
  await expect(views).toContainText("Primary view");

  page.once("dialog", (dialog) => dialog.accept("Renamed view"));
  await panel.locator('[data-action="view-rename"]').click();
  await expect(views).toContainText("Renamed view");
  await expect(views).not.toContainText("Primary view");

  page.once("dialog", (dialog) => dialog.accept("Independent copy"));
  await panel.locator('[data-action="view-duplicate"]').click();
  await expect(views).toContainText("Independent copy");
  await page.locator("#list-pane #row-filter").fill("bravo");
  await panel.locator('[data-action="view-update"]').click();
  await expect(page.locator("#kui-view-state")).toContainText("Active view: Independent copy");
  await views.selectOption({ label: "Renamed view" });
  const visibleRows = page.locator("#list-pane #kui-table tbody tr[data-name]:visible");
  await expect(visibleRows).toHaveCount(1);
  await expect(visibleRows.first()).toHaveAttribute("data-name", "alpha");

  const exportPromise = page.waitForEvent("download");
  await panel.locator('[data-action="view-export"]').click();
  const exported = await exportPromise;
  expect(exported.suggestedFilename()).toBe("kui-resource-views.json");
  const payload = JSON.parse(await readFile(await exported.path(), "utf8"));
  expect(payload.scope.namespace).toBe("dev");
  expect(payload.views.map((view) => view.name).sort()).toEqual(["Independent copy", "Renamed view"]);
  expect(JSON.stringify(payload).toLowerCase()).not.toMatch(/password|bearer|token/);

  const importFile = async (name, content) => {
    const chooserPromise = page.waitForEvent("filechooser");
    await panel.locator('[data-action="view-import"]').click();
    const chooser = await chooserPromise;
    await chooser.setFiles({ name, mimeType: "application/json", buffer: Buffer.from(content) });
  };
  await importFile("views.json", JSON.stringify(payload));
  await expect(views).toContainText("Renamed view (imported)");
  await expect(views).toContainText("Independent copy (imported)");
  const countAfterValidImport = await views.locator("option").count();
  const wrongScope = { ...payload, scope: { ...payload.scope, namespace: "qa" } };
  await importFile("wrong-scope.json", JSON.stringify(wrongScope));
  await expect(panel.locator(".kui-view-message")).toContainText("different context, namespace, or resource kind");
  await expect(views.locator("option")).toHaveCount(countAfterValidImport);

  await views.selectOption({ label: "Independent copy" });
  page.once("dialog", (dialog) => dialog.dismiss());
  await panel.locator('[data-action="view-delete"]').click();
  await expect(views).toContainText("Independent copy");
  page.once("dialog", (dialog) => dialog.accept());
  await panel.locator('[data-action="view-delete"]').click();
  await expect.poll(() => views.locator("option").allTextContents()).not.toContain("Independent copy");
  await expect(page.locator("#list-pane #kui-table tbody tr[data-name]:visible")).toHaveCount(8);
});

test("resource comparisons filter changes, export, copy, and preserve table selection on clear", async ({ page }) => {
  await openResource(page, "pods");
  await page.locator("#list-pane #kui-table tbody tr[data-name='alpha'] .row-select").check();
  await page.locator("#list-pane #kui-table tbody tr[data-name='bravo'] .row-select").check();
  await page.locator("#compare-selected").click();
  const panel = page.locator("#comparison-panel");
  await expect(panel.locator(".kui-comparison-table")).toBeVisible();
  const rows = panel.locator(".kui-comparison-table tbody tr");
  const allRows = await rows.count();
  expect(allRows).toBeGreaterThan(1);
  const changedOnly = panel.locator('[data-action="comparison-changed"]');
  await changedOnly.check();
  await expect.poll(() => rows.count()).toBeLessThan(allRows);
  await changedOnly.uncheck();

  // Clipboard payloads are covered in Chromium; WebKit's clipboard API exposes no grantable permissions.
  if (page.context().browser()?.browserType().name() !== "webkit") {
    await page.context().grantPermissions(["clipboard-read", "clipboard-write"], { origin: new URL(page.url()).origin });
    await panel.locator('[data-action="comparison-copy-text"]').click();
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain("alpha vs bravo");
    await panel.locator('[data-action="comparison-copy-json"]').click();
    const exportedJSON = JSON.parse(await page.evaluate(() => navigator.clipboard.readText()));
    expect(exportedJSON.resources.map((item) => item.name).sort()).toEqual(["alpha", "bravo"]);
    expect(JSON.stringify(exportedJSON.differences)).toContain("frontend");
    expect(JSON.stringify(exportedJSON.differences)).toContain("backend");
  }

  const csvPromise = page.waitForEvent("download");
  await panel.locator('[data-action="comparison-export-csv"]').click();
  const csv = await csvPromise;
  expect(csv.suggestedFilename()).toBe("resource-comparison.csv");
  const csvText = await readFile(await csv.path(), "utf8");
  expect(csvText).toContain('"Field path","alpha","bravo"');
  expect(csvText).toContain("frontend");
  expect(csvText).toContain("backend");

  await panel.locator('[data-action="comparison-clear"]').click();
  await expect(panel.locator(".kui-comparison-status")).toContainText("Comparison cleared");
  await expect(page.locator("#list-pane #kui-table tbody tr[data-name='alpha'] .row-select")).toBeChecked();
  await expect(page.locator("#list-pane #kui-table tbody tr[data-name='bravo'] .row-select")).toBeChecked();
});

test("selected resources download as a deterministic multi-document YAML bundle", async ({ page }) => {
  await openResource(page, "pods");
  await page.locator("#list-pane #kui-table tbody tr[data-name='bravo'] .row-select").check();
  await page.locator("#list-pane #kui-table tbody tr[data-name='alpha'] .row-select").check();
  const button = page.locator("#bulk-yaml-export");
  await expect(button).toBeVisible();
  await expect(button).toBeEnabled();
  await expect(button).toHaveAttribute("aria-description", /Secret data/);
  await expect(button).toHaveAttribute("title", /100 resources and 8 MiB/);
  const downloadPromise = page.waitForEvent("download");
  await button.click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("kui-selected-resources.yaml");
  const bundle = await readFile(await download.path(), "utf8");
  expect(bundle.match(/^---$/gm)).toHaveLength(2);
  expect(bundle.indexOf("name: alpha")).toBeLessThan(bundle.indexOf("name: bravo"));
  expect(bundle).toContain("kind: Pod");
  expect(bundle).not.toContain("managedFields");
  await expect(page.locator("#toast-container")).toContainText("Downloaded 2 selected resources as YAML.");
});

test("batch metadata previews exact patches and retries only failed resources", async ({ app, page }) => {
  await openResource(page, "pods");
  await page.locator("#list-pane #kui-table tbody tr[data-name='alpha'] .row-select").check();
  await page.locator("#list-pane #kui-table tbody tr[data-name='conflict'] .row-select").check();
  await page.locator("#batch-metadata-button").click();
  const form = page.locator("#batch-metadata-form");
  await expect(form).toBeVisible();
  await form.locator("#batch-metadata-kind").selectOption("labels");
  await form.locator("#batch-metadata-action").selectOption("add");
  await form.locator("#batch-metadata-key").fill("ui-test");
  await form.locator("#batch-metadata-value").fill("verified");
  await form.locator("button[type=submit]").click();
  const results = page.locator("#batch-metadata-results");
  await expect(results).toContainText("alpha");
  await expect(results).toContainText("conflict");
  await expect(results).toContainText("READY alpha (");
  await expect(results).toContainText("/pods/alpha)");
  await expect(results).toContainText("READY conflict (");
  await expect(results).toContainText("/pods/conflict)");
  await expect(results).toContainText('{"metadata":{"labels":{"ui-test":"verified"},"resourceVersion":"1"}}');
  await expect(page.locator("#batch-metadata-submit")).toBeVisible();

  page.once("dialog", (dialog) => dialog.accept());
  await page.locator("#batch-metadata-submit").click();
  await expect(results).toContainText("SUCCESS alpha: updated");
  await expect(results).toContainText(/FAILED conflict:/i);
  await expect(page.locator("#batch-metadata-retry")).toBeVisible();
  const alphaWritesBeforeRetry = app.api.requests.filter((request) => request.method !== "GET" && request.path.includes("/pods/alpha")).length;
  const conflictWritesBeforeRetry = app.api.requests.filter((request) => request.method !== "GET" && request.path.includes("/pods/conflict")).length;
  expect(alphaWritesBeforeRetry).toBe(1);
  expect(conflictWritesBeforeRetry).toBe(1);

  await page.locator("#batch-metadata-retry").click();
  await expect.poll(() => app.api.requests.filter((request) => request.method !== "GET" && request.path.includes("/pods/conflict")).length).toBe(2);
  expect(app.api.requests.filter((request) => request.method !== "GET" && request.path.includes("/pods/alpha"))).toHaveLength(alphaWritesBeforeRetry);
});

test("batch metadata surfaces Kubernetes validation failures", async ({ app, page }) => {
  await openResource(page, "pods");
  await page.locator("#list-pane #kui-table tbody tr[data-name='invalid-metadata'] .row-select").check();
  await page.locator("#batch-metadata-button").click();
  const form = page.locator("#batch-metadata-form");
  await form.locator("#batch-metadata-kind").selectOption("labels");
  await form.locator("#batch-metadata-action").selectOption("add");
  await form.locator("#batch-metadata-key").fill("ui-test");
  await form.locator("button[type=submit]").click();
  await expect(page.locator("#batch-metadata-submit")).toBeVisible();
  page.once("dialog", (dialog) => dialog.accept());
  await page.locator("#batch-metadata-submit").click();
  await expect(page.locator("#batch-metadata-results")).toContainText(/invalid|metadata key/i);
  expect(app.api.requests.some((request) => request.path.includes("/pods/invalid-metadata") && request.method !== "GET")).toBe(true);
});

test("batch metadata previews and applies label and annotation add, update, and remove", async ({ app, page }) => {
  await openResource(page, "pods");
  await page.locator("#list-pane #kui-table tbody tr[data-name='alpha'] .row-select").check();
  await page.locator("#batch-metadata-button").click();
  const form = page.locator("#batch-metadata-form");
  const key = form.locator("#batch-metadata-key");
  const value = form.locator("#batch-metadata-value");
  const results = page.locator("#batch-metadata-results");

  const apply = async (kind, action, metadataKey, metadataValue) => {
    await form.locator("#batch-metadata-kind").selectOption(kind);
    await form.locator("#batch-metadata-action").selectOption(action);
    await key.fill(metadataKey);
    if (action !== "remove") await value.fill(metadataValue);
    const target = app.api.objects.pods.find((item) => item.metadata.name === "alpha");
    const resourceVersion = target.metadata.resourceVersion;
    await form.locator("button[type=submit]").click();
    await expect(results).toContainText("READY alpha (");
    const expectedValue = action === "remove" ? null : metadataValue;
    const expectedPatch = JSON.stringify({ metadata: { [kind]: { [metadataKey]: expectedValue }, resourceVersion } });
    await expect(results).toContainText(expectedPatch);
    await page.once("dialog", (dialog) => dialog.accept());
    await page.locator("#batch-metadata-submit").click();
    await expect(results).toContainText(/SUCCESS\s+alpha/);
    const mutation = app.api.requests.filter((request) => request.method !== "GET" && request.path.includes("/pods/alpha")).at(-1);
    expect(mutation.body.metadata[kind]).toEqual({ [metadataKey]: expectedValue });
    expect(target.metadata[kind][metadataKey]).toBe(action === "remove" ? undefined : metadataValue);
  };

  await apply("labels", "add", "ui.example/review", "first");
  await apply("labels", "update", "tier", "reviewed");
  await apply("labels", "remove", "ui.example/review", "");
  await apply("annotations", "add", "ui.example/description", "first annotation");
  await apply("annotations", "update", "ui.example/description", "updated annotation");
  await apply("annotations", "remove", "ui.example/description", "");
});

test("the Kubernetes watch reconnects after disconnect and closes after leaving the application", async ({ app, page }) => {
  app.api.controls.closeNextWatch = true;
  await openResource(page, "pods");
  await expect.poll(() => app.api.watches.started, { timeout: 10_000 }).toBeGreaterThanOrEqual(2);
  await expect.poll(() => app.api.watches.closed).toBeGreaterThan(0);
  await expect(page.locator("#statusbar")).toHaveAttribute("data-watch-state", "connected");
  await page.goto("about:blank");
  await expect.poll(() => app.api.watches.closed).toBe(app.api.watches.started);
});

test("shortcut help is announced in a dialog and closes with focus restored", async ({ page }) => {
  await page.getByRole("button", { name: "Open search" }).click();
  await page.locator("#palette-input").fill("kind:Pod");
  const shortcutButton = page.locator("[data-search-shortcuts]");
  await expect(shortcutButton).toBeVisible();
  await shortcutButton.click();
  const dialog = page.getByRole("dialog", { name: "Search keyboard shortcuts" });
  await expect(dialog).toBeVisible();
  const shortcut = process.platform === "darwin" ? /⌘\s*K/ : /Ctrl\s*\+\s*K/i;
  await expect(dialog).toContainText(shortcut);
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(shortcutButton).toBeFocused();
});

test("overview severity, layout, pins, refresh, and namespace/context scope persist independently", async ({ app, page }) => {
  const overview = page.locator("#overview");
  await expect(overview).toHaveAttribute("data-namespace", "dev");
  await expect(overview).toHaveAttribute("data-context", /.+/);

  const attentionRows = page.locator('#overview [data-overview-section="attention"] [data-severity]:visible');
  await expect(attentionRows).toHaveCount(3);
  await page.locator("#attention-severity").selectOption("warning");
  await expect(attentionRows).toHaveCount(1);
  await expect(attentionRows.first()).toHaveAttribute("data-severity", "warning");
  await expect(attentionRows.first()).toContainText("registry-unavailable");

  const settingsButton = page.locator("#overview-settings-button");
  await settingsButton.click();
  const settings = page.locator("#overview-settings-panel");
  await expect(settings).toBeVisible();
  await settings.locator('[data-toggle-section="objects"]').uncheck();
  await expect(page.locator('#overview [data-overview-section="objects"]')).toBeHidden();
  await page.locator('#overview [data-overview-section="activity"] [data-section-move="up"]').click();
  const sectionOrder = () => page.locator("#overview > .overview-section").evaluateAll((items) => items.map((item) => item.dataset.overviewSection));
  let order = await sectionOrder();
  expect(order.indexOf("activity")).toBeLessThan(order.indexOf("attention"));

  const warningTile = page.locator('[data-overview-tile="warnings"]');
  const pin = warningTile.locator("[data-pin-tile]");
  await pin.click();
  await expect(pin).toHaveAttribute("aria-pressed", "true");
  await expect(pin).toHaveAccessibleName("Unpin warnings tile");
  await expect(page.locator("#overview .tile-grid [data-overview-tile]").first()).toHaveAttribute("data-overview-tile", "warnings");

  const podListsBeforeRefresh = app.api.requests.filter((request) => request.method === "GET" && request.path.endsWith("/namespaces/dev/pods")).length;
  await page.locator("#overview-refresh").click();
  await expect.poll(() => app.api.requests.filter((request) => request.method === "GET" && request.path.endsWith("/namespaces/dev/pods")).length).toBeGreaterThan(podListsBeforeRefresh);
  await expect(page.locator('#overview [data-overview-section="objects"]')).toBeHidden();
  await expect(page.locator("#attention-severity")).toHaveValue("warning");

  await page.reload();
  await expect(page.locator("#attention-severity")).toHaveValue("warning");
  await expect(page.locator('#overview [data-overview-section="objects"]')).toBeHidden();
  await expect(page.locator("#overview .tile-grid [data-overview-tile]").first()).toHaveAttribute("data-overview-tile", "warnings");
  order = await sectionOrder();
  expect(order.indexOf("activity")).toBeLessThan(order.indexOf("attention"));

  await page.locator("#namespace-select").selectOption("qa");
  await expect(page.locator("#overview")).toHaveAttribute("data-namespace", "qa");
  await expect(page.locator("#attention-severity")).toHaveValue("all");
  await expect(page.locator('#overview [data-overview-section="objects"]')).toBeVisible();
  await expect(page.locator('[data-overview-tile="warnings"] [data-pin-tile]')).toHaveAttribute("aria-pressed", "false");

  await page.locator("#namespace-select").selectOption("dev");
  await expect(page.locator("#overview")).toHaveAttribute("data-namespace", "dev");
  await expect(page.locator("#attention-severity")).toHaveValue("warning");
  await expect(page.locator('#overview [data-overview-section="objects"]')).toBeHidden();
  await page.locator("#context-select").selectOption({ label: "ui-test-alt" });
  await expect(page.locator("#overview")).toHaveAttribute("data-namespace", "dev");
  await expect(page.locator("#attention-severity")).toHaveValue("all");
  await expect(page.locator('#overview [data-overview-section="objects"]')).toBeVisible();
  await page.locator("#context-select").selectOption({ label: "ui-test" });
  await expect(page.locator("#attention-severity")).toHaveValue("warning");
  await page.locator("#overview-settings-button").click();
  await page.locator("#overview-settings-reset").click();
  await expect(page.locator("#attention-severity")).toHaveValue("all");
  await expect(page.locator('#overview [data-overview-section="objects"]')).toBeVisible();
  await expect(page.locator('[data-overview-tile="warnings"] [data-pin-tile]')).toHaveAttribute("aria-pressed", "false");
});

test("global shortcut dialog restores focus and keyboard commands navigate and focus landmarks", async ({ page }) => {
  const shortcutButton = page.locator("#shortcuts-button");
  await shortcutButton.click();
  const dialog = page.getByRole("dialog", { name: "Keyboard shortcuts" });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("g, then p");
  await expect(dialog).toContainText("f, then t");
  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  await expect(shortcutButton).toBeFocused();

  await page.keyboard.press("g");
  await page.keyboard.press("p");
  await expect(page.locator("#breadcrumb-resource")).toHaveText("Pod");
  await expect(page.locator("#list-pane #kui-table")).toBeVisible();
  await page.keyboard.press("f");
  await page.keyboard.press("t");
  await expect.poll(() => page.evaluate(() => document.activeElement === document.querySelector("#kui-table"))).toBe(true);
  const alpha = page.locator("#list-pane #kui-table tbody tr[data-name='alpha']");
  await alpha.focus();
  await page.keyboard.press("Alt+ArrowDown");
  await expect.poll(() => page.evaluate(() => document.activeElement?.closest("tr[data-name]")?.dataset.name)).toBe("bravo");
  await page.keyboard.press("Space");
  await expect(page.locator("#list-pane #kui-table tbody tr[data-name='bravo'] .row-select")).toBeChecked();

  await page.keyboard.press("g");
  await page.keyboard.press("n");
  await expect(page.locator("#breadcrumb-resource")).toHaveText("Node");
  await page.keyboard.press("g");
  await page.keyboard.press("d");
  await expect(page.locator("#breadcrumb-resource")).toHaveText("Deployment");
  await openDetail(page, "deployments", "api-gateway");
  await page.keyboard.press("f");
  await page.keyboard.press("d");
  await expect.poll(() => page.evaluate(() => document.activeElement?.closest("#detail-pane") !== null)).toBe(true);
  await page.keyboard.press("f");
  await page.keyboard.press("n");
  await expect.poll(() => page.evaluate(() => document.activeElement?.closest(".sidebar") !== null)).toBe(true);
});

test("overview metric links render their actual resource or event filter scope", async ({ app, page }) => {
  const metricValue = (key) => page.locator(`[data-overview-tile="${key}"] .stat-tile .value`);
  await expect(metricValue("nodes")).toHaveText("1/1");
  await expect(metricValue("running")).toHaveText("6");
  await expect(metricValue("pending")).toHaveText("1");
  await expect(metricValue("failed")).toHaveText("1");
  await expect(metricValue("warnings")).toHaveText("1");

  await page.locator('[data-overview-tile="nodes"] a.stat-tile-link').click();
  await expect(page.locator("#breadcrumb-resource")).toHaveText("Node");
  await expect(page.locator("#list-pane #kui-table")).toContainText("node-a");

  await page.goto(app.url);
  await page.locator('[data-overview-tile="running"] a.stat-tile-link').click();
  const names = async () => page.locator("#list-pane #kui-table tbody tr[data-name]").evaluateAll((rows) =>
    rows.map((row) => row.dataset.name).sort());
  await expect.poll(names).toEqual(["alpha", "bravo", "conflict", "crashy", "invalid-metadata", "registry-unavailable"]);

  await page.goto(app.url);
  await page.locator('[data-overview-tile="pending"] a.stat-tile-link').click();
  await expect(page.locator("#list-pane #kui-table tbody tr[data-name]")).toHaveCount(1);
  await expect(page.locator("#list-pane #kui-table tbody tr[data-name]").first()).toHaveAttribute("data-name", "pending-pod");

  await page.goto(app.url);
  await page.locator('[data-overview-tile="failed"] a.stat-tile-link').click();
  await expect(page.locator("#list-pane #kui-table tbody tr[data-name]")).toHaveCount(1);
  await expect(page.locator("#list-pane #kui-table tbody tr[data-name]").first()).toHaveAttribute("data-name", "failed-pod");
  await expect(page.locator("#list-pane #kui-table")).not.toContainText("crashy");

  await page.goto(app.url);
  await page.locator('[data-overview-tile="warnings"] a.stat-tile-link').click();
  await expect(page.locator("#breadcrumb-resource")).toHaveText("Event");
  await expect(page.locator("#list-pane #kui-table")).toContainText("Container app is waiting");
  await expect(page.locator("#list-pane #kui-table")).not.toContainText("Container app started");
});
