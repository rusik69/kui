import { readFile } from "node:fs/promises";
import { test, expect } from "./fixtures/application.mjs";


async function installAppearance(page, colorScheme = "light") {
  await page.emulateMedia({ colorScheme });
  await expect(page.getByRole("button", { name: "Appearance settings" })).toBeVisible();
}

async function openSettings(page) {
  await page.getByRole("button", { name: "Appearance settings" }).click();
  return page.getByRole("dialog", { name: "Appearance preferences" });
}
function contrastRatio(foreground, background) {
  const luminance = (color) => {
    const channels = color.match(/\d+/g).slice(0, 3).map((part) => Number(part) / 255).map((part) => part <= 0.04045 ? part / 12.92 : ((part + 0.055) / 1.055) ** 2.4);
    return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
  };
  const values = [luminance(foreground), luminance(background)].sort((a, b) => b - a);
  return (values[0] + 0.05) / (values[1] + 0.05);
}

test("system theme follows live OS changes and custom preference persists", async ({ app, page }) => {
  await page.goto(app.url);
  await installAppearance(page, "light");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await page.emulateMedia({ colorScheme: "dark" });
  await expect.poll(() => page.evaluate(() => localStorage.getItem("kui.theme"))).toBe("dark");
  const dialog = await openSettings(page);
  await dialog.locator("[name=theme]").selectOption("light");
  await dialog.locator("[name=accent]").evaluate((input) => { input.value = "#cc5500"; input.dispatchEvent(new Event("change", { bubbles: true })); });
  await dialog.locator("[name=fontScale]").selectOption("1.1");
  await dialog.locator("[name=spacing]").selectOption("compact");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect(page.locator("html")).toHaveAttribute("data-spacing", "compact");
  await expect(page.locator("html")).toHaveCSS("--appearance-font-scale", "1.1");
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem("kui.appearance.v1")))).toMatchObject({ version: 1, theme: "light", accent: "#cc5500", fontScale: 1.1, spacing: "compact" });
  const downloadPromise = page.waitForEvent("download");
  await dialog.getByRole("button", { name: "Export" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("kui-appearance-v1.json");
  const exported = JSON.parse(await readFile(await download.path(), "utf8"));
  expect(exported).toEqual({ version: 1, theme: "light", accent: "#cc5500", fontScale: 1.1, spacing: "compact" });
  await page.reload();
  await installAppearance(page, "dark");
  const toggle = page.locator("#theme-toggle");
  await expect(toggle).toHaveAttribute("aria-pressed", "true");
  await expect(toggle.locator("use")).toHaveAttribute("href", "#icon-sun");
  await expect.poll(() => page.evaluate(() => localStorage.getItem("kui.theme"))).toBe("light");
  await toggle.click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(toggle).toHaveAttribute("aria-pressed", "false");
  await expect(toggle.locator("use")).toHaveAttribute("href", "#icon-moon");
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem("kui.appearance.v1")).theme)).toBe("dark");
  await expect.poll(() => page.evaluate(() => localStorage.getItem("kui.theme"))).toBe("dark");
});

