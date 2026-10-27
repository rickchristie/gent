// This static-file test follows the existing docs browser checks. It needs no web server.
import assert from "node:assert/strict";
import { readFile, mkdir } from "node:fs/promises";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const root = new URL("../../../", import.meta.url);
const require = createRequire(new URL("docs/code/package.json", root));
const { chromium, expect } = require("playwright/test");
const file = new URL("docs/quickstart/dist/gent-policy-search-lessons.html", root);
const shots = new URL("docs/quickstart/screenshots/", root);
await mkdir(shots, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  executablePath: process.env.GENT_LESSON_CHROMIUM,
});
const context = await browser.newContext({
  viewport: { width: 1440, height: 1050 },
  offline: true,
  reducedMotion: "reduce",
});
const page = await context.newPage();
const errors = [];
const requests = [];
page.on("pageerror", (error) => errors.push(error.message));
page.on("console", (message) => {
  if (message.type() === "error") errors.push(message.text());
});
page.on("request", (request) => {
  if (/^https?:/.test(request.url())) requests.push(request.url());
});
const navigate = async (name) => {
  await page.getByRole("navigation", { name: "Lessons" }).getByRole("link", { name }).click();
};
const range = async (id, value) => {
  await page.locator(`#${id}`).evaluate((element, next) => {
    element.value = String(next);
    element.dispatchEvent(new Event("input", { bubbles: true }));
  }, value);
};
const check = async (name, action) => {
  await action();
  console.log(`PASS ${name}`);
};

