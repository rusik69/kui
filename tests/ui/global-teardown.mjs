import { rm } from "node:fs/promises";

export default async function globalTeardown(config) {
  const outputDirs = new Set(config.projects.map((project) => project.outputDir));
  await Promise.all([...outputDirs].map((outputDir) => rm(outputDir, { recursive: true, force: true })));
}
