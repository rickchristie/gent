import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { lessons } from './lesson-models.mjs';

const require = createRequire(new URL('../quickstart/package.json', import.meta.url));
const { chromium } = require('playwright');
const launch = { headless: true };
if (process.env.GENT_REVIEW_CHROMIUM) launch.executablePath = process.env.GENT_REVIEW_CHROMIUM;
const browser = await chromium.launch(launch);
const context = await browser.newContext({
  viewport: { width: 1440, height: 1100 },
  offline: true,
  reducedMotion: 'reduce',
});
const page = await context.newPage();
const url = new URL('./gent-interfaces.html', import.meta.url).href;
const catalog = JSON.parse(await readFile('/tmp/gent-interface-catalog.json', 'utf8'));
const errors = [];
const requests = [];
page.on('pageerror', (error) => errors.push(error.message));
page.on('console', (message) => {
  if (message.type() === 'error') errors.push(message.text());
});
page.on('request', (request) => {
  if (/^https?:/.test(request.url())) requests.push(request.url());
});
const text = async (selector) => (await page.locator(selector).textContent()).trim();
const click = (action, value) =>
  page
    .locator(`[data-action="${action}"]` + (value === undefined ? '' : `[data-value="${value}"]`))
    .click();
async function lesson(id) {
  await page.locator(`#lesson-rail [data-lesson="${id}"]`).click();
}
async function advance(action, count) {
  for (let index = 0; index < count; index++) await click(action);
}
async function check(name, test) {
  await test();
  console.log(`PASS ${name}`);
}
async function noOverflow(label) {
  const width = await page.evaluate(() => ({
    content: document.documentElement.scrollWidth,
    viewport: document.documentElement.clientWidth,
  }));
  assert.ok(width.content <= width.viewport, `${label}: ${JSON.stringify(width)}`);
}
async function saveState() {
  const values = await Promise.all(
    [
      'stored-cursor',
      'accepted-cursor',
      'checkpoint-cursor',
      'save-attempts',
      'provider-state',
      'observer-state',
    ].map((id) => text(`#${id}`)),
  );
  return values;
}

