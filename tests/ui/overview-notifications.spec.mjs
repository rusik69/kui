import { test, expect } from "./fixtures/application.mjs";

async function expandNotifications(page) {
  const center = page.locator(".notification-center");
  if (!(await center.evaluate(element => element.open))) await center.locator("summary").click();
}

test.beforeEach(async ({ app, page }) => {
  await page.goto(app.url);
  await expect(page.locator("#overview")).toBeVisible();
});

test("notification center stays compact until opened and exposes unread count", async ({ page }) => {
  const center = page.locator(".notification-center");
  await expect(center).not.toHaveAttribute("open");
  await expect(page.getByRole("heading", { name: "Notifications" })).toBeVisible();
  await expect(page.locator("#notification-list")).toBeHidden();
  await expect(page.locator("#notification-unread")).toHaveText(/\d+ unread/);
  await expandNotifications(page);
  await expect(page.locator("#notification-list")).toBeVisible();
});

test("expanded notifications stay within a narrow viewport", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await expandNotifications(page);
  const layout = await page.evaluate(() => ({
    documentWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
    centerRight: document.querySelector(".notification-center").getBoundingClientRect().right,
    inputRights: [...document.querySelectorAll(".notification-controls input[type=search]")].map(input => input.getBoundingClientRect().right),
  }));
  expect(layout.documentWidth).toBe(layout.clientWidth);
  expect(layout.centerRight).toBeLessThanOrEqual(layout.clientWidth);
  expect(layout.inputRights.every(right => right <= layout.clientWidth)).toBeTruthy();
});

