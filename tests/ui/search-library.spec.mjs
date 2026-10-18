import { test, expect } from "./fixtures/application.mjs";

test.beforeEach(async ({ app, page }) => {
  await page.goto(app.url);
  await expect(page.locator("#content")).toContainText("Cluster Overview");
});

test("search queries have separate scoped history and saved-query lifecycle with import/export", async ({ page }) => {
  await page.getByRole("button", { name: "Open search" }).click();
  const input = page.locator("#palette-input");
  await input.fill("kind:Pod");
  await expect(page.locator(".search-query-tools")).toBeVisible();
  await page.locator('[data-save-query-form] input[name="name"]').fill("Pods");
  await page.locator('[data-save-query-form] button[type="submit"]').click();
  await page.locator('[data-save-query-form] input[name="name"]').fill("Pods");
  await page.locator('[data-save-query-form] button[type="submit"]').click();
  await expect(page.locator("#search-query-library-status")).toContainText("already exists");
  await expect.poll(async () => page.evaluate(() => JSON.parse(localStorage.getItem("kui.searchQueryHistory.v1") || "[]"))).toContainEqual(expect.objectContaining({ query: "kind:Pod" }));
  await page.locator('[data-query-library] button[data-restore-query="kind:Pod"]').last().click();
  await expect(input).toHaveValue("kind:Pod");
  page.once("dialog", (dialog) => dialog.accept("Workload pods"));
  await page.locator('[data-query-library] [data-rename-query="Pods"]').click();
  await expect(page.locator('[data-query-library]')).toContainText("Workload pods");
  const download = page.waitForEvent("download");
  await page.locator("[data-export-queries]").click();
  const file = await download;
  expect(file.suggestedFilename()).toBe("kui-saved-queries.json");
  const scope = await page.locator(".search-workbench").evaluate((box) => ({context: box.dataset.searchContext, namespace: box.dataset.searchNamespace}));
  await page.locator("[data-import-queries]").setInputFiles({ name: "queries.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify({version: 1, queries: [{name: "Imported", query: "kind:Pod", ...scope}]})) });
  await expect(page.locator('[data-query-library]')).toContainText("Imported");
  await page.locator("[data-import-queries]").setInputFiles({ name: "invalid.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify({version:1, queries:[], extra:true})) });
  await expect(page.locator("#search-query-library-status")).toContainText("Unsupported query file version or schema");
  await page.locator('[data-query-library] [data-delete-query="Workload pods"]').click();
  await expect(page.locator('[data-undo-query]')).toBeVisible();
  await page.locator("[data-undo-query]").click();
  await expect(page.locator('[data-query-library]')).toContainText("Workload pods");
});

test("saved queries are isolated by context and namespace", async ({ page }) => {
  const scope = await page.evaluate(() => {
    const path = location.pathname.split("/");
    return {context: decodeURIComponent(path[2] || ""), namespace: new URL(location.href).searchParams.get("ns") || decodeURIComponent(path[3] || "_all")};
  });
  await page.evaluate((current) => {
    localStorage.setItem("kui.savedSearchQueries.v1", JSON.stringify([
      {name:"Current scope", query:"kind:Pod", ...current},
      {name:"Other namespace", query:"kind:Secret", context:current.context, namespace:"foreign-ns"},
      {name:"Other context", query:"kind:Node", context:"foreign-context", namespace:current.namespace},
    ]));
  }, scope);
  await page.getByRole("button", { name: "Open search" }).click();
  const input = page.locator("#palette-input");
  await input.focus();
  await expect(page.locator(".search-query-library")).toContainText("Current scope");
  await expect(page.locator(".search-query-library")).not.toContainText("Other namespace");
  await expect(page.locator(".search-query-library")).not.toContainText("Other context");
});

test("qualifier suggestions populate search and malformed selectors show precise errors", async ({ page }) => {
  await page.getByRole("button", { name: "Open search" }).click();
  await page.locator('[data-suggest-query="label:app=web"]').click();
  await expect(page.locator("#palette-input")).toHaveValue("label:app=web");
  await page.locator("#palette-input").fill("label:app=web,");
  await expect(page.locator("#palette-results [role=alert]")).toContainText("invalid label selector");
});
