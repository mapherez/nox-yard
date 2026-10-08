#!/usr/bin/env node
// Deterministic user-visible regressions against the built frontend, no Docker.
import { spawn, spawnSync } from "node:child_process";
import { createServer } from "node:net";
import { mkdir, readFile, writeFile, rm } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { randomUUID } from "node:crypto";

const root = fileURLToPath(new URL("../", import.meta.url));
const session = "regression-" + randomUUID();
const temp = path.join(root, ".tmp", session);
const tests = ["smoke-history.cjs", "smoke-recreate.cjs", "smoke-schedules.cjs", "smoke-sessions.cjs"];
const port = await new Promise((resolve, reject) => {
 const server = createServer(); server.on("error", reject);
 server.listen(0, "127.0.0.1", () => { const port = server.address().port; server.close(() => resolve(port)); });
});
const url = `http://127.0.0.1:${port}`;
function cli(...args) {
 // Windows requires cmd for npx.cmd; arguments contain only fixed commands,
 // generated session IDs and relative fixture paths (no user input).
 const parts = ["--yes", "--package", "@playwright/cli@0.1.22", "playwright-cli", `-s=${session}`, ...args];
 const result = process.platform === "win32"
  ? spawnSync("cmd.exe", ["/d", "/s", "/c", "npx.cmd " + parts.join(" ")], {cwd: root, encoding: "utf8", timeout: 180_000})
  : spawnSync("npx", parts, {cwd: root, encoding: "utf8", timeout: 180_000});
 if (result.error || result.status !== 0 || /### Error\b/.test(result.stdout ?? "")) {
  throw new Error((result.stdout ?? "") + (result.stderr ?? "") + (result.error?.message ?? "CLI failed"));
 }
 return result.stdout;
}
let preview;
let opened = false;
try {
 await mkdir(temp, {recursive: true});
 await mkdir(path.join(root, "output/playwright"), {recursive: true});
 await readFile(path.join(root, "web/dist/index.html"));
 preview = spawn(process.execPath, [path.join(root, "web/node_modules/vite/bin/vite.js"), "preview", "--host", "127.0.0.1", "--port", String(port), "--strictPort"], {cwd: path.join(root, "web"), stdio: "ignore"});
 for (let attempt = 0; ; attempt++) {
  try { if ((await fetch(url)).ok) break; } catch {}
  if (attempt >= 100 || preview.exitCode !== null) throw new Error("Frontend preview did not become ready");
  await new Promise(resolve => setTimeout(resolve, 100));
 }
 cli("open", "about:blank", "--browser", "chrome"); opened = true;
 for (const name of tests) {
  const code = (await readFile(path.join(root, "scripts", name), "utf8")).replaceAll("http://127.0.0.1:5173", url);
  const relative = `.tmp/${session}/${name}`;
  await writeFile(path.join(root, relative), code);
  const output = cli("run-code", "--filename", relative);
  if (!output.includes("PASS:")) throw new Error(name + " did not record successful assertions: " + output);
  console.log("[pass] " + name);
 }
} finally {
 if (opened) { try { cli("close"); } catch (error) {console.error(error.message);} }
 preview?.kill();
 await rm(temp, {recursive: true, force: true});
}
