import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { once } from "node:events";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = new URL("../../../", import.meta.url);
const require = createRequire(new URL("docs/quickstart/package.json", root));
const { chromium, expect } = require("playwright/test");
const folder = await mkdtemp(join(tmpdir(), "gent-streaming-browser-"));
const binary = join(folder, "server");
const shots = fileURLToPath(new URL("docs/quickstart/screenshots/", root));
await mkdir(shots, { recursive: true });
// The local model is always selected here. A user's shell credentials must not make API calls.
const env = {
  ...process.env,
  OPENAI_API_KEY: "",
  GENT_MODEL: "",
  CGO_ENABLED: "0",
  GOCACHE: process.env.GOCACHE || "/tmp/gent-policy-lessons-go-cache",
  RUN_DB: join(folder, "runs.db"),
  UI_DIR: fileURLToPath(new URL("docs/quickstart/client/dist", root)),
};
let processHandle, browser;
let logs = "";
const errors = [];
const frames = [];
async function startServer(address = "127.0.0.1:0") {
  const child = spawn(binary, [], { env: { ...env, API_ADDR: address } });
  processHandle = child;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`Server did not start: ${logs}`)), 10000);
    child.once("error", reject);
    child.once("exit", (code, signal) => {
      clearTimeout(timer);
      reject(new Error(`Server exited: ${code}/${signal}\n${logs}`));
    });
    child.stderr.on("data", (data) => {
      logs += data;
      const match = String(data).match(/LISTEN (http:\/\/[^\s]+)/);
      if (!match) return;
      clearTimeout(timer);
      resolve(match[1]);
    });
  });
}
async function stopServer() {
  if (!processHandle || processHandle.exitCode !== null) return;
  const child = processHandle;
  const exited = once(child, "exit");
  child.kill("SIGINT");
  const timer = setTimeout(() => child.kill("SIGKILL"), 15000);
  const [code, signal] = await exited;
  clearTimeout(timer);
  assert.deepEqual([code, signal], [0, null], logs);
  processHandle = null;
}
async function newPage() {
  const page = await browser.newPage({ viewport: { width: 1100, height: 850 } });
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("websocket", (socket) =>
    socket.on("framereceived", ({ payload }) => {
      frames.push(JSON.parse(String(payload)));
    }),
  );
  return page;
}
async function saved(url, runId) {
  const response = await fetch(`${url}/api/runs/${runId}/state`);
  assert.equal(response.status, 200);
  return response.json();
}
try {
  execFileSync("go", ["build", "-o", binary, "."], {
    cwd: fileURLToPath(new URL("docs/quickstart/server", root)),
    env,
    stdio: "pipe",
  });
  await readFile(join(env.UI_DIR, "index.html"));
  const url = await startServer();
  browser = await chromium.launch({
    headless: true,
    executablePath: process.env.GENT_LESSON_CHROMIUM,
  });
  let page = await newPage();
  await page.goto(url);
  await page.getByRole("button", { name: "Start order lookup", exact: true }).click();
  await expect(page.getByRole("region", { name: "Model preview" })).toBeVisible();
  const runId = new URL(page.url()).searchParams.get("run");
  assert.ok(runId);
  const beforeRefresh = await saved(url, runId);
  assert.equal(beforeRefresh.status, "running");
  await page.screenshot({ path: join(shots, "gent-streaming-preview.png") });
  await page.reload();
  const answer = page.getByRole("region", { name: "Accepted answer" });
  await expect(answer.locator(".answer-content")).toHaveText("Order order-1001 has shipped.", { timeout: 15000 });
  await expect(answer).toHaveCount(1);
  await expect(page.getByRole("region", { name: "Model preview" })).toHaveCount(0);
  await expect(page.getByRole("status")).toHaveText("Complete");
  assert.equal(new URL(page.url()).searchParams.get("run"), runId);
  const final = await saved(url, runId);
  assert.ok(BigInt(final.lastEventNumber) > BigInt(beforeRefresh.lastEventNumber));
  const firstFrames = frames.filter((frame) => frame.runId === runId);
  assert.ok(firstFrames.some((frame) => frame.kind === "ready"));
  assert.ok(
    firstFrames.some(
      (frame) => frame.kind === "event" && frame.event.type === "model_stream_chunk",
    ),
  );
  await page.screenshot({ path: join(shots, "gent-streaming-complete.png") });
  console.log("PASS refresh during streaming keeps the run and shows one exact accepted answer");

  await page.getByRole("button", { name: "Start order lookup", exact: true }).click();
  await expect(page.getByRole("region", { name: "Model preview" })).toBeVisible();
  const resumeURL = page.url();
  const interruptedID = new URL(resumeURL).searchParams.get("run");
  const beforeStop = await saved(url, interruptedID);
  assert.equal(beforeStop.status, "running");
  // Close the browser first; the service owns the worker independently of this connection.
  await page.close();
  await stopServer();
  await startServer(new URL(url).host);
  const reopened = await saved(url, interruptedID);
  assert.equal(reopened.runId, interruptedID);
  assert.ok(BigInt(reopened.lastEventNumber) >= BigInt(beforeStop.lastEventNumber));
  assert.equal(reopened.answers.length, 0);
  assert.deepEqual(reopened.previews.map((preview) => preview.status), ["succeeded", "failed"]);
  page = await newPage();
  await page.goto(resumeURL);
  await expect(page.getByRole("region", { name: "Model preview" })).toBeVisible();
  await page.getByRole("button", { name: "Resume saved run", exact: true }).click();
  await expect(page.getByRole("region", { name: "Accepted answer" }).locator(".answer-content")).toHaveText(
    "Order order-1001 has shipped.",
    { timeout: 15000 },
  );
  await expect(page.getByRole("region", { name: "Accepted answer" })).toHaveCount(1);
  await expect(page.getByRole("status")).toHaveText("Complete");
  const resumed = await saved(url, interruptedID);
  assert.equal(resumed.runId, interruptedID);
  assert.equal(resumed.status, "succeeded");
  assert.ok(BigInt(resumed.lastEventNumber) > BigInt(reopened.lastEventNumber));
  assert.ok(
    resumed.previews.some((preview) => preview.attemptId !== reopened.previews[0].attemptId),
  );
  const response = await fetch(`${url}/api/runs/${interruptedID}/resume`, { method: "POST" });
  assert.equal(response.status, 202);
  await expect.poll(async () => saved(url, interruptedID)).toEqual(resumed);
  await page.setViewportSize({ width: 390, height: 844 });
  const size = await page.evaluate(() => [
    document.documentElement.scrollWidth,
    document.documentElement.clientWidth,
  ]);
  assert.equal(size[0], size[1]);
  await page.screenshot({
    path: join(shots, "gent-streaming-recovered-mobile.png"),
    fullPage: true,
  });
  console.log("PASS database reopen, fresh worker recovery, higher cursors, and terminal reuse");
  // Reuse the completed run's event cursor and exercise the binary display contract offline.
  const mediaState = structuredClone(resumed);
  const pixel = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jf1s" +
    "AAAAASUVORK5CYII=";
  mediaState.answers[0].content = [
    { Text: "Image and report." },
    { MIMEType: "image/png", Data: pixel },
    { MIMEType: "application/pdf", Data: "JVBERi0xLjcK" },
  ];
  await page.route(`**/api/runs/${interruptedID}/state`, (route) => route.fulfill({
    json: mediaState,
  }));
  await page.reload();
  const image = page.getByRole("img", { name: "Generated image 2" });
  await expect(image).toHaveJSProperty("naturalWidth", 1);
  await expect(page.getByRole("link", { name: "Download PDF" }))
    .toHaveAttribute("href", "data:application/pdf;base64,JVBERi0xLjcK");
  await expect(page.getByRole("region", { name: "Accepted answer" }).locator(".answer-content"))
    .toHaveText("Image and report.Download PDF");
  await page.screenshot({ path: join(shots, "gent-streaming-media-mobile.png"), fullPage: true });
  console.log("PASS ordered binary image and PDF display from a saved snapshot");
  assert.deepEqual(errors, []);
  console.log("PASS no browser errors and no mobile page overflow");
} finally {
  if (browser) await browser.close();
  await stopServer();
  await writeFile(join(shots, "gent-streaming-server.log"), logs);
  await rm(folder, { recursive: true, force: true });
}
