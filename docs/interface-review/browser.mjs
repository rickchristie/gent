import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';

// Reuse the field guide's installed test dependency without changing its package or lockfile.
const require = createRequire(new URL('../code/package.json', import.meta.url));
const { chromium } = require('playwright');
const launch = { headless: true };
if (process.env.GENT_REVIEW_CHROMIUM) launch.executablePath = process.env.GENT_REVIEW_CHROMIUM;
const browser = await chromium.launch(launch);
const url = new URL('./gent-interfaces.html', import.meta.url).href;
const catalog = JSON.parse(await readFile('/tmp/gent-interface-catalog.json', 'utf8'));
const expectedInterfaces = catalog.declarations.filter((item) => item.kind === 'interface');
const context = await browser.newContext({
  viewport: { width: 1440, height: 1000 },
  offline: true,
  reducedMotion: 'reduce',
});
const page = await context.newPage();
const failures = [];
const network = [];
const passed = [];
page.on('pageerror', (error) => failures.push(error.message));
page.on('console', (message) => {
  if (message.type() === 'error') failures.push(message.text());
});
page.on('request', (request) => {
  if (/^https?:/.test(request.url())) network.push(request.url());
});
const text = (selector) => page.locator(selector).textContent();
async function check(name, run) {
  await run();
  passed.push(name);
  console.log(`PASS ${name}`);
}
async function noOverflow(label) {
  const widths = await page.evaluate(() => ({
    page: document.documentElement.scrollWidth,
    viewport: document.documentElement.clientWidth,
  }));
  assert.ok(widths.page <= widths.viewport, `${label}: ${JSON.stringify(widths)}`);
}
async function open(id) {
  await page.goto(`${url}#view=atlas&item=${encodeURIComponent(id)}`);
  await page.locator('#selected-contract').waitFor();
  assert.equal(await text('#selected-contract'), id);
}