test("invalid appearance import is atomic and reset requires confirmation", async ({ app, page }) => {
  await page.goto(app.url);
  await installAppearance(page);
  const dialog = await openSettings(page);
  await dialog.locator("[name=theme]").selectOption("dark");
  await dialog.locator("[name=spacing]").selectOption("compact");
  const before = await page.evaluate(() => localStorage.getItem("kui.appearance.v1"));
  await dialog.locator(".appearance-file").setInputFiles({ name: "bad.json", mimeType: "application/json", buffer: Buffer.from('{"version":1,"theme":"dark","accent":"url(javascript:alert(1))","fontScale":99,"spacing":"compact"}') });
  await expect(dialog.locator(".appearance-status")).toContainText("Nothing was changed");
  expect(await page.evaluate(() => localStorage.getItem("kui.appearance.v1"))).toBe(before);
  page.once("dialog", (confirm) => confirm.dismiss());
  await dialog.getByRole("button", { name: "Reset appearance" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  page.once("dialog", (confirm) => confirm.accept());
  await dialog.getByRole("button", { name: "Reset appearance" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem("kui.appearance.v1")))).toMatchObject({ theme: "system", spacing: "comfortable", fontScale: 1 });
});

test("keyboard and touch tooltips are visible, dialogs restore focus, and motion/forced-colors adapt", async ({ app, page }) => {
  await page.goto(app.url);
  await installAppearance(page);
  const toggle = page.locator("#theme-toggle");
  for (let index = 0; index < 80; index++) {
    if (await toggle.evaluate((element) => element === document.activeElement)) break;
    await page.keyboard.press("Tab");
  }
  await expect(toggle).toBeFocused();
  await expect.poll(() => toggle.evaluate((element) => Number(getComputedStyle(element, "::after").opacity))).toBeGreaterThan(0);
  const tooltip = await toggle.evaluate((element) => {
    const style = getComputedStyle(element, "::after");
    return { opacity: style.opacity, color: style.color, background: style.backgroundColor };
  });
  expect(Number(tooltip.opacity)).toBeGreaterThan(0);
  expect(contrastRatio(tooltip.color, tooltip.background)).toBeGreaterThanOrEqual(4.5);
  await toggle.dispatchEvent("pointerup", { pointerType: "touch" });
  await toggle.evaluate((element) => element.blur());
  await expect.poll(() => toggle.evaluate((element) => Number(getComputedStyle(element, "::after").opacity))).toBeGreaterThan(0);

  const launcher = page.getByRole("button", { name: "Appearance settings" });
  await launcher.focus();
  await page.keyboard.press("Enter");
  const dialog = page.getByRole("dialog", { name: "Appearance preferences" });
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(launcher).toBeFocused();
  await page.emulateMedia({ reducedMotion: "reduce", forcedColors: "active" });
  await launcher.focus();
  const adaptations = await launcher.evaluate((element) => ({
    transitionDuration: getComputedStyle(element, "::after").transitionDuration,
    outlineWidth: getComputedStyle(element).outlineWidth,
    outlineStyle: getComputedStyle(element).outlineStyle,
  }));
  expect(parseFloat(adaptations.transitionDuration)).toBeLessThanOrEqual(0.001);
  expect(adaptations.outlineStyle).not.toBe("none");
  expect(parseFloat(adaptations.outlineWidth)).toBeGreaterThanOrEqual(2);
});

test("light, dark, and high contrast surfaces retain readable colors at narrow widths", async ({ app, page }) => {
  await page.goto(app.url);
  await installAppearance(page, "light");
  const dialog = await openSettings(page);
  for (const [mode, expectedBackground, expectedText] of [["light", "rgb(245, 246, 248)", "rgb(26, 29, 35)"], ["dark", "rgb(27, 30, 35)", "rgb(228, 230, 235)"], ["high-contrast", "rgb(8, 8, 8)", "rgb(255, 255, 255)"]]) {
    await dialog.locator("[name=theme]").selectOption(mode);
    await expect(page.locator("html")).toHaveAttribute("data-theme", mode);
    const colors = await dialog.evaluate((element) => {
      const style = getComputedStyle(element);
      const root = getComputedStyle(document.documentElement);
      return {
        background: style.backgroundColor,
        text: style.color,
        accent: root.getPropertyValue("--accent").trim(),
        status: [root.getPropertyValue("--green"), root.getPropertyValue("--yellow"), root.getPropertyValue("--red")],
      };
    });
    expect(colors.background).toBe(expectedBackground);
    expect(colors.text).toBe(expectedText);
    expect(contrastRatio(colors.text, colors.background)).toBeGreaterThanOrEqual(4.5);
    expect(new Set(colors.status).size).toBe(3);
    if (mode === "high-contrast") expect(colors.accent).toBe("#ff0");
  }
  for (const width of [1280, 768, 320]) {
    await page.setViewportSize({ width, height: 800 });
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveCSS("width", width <= 420 ? `${width - 24}px` : "460px");
  }
});

test("legacy explicit theme preferences migrate without losing the chosen mode", async ({ app, page }) => {
  await page.addInitScript(() => localStorage.setItem("kui.theme", "dark"));
  await page.goto(app.url);
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem("kui.appearance.v1")).theme)).toBe("dark");
});