test("overview refresh controls and snapshot exports are scoped and accessible", async ({ page }) => {
  const interval = page.getByRole("combobox", { name: "Overview refresh interval" });
  await interval.selectOption("30");
  await expect(interval).toHaveValue("30");
  await page.getByRole("button", { name: "Pause refresh" }).click();
  await expect(page.getByRole("button", { name: "Resume refresh" })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Copy health summary" }).click();
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export JSON" }).click();
  expect((await download).suggestedFilename()).toBe("kui-overview.json");
});

test("capacity thresholds validate range and event summaries can be filtered", async ({ page }) => {
  const threshold = page.getByRole("spinbutton", { name: "Warning threshold percentage" });
  await threshold.fill("101");
  await page.getByRole("button", { name: "Save threshold" }).click();
  await expect(threshold).toHaveAttribute("aria-invalid", "true");
  await threshold.fill("75");
  await page.getByRole("button", { name: "Save threshold" }).click();
  await expect(threshold).not.toHaveAttribute("aria-invalid", "true");
  const filter = page.getByRole("searchbox", { name: "Filter event summaries by type, reason, or message" });
  await filter.fill("unlikely-event-text");
  await expect(page.locator("[data-event-group]:visible")).toHaveCount(0);
  await filter.fill("Container app started");
  await expect(page.locator("#event-summary-body [data-event-group]:visible")).toHaveCount(1);
});

test("notification center retains read state and quiet preference", async ({ page }) => {
  await expect(page.getByRole("heading", { name: /Notifications/ })).toBeVisible();
  await expandNotifications(page);

  const quiet = page.getByRole("checkbox", { name: "Quiet mode" });
  await quiet.check();
  await page.reload();
  await expandNotifications(page);
  await expect(page.getByRole("checkbox", { name: "Quiet mode" })).toBeChecked();
  const key = `kui.notifications.v1:${encodeURIComponent(await page.locator("#overview").getAttribute("data-context"))}:dev`;
  await expect.poll(() => page.evaluate(storageKey => JSON.parse(localStorage.getItem(storageKey) || "{\"items\":[]}").items.length, key)).toBeGreaterThan(0);
  const first = page.locator("#notification-list li").first();
  await first.getByRole("button", { name: "Mark read" }).click();
  await expect(first.getByRole("button", { name: "Mark unread" })).toBeVisible();
  await page.reload();
  await expandNotifications(page);
  await expect(page.getByRole("checkbox", { name: "Quiet mode" })).toBeChecked();
  await expect(page.locator("#notification-list li").first().getByRole("button", { name: "Mark unread" })).toBeVisible();
  const severity = page.locator("#notification-severity");
  await severity.selectOption("Warning");
  const warningCount = await page.evaluate(storageKey => JSON.parse(localStorage.getItem(storageKey)).items.filter(item => item.severity === "Warning").length, key);
  await expect(page.locator("#notification-list li")).toHaveCount(warningCount);
  await page.reload();
  await expandNotifications(page);
  await expect(page.locator("#notification-severity")).toHaveValue("Warning");
});
test("clear all notifications requires confirmation and supports cancellation", async ({ page }) => {
  await expect.poll(() => page.locator("#notification-list li").count()).toBeGreaterThan(0);
  await expandNotifications(page);
  let count = await page.locator("#notification-list li").count();
  page.once("dialog", dialog => dialog.dismiss());
  await page.getByRole("button", { name: "Clear all" }).click();
  await expect(page.locator("#notification-list li")).toHaveCount(count);
  page.once("dialog", dialog => dialog.accept());
  await page.getByRole("button", { name: "Clear all" }).click();
  await expect(page.locator("#notification-list li")).toHaveCount(0);
});
test("notification filters include context, namespace, kind, and severity", async ({ page }) => {
  await expect.poll(() => page.locator("#notification-list li").count()).toBeGreaterThan(0);
  await expandNotifications(page);
  const count = await page.locator("#notification-list li").count();
  const contextFilter = page.getByRole("searchbox", { name: "Filter notifications by context" });
  await contextFilter.fill("no-matching-context");
  await expect(page.locator("#notification-list li")).toHaveCount(0);
  await contextFilter.clear();
  await expect(page.locator("#notification-list li")).toHaveCount(count);
  await page.getByRole("searchbox", { name: "Filter notifications by namespace" }).fill("no-such-namespace");
  await expect(page.locator("#notification-list li")).toHaveCount(0);
});

test("quiet mode suppresses event toasts but preserves ingested notifications", async ({ page }) => {
  const context = await page.locator("#overview").getAttribute("data-context");
  const key = `kui.notifications.v1:${encodeURIComponent(context)}:dev`;
  await page.evaluate(storageKey => localStorage.setItem(storageKey, JSON.stringify({ items: [], severities: ["Normal", "Warning"], quiet: true })), key);
  await page.reload();
  await expect.poll(() => page.locator("#notification-list li").count()).toBeGreaterThan(0);
  await expect(page.locator("#toast-container")).not.toContainText("added to notifications");
});

test("malformed notification storage is discarded without breaking the center", async ({ page }) => {
  const context = await page.locator("#overview").getAttribute("data-context");
  const key = `kui.notifications.v1:${encodeURIComponent(context)}:dev`;
  await page.evaluate(storageKey => localStorage.setItem(storageKey, JSON.stringify({ items: [{ id: "bad", namespace: null, kind: 4 }], severities: ["Bogus"] })), key);
  await page.reload();
  await expect(page.getByRole("heading", { name: /Notifications/ })).toBeVisible();
  await expandNotifications(page);
  await expect(page.locator("#notification-severity")).toHaveValue("all");
  await expect(page.locator("#notification-list li").first()).toContainText(/Warning|Normal/);
});
test("new events use a toast when quiet mode is off", async ({ page }) => {
  const context = await page.locator("#overview").getAttribute("data-context");
  const key = `kui.notifications.v1:${encodeURIComponent(context)}:dev`;
  await page.evaluate(storageKey => localStorage.setItem(storageKey, JSON.stringify({ items: [], severities: ["Normal", "Warning"], quiet: false })), key);
  await page.reload();
  await expect(page.locator("#toast-container .toast").filter({ hasText: "added to notifications" })).toHaveCount(1);
  await expect.poll(() => page.locator("#notification-list li").count()).toBeGreaterThan(0);
});
test("event summaries expand into matching live event records", async ({ page }) => {
  const eventRow = page.locator("#event-summary-body [data-event-group]").first();
  await expect(eventRow).toBeVisible();
  const reason = await eventRow.getAttribute("data-reason");
  const toggle = eventRow.getByRole("button");
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  const records = eventRow.locator("xpath=following-sibling::tr[1]");
  await expect(records).toBeVisible();
  await expect(records.locator("[data-event-record]")).toHaveCount(1);
  await expect(records.locator("[data-event-record]").first()).toHaveAttribute("data-reason", reason);
  await expect(records.locator("[data-event-record]").first()).toHaveAttribute("data-event-uid", /.+/);
  await expect(records.locator("[data-event-record]").first()).toHaveAttribute("data-event-resource-version", /.+/);
});

test("notification identity uses event UID and resourceVersion across repeated ingestion", async ({ page }) => {
  const response = await page.request.get(new URL("/c/" + encodeURIComponent(await page.locator("#overview").getAttribute("data-context")) + "/dev/overview?frame=event-records", page.url()).toString());
  expect(response.ok()).toBeTruthy();
  const events = await response.json();
  expect(events.length).toBeGreaterThan(0);
  for (const event of events) {
    expect(event.uid).toBeTruthy();
    expect(event.resourceVersion).toBeTruthy();
  }
  const storageKey = `kui.notifications.v1:${encodeURIComponent(await page.locator("#overview").getAttribute("data-context"))}:dev`;
  await expect.poll(() => page.evaluate(key => {
    const raw = JSON.parse(localStorage.getItem(key) || "{\"items\":[]}");
    return raw.items.length;
  }, storageKey)).toBeGreaterThan(0);
  const firstCount = await page.evaluate(key => JSON.parse(localStorage.getItem(key)).items.length, storageKey);
  await page.evaluate(() => document.body.dispatchEvent(new CustomEvent("htmx:sseMessage", { detail: { type: "message" } })));
  await expect.poll(() => page.evaluate(key => JSON.parse(localStorage.getItem(key)).items.length, storageKey)).toBe(firstCount);
});
test("watch errors are persisted as accessible notifications", async ({ page }) => {
  await page.evaluate(() => document.body.dispatchEvent(new CustomEvent("htmx:sseMessage", {
    detail: { type: "watch-error", data: "Live updates stopped after a watch API error." },
  })));
  const errorNotification = page.locator("#notification-list li[data-severity='Error']");
  await expect(errorNotification).toContainText("Watch error");
  await expect(errorNotification).toContainText("watch API error");
  await page.reload();
  await expect(page.locator("#notification-list li[data-severity='Error']")).toContainText("Watch error");
});
test("overview refresh monitor is reused when overview roots are replaced", async ({ page }) => {
  const activeTimers = await page.evaluate(() => {
    const nativeSet = window.setInterval.bind(window), nativeClear = window.clearInterval.bind(window);
    const active = new Set();
    window.setInterval = (callback, delay, ...args) => {
      const id = nativeSet(callback, delay, ...args);
      if (delay === 1000) active.add(id);
      return id;
    };
    window.clearInterval = id => { active.delete(id); nativeClear(id); };
    for (let i = 0; i < 3; i++) {
      const current = document.querySelector("#overview");
      const replacement = current.cloneNode(true);
      delete replacement.dataset.extrasReady;
      current.replaceWith(replacement);
      document.dispatchEvent(new CustomEvent("htmx:afterSwap", { detail: { target: replacement } }));
    }
    return active.size;
  });
  expect(activeTimers).toBe(1);
});
