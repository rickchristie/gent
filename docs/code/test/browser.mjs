import assert from 'node:assert/strict';
import { chromium } from 'playwright';

const url = new URL('../../dist/gent.html', import.meta.url).href;
const browser = await chromium.launch({ headless: true });
const failures = [];
const requests = [];
const context = await browser.newContext({
  viewport: { width: 1440, height: 1050 },
  offline: true,
});
const page = await context.newPage();
page.on('pageerror', (error) => failures.push(error.message));
page.on('console', (message) => {
  if (message.type() === 'error') failures.push(message.text());
});
page.on('request', (request) => {
  if (/^https?:/.test(request.url())) requests.push(request.url());
});
const text = (selector) => page.locator(selector).textContent();
const click = (selector) => page.locator(selector).click();
const setRange = (selector, value) =>
  page.locator(selector).evaluate((element, next) => {
    element.value = next;
    element.dispatchEvent(new Event('input', { bubbles: true }));
  }, String(value));
const check = async (name, action) => {
  await action();
  console.log(`PASS ${name}`);
};

try {
  await page.goto(url);
  await page.evaluate(() => document.fonts.ready);
  await page.locator('#run-step-title').waitFor();
  await check('offline HTML initializes with embedded fonts and 46 atlas entries', async () => {
    assert.equal(await page.title(), 'Gent · A field guide to agents that remember');
    assert.equal(await page.locator('.atlas-row').count(), 46);
    assert.equal(await page.evaluate(() => document.fonts.check('16px "DM Sans"')), true);
    assert.equal(await page.evaluate(() => document.fonts.check('48px "Instrument Serif"')), true);
  });
  // Pause motion for reproducible screenshots and controls; explicit waits check readiness.
  await click('#motion-toggle');
  await page.screenshot({ path: '/tmp/gent-guide-desktop.png' });
  await check('request walkthrough reaches the saved terminal result and can rewind', async () => {
    await setRange('#run-slider', 10);
    assert.equal(await text('#run-phase'), 'terminal');
    assert.equal(await text('#run-tokens'), '56');
    assert.equal(await text('#run-tools'), '1');
    assert.equal(await page.locator('.run-record').count(), 2);
    assert.equal(await page.locator('#run-next').isDisabled(), true);
    await click('#run-back');
    assert.equal(await text('#run-phase'), 'decision_ready');
    await setRange('#run-slider', 0);
    assert.equal(await text('#run-step-title'), 'Start with a request.');
  });
  await check(
    'system map explains the selected component and opens its actual contract',
    async () => {
      await click('[data-map="journal"]');
      assert.equal(await text('#map-detail h3'), 'Run recording');
      await click('#map-detail [data-inspect="journal"]');
      assert.equal(await text('#dialog-title'), 'Run recording');
      await page.keyboard.press('Escape');
      assert.equal(await page.locator('#detail-dialog').isVisible(), false);
    },
  );
  await page.locator('#system-map').scrollIntoViewIfNeeded();
  await page.screenshot({ path: '/tmp/gent-guide-system.png' });
  await check('format selector and answer/error routes update together', async () => {
    await page.selectOption('#format-select', 'markdown');
    await click('[data-response="answer"]');
    assert.equal(
      await text('#parser-output'),
      `System prompt:
# instructions
Use tools to check the policy.

Model response:
Refunds are allowed within 14 days.`,
    );
    await click('[data-response="invalid"]');
    assert.equal(
      await text('#parser-output'),
      `System prompt:
# instructions
Use tools to check the policy.

Model response:
{
  "id": "call-1",
  "type": "function",
  "function": {
    "name": "policy",
    "arguments": "{"
  }
}`,
    );
    await click('[data-response="tool"]');
  });
  await check('two compactions keep full evidence and every earlier selection', async () => {
    await click('#history-compact');
    await click('#history-compact');
    assert.equal(await page.locator('.history-entry').count(), 6);
    assert.equal(
      await text('#history-selection .context-sheet p'),
      'The user requests a refund after 10 days. Policy P10 has a 14-day refund window.',
    );
    await click('[data-entry="B"]');
    assert.equal(
      await text('#dialog-content .dialog-prose p:first-child'),
      'Policy P10, version 4. Refunds are allowed within 14 days of purchase. Opened products ' +
        'are excluded unless defective. The receipt and original order ID are required.',
    );
    await page.keyboard.press('Escape');
    await setRange('#history-slider', 1);
    assert.equal(await page.locator('#history-selection .context-sheet').count(), 2);
    await setRange('#history-slider', 3);
  });
  await page.locator('#history').scrollIntoViewIfNeeded();
  await page.screenshot({ path: '/tmp/gent-guide-history.png' });
  await check(
    'lost acknowledgment reconciles the exact batch without starting another effect',
    async () => {
      await click('[data-journal-mode="lost"]');
      for (let step = 0; step < 3; step += 1) await click('#journal-next');
      assert.equal(await text('#journal-accepted'), '120');
      assert.equal(await text('#journal-stored'), '121');
      assert.equal(await text('#journal-provider'), 'Stopped');
      await click('#journal-next');
      assert.equal(await text('#journal-accepted'), '121');
      assert.equal(await text('#journal-stored'), '121');
      assert.equal(await text('#journal-provider'), 'Stopped');
      assert.equal(await text('#journal-batch-id'), 'refund-42/b17 · EVENT 121 · SAVE CALLS 2');
    },
  );
  await page.locator('#journal').scrollIntoViewIfNeeded();
  await page.screenshot({ path: '/tmp/gent-guide-journal.png' });
  await check(
    'successful and stale-writer scenarios distinguish acceptance from rejection',
    async () => {
      for (const mode of ['success', 'rejected']) {
        await click(`[data-journal-mode="${mode}"]`);
        for (let step = 0; step < 3; step += 1) await click('#journal-next');
        assert.equal(await text('#journal-accepted'), mode === 'success' ? '121' : '120');
        assert.equal(await text('#journal-provider'), mode === 'success' ? 'Allowed' : 'Stopped');
      }
    },
  );
  await check(
    'recovery reuses known work and requires explicit uncertain-tool decisions',
    async () => {
      for (const scenario of ['partial', 'saved', 'tool', 'terminal']) {
        await page.selectOption('#recovery-scenario', scenario);
        await click('#recovery-next');
        await click('#recovery-next');
        if (scenario === 'tool') {
          assert.equal(await page.locator('#tool-recovery-actions').isVisible(), true);
          assert.equal(await page.locator('#recovery-next').isDisabled(), true);
          await click('[data-tool-recovery="unresolved"]');
          assert.equal(await text('#worker-b-state'), 'Model receives recovery guidance');
        } else if (scenario === 'terminal') {
          assert.equal(await text('#recovery-cursor'), '128');
          assert.equal(await text('#worker-b-state'), 'Returned saved result');
        } else {
          assert.equal(await text('#recovery-cursor'), '129+');
          await click('#recovery-next');
          assert.equal(await text('#worker-b-state'), 'Continued');
        }
      }
      for (const decision of ['completed', 'retry']) {
        await page.selectOption('#recovery-scenario', 'tool');
        await click('#recovery-reset');
        await click('#recovery-next');
        await click('#recovery-next');
        await click(`[data-tool-recovery="${decision}"]`);
        assert.equal(await text('#worker-b-state'), 'Decision applied');
      }
    },
  );
  await check('viewer buffers the gap and periodically discovers the lost finish', async () => {
    await click('#viewer-next');
    await click('#viewer-next');
    assert.equal(await text('#viewer-cursor'), '128');
    assert.equal(await page.locator('#viewer-events .buffered').count(), 1);
    await click('#viewer-next');
    assert.equal(await text('#viewer-cursor'), '130');
    await click('#viewer-next');
    await click('#viewer-next');
    assert.equal(await text('#viewer-status'), 'Quietly stale');
    assert.equal(await text('#viewer-cursor'), '131');
    await click('#viewer-next');
    assert.equal(await text('#viewer-cursor'), '132');
    assert.equal(await text('#viewer-status'), 'Terminal result recovered');
  });
  await check(
    'stats propagate counters, isolate gauges, and enforce both budget scopes',
    async () => {
      await click('#stats-fail');
      await click('#stats-fail');
      assert.equal(await text('#stats-errors'), '2');
      await click('#stats-succeed');
      assert.equal(await text('#stats-errors'), '0');
      await click('#stats-add-child');
      await click('#stats-add-child');
      assert.equal(await text('#stats-total'), '110');
      assert.equal(await page.locator('#stats-add-root').isDisabled(), true);
      await page.selectOption('#stats-scope', 'self');
      for (let call = 0; call < 6; call += 1) await click('#stats-add-child');
      assert.equal(await text('#stats-total'), '170');
      assert.equal(await text('#stats-self'), '60');
      assert.equal(await page.locator('#stats-add-child').isDisabled(), true);
      assert.equal(await page.locator('#stats-add-root').isDisabled(), false);
    },
  );
  await check('recipes expose the embedded source and each tab renders its exact Go', async () => {
    const payload = await page
      .locator('#guide-data')
      .evaluate((element) => JSON.parse(element.textContent));
    for (const recipe of payload.recipes) {
      await click(`[data-recipe="${recipe.id}"]`);
      assert.equal(await text('#recipe-code'), recipe.code);
    }
    await click('#open-example');
    assert.equal(await text('#dialog-content pre code'), payload.example);
    await page.keyboard.press('Escape');
  });
  await check(
    'every atlas contract opens, search filters, and related links remain navigable',
    async () => {
      const payload = await page
        .locator('#guide-data')
        .evaluate((element) => JSON.parse(element.textContent));
      for (const component of payload.components) {
        await click(`.atlas-row[data-inspect="${component.id}"]`);
        assert.equal(await text('#dialog-title'), component.name);
        assert.equal(await text('#dialog-content pre code'), component.code);
        await click('#close-dialog');
      }
      await page.fill('#atlas-search', 'run recording');
      assert.equal(await page.locator('.atlas-row[data-inspect="journal"]').isVisible(), true);
      await page.selectOption('#atlas-layer', 'Persistence');
      const rows = await page.locator('.atlas-row:not([hidden]) small').allTextContents();
      assert.ok(rows.length > 0);
      assert.deepEqual([...new Set(rows)], ['Persistence']);
      await page.fill('#atlas-search', 'no-such-component');
      assert.equal(await page.locator('#atlas-empty').isVisible(), true);
      await page.fill('#atlas-search', '');
      await page.selectOption('#atlas-layer', 'all');
      await click('.atlas-row[data-inspect="loopdata"]');
      await click('#detail-dialog [data-inspect="extension"]');
      assert.equal(await text('#dialog-title'), 'CheckpointState');
      await page.keyboard.press('Escape');
    },
  );
  await check('knowledge check gives corrective feedback and tracks all four answers', async () => {
    await click('[data-question="0"][data-option="1"]');
    assert.equal(await text('#quiz-progress'), '0 of 4 ideas connected.');
    for (const [index, answer] of [0, 1, 2, 1].entries()) {
      await click(`[data-question="${index}"][data-option="${answer}"]`);
    }
    assert.equal(await text('#quiz-progress'), '4 of 4 ideas connected. You have the core model.');
  });
  await check('motion control stops autoplay and manual steps remain usable', async () => {
    await page.clock.install();
    await click('#motion-toggle');
    await setRange('#run-slider', 0);
    await click('#run-play');
    assert.equal(await page.locator('#run-play').getAttribute('aria-pressed'), 'true');
    await page.clock.fastForward(9100);
    assert.equal(await text('#run-step-title'), 'Open iteration one.');
    await click('#motion-toggle');
    assert.equal(await page.locator('#run-play').isDisabled(), true);
    assert.equal(await page.locator('#run-play').getAttribute('aria-pressed'), 'false');
    await page.clock.fastForward(20000);
    assert.equal(await text('#run-step-title'), 'Open iteration one.');
    await click('#run-next');
    assert.equal(await text('#run-step-title'), 'Record the effective request.');
  });
  await check(
    'all chapters and diagrams fit a narrow viewport without horizontal overflow',
    async () => {
      await page.setViewportSize({ width: 390, height: 844 });
      await page.evaluate(() => window.scrollTo(0, 0));
      assert.equal(
        await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
        true,
      );
      await page.screenshot({ path: '/tmp/gent-guide-mobile.png' });
      for (const id of ['system-map', 'history', 'journal', 'recovery', 'viewers', 'atlas']) {
        await page.locator(`#${id}`).scrollIntoViewIfNeeded();
        assert.equal(
          await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
          true,
          id,
        );
      }
      await page.locator('#journal').scrollIntoViewIfNeeded();
      await page.screenshot({ path: '/tmp/gent-guide-mobile-journal.png' });
      await click('.atlas-row[data-inspect="journal"]');
      assert.equal(await text('#dialog-title'), 'Run recording');
      await page.keyboard.press('Escape');
    },
  );
  await check('reduced-motion preference is respected on a fresh load', async () => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.reload();
    assert.equal(await page.locator('#motion-toggle').getAttribute('aria-pressed'), 'true');
    assert.equal(await page.locator('#run-play').isDisabled(), true);
    await click('#run-next');
    assert.equal(await text('#run-step-title'), 'Open iteration one.');
  });
  assert.deepEqual(failures, [], 'Browser errors');
  assert.deepEqual(requests, [], 'External network requests');
  console.log('PASS zero browser errors and zero external requests while offline on file://');
  console.log('Screenshots saved to /tmp/gent-guide-*.png for desktop, chapters, and mobile.');
} finally {
  await browser.close();
}
