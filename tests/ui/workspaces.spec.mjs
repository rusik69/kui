import { test, expect } from "./fixtures/application.mjs";

test.beforeEach(async ({ app, page }) => {
  await page.goto(app.url);
  await expect(page.locator("#content")).toContainText("Cluster Overview");
});

test("workspace tabs isolate routes and restore bounded session state", async ({ page }) => {
  await expect(page.getByRole("navigation", { name: "Workspaces" })).toBeVisible();
  const strip = page.getByRole("navigation", { name: "Workspaces" });
  await strip.getByRole("button", { name: "New workspace tab" }).click();
  await expect(strip.getByRole("tab")).toHaveCount(2);
  await page.locator(".sidebar-link[data-resource='pods']").click();
  await expect(page).toHaveURL(/\/pods$/);
  await strip.getByRole("tab").first().click();
  await expect(page.locator("#overview")).toBeVisible();
  await strip.getByRole("tab").nth(1).click();
  await expect(page).toHaveURL(/\/pods$/);
  await page.reload();
  await expect(page.getByRole("navigation", { name: "Workspaces" }).getByRole("tab")).toHaveCount(2);
  await expect(page).toHaveURL(/\/pods$/);
});

test("workspace controls rename, pin, duplicate, reorder and reopen tabs", async ({ page }) => {
  const strip = page.getByRole("navigation", { name: "Workspaces" });
  await strip.getByRole("button", { name: "New workspace tab" }).click();
  // Per-tab actions live in the tab's "…" menu.
  const tabMenu = strip.locator(".workspace-tab").last();
  await tabMenu.getByRole("button", { name: /More actions for tab/ }).click();
  page.once("dialog", dialog => dialog.accept("My workspace"));
  await tabMenu.getByRole("menuitem", { name: "Rename tab" }).click();
  await expect(strip.getByRole("tab", { name: /My workspace/ })).toBeVisible();
  await tabMenu.getByRole("button", { name: /More actions for tab/ }).click();
  await tabMenu.getByRole("menuitem", { name: "Pin tab" }).click();
  // Strip-level actions live in the "…" overflow menu.
  // Pin re-rendered the strip; wait for the fresh overflow trigger, open the
  // menu once, and wait for the item before clicking.
  const overflow = strip.getByRole("button", { name: "More workspace actions" });
  await expect(overflow).toBeVisible();
  await overflow.click();
  const duplicate = strip.getByRole("menuitem", { name: "Duplicate workspace" });
  await expect(duplicate).toBeAttached();
  // The menu must escape the strip's overflow clipping: fully inside the
  // viewport, below the trigger.
  await expect(duplicate).toBeVisible();
  await expect.poll(async () => page.evaluate(() => {
    const menu = document.querySelector(".workspace-overflow .workspace-menu");
    const rect = menu.getBoundingClientRect();
    return rect.top > 0 && rect.bottom <= window.innerHeight && rect.width > 0;
  })).toBe(true);
  await duplicate.click();
  await expect(strip.getByRole("tab")).toHaveCount(3);
  const second = strip.locator(".workspace-tab").nth(1).getByRole("tab");
  await second.focus();
  await second.press("Alt+ArrowRight");
  await expect(page.locator("#workspace-announcement")).toContainText("moved to position");
  await strip.locator(".workspace-tab").last().getByRole("button", { name: /More actions for tab/ }).click();
  await strip.locator(".workspace-tab").last().getByRole("menuitem", { name: "Close tab" }).click();
  await overflow.click();
  await strip.getByRole("menuitem", { name: "Reopen closed workspace" }).click();
  await expect(strip.getByRole("tab")).toHaveCount(3);
});

test("corrupt workspace routes are discarded and sidebar preferences are keyboard operable", async ({ app, page }) => {
  await page.goto(app.url);
  await page.evaluate(() => localStorage.setItem("kui.workspaces.v1", JSON.stringify({version:1,active:0,tabs:[{id:"bad",title:"bad",route:"https://evil.invalid/c/a/b/pods"}]})));
  await page.reload();
  await expect(page.getByRole("navigation", { name: "Workspaces" }).getByRole("tab")).toHaveCount(1);
  const sidebar = page.locator(".sidebar");
  const filter = sidebar.getByRole("searchbox", { name: "Filter resources" });
  await filter.fill("nonexistent-resource");
  await expect(sidebar.locator("#sidebar-filter-empty")).toBeVisible();
  await sidebar.getByRole("button", { name: "Clear resource filter" }).focus();
  await page.keyboard.press("Enter");
  await expect(filter).toHaveValue("");
  const discovery = page.locator('[data-sidebar-group="discovery"]');
  await page.getByRole("button", { name: "Collapse Discovery group" }).click();
  await expect(discovery).toHaveClass(/group-collapsed/);
  await filter.fill("All resources");
  await expect(discovery).not.toHaveClass(/group-collapsed/);
  await sidebar.getByRole("button", { name: "Clear resource filter" }).click();
  await expect(discovery).toHaveClass(/group-collapsed/);
  const collapse = page.getByRole("button", { name: "Collapse sidebar" });
  await collapse.focus();
  await page.keyboard.press("Enter");
  await expect(page.locator(".sidebar")).toHaveClass(/collapsed/);
});
test("workspace state follows HTMX routes including object details and retains active tab after reorder", async ({ page }) => {
  const strip = page.getByRole("navigation", { name: "Workspaces" });
  await page.locator(".sidebar-link[data-resource='pods']").click();
  await expect(page).toHaveURL(/\/pods$/);
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem("kui.workspaces.v1")).tabs[0].route)).toMatch(/\/pods$/);
  const row = page.locator("#kui-table tbody tr[data-name]").first();
  await expect(row).toBeVisible();
  const detailURL = await row.getAttribute("data-id");
  await row.focus();
  await row.press("Enter");
  await expect(page).toHaveURL(new URL(detailURL, page.url()).href);
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem("kui.workspaces.v1")).tabs[0].route)).toContain(detailURL.split("/").at(-1));
  await strip.getByRole("button", { name: "New workspace tab" }).click();
  await strip.getByRole("button", { name: "New workspace tab" }).click();
  const active = strip.getByRole("tab").nth(2);
  await active.focus();
  await active.press("Alt+ArrowLeft");
  await expect(page).toHaveURL(new URL(detailURL, page.url()).href);
  await expect(strip.getByRole("tab").nth(1)).toHaveAttribute("aria-selected", "true");
});

