import assert from 'node:assert/strict';
import { chromium } from 'playwright';

const browser = await chromium.launch();
const failures = [];
const requests = [];

try {
  const page = await browser.newPage({
    viewport: { width: 390, height: 844 },
    offline: true,
    reducedMotion: 'reduce',
  });
  page.on('pageerror', (error) => failures.push(error.message));
  page.on('console', (message) => {
    if (message.type() === 'error') failures.push(message.text());
  });
  page.on('request', (request) => {
    if (/^https?:/.test(request.url())) requests.push(request.url());
  });
  await page.goto(new URL('../../dist/gent.html', import.meta.url).href);
  const payload = await page.locator('#guide-data').evaluate((e) => JSON.parse(e.textContent));

  // A reader deep in the guide can change chapters without returning to the opening.
  await page.locator('#viewers').evaluate((e) => e.scrollIntoView({ behavior: 'instant' }));
  await page.locator('#chapter-nav a[href="#viewers"][aria-current="location"]').waitFor();
  const nav = await page.locator('#chapter-nav').boundingBox();
  const header = await page.locator('.masthead').boundingBox();
  assert.equal(nav.y, header.height);
  await page.locator('#chapter-nav a[href="#guardrails"]').click();
  await page.locator('#chapter-nav a[href="#guardrails"][aria-current="location"]').waitFor();
  const chapter = await page.locator('#guardrails .chapter-heading').boundingBox();
  assert.ok(chapter.y >= nav.y + nav.height, 'Chapter title stays below navigation');
  console.log('PASS mobile chapter navigation remains visible and reaches the requested chapter');

  await page.locator('.atlas-row[data-inspect="trace"]').evaluate((e) => {
    e.scrollIntoView({ behavior: 'instant', block: 'center' });
  });
  const filters = await page.locator('.atlas-controls').boundingBox();
  assert.equal(filters.y, header.height + nav.height);
  await page.locator('#atlas-search').fill('no-such-component');
  assert.equal(await page.locator('.atlas-row:not([hidden])').count(), 0);
  assert.equal(await page.locator('#atlas-empty').isVisible(), true);
  await page.locator('#atlas-search').fill('');
  console.log('PASS atlas filters remain usable while browsing lower entries');

  // Every declaration must be readable without a second vertical or horizontal code scroller.
  for (const viewport of [
    { width: 390, height: 844 },
    { width: 1440, height: 1000 },
  ]) {
    await page.setViewportSize(viewport);
    for (const component of payload.components) {
      await page.locator(`.atlas-row[data-inspect="${component.id}"]`).click();
      const code = page.locator('.dialog-code');
      assert.equal(await code.textContent(), component.code);
      assert.equal(await page.locator('#dialog-current').textContent(), component.name);
      const overflow = await code.evaluate((e) => ({
        horizontal: e.scrollWidth > e.clientWidth,
        vertical: e.scrollHeight > e.clientHeight,
      }));
      assert.deepEqual(overflow, { horizontal: false, vertical: false }, component.name);
      await page.locator('#detail-dialog').evaluate((e) => {
        e.scrollTo({ top: e.scrollHeight, behavior: 'instant' });
      });
      const dialog = await page.locator('#detail-dialog').boundingBox();
      const title = await page.locator('#dialog-current').boundingBox();
      assert.ok(title.y >= dialog.y && title.y + title.height <= dialog.y + dialog.height);
      await page.keyboard.press('Escape');
    }
  }
  console.log('PASS all 46 contracts wrap, scroll, and retain their names on mobile and desktop');

  await page.locator('#history-reset').click();
  const selected = () => page.locator('.history-entry.in-context .entry-id').allTextContents();
  assert.deepEqual(await selected(), ['A', 'B']);
  await page.locator('#history-compact').click();
  assert.deepEqual(await selected(), ['S1']);
  await page.locator('#history-compact').click();
  assert.deepEqual(await selected(), ['S2']);
  await page.locator('#history-slider').focus();
  await page.keyboard.press('Home');
  assert.deepEqual(await selected(), ['A', 'B']);
  assert.equal(await page.locator('.history-entry').count(), 6);
  console.log('PASS active context markers follow compaction and rewind without losing evidence');

  // A user click can grant clipboard access in some browsers. Force denial for the fallback check.
  await page.evaluate(() => {
    window.savedClipboardWrite = navigator.clipboard.writeText.bind(navigator.clipboard);
    navigator.clipboard.writeText = async () => {
      throw new DOMException('Clipboard access denied', 'NotAllowedError');
    };
  });
  await page.locator('#copy-recipe').click();
  await page.locator('#toast.visible').waitFor();
  assert.equal(await page.locator('#toast').textContent(), 'Code selected. Press Ctrl+C to copy.');
  assert.equal(await page.evaluate(() => getSelection().toString()), payload.recipes[0].code);
  await page.evaluate(() => {
    navigator.clipboard.writeText = window.savedClipboardWrite;
    delete window.savedClipboardWrite;
  });
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.locator('#copy-recipe').click();
  await page.waitForFunction(
    () => document.getElementById('toast').textContent === 'Go example copied.',
  );
  assert.equal(await page.locator('#toast').textContent(), 'Go example copied.');
  console.log('PASS clipboard fallback selects exact code; granted access copies successfully');

  assert.deepEqual(failures, [], 'Browser errors');
  assert.deepEqual(requests, [], 'External requests');
} finally {
  await browser.close();
}