try {
  await page.goto(url);
  await page.evaluate(() => document.fonts.ready);
  await check('offline guide initializes with complete source inventory', async () => {
    assert.equal(await page.title(), 'Gent — An interactive explainer');
    assert.equal(expectedInterfaces.length, 53);
    assert.equal(await text('#interface-count'), '53named interfaces');
    assert.equal(
      await text('#scope-detail'),
      '16 new or changed declarations · 18 supporting types · 6 structural capabilities',
    );
    assert.equal(await page.locator('.map-group').count(), 7);
    assert.equal(await page.evaluate(() => document.fonts.check('16px GuideSans')), true);
    assert.equal(await page.evaluate(() => document.fonts.check('16px GuideMono')), true);
    assert.equal(await page.evaluate(() => document.fonts.check('48px GuideSerif')), true);
  });
  await page.screenshot({ path: '/tmp/gent-interface-architecture-desktop.png', fullPage: true });

  await check(
    'every named interface has exact current source and a valid source line',
    async () => {
      for (const item of expectedInterfaces) {
        await open(item.id);
        assert.equal(await text('#selected-code'), item.code);
        assert.equal(await text('.contract-detail > .source-line a'), `${item.file}:${item.line}`);
        const source = await readFile(new URL(`../../${item.file}`, import.meta.url), 'utf8');
        assert.equal(source.split('\n')[item.line - 1].trimStart().split(/\s|\[/)[1], item.name);
        assert.equal(
          await page.locator('.contract-row[aria-current]').getAttribute('data-open'),
          item.id,
        );
        await noOverflow(item.id);
      }
    },
  );

  await check('search, group, kind, and change filters produce complete result sets', async () => {
    await open('gent.LoopData');
    await page.fill('#search', 'gent.Recorder');
    assert.deepEqual(
      await page
        .locator('.contract-row')
        .evaluateAll((nodes) => nodes.map((node) => node.dataset.open)),
      ['gent.Recorder'],
    );
    await page.locator('.contract-row').click();
    assert.equal(await text('#selected-contract'), 'gent.Recorder');
    await page.fill('#search', 'NoSuchGentContract');
    assert.equal(await text('#result-count'), '0 matching contracts');
    await page.click('#clear-filters');
    assert.equal(await page.locator('.contract-row').count(), 53);
    await page.selectOption('#group', 'Search');
    assert.deepEqual(
      (
        await page
          .locator('.contract-row')
          .evaluateAll((nodes) => nodes.map((node) => node.dataset.open))
      ).sort(),
      [
        'gent.IndexableTool',
        'gent.ToolSearcher',
        'search.BleveAdapter',
        'search.BleveIDFProvider',
        'search.ChunkAdapter',
        'search.Embedder',
        'search.Fuser',
        'search.SearchIndex',
        'search.TokenCounter',
      ],
    );
    await page.click('#clear-filters');
    await page.selectOption('#change', 'different');
    assert.deepEqual(
      (
        await page
          .locator('.contract-row')
          .evaluateAll((nodes) => nodes.map((node) => node.dataset.open))
      ).sort(),
      expectedInterfaces
        .filter((item) => item.change !== 'same')
        .map((item) => item.id)
        .sort(),
    );
    await page.click('#clear-filters');
    await page.selectOption('#kind', 'support');
    assert.equal(await page.locator('.contract-row').count(), 18);
    const supports = await page
      .locator('.contract-row')
      .evaluateAll((nodes) => nodes.map((node) => node.dataset.open));
    for (const id of supports) {
      await open(id);
      assert.equal(
        await text('#selected-code'),
        catalog.declarations.find((item) => item.id === id).code,
      );
      await noOverflow(id);
    }
  });

  await check(
    'embedded contracts, method comments, and previous signatures are inspectable',
    async () => {
      await open('gent.RecoverableToolChain');
      await page.locator('.detail-section .related [data-open="gent.ToolChain"]').first().click();
      assert.equal(await text('#selected-contract'), 'gent.ToolChain');
      await page.locator('.detail-section .related [data-open="gent.TextSection"]').first().click();
      assert.equal(await text('#selected-contract'), 'gent.TextSection');
      await page.getByText('Declared methods (3)', { exact: true }).click();
      await page.locator('.method-row summary').first().click();
      assert.equal(await page.locator('.method-row[open]').count(), 1);
      await open('gent.Model');
      await page
        .getByText(`Previous declaration at ${catalog.head.slice(0, 8)}`, { exact: true })
        .click();
      assert.equal(
        await text('.previous-code code'),
        catalog.declarations.find((item) => item.id === 'gent.Model').previous,
      );
      await page.screenshot({ path: '/tmp/gent-interface-model-desktop.png', fullPage: true });
    },
  );

  const reviewText = `Check writer fencing & <ownership>.
Can we explain a lost acknowledgment?`;
  await check(
    'review notes persist, remain escaped, and export the exact discussion text',
    async () => {
      await open('gent.Recorder');
      await page.locator('[data-status="question"]').click();
      await page.fill('#review-note', reviewText);
      await page.reload();
      assert.equal(await page.inputValue('#review-note'), reviewText);
      assert.equal(
        await page.locator('[data-status="question"]').getAttribute('aria-pressed'),
        'true',
      );
      await page.click('[data-view="review"]');
      assert.equal(await text('.review-item p'), reviewText);
      const download = page.waitForEvent('download');
      await page.click('[data-export]');
      const file = await download;
      await file.saveAs('/tmp/gent-interface-review-export.md');
      const recorder = expectedInterfaces.find((item) => item.id === 'gent.Recorder');
      assert.equal(
        await readFile('/tmp/gent-interface-review-export.md', 'utf8'),
        `# Gent interface review

Source fingerprint: ${catalog.fingerprint}
Comparison HEAD: ${catalog.head}

This review refers to the source snapshot embedded in gent-interfaces.html.

## gent.Recorder

Status: Discuss
Source: ${recorder.file}:${recorder.line}

${reviewText}
`,
      );
    },
  );

  await check('all recovery phases and save outcomes expose distinct contracts', async () => {
    await page.click('[data-view="recovery"]');
    const phases = [
      'initial',
      'iteration_ready',
      'decision_ready',
      'tool_ready',
      'tool_completed',
      'iteration_completed',
      'compaction_completed',
      'terminal',
    ];
    for (let index = 0; index < phases.length; index++) {
      await page.click(`[data-phase="${index}"]`);
      assert.equal(await text('.phase-label'), phases[index]);
      assert.equal(await page.locator('[data-phase][aria-pressed="true"]').count(), 1);
    }
    const outcomes = [
      {
        input: 'success',
        expected: ['Batch committed', 'Cursor advances', 'Delivery may proceed'],
      },
      { input: 'lost', expected: ['Batch committed', 'Cursor unchanged', 'Execution stopped'] },
      { input: 'rejected', expected: ['Batch rejected', 'Cursor unchanged', 'Execution stopped'] },
    ];
    for (const scenario of outcomes) {
      await page.selectOption('#save-outcome', scenario.input);
      assert.deepEqual(
        await page.locator('.save-lane strong').allTextContents(),
        scenario.expected,
      );
    }
    assert.deepEqual((await page.locator('.capability h3').allTextContents()).sort(), [
      'ConfigureExecution',
      'EventBase',
      'ModelName',
      'Provider',
      'RegisterCodecs',
      'Unwrap',
    ]);
    await page.selectOption('#save-outcome', 'lost');
    await page.screenshot({ path: '/tmp/gent-interface-recovery-desktop.png', fullPage: true });
  });

  await check('keyboard search and native dialog behavior work', async () => {
    await page.keyboard.press('/');
    assert.equal(
      await page.locator('#search').evaluate((node) => document.activeElement === node),
      true,
    );
    await page.click('#show-licenses');
    assert.equal(await page.locator('#licenses').isVisible(), true);
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#licenses').isVisible(), false);
    await page.locator('#search').focus();
    await page.keyboard.press('Tab');
    assert.equal(
      await page.locator('#group').evaluate((node) => document.activeElement === node),
      true,
    );
  });

  await check('denied clipboard access offers the exact signature and reference', async () => {
    const denied = await context.newPage();
    await denied.addInitScript(() => {
      Object.defineProperty(navigator, 'clipboard', {
        value: {
          writeText: async () => {
            throw new DOMException('Clipboard denied', 'NotAllowedError');
          },
        },
      });
    });
    await denied.goto(`${url}#view=atlas&item=gent.Recorder`);
    await denied.click('#copy-signature');
    assert.equal(
      await denied.inputValue('#manual-copy'),
      expectedInterfaces.find((item) => item.id === 'gent.Recorder').code,
    );
    await denied.keyboard.press('Escape');
    await denied.click('#copy-reference');
    const item = expectedInterfaces.find((item) => item.id === 'gent.Recorder');
    assert.equal(
      await denied.inputValue('#manual-copy'),
      `${item.id} — ${item.file}:${item.line} (source ${catalog.fingerprint})`,
    );
    await denied.close();
  });

  await check('narrow layouts fit and every mobile contract remains readable', async () => {
    for (const width of [900, 736, 390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      for (const view of ['architecture', 'atlas', 'recovery', 'review']) {
        await page.goto(`${url}#view=${view}`);
        await noOverflow(`${view} at ${width}px`);
      }
    }
    await page.setViewportSize({ width: 390, height: 844 });
    for (const item of expectedInterfaces) {
      await open(item.id);
      await noOverflow(`mobile ${item.id}`);
      assert.equal(await text('#selected-code'), item.code);
      const size = await page
        .locator('#selected-code')
        .evaluate((node) => parseFloat(getComputedStyle(node).fontSize));
      assert.ok(size >= 12);
    }
    await page.goto(`${url}#view=architecture`);
    await page.screenshot({ path: '/tmp/gent-interface-architecture-mobile.png', fullPage: true });
    await page.locator('[data-open="gent.LoopData"]').first().click();
    assert.equal(await page.locator('#catalog-panel').getAttribute('open'), null);
    await page.screenshot({ path: '/tmp/gent-interface-atlas-mobile.png', fullPage: true });
  });
  assert.deepEqual(failures, []);
  assert.deepEqual(network, []);
  console.log('PASS zero browser errors and zero external requests');
} finally {
  await writeFile(
    '/tmp/gent-interface-browser-report.json',
    JSON.stringify({ passed, failures, network }, null, 2),
  );
  await browser.close();
}
