import { test, expect } from "./fixtures/application.mjs";

test.beforeEach(async ({ app, page }) => {
  await page.goto(app.url);
  await expect(page.locator("#content")).toContainText("Cluster Overview");
});

test("create dialog scaffolds, validates and creates a ConfigMap", async ({ page }) => {
  await page.locator('.sidebar-link[data-resource="configmaps"]').click();
  const table = page.locator("#list-pane #kui-table");
  await expect(table).toBeVisible();

  const createButton = page.locator(".table-create-button");
  await expect(createButton).toContainText("Create ConfigMap");
  await createButton.click();

  const dialog = page.getByRole("dialog", { name: /Create ConfigMap/ });
  await expect(dialog).toBeVisible();
  const editor = dialog.locator("#create-editor");
  await expect(editor).toHaveValue(/kind: ConfigMap/);
  await expect(editor).toHaveValue(/namespace: dev/);

  await dialog.locator("#create-validate").click();
  await expect(dialog.locator("#create-status")).toContainText("validation passed");

  await dialog.locator("#create-submit").click();
  await expect(dialog.locator("#create-status")).toContainText("Created.");
});

test("create dialog surfaces server-side validation errors", async ({ page }) => {
  await page.locator('.sidebar-link[data-resource="deployments"]').click();
  await expect(page.locator("#list-pane #kui-table")).toBeVisible();
  await page.locator(".table-create-button").click();

  const dialog = page.getByRole("dialog", { name: /Create Deployment/ });
  await expect(dialog).toBeVisible();
  const editor = dialog.locator("#create-editor");
  // Wait for the async scaffold before overwriting it.
  await expect(editor).toHaveValue(/kind: Deployment/, { timeout: 10000 });

  // A Deployment without a spec.selector template fails the server-side
  // dry-run; the error surfaces inline and no object is created.
  await editor.fill("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: rejected\n  namespace: dev\nspec: {}\n");
  await dialog.locator("#create-validate").click();
  await expect(dialog.locator("#create-status")).toContainText(/dry-run failed|Request failed/);
});

test("create dialog rejects a kind mismatch before touching the cluster", async ({ page }) => {
  await page.locator('.sidebar-link[data-resource="configmaps"]').click();
  await page.locator(".table-create-button").click();
  const dialog = page.getByRole("dialog", { name: /Create ConfigMap/ });
  await expect(dialog).toBeVisible();
  const editor = dialog.locator("#create-editor");
  // Wait for the async scaffold before overwriting it.
  await expect(editor).toHaveValue(/kind: ConfigMap/, { timeout: 10000 });
  await editor.fill("apiVersion: v1\nkind: Pod\nmetadata:\n  name: wrong-kind\n  namespace: dev\n");
  await dialog.locator("#create-submit").click();
  await expect(dialog.locator("#create-status")).toContainText("does not match resource kind");
});