try {
  await page.goto(file.href);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "A normal loop.Two ways to find a policy.",
  );
  await check("offline start and architecture handoff", async () => {
    await page.getByRole("button", { name: "GENERATION Model adapter gent.Model" }).click();
    await expect(page.locator("#map-detail h3")).toHaveText("Model is the provider boundary");
    await page.screenshot({ path: fileURLToPath(new URL("gent-policy-lessons-start.png", shots)) });
    await page.getByRole("button", { name: "Open Model contract →" }).click();
    await expect(page.locator("#contract-detail h2")).toHaveText("Model");
  });
  await check("all interface declarations and empty search", async () => {
    await expect(page.locator("#contract-list button")).toHaveCount(21);
    await page.getByLabel("Find an interface").fill("no such interface");
    await expect(page.locator("#contract-detail h2")).toHaveText("No matching interface");
    await page.getByLabel("Find an interface").fill("");
    await page.getByLabel("Area", { exact: true }).selectOption("Search");
    await expect(page.locator("#contract-list button")).toHaveCount(7);
    await page.getByRole("button", { name: "Embedder Supply and own one" }).click();
    const source = await readFile(new URL("search/embedder.go", root), "utf8");
    const signature = source
      .match(/^type Embedder interface \{[\s\S]*?^}/m)[0]
      .split(String.fromCharCode(10))
      .filter((line) => line.trim() && !line.trim().startsWith("//"))
      .join(String.fromCharCode(10));
    assert.equal(await page.locator("#contract-detail code").textContent(), signature);
    await page.getByLabel("Area", { exact: true }).selectOption("All");
  });
  await check("policy fields go to the correct index input", async () => {
    await navigate("03 Prepare the policies");
    const before = await page.locator("#bm25-document").textContent();
    const vectorBefore = await page.locator("#vector-document").textContent();
    await page.getByLabel("Description", { exact: true }).fill("Display text only.");
    assert.equal(await page.locator("#bm25-document").textContent(), before);
    assert.equal(await page.locator("#vector-document").textContent(), vectorBefore);
    await page.getByLabel("Id", { exact: true }).fill("test-policy");
    await page.getByLabel("FullContent", { exact: true }).fill("An exact test rule.");
    await page.getByLabel("Keywords / comma separated").fill("exact, rule");
    await page.getByLabel("SyntheticQueries / one per line").fill("Find my rule");
    assert.deepEqual(JSON.parse(await page.locator("#bm25-document").textContent()), {
      id: "test-policy",
      keywords: "exact rule",
      fullcontent: "An exact test rule.",
    });
    assert.equal(
      await page.locator("#vector-document").textContent(),
      `# test-policy

An exact test rule.`,
    );
    assert.equal(
      await page.locator("#vector-queries").textContent(),
      `# test-policy

Find my rule`,
    );
    await page.getByLabel("Id", { exact: true }).fill("");
    await expect(page.locator("#policy-warning")).toHaveText(
      "Add an ID so each result can refer to one policy.",
    );
    await page.getByRole("button", { name: "Reset example", exact: true }).click();
    assert.equal(await page.locator("#bm25-document").textContent(), before);
  });
  await check("fusion changes rank and final top-K", async () => {
    await navigate("04 Mix the two signals");
    const cases = [
      {
        input: { preset: "intent", weight: 40, topK: "3" },
        expected: [
          ["1. return-window", "0.592"],
          ["2. order-cancel", "0.568"],
          ["3. damaged-delivery", "0.348"],
        ],
      },
      {
        input: { preset: "intent", weight: 100, topK: "2" },
        expected: [
          ["1. order-cancel", "0.700"],
          ["2. return-window", "0.100"],
        ],
      },
      {
        input: { preset: "keyword", weight: 40, topK: "3" },
        expected: [
          ["1. return-window", "0.830"],
          ["2. damaged-delivery", "0.440"],
          ["3. order-cancel", "0.250"],
        ],
      },
      { input: { preset: "id", weight: 0, topK: "1" }, expected: [["1. return-window", "0.730"]] },
    ];
    for (const test of cases) {
      await page.getByLabel("Query example").selectOption(test.input.preset);
      await page.getByLabel("Return at most").selectOption(test.input.topK);
      await range("bm25-weight", test.input.weight);
      const rows = await page
        .locator("#ranking-rows tr")
        .evaluateAll((elements) =>
          elements.map((row) => [
            row.querySelector("button").textContent,
            row.querySelector("strong").textContent,
          ]),
        );
      assert.deepEqual(rows, test.expected);
    }
    await page.getByRole("button", { name: "Use policy defaults" }).click();
    await page.getByRole("button", { name: "3. damaged-delivery" }).click();
    await expect(page.locator("#rank-detail h3")).toHaveText("damaged-delivery: 0.348");
    await page.screenshot({
      path: fileURLToPath(new URL("gent-policy-lessons-ranking.png", shots)),
    });
  });
  await check("all loop paths end and can restart", async () => {
    await navigate("05 Follow the loop");
    const cases = [
      {
        input: "full",
        expected: { title: "Return the accepted answer", iteration: "2", tools: "1" },
      },
      {
        input: "snippet",
        expected: { title: "Return the accepted answer", iteration: "3", tools: "2" },
      },
      {
        input: "reject",
        expected: { title: "Return the accepted answer", iteration: "3", tools: "1" },
      },
      {
        input: "error",
        expected: { title: "Return the accepted answer", iteration: "3", tools: "2" },
      },
      {
        input: "limit",
        expected: { title: "Stop the unfinished run", iteration: "1", tools: "1" },
      },
    ];
    for (const test of cases) {
      await page.getByLabel("Run path").selectOption(test.input);
      const next = page.getByRole("button", { name: "Next step →", exact: true });
      for (let index = 0; index < 12 && (await next.isEnabled()); index++) await next.click();
      await expect(next).toBeDisabled();
      assert.deepEqual(
        {
          title: await page.locator("#run-title").textContent(),
          iteration: await page.locator("#run-iteration").textContent(),
          tools: await page.locator("#run-tools").textContent(),
        },
        test.expected,
      );
      await page.getByRole("button", { name: "Back", exact: true }).click();
      await expect(next).toBeEnabled();
      await page.getByRole("button", { name: "Restart", exact: true }).click();
      await expect(page.locator("#run-title")).toHaveText("Start with a task");
    }
    await page.getByLabel("Run path").selectOption("full");
    await page.getByRole("button", { name: "Step 4: Validate, plan, execute" }).click();
    await expect(page.locator("#run-title")).toHaveText("Validate, plan, execute");
  });
  await check("Go download matches the source and clipboard fallback selects it", async () => {
    await navigate("06 Connect the code");
    const source = await readFile(
      new URL("docs/quickstart/policy-search-lessons/example/main.go", root),
      "utf8",
    );
    assert.equal(await page.locator("#go-example").textContent(), source);
    const downloadReady = page.waitForEvent("download");
    await page.getByRole("button", { name: "Download main.go", exact: true }).click();
    const download = await downloadReady;
    assert.equal(download.suggestedFilename(), "main.go");
    assert.equal(await readFile(await download.path(), "utf8"), source);
    await page.evaluate(() =>
      Object.defineProperty(navigator, "clipboard", {
        configurable: true,
        value: { writeText: () => Promise.reject(new Error("Clipboard test blocked")) },
      }),
    );
    await page.getByRole("button", { name: "Copy Go", exact: true }).click();
    await expect(page.locator("#copy-status")).toHaveText(
      "Clipboard test blocked. Source selected; copy it or use Download main.go.",
    );
    // Range text keeps the final newline that Chromium omits from the rendered selection text.
    assert.equal(await page.evaluate(() => window.getSelection().getRangeAt(0).toString()), source);
  });
  await check("output modes include the current title-mode description", async () => {
    await navigate("07 Run it reliably");
    await page.getByRole("button", { name: "Title", exact: true }).click();
    await expect(page.locator("#mode-result")).toHaveText(`id: return-window
description: Return unused items within 30 days of delivery.

Then call: get_policy({"policy_id":"return-window"})`);
    await page.getByRole("button", { name: "Snippet", exact: true }).click();
    await expect(page.locator("#mode-title")).toHaveText("Snippets reduce the first result size");
  });
  await check("quiz grades unanswered, wrong, and correct choices", async () => {
    await navigate("08 Check your model");
    await page.getByRole("button", { name: "Check answers", exact: true }).click();
    await expect(page.locator("#quiz-result")).toHaveText(
      "0 / 5 — Read the feedback and try again.",
    );
    const answers = [1, 2, 0, 0, 1];
    for (let index = 0; index < answers.length; index++) {
      await page.locator(`input[name="question-${index}"][value="${answers[index]}"]`).check();
    }
    await page.getByRole("button", { name: "Check answers", exact: true }).click();
    await expect(page.locator("#quiz-result")).toHaveText("5 / 5 — You have the complete path.");
    await page.getByLabel("A new AgentLoop implementation", { exact: true }).check();
    await page.getByRole("button", { name: "Check answers", exact: true }).click();
    await expect(page.locator("#quiz-result")).toHaveText(
      "4 / 5 — Read the feedback and try again.",
    );
    await page.getByRole("button", { name: "Clear answers", exact: true }).click();
    await expect(page.locator("#quiz-result")).toHaveText("");
    await expect(page.locator('input[type="radio"]:checked')).toHaveCount(0);
  });
  await check("progress persists and keyboard navigation works", async () => {
    await page.getByRole("button", { name: "Mark as complete", exact: true }).click();
    await expect(page.locator("#progress-text")).toHaveText("1 / 8");
    await page.reload();
    await expect(page.getByRole("button", { name: "Completed ✓", exact: true })).toBeVisible();
    await expect(page.locator("#progress-text")).toHaveText("1 / 8");
    await page.getByRole("button", { name: "Reset progress", exact: true }).click();
    await expect(page.locator("#progress-text")).toHaveText("0 / 8");
    await navigate("01 The complete path");
    await page.getByRole("button", { name: "Next lesson →", exact: true }).focus();
    await page.keyboard.press("Enter");
    await expect(page.locator("#contracts")).toBeVisible();
  });
  await check("all lessons fit narrow screens and print includes hidden lessons", async () => {
    await page.setViewportSize({ width: 390, height: 844 });
    for (const id of [
      "start",
      "contracts",
      "documents",
      "ranking",
      "loop",
      "code",
      "operation",
      "check",
    ]) {
      await page.locator(`nav [data-page="${id}"]`).click();
      const size = await page.evaluate(() => ({
        viewport: document.documentElement.clientWidth,
        content: document.documentElement.scrollWidth,
      }));
      assert.equal(size.content, size.viewport, `${id} must not overflow the viewport`);
    }
    await navigate("01 The complete path");
    await page.screenshot({
      path: fileURLToPath(new URL("gent-policy-lessons-mobile.png", shots)),
      fullPage: true,
    });
    await page.emulateMedia({ media: "print" });
    for (const id of [
      "start",
      "contracts",
      "documents",
      "ranking",
      "loop",
      "code",
      "operation",
      "check",
    ]) {
      await expect(page.locator(`#${id}`)).toBeVisible();
    }
    await page.emulateMedia({ media: "screen" });
  });
  assert.deepEqual(errors, []);
  assert.deepEqual(requests, []);
  console.log("PASS no browser errors and no network requests");
} catch (error) {
  await page.screenshot({ path: fileURLToPath(new URL("gent-policy-lessons-failure.png", shots)) });
  throw error;
} finally {
  await browser.close();
}
