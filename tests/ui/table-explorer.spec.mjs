import { test, expect } from "./fixtures/application.mjs";


test.beforeEach(async ({ app, page }) => {
  await page.goto(app.url);
  await expect(page.locator("#content")).toContainText("Cluster Overview");
});

test("table explorer composes stable multi-sort priorities and supports keyboard removal", async ({ page }) => {
  await page.locator('.sidebar-link[data-resource="pods"]').click();
  await expect(page.locator("#list-pane #kui-table")).toBeVisible();
  const table = page.locator("#list-pane #kui-table");
  const status = table.locator("thead th").filter({ hasText: /^State$/ });
  const name = table.locator("thead th").filter({ hasText: /^Name$/ });
  await status.click();
  await name.press("Shift+Enter");
  await expect(page.locator(".kui-sort-priority li")).toHaveCount(2);
  await expect(status).toHaveAttribute("aria-sort", "ascending");
  await expect(name).toHaveAttribute("aria-label", /sort priority 2/);
  const values = await table.locator("tbody tr[data-name]").evaluateAll((rows) => {
    const headers = Array.from(rows[0].closest("table").querySelectorAll("thead th"));
    const statusIndex = headers.findIndex((cell) => cell.textContent.trim() === "State");
    return rows.map((row) => [row.cells[statusIndex].textContent.trim(), row.dataset.name]);
  });
  expect(values).toEqual([...values].sort((a, b) => a[0].localeCompare(b[0]) || a[1].localeCompare(b[1])));
  await name.focus();
  await page.keyboard.press("Shift+Enter");
  await expect(name).toHaveAttribute("aria-sort", "descending");
  await page.getByRole("button", { name: /Remove sort priority 1/ }).click();
  await expect(page.locator(".kui-sort-priority li")).toHaveCount(1);
  await page.getByRole("button", { name: "Clear sort keys" }).click();
  await expect(page.locator(".kui-sort-priority li")).toHaveCount(0);
});

test("filter modes, selected-only counts, and scoped pins remain safe", async ({ page }) => {
  await page.locator('.sidebar-link[data-resource="pods"]').click();
  await expect(page.locator("#list-pane #kui-table")).toBeVisible();
  const rows = page.locator("#list-pane #kui-table tbody tr[data-name]");
  await page.locator("#list-pane #row-filter").fill("ALPHA");
  const alpha = page.locator('#list-pane #kui-table tbody tr[data-name="alpha"]');
  await expect(alpha).toBeVisible();
  await page.locator('[data-explorer="case"]').check();
  await expect(alpha).toBeHidden();
  await expect(page.locator("#list-pane [data-filter-empty]")).toBeVisible();
  await page.locator('[data-explorer="case"]').uncheck();
  await page.locator('[data-explorer="regex"]').check();
  await page.locator("#list-pane #row-filter").fill("^alpha");
  await expect(alpha).toBeVisible();
  await expect(page.locator("#list-pane #kui-table tbody tr[data-name]:visible")).toHaveCount(1);
  await page.locator("#list-pane #row-filter").fill("[");
  await expect(page.locator("#kui-explorer-status")).toContainText("Invalid regular expression");
  await expect(alpha).toBeVisible();
  await expect(rows).toHaveCount(8);
  await page.locator('[data-explorer="regex"]').uncheck();
  await page.locator("#list-pane #row-filter").fill("");
  await page.getByRole("searchbox", { name: "Name column filter" }).fill("alpha");
  await expect(page.locator("#list-pane #kui-table tbody tr[data-name]:visible")).toHaveCount(1);
  await expect(page.locator(".kui-explorer-counts")).toContainText("1 matching");
  await page.getByRole("searchbox", { name: "Name column filter" }).fill("");
  await alpha.locator(".row-select").check();
  await page.locator('[data-explorer="selected"]').check();
  await expect(page.locator("#list-pane #kui-table tbody tr[data-name]:visible")).toHaveCount(1);
  await expect(page.locator(".kui-explorer-counts")).toContainText("1 selected");
  await page.locator('[data-explorer="selected"]').uncheck();
  await page.locator('#list-pane #kui-table tbody tr[data-name="alpha"] [data-explorer="pin"]').click();
  await expect(page.locator('#list-pane #kui-table tbody tr[data-name="alpha"]')).toHaveClass(/kui-pinned-row/);
  await page.locator("#namespace-select").selectOption("qa");
  await expect(page).toHaveURL(/\/qa\/pods/);
  await expect(page.locator('#list-pane #kui-table tbody tr[data-name="qa-only"]')).not.toHaveClass(/kui-pinned-row/);
  await page.locator("#namespace-select").selectOption("dev");
  await expect(page.locator('#list-pane #kui-table tbody tr[data-name="alpha"]')).toHaveClass(/kui-pinned-row/);
  await page.reload();
  await page.locator('.sidebar-link[data-resource="pods"]').click();
  await expect(page.locator('#list-pane #kui-table tbody tr[data-name="alpha"]')).toHaveClass(/kui-pinned-row/);
});

test("selected JSON downloads structured values and TSV copies quoted cell values", async ({ page, context }) => {
  await page.locator('.sidebar-link[data-resource="pods"]').click();
  await expect(page.locator("#list-pane #kui-table")).toBeVisible();
  await page.locator('#list-pane #kui-table tbody tr[data-name="alpha"] .row-select').check();
  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export selected JSON" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("selected-resources.json");
  const fs = await import("node:fs/promises");
  const json = JSON.parse(await fs.readFile(await download.path(), "utf8"));
  expect(json).toHaveLength(1);
  expect(json[0]).toMatchObject({ namespace: "dev", Name: "alpha" });
  expect(json[0].id).toContain("/pods/alpha");
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.getByRole("button", { name: "Copy selected TSV" }).click();
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain('"Name"');
  const tsv = await page.evaluate(() => navigator.clipboard.readText());
  expect(tsv).toContain('"alpha"');
  expect(tsv.split("\r\n")).toHaveLength(2);
  expect(tsv).not.toContain("Copy resource name");
  await page.evaluate(() => Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async () => { throw new Error("denied"); } } }));
  await page.getByRole("button", { name: "Copy selected TSV" }).click();
  await expect(page.locator("#kui-explorer-status")).toContainText("Clipboard access was denied");
});
