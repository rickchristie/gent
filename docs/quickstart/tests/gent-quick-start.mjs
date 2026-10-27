import assert from "node:assert/strict";
import { readFile, mkdir } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { createServer } from "node:http";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const root = new URL("../../../", import.meta.url);
const sourceRoot = new URL("docs/quickstart/", root);
const require = createRequire(new URL("docs/code/package.json", root));
const { chromium, expect } = require("playwright/test");
const html = await readFile(new URL("docs/quickstart/dist/gent-quick-start.html", root), "utf8");
const shots = new URL("docs/quickstart/screenshots/", root);
await mkdir(shots, { recursive: true });
const server = createServer((request, response) => {
  if (request.url !== "/") {
    response.writeHead(404).end();
    return;
  }
  response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" }).end(html);
});
await new Promise((resolve, reject) => {
  server.once("error", reject);
  server.listen(0, "127.0.0.1", resolve);
});
const url = `http://127.0.0.1:${server.address().port}/`;
let browser;
try {
  browser = await chromium.launch({
    headless: true,
    executablePath: process.env.GENT_LESSON_CHROMIUM,
  });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1050 },
    reducedMotion: "reduce",
  });
  await context.addInitScript(() => {
    for (const name of ["localStorage", "sessionStorage"]) {
      Object.defineProperty(window, name, {
        get() {
          throw new Error("Storage disabled");
        },
      });
    }
  });
  const page = await context.newPage();
  const errors = [],
    requests = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("request", (request) => requests.push([request.resourceType(), request.url()]));
  const go = async (scenario, step) => {
    await page.locator(`#scenarios a[data-scenario="${scenario}"]`).click();
    if (step === "files") await page.locator("#files-footer").click();
    else await page.locator(`#steps a[href="#${scenario}/${step}"]`).click();
    await expect(page.locator(`#${scenario}-${step}`)).toBeVisible();
  };
  await page.goto(url);
  await context.setOffline(true);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Create a Go project.");
  assert.deepEqual(await page.locator("#scenarios a").allTextContents(), [
    "Simple agent",
    "Saved streaming API",
    "React streaming client",
    "Resume a saved run",
  ]);
  assert.equal(await page.locator("progress, input[type=checkbox], #progress-text").count(), 0);
  await page.screenshot({ path: fileURLToPath(new URL("gent-quick-start-desktop.png", shots)) });

  await expect(page.getByLabel("Path to your Gent checkout")).toHaveValue("/absolute/path/to/gent");
  await page.getByLabel("Path to your Gent checkout").fill("/tmp/Gent's demo");
  assert.equal(
    await page.locator("#setup-commands").textContent(),
    String.raw`mkdir gent-demo
cd gent-demo
go mod init example.com/gent-demo
go mod edit '-replace=github.com/rickchristie/gent=/tmp/Gent'\''s demo'
go get github.com/rickchristie/gent`,
  );
  console.log("PASS setup commands quote the checkout path; no progress storage is used");

  const data = await page.evaluate(() => guideData);
  const checkReading = async (id, reading, source) => {
    const study = page.locator(`[data-reader="${id}"]`);
    // Wait for the browser's line wrapping and the connector layout, not a fixed delay.
    await page.evaluate(
      () =>
        new Promise((resolve) => {
          requestAnimationFrame(() => requestAnimationFrame(resolve));
        }),
    );
    const notes = reading.notes.filter((note) => note.first);
    await expect(study.locator(".connector")).toHaveCount(notes.length);
    for (const note of notes) {
      assert.equal(
        await page.locator(`#${id}-L${note.first}`).textContent(),
        source.split("\n")[note.first - 1],
      );
    }
    const geometry = await study.evaluate((element) => {
      const cards = [...element.querySelectorAll(".annotation")];
      const arrows = [...element.querySelectorAll(".connector")];
      return {
        clearCards: cards.every(
          (card, index) =>
            !index ||
            card.getBoundingClientRect().top >= cards[index - 1].getBoundingClientRect().bottom,
        ),
        arrowsOnLines: arrows.every((arrow) => {
          const end = arrow.getPointAtLength(arrow.getTotalLength());
          const screen = new DOMPoint(end.x, end.y).matrixTransform(arrow.getScreenCTM());
          const line = element
            .querySelector(`[data-line="${arrow.dataset.line}"]`)
            .getBoundingClientRect();
          return (
            screen.y >= line.top &&
            screen.y <= line.bottom &&
            screen.x >= line.right - 15 &&
            screen.x <= line.right
          );
        }),
        noInnerScroll: [...element.querySelectorAll("pre")].every(
          (pre) =>
            getComputedStyle(pre).maxHeight === "none" && pre.scrollHeight === pre.clientHeight,
        ),
      };
    });
    assert.deepEqual(geometry, { clearCards: true, arrowsOnLines: true, noInnerScroll: true }, id);
  };
  for (const scenario of data.scenarios) {
    for (const step of scenario.steps) {
      await go(scenario.id, step.id);
      const section = page.locator(`#${scenario.id}-${step.id}`);
      await expect(section.locator(".instruction")).toBeVisible();
      await expect(section.locator("h1")).toBeVisible();
      const location = await section.locator(".code-head > span").last().textContent();
      const code = await section.locator(".code-panel pre code").textContent();
      const path = scenario.id === "basic" ? "example/main.go" : location;
      const source = await readFile(new URL(path, sourceRoot), "utf8");
      const offset = source.indexOf(code);
      assert.notEqual(offset, -1, `${scenario.id}/${step.id}`);
      assert.equal(source.slice(offset, offset + code.length), code);
      const id = `code-${scenario.id}-${step.id}`;
      await checkReading(id, data.readers[id], source);
      assert.equal(
        await page.locator("#steps a[aria-current=step]").getAttribute("href"),
        `#${scenario.id}/${step.id}`,
      );
    }
    await go(scenario.id, "files");
    for (const path of data.projects[scenario.project].choices) {
      await page.locator(`#select-${scenario.id}`).selectOption(path);
      const source = await readFile(new URL(`${scenario.project}/${path}`, sourceRoot), "utf8");
      assert.equal(await page.locator(`#complete-${scenario.id}`).textContent(), source);
      await checkReading(
        `complete-${scenario.id}`,
        data.projects[scenario.project].reading[path],
        source,
      );
    }
  }
  console.log("PASS all 26 coding steps and all source-file choices match the tested files");
  console.log("PASS source line numbers, connector positions, and notes without overlap");

  await go("basic", "agent");
  const study = page.locator('[data-reader="code-basic-agent"]');
  const colors = await study
    .locator(".token")
    .evaluateAll((tokens) =>
      [...new Set(tokens.map((token) => getComputedStyle(token).color))].sort(),
    );
  assert.ok(colors.length >= 5, JSON.stringify(colors));
  await expect(study.locator(".token.keyword").first()).toHaveCSS("color", "rgb(203, 188, 245)");
  await expect(study.locator(".token.string").first()).toHaveCSS("color", "rgb(212, 229, 160)");
  const background = await study
    .locator("pre")
    .evaluate((pre) => getComputedStyle(pre).backgroundColor);
  const luminance = (color) =>
    color
      .match(/\d+/g)
      .slice(0, 3)
      .map(Number)
      .map((channel) => channel / 255)
      .map((channel) => (channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4))
      .reduce((sum, channel, index) => sum + channel * [0.2126, 0.7152, 0.0722][index], 0);
  for (const color of colors) {
    const ratio = (luminance(color) + 0.05) / (luminance(background) + 0.05);
    assert.ok(ratio >= 4.5, `${color} has contrast ${ratio}`);
  }
  // Resolve the label from source anchors so line changes do not require editing this test.
  const firstNote = data.readers["code-basic-agent"].notes.find((note) => note.first);
  const firstButton = study.getByRole("button", {
    name: `Show lines ${firstNote.first}–${firstNote.last}: ${firstNote.title}`,
    exact: true,
  });
  await firstButton.focus();
  await page.keyboard.press("Enter");
  await expect(page.locator(`#code-basic-agent-L${firstNote.first}`)).toBeFocused();
  assert.deepEqual(
    await study
      .locator(".line-focus")
      .evaluateAll((lines) => lines.map((line) => Number(line.dataset.line))),
    Array.from(
      { length: firstNote.last - firstNote.first + 1 },
      (_, index) => firstNote.first + index,
    ),
  );
  await expect(study.locator(".connector.selected")).toHaveCount(1);
  const columns = await page.evaluate(() => {
    const nav = document.querySelector(".site-nav").getBoundingClientRect();
    const reader = document.querySelector('[data-reader="code-basic-agent"]');
    const code = reader.querySelector(".code-panel").getBoundingClientRect();
    const notes = reader.querySelector(".annotation-column").getBoundingClientRect();
    return nav.right < code.left && code.right < notes.left;
  });
  assert.equal(columns, true);
  await page.screenshot({ path: fileURLToPath(new URL("gent-quick-start-line-focus.png", shots)) });

  await go("basic", "files");
  await page.locator("#complete-basic-L35").scrollIntoViewIfNeeded();
  const scrollBefore = await page.evaluate(() => scrollY);
  const codeBox = await page.locator("#complete-basic-L35").boundingBox();
  await page.mouse.move(codeBox.x + 100, codeBox.y + 10);
  await page.mouse.wheel(0, 550);
  await expect.poll(() => page.evaluate(() => scrollY)).toBeGreaterThan(scrollBefore);
  const scrollContainers = await page.evaluate(() =>
    [...document.querySelectorAll("body *")]
      .filter(
        (element) =>
          ["auto", "scroll"].includes(getComputedStyle(element).overflowY) &&
          element.scrollHeight > element.clientHeight + 1,
      )
      .map((element) => element.className),
  );
  assert.deepEqual(scrollContainers, []);
  await page.screenshot({ path: fileURLToPath(new URL("gent-quick-start-complete.png", shots)) });
  console.log("PASS syntax theme, three columns, keyboard line focus, and one page scrollbar");

  await page.evaluate(() =>
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (text) => {
          window.copiedText = text;
        },
      },
    }),
  );
  for (const [scenario, project] of [
    ["basic", "example"],
    ["server", "server"],
    ["react", "client"],
  ]) {
    await go(scenario, "files");
    const path = project === "example" ? "main.go" : "README.md";
    await page.locator(`#select-${scenario}`).selectOption(path);
    await page.locator(`#${scenario}-files [data-copy]`).click();
    assert.equal(await page.evaluate(() => window.copiedText), data.projects[project].files[path]);
    const ready = page.waitForEvent("download");
    await page.locator("#download-top").click();
    const download = await ready;
    assert.equal(download.suggestedFilename(), data.projects[project].downloadName);
    assert.deepEqual(
      await readFile(await download.path()),
      Buffer.from(data.projects[project].download, "base64"),
    );
    if (project !== "example") {
      execFileSync("python3", [
        "-c",
        `
import pathlib, sys, zipfile
with zipfile.ZipFile(sys.argv[1]) as archive:
    assert archive.namelist()
    for name in archive.namelist():
        assert archive.read(name) == (pathlib.Path(sys.argv[2]) / name).read_bytes(), name
`,
        await download.path(),
        fileURLToPath(sourceRoot),
      ]);
    }
  }
  await page.locator("#select-react").selectOption("src/connection.ts");
  const fileReady = page.waitForEvent("download");
  await page.locator("#react-files [data-download-file]").click();
  const file = await fileReady;
  assert.equal(file.suggestedFilename(), "connection.ts");
  assert.equal(
    await readFile(await file.path(), "utf8"),
    data.projects.client.files["src/connection.ts"],
  );
  await page.evaluate(() =>
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async () => {
          throw new Error("Clipboard blocked");
        },
      },
    }),
  );
  await page.locator("#react-files [data-copy]").click();
  await expect(page.locator("#copy-status")).toHaveText(
    "Clipboard blocked. Text selected; use your Copy command.",
  );
  assert.equal(
    await page.evaluate(() => getSelection().getRangeAt(0).toString()),
    data.projects.client.files["src/connection.ts"],
  );
  console.log("PASS copy, fallback selection, full-file download, and complete ZIP downloads");

  await go("basic", "setup");
  await page.locator("#next").focus();
  await page.keyboard.press("Enter");
  await expect(page.locator("#basic-model")).toBeVisible();
  await page.locator("#next").click();
  await expect(page.locator("#basic-tool")).toBeVisible();
  await page.goBack();
  await expect(page.locator("#basic-model")).toBeVisible();
  await page.goForward();
  await expect(page.locator("#basic-tool")).toBeVisible();
  await page.getByRole("link", { name: "Skip to the current step", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("main")).toBeFocused();
  await go("react", "catchup");
  await context.setOffline(false);
  await page.reload();
  await context.setOffline(true);
  await expect(page.locator("#react-catchup")).toBeVisible();
  await page.evaluate(() => {
    location.hash = "execute";
  });
  await expect(page.locator("#basic-execute")).toBeVisible();
  await go("server", "save");
  await page.screenshot({ path: fileURLToPath(new URL("gent-quick-start-server.png", shots)) });
  await go("react", "cursor");
  await page.screenshot({ path: fileURLToPath(new URL("gent-quick-start-react.png", shots)) });
  console.log("PASS keyboard use, history, reload, and old direct links");

  await go("server", "save");
  for (const width of [1280, 1100, 1024]) {
    await page.setViewportSize({ width, height: 900 });
    await page.evaluate(
      () =>
        new Promise((resolve) => {
          requestAnimationFrame(() => requestAnimationFrame(resolve));
        }),
    );
    const placement = await page.locator('[data-reader="code-server-save"]').evaluate((element) => {
      const code = element.querySelector(".code-panel").getBoundingClientRect();
      const notes = element.querySelector(".annotation-column").getBoundingClientRect();
      return {
        atRight: notes.left > code.right,
        below: notes.top >= code.bottom,
        fits: document.documentElement.scrollWidth === document.documentElement.clientWidth,
      };
    });
    assert.deepEqual(placement, { atRight: width >= 1100, below: width < 1100, fits: true });
  }
  console.log("PASS token contrast and layout at desktop and tablet breakpoints");

  await page.setViewportSize({ width: 390, height: 844 });
  for (const scenario of data.scenarios) {
    for (const step of [...scenario.steps, { id: "files" }]) {
      await go(scenario.id, step.id);
      const size = await page.evaluate(() => [
        document.documentElement.scrollWidth,
        document.documentElement.clientWidth,
      ]);
      assert.equal(size[0], size[1], `${scenario.id}/${step.id}`);
    }
  }
  await go("react", "cursor");
  const mobileNote = page.locator('[data-reader="code-react-cursor"] .annotation-jump').first();
  const mobileTarget = await mobileNote.getAttribute("aria-controls");
  await mobileNote.click();
  await expect(page.locator(`#${mobileTarget}`)).toBeFocused();
  await expect(page.locator(`#${mobileTarget}`)).toBeInViewport();
  await page.screenshot({
    path: fileURLToPath(new URL("gent-quick-start-mobile.png", shots)),
    fullPage: true,
  });
  await page.emulateMedia({ media: "print" });
  for (const scenario of data.scenarios) {
    for (const step of scenario.steps) {
      await expect(page.locator(`#${scenario.id}-${step.id}`)).toBeVisible();
    }
  }
  assert.deepEqual(errors, []);
  assert.deepEqual(requests, [
    ["document", url],
    ["document", url],
  ]);
  console.log("PASS phone layout, all-scenario print, offline use, and no browser errors");
} finally {
  if (browser) await browser.close();
  await new Promise((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
}