test("malformed routes do not shift the restored active workspace index", async ({ app, page }) => {
  await page.goto(app.url);
  await page.evaluate(() => {
    const current = location.pathname;
    localStorage.setItem("kui.workspaces.v1", JSON.stringify({version:1,active:1,tabs:[
      {id:"ok",title:"valid",route:current},{id:"bad",title:"invalid",route:"javascript:alert(1)"},
    ]}));
  });
  await page.reload();
  await expect(page.getByRole("navigation", { name: "Workspaces" }).getByRole("tab")).toHaveCount(1);
  await expect(page.getByRole("navigation", { name: "Workspaces" }).getByRole("tab")).toHaveAttribute("aria-selected", "true");
});

test("save completes a deferred draft close; discard closes and cancel preserves it", async ({ page }) => {
  const strip = page.getByRole("navigation", { name: "Workspaces" });
  const closeViaMenu = async () => {
    const shell = strip.locator(".workspace-tab").last();
    await shell.getByRole("button", { name: /More actions for tab/ }).click();
    await shell.getByRole("menuitem", { name: "Close tab" }).click();
  };
  await strip.getByRole("button", { name: "New workspace tab" }).click();
  await page.evaluate(() => {
    const form = document.createElement("form");
    form.innerHTML = '<input name="original" value="server"><textarea id="yaml-editor">draft</textarea><button type="submit">Review</button>';
    form.addEventListener("submit", event => {
      event.preventDefault();
      form.querySelector('input[name="original"]').value = form.querySelector("#yaml-editor").value;
      document.body.dispatchEvent(new Event("htmx:afterSettle"));
    });
    document.body.append(form);
  });
  page.once("dialog", dialog => dialog.accept("cancel"));
  await closeViaMenu();
  await expect(strip.getByRole("tab")).toHaveCount(2);
  page.once("dialog", dialog => dialog.accept("save"));
  await closeViaMenu();
  await expect(strip.getByRole("tab")).toHaveCount(1);
});
test("failed YAML review clears deferred close and leaves workspace open", async ({ page }) => {
  const strip = page.getByRole("navigation", { name: "Workspaces" });
  await strip.getByRole("button", { name: "New workspace tab" }).click();
  await page.evaluate(() => {
    const form = document.createElement("form");
    form.innerHTML = '<input name="original" value="server"><textarea id="yaml-editor">draft</textarea><button type="submit">Review</button>';
    const result = document.createElement("div");
    result.id = "yaml-result";
    result.className = "yaml-error";
    document.body.append(form, result);
    form.addEventListener("submit", event => {
      event.preventDefault();
      document.body.dispatchEvent(new CustomEvent("htmx:afterSettle", { detail: { target: result, elt: form } }));
    });
  });
  page.once("dialog", dialog => dialog.accept("save"));
  const tabShell = strip.locator(".workspace-tab").last();
  await tabShell.getByRole("button", { name: /More actions for tab/ }).click();
  await tabShell.getByRole("menuitem", { name: "Close tab" }).click();
  await expect(page.locator("#workspace-save-status")).toContainText("failed");
  await page.evaluate(() => {
    const editor = document.querySelector("#yaml-editor");
    editor.form.querySelector('input[name="original"]').value = editor.value;
    document.body.dispatchEvent(new CustomEvent("htmx:afterSettle", { detail: { target: document.body } }));
  });
  await expect(strip.getByRole("tab")).toHaveCount(2);
});

test("closing a tab with unsaved YAML requires discard, save, or cancel", async ({ page }) => {
  await page.evaluate(() => {
    const form = document.createElement("form"); form.id="draft-form";
    form.innerHTML='<input name="original" value="server"><textarea id="yaml-editor"></textarea><button type="submit">Review</button>';
    document.body.append(form); document.querySelector("#yaml-editor").value="draft";
  });
  const strip = page.getByRole("navigation", { name: "Workspaces" });
  await strip.getByRole("button", { name: "New workspace tab" }).click();
  const closeViaMenu = async () => {
    const shell = strip.locator(".workspace-tab").last();
    await shell.getByRole("button", { name: /More actions for tab/ }).click();
    await shell.getByRole("menuitem", { name: "Close tab" }).click();
  };
  page.once("dialog", dialog => dialog.accept("cancel"));
  await closeViaMenu();
  await expect(strip.getByRole("tab")).toHaveCount(2);
  page.once("dialog", dialog => dialog.accept("discard"));
  await closeViaMenu();
  await expect(strip.getByRole("tab")).toHaveCount(1);
});