try {
  await page.goto(url);
  await page.evaluate(() => document.fonts.ready);
  await check('the initial experience is an experiment with all five lessons', async () => {
    assert.equal(await page.title(), 'Gent — An interactive explainer');
    assert.equal(await page.locator('#learn').isVisible(), true);
    assert.equal(await page.locator('#atlas').isVisible(), false);
    assert.equal(await page.locator('#lesson-rail button').count(), 5);
    assert.equal(await text('#loop-headline'), 'The application supplies the task.');
    await noOverflow('desktop');
    await page.screenshot({
      path: '/tmp/gent-learning-loop.png',
      fullPage: true,
    });
    await page.screenshot({ path: '/tmp/gent-learning-loop-viewport.png' });
  });
  await check(
    'loop branches teach tool continuation, rejection, acceptance, and malformed output',
    async () => {
      await advance('loop-next', 2);
      assert.equal(
        await text('#loop-output'),
        `{
  "id": "call-1",
  "type": "function",
  "function": {
    "name": "lookup_order",
    "arguments": "{\\"order_id\\":42}"
  }
}`,
      );
      await advance('loop-next', 3);
      assert.equal(
        await text('#loop-output'),
        `HistoryEntry H1: Order 42 ships Friday.
Next working context: [H1]`,
      );
      await click('loop-next');
      assert.equal(
        await text('#loop-output'),
        'Order 42 ships on Friday. → accepted → LATerminate',
      );
      const cases = [
        {
          input: 'rejected',
          expected: '“Use the order tool; do not guess.” → LAContinue',
        },
        {
          input: 'accepted',
          expected: 'Order 42 ships on Friday. → LATerminate',
        },
        { input: 'malformed', expected: 'parse error → feedback → LAContinue' },
      ];
      for (const { input, expected } of cases) {
        await page.selectOption('#loop-reply', input);
        await advance('loop-next', 4);
        assert.equal(await text('#loop-output'), expected);
      }
      await page.selectOption('#loop-reply', 'tool');
      await page.selectOption('#loop-format', 'markdown');
      await advance('loop-next', 2);
      assert.equal(
        await text('#loop-output'),
        `{
  "id": "call-1",
  "type": "function",
  "function": {
    "name": "lookup_order",
    "arguments": "{\\"order_id\\":42}"
  }
}`,
      );
      await click('answer', '1');
      assert.equal(await page.locator('.answer-feedback strong').textContent(), 'Exactly.');
    },
  );
  await check(
    'compaction changes selection, preserves originals, and exposes earlier revisions',
    async () => {
      await lesson('memory');
      await click('memory-edit', 'window');
      assert.deepEqual(
        await Promise.all(['#history-count', '#active-count', '#selected-ids'].map(text)),
        ['5', '2', '[H3, H4]'],
      );
      await click('reset');
      await page.check('#pin-policy');
      await click('memory-edit', 'summary');
      assert.deepEqual(
        await Promise.all(['#history-count', '#active-count', '#selected-ids'].map(text)),
        ['6', '3', '[S2, H1, H4]'],
      );
      assert.deepEqual(
        await page
          .locator('.history-record')
          .evaluateAll((nodes) => nodes.map((node) => node.dataset.record)),
        ['H1', 'H2', 'H3', 'H4', 'S2', 'C2'],
      );
      await page.locator('#memory-revision').focus();
      const slider = await page.locator('#memory-revision').elementHandle();
      await page.keyboard.press('Home');
      assert.equal(await text('#selected-ids'), '[H1, H2, H3, H4]');
      assert.equal(await page.locator('[data-action="memory-edit"]').first().isDisabled(), true);
      assert.equal(await slider.evaluate((node) => node === document.activeElement), true);
      await page.keyboard.press('End');
      assert.equal(await text('#selected-ids'), '[S2, H1, H4]');
      await click('memory-edit', 'reverse');
      assert.equal(await text('#selected-ids'), '[H4, H1, S2]');
      assert.equal(await text('#history-count'), '6');
      await page.screenshot({
        path: '/tmp/gent-learning-memory.png',
        fullPage: true,
      });
    },
  );
  await check(
    'lost acknowledgment, retry, success, and fencing move the correct cursors',
    async () => {
      await lesson('save');
      await advance('save-next', 3);
      assert.deepEqual(await saveState(), [
        '121',
        '120',
        '120',
        '1',
        'Stopped — context canceled',
        'Nothing delivered',
      ]);
      await page.screenshot({
        path: '/tmp/gent-learning-save.png',
        fullPage: true,
      });
      await click('save-next');
      assert.deepEqual(await saveState(), [
        '121',
        '121',
        '120',
        '2',
        'Stopped — context canceled',
        'Event 121 delivered once',
      ]);
      const cases = [
        {
          input: 'success',
          expected: [
            '121',
            '121',
            '120',
            '1',
            'Allowed after acceptance',
            'Event 121 delivered once',
          ],
        },
        {
          input: 'rejected',
          expected: ['120', '120', '120', '1', 'Stopped — context canceled', 'Nothing delivered'],
        },
      ];
      for (const { input, expected } of cases) {
        await page.selectOption('#recorder-outcome', input);
        await advance('save-next', 3);
        assert.deepEqual(await saveState(), expected);
      }
    },
  );
  await check(
    'recovery provides guidance and shows duplicate effects when retry is allowed',
    async () => {
      await lesson('restart');
      await click('restore');
      assert.equal(
        await text('#recovery-result'),
        'unknown outcome; model receives recovery guidance',
      );
      assert.equal(
        await text('#recovery-status'),
        'The checkpoint proves intent, not outcome. Gent saves an unknown-outcome result with ' +
          'recovery guidance. The model can check the refund service before making a new plan.',
      );
      assert.equal(await text('#attempt-id'), 'attempt: worker-B');
      await click('recover', 'completed');
      assert.equal(await text('#recovery-result'), 'reconciled with receipt');
      assert.equal(await text('#refund-count'), '1');
      await page.uncheck('#idempotent-service');
      await click('restore');
      await click('recover', 'retry');
      assert.equal(await text('#refund-count'), '2');
      await page.screenshot({
        path: '/tmp/gent-learning-restart.png',
        fullPage: true,
      });
      await page.check('#idempotent-service');
      await click('restore');
      await click('recover', 'retry');
      assert.equal(await text('#refund-count'), '1');
      await page.selectOption('#crash-point', 'after-result');
      await click('restore');
      assert.equal(await text('#recovery-result'), 'saved result reused');
      assert.equal(await page.locator('[data-action="recover"]').first().isDisabled(), true);
      await page.selectOption('#crash-point', 'before-effect');
      await click('restore');
      await click('recover', 'completed');
      assert.equal(await text('#recovery-result'), 'no external receipt to return');
      assert.equal(await text('#refund-count'), '0');
      await click('recover', 'retry');
      assert.equal(await text('#refund-count'), '1');
      await page.locator('.compatibility-controls summary').click();
      await page.selectOption('#recovery-version', 'v2');
      await click('restore');
      assert.equal(await text('#recovery-result'), 'incompatible version');
      assert.equal(await text('#attempt-id'), 'attempt: worker-A');
      await page.locator('.compatibility-controls summary').click();
      await page.selectOption('#recovery-version', 'v1');
      await page.locator('.compatibility-controls summary').click();
      await page.uncheck('#receipt-codec');
      await click('restore');
      assert.equal(await text('#recovery-result'), 'missing value codec');
      assert.equal(await text('#attempt-id'), 'attempt: worker-A');
    },
  );
  await check(
    'retrieval responds to meaning, weighting, query changes, and tool-chain paging',
    async () => {
      await lesson('search');
      assert.equal(await text('.no-matches h3'), 'No keyword candidates.');
      await click('search-mode', 'semantic');
      assert.deepEqual(
        await page
          .locator('.ranked-tool')
          .evaluateAll((nodes) => nodes.map((node) => node.dataset.tool)),
        ['refund_order', 'lookup_order', 'cancel_reservation'],
      );
      assert.deepEqual(await page.locator('.score-numbers b').allTextContents(), [
        '0.94',
        '0.32',
        '0.06',
      ]);
      await click('search-page', '1');
      assert.equal(await text('#tool-page-name'), 'lookup_order');
      assert.equal(await text('#tool-page-number'), '2 / 3');
      await page.selectOption('#search-query', 'travel');
      assert.equal(await text('#tool-page-name'), 'cancel_reservation');
      await click('search-mode', 'fused');
      await page.locator('#semantic-weight').focus();
      const slider = await page.locator('#semantic-weight').elementHandle();
      await page.keyboard.press('Home');
      assert.equal(await text('#weight-label'), '0%');
      assert.deepEqual(await page.locator('.score-numbers b').allTextContents(), ['0.96', '0.05']);
      await page.keyboard.press('End');
      assert.equal(await text('#weight-label'), '100%');
      assert.deepEqual(await page.locator('.score-numbers b').allTextContents(), [
        '0.97',
        '0.12',
        '0.08',
      ]);
      assert.equal(await slider.evaluate((node) => node === document.activeElement), true);
      await page.screenshot({
        path: '/tmp/gent-learning-search.png',
        fullPage: true,
      });
    },
  );
  await check('all 47 interfaces have on-demand contracts inside their lesson', async () => {
    const covered = [];
    for (const item of lessons) {
      await lesson(item.id);
      await page.locator('.lesson-contracts summary').click();
      for (const id of item.contracts) {
        await page.locator(`.lesson-contract-grid [data-contract="${id}"]`).click();
        assert.equal(
          await text('#lesson-contract-code'),
          catalog.declarations.find((entry) => entry.id === id).code.trim(),
        );
        await page.keyboard.press('Escape');
        assert.equal(await page.locator('#lesson-contract').isVisible(), false);
        covered.push(id);
      }
    }
    assert.deepEqual(
      covered.sort(),
      catalog.declarations
        .filter((item) => item.kind === 'interface')
        .map((item) => item.id)
        .sort(),
    );
    assert.equal(await text('#lesson-coverage'), '5 / 5 explored · all 47 interfaces connected');
  });
  await check('each experiment fits narrow and tablet viewports and remains usable', async () => {
    for (const width of [900, 736, 390, 320]) {
      await page.setViewportSize({ width, height: 1000 });
      for (const item of lessons) {
        await lesson(item.id);
        await noOverflow(`${item.id} at ${width}px`);
        if (width === 390) {
          await page.screenshot({
            path: `/tmp/gent-learning-${item.id}-mobile.png`,
            fullPage: true,
          });
        }
      }
    }
  });
  await check('lessons can be shared directly and the reference remains secondary', async () => {
    await page.goto(`${url}#view=learn&lesson=save`);
    assert.equal(await text('#stored-cursor'), '120');
    await page
      .locator('.control-note [data-contract="gent.Recorder"]')
      .evaluate((node) => node.click());
    await page.locator('#lesson-contract [data-open]').click();
    assert.equal(await page.locator('#lesson-contract').isVisible(), false);
    assert.equal(await text('#selected-contract'), 'gent.Recorder');
    await page.click('[data-view="learn"]');
    assert.equal(await page.locator('#learn').isVisible(), true);
  });
  assert.deepEqual(errors, []);
  assert.deepEqual(requests, []);
  console.log('PASS no console errors, page errors, or external network requests.');
} finally {
  await browser.close();
}
