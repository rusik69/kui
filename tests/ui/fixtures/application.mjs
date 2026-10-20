import { spawn } from "node:child_process";
import { mkdtemp, rm, writeFile, access } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as delay } from "node:timers/promises";
import { test as base, expect } from "@playwright/test";
import { startFakeKubernetes } from "./fake-kubernetes.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const defaultBinary = resolve(root, "bin/kui");

async function stopChild(child) {
  if (child.exitCode !== null || child.signalCode !== null) return;
  const exited = new Promise((resolveExit) => child.once("exit", resolveExit));
  child.kill("SIGTERM");
  await Promise.race([exited, delay(4000)]);
  if (child.exitCode === null && child.signalCode === null) {
    child.kill("SIGKILL");
    await Promise.race([exited, delay(1000)]);
  }
}

async function waitForAddress(child, getOutput) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const match = /kui listening on (http:\/\/127\.0\.0\.1:\d+)/.exec(getOutput());
    if (match) return match[1];
    if (child.exitCode !== null || child.signalCode !== null) {
      throw new Error(`kui exited before serving (code=${child.exitCode}, signal=${child.signalCode}):\n${getOutput()}`);
    }
    await delay(25);
  }
  throw new Error(`timed out waiting for kui to serve:\n${getOutput()}`);
}

export const test = base.extend({
  app: async ({}, use) => {
    const binary = process.env.KUI_BINARY || defaultBinary;
    await access(binary).catch(() => {
      throw new Error(`kui binary not found at ${binary}; build it with make build before running the UI suite`);
    });

    const api = await startFakeKubernetes();
    let directory;
    let child;
    try {
      directory = await mkdtemp(resolve(tmpdir(), "kui-playwright-"));
      const kubeconfig = resolve(directory, "config");
      await writeFile(kubeconfig, `apiVersion: v1
kind: Config
clusters:
- name: fixture
  cluster:
    server: ${api.url}
    insecure-skip-tls-verify: true
contexts:
- name: ui-test
  context:
    cluster: fixture
    user: ui-test
    namespace: dev
- name: ui-test-alt
  context:
    cluster: fixture
    user: ui-test
    namespace: dev
users:
- name: ui-test
  user:
    token: deterministic-ui-test-token
current-context: ui-test
`, { mode: 0o600 });

      child = spawn(binary, [
        "--headless",
        "--addr=127.0.0.1:0",
        `--kubeconfig=${kubeconfig}`,
        "--namespace=dev",
      ], {
        cwd: root,
        env: { ...process.env, HOME: directory, KUBECONFIG: kubeconfig },
        stdio: ["ignore", "pipe", "pipe"],
      });
      let output = "";
      child.stdout.setEncoding("utf8").on("data", (chunk) => { output += chunk; });
      child.stderr.setEncoding("utf8").on("data", (chunk) => { output += chunk; });
      let spawnError;
      child.on("error", (error) => { spawnError = error; });

      const url = await waitForAddress(child, () => {
        if (spawnError) output += `${spawnError.message}\n`;
        return output;
      });
      const healthDeadline = Date.now() + 5000;
      let healthy = false;
      while (Date.now() < healthDeadline) {
        try {
          const response = await fetch(`${url}/healthz`);
          if (response.ok) {
            healthy = true;
            break;
          }
        } catch {
          // The log line may precede the listener becoming reachable.
        }
        await delay(25);
      }
      if (!healthy) throw new Error(`kui did not become healthy after startup:\n${output}`);
      await use({ url, api, process: child });
    } finally {
      if (child) await stopChild(child);
      await api.close();
      if (directory) await rm(directory, { recursive: true, force: true });
    }
  },
});

export { expect };

export async function openTableFilters(page) {
  const filters = page.locator("#list-pane .table-filter-panel");
  if (!(await filters.count())) return null;
  if (!(await filters.evaluate(element => element.open))) await filters.locator(":scope > summary").click();
  return filters;
}
