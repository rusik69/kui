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
  page.once("dialog", dialog => dialog.accept("My workspace"));
  await strip.getByRole("button", { name: "Rename tab" }).last().click();
  await expect(strip.getByRole("tab", { name: /My workspace/ })).toBeVisible();
  await strip.getByRole("button", { name: "Pin tab" }).last().click();
  await strip.getByRole("button", { name: "Duplicate workspace" }).click();
  await expect(strip.getByRole("tab")).toHaveCount(3);
  const second = strip.locator(".workspace-tab").nth(1).getByRole("tab");
  await second.focus();
  await second.press("Alt+ArrowRight");
  await expect(page.locator("#workspace-announcement")).toContainText("moved to position");
  await strip.getByRole("button", { name: "Close tab" }).last().click();
  await strip.getByRole("button", { name: "Reopen closed workspace" }).click();
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
  await strip.getByRole("button", { name: "Close tab" }).last().click();
  await expect(strip.getByRole("tab")).toHaveCount(2);
  page.once("dialog", dialog => dialog.accept("save"));
  await strip.getByRole("button", { name: "Close tab" }).last().click();
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
  await strip.getByRole("button", { name: "Close tab" }).last().click();
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
  page.once("dialog", dialog => dialog.accept("cancel"));
  await strip.getByRole("button", { name: "Close tab" }).last().click();
  await expect(strip.getByRole("tab")).toHaveCount(2);
  page.once("dialog", dialog => dialog.accept("discard"));
  await strip.getByRole("button", { name: "Close tab" }).last().click();
  await expect(strip.getByRole("tab")).toHaveCount(1);
});
