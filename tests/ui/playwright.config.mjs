import { defineConfig } from "@playwright/test";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const configDirectory = dirname(fileURLToPath(import.meta.url));
const outputDir = join(tmpdir(), `kui-ui-playwright-${process.pid}`);

export default defineConfig({
  testDir: configDirectory,
  testMatch: "**/*.spec.mjs",
  outputDir,
  globalTeardown: join(configDirectory, "global-teardown.mjs"),
  fullyParallel: true,
  workers: 2,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  reporter: "list",
  timeout: 45000,
  expect: { timeout: 8000 },
  use: {
    launchOptions: process.env.KUI_CHROMIUM_EXECUTABLE ? { executablePath: process.env.KUI_CHROMIUM_EXECUTABLE } : {},
    browserName: process.env.KUI_BROWSER || "chromium",
    viewport: { width: 1440, height: 1000 },
    locale: "en-US",
    timezoneId: "UTC",
    colorScheme: "dark",
    reducedMotion: "reduce",
    trace: "off",
    screenshot: "off",
    video: "off",
  },
});
