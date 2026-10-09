import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { chromium } from 'playwright';
import { captureStates } from './screenshot-scenarios.mjs';
import { createGallery } from './screenshot-gallery.mjs';

const project = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const argument = process.argv.find((value) => value.startsWith('--label='));
const label = argument?.slice('--label='.length) || new Date().toISOString().replace(/[:.]/g, '-');
if (!/^[a-zA-Z0-9_-]+$/.test(label))
  throw new Error('Use letters, digits, hyphens, or underscores.');
const directory = join(project, 'screenshots', label);
await mkdir(join(project, 'screenshots'), { recursive: true });
// Refuse to overwrite a prior run: its screenshots may already be cited in a design review.
await mkdir(directory);
await mkdir(join(directory, 'images'));
const source = await readFile(join(project, '../dist/gent.html'));
const manifest = {
  label,
  sourceSHA256: createHash('sha256').update(source).digest('hex'),
  browser: 'Chromium',
  captures: [],
  pageErrors: [],
  externalRequests: [],
  coverage: {},
};
const browser = await chromium.launch({ headless: true });
const profiles = [
  { name: 'desktop', width: 1440, height: 1000 },
  { name: 'mobile', width: 390, height: 844 },
  { name: 'tablet', width: 900, height: 1000 },
];

async function settle(page) {
  await page.evaluate(
    () =>
      new Promise((done) =>
        requestAnimationFrame(() => {
          requestAnimationFrame(done);
        }),
      ),
  );
}

try {
  for (const profile of profiles) {
    const page = await browser.newPage({
      viewport: { width: profile.width, height: profile.height },
      deviceScaleFactor: 1,
      offline: true,
      reducedMotion: 'no-preference',
    });
    page.on('pageerror', (error) => manifest.pageErrors.push(`${profile.name}: ${error.message}`));
    page.on('console', (message) => {
      if (message.type() === 'error')
        manifest.pageErrors.push(`${profile.name}: ${message.text()}`);
    });
    page.on('request', (request) => {
      if (/^https?:/.test(request.url())) manifest.externalRequests.push(request.url());
    });
    await page.goto(pathToFileURL(join(project, '../dist/gent.html')).href);
    await page.evaluate(() => document.fonts.ready);
    const payload = await page.locator('#guide-data').evaluate((e) => JSON.parse(e.textContent));

    async function saveCapture(title, group, selector, note = '') {
      const id = String(manifest.captures.length + 1).padStart(3, '0');
      const slug = title
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/-$/, '');
      const file = `images/${id}-${profile.name}-${slug}.png`;
      await settle(page);
      const metrics = await page.evaluate(() => ({
        scrollY,
        viewportWidth: innerWidth,
        viewportHeight: innerHeight,
        documentWidth: document.documentElement.scrollWidth,
        chapters: [...document.querySelectorAll('.chapter')]
          .filter((element) => {
            const box = element.getBoundingClientRect();
            return box.bottom > 90 && box.top < innerHeight;
          })
          .map((element) => element.id),
      }));
      const png = selector
        ? await page
            .locator(selector)
            .screenshot({ path: join(directory, file), animations: 'disabled' })
        : await page.screenshot({ path: join(directory, file), animations: 'disabled' });
      // PNG IHDR dimensions avoid a second image dependency in this small documentation project.
      manifest.captures.push({
        id,
        title,
        group,
        device: profile.name,
        file,
        note,
        selector: selector || 'viewport',
        width: png.readUInt32BE(16),
        height: png.readUInt32BE(20),
        ...metrics,
      });
      console.log(`${id} ${profile.name}: ${title}`);
    }

    async function headerHeight() {
      return page.evaluate(() => {
        const header = document.querySelector('.masthead').getBoundingClientRect().height;
        const nav = getComputedStyle(document.documentElement).getPropertyValue(
          '--chapter-nav-height',
        );
        return header + (Number.parseFloat(nav) || 0);
      });
    }

    async function capture(title, group, selector, note = '') {
      if (!selector || selector === '#detail-dialog') {
        await saveCapture(title, group, selector, note);
        return;
      }
      const header = await headerHeight();
      const region = await page.locator(selector).evaluate((element) => {
        const box = element.getBoundingClientRect();
        return { top: scrollY + box.top, height: box.height };
      });
      const available = profile.height - header - 40;
      await scrollTo(selector);
      if (region.height <= available) {
        await saveCapture(title, group, selector, note);
        return;
      }
      // Locator screenshots of tall elements can insert a sticky header into the middle of a
      // panel. Real viewport slices preserve what a reader sees, with overlap between slices.
      const stride = available - 80;
      let part = 1;
      for (let offset = 0; offset < region.height; offset += stride) {
        await page.evaluate(
          (top) => window.scrollTo({ top, behavior: 'instant' }),
          region.top + offset - header - 20,
        );
        await saveCapture(`${title} · scroll ${part++}`, group, undefined, selector);
        if (offset + available >= region.height) break;
      }
    }

    async function scrollTo(selector) {
      const header = await headerHeight();
      await page.locator(selector).evaluate((element, offset) => {
        window.scrollTo({
          top: scrollY + element.getBoundingClientRect().top - offset - 20,
          behavior: 'instant',
        });
      }, header);
      await settle(page);
    }

    async function setRange(selector, value) {
      await page.locator(selector).focus();
      await page.keyboard.press('Home');
      const minimum = Number(await page.locator(selector).getAttribute('min'));
      for (let step = minimum; step < value; step += 1) await page.keyboard.press('ArrowRight');
    }

    async function inspect(title) {
      const dialog = page.locator('#detail-dialog');
      await capture(`${title} · top`, 'Inspectors', '#detail-dialog');
      const region = await dialog.evaluate((element) => ({
        maximum: element.scrollHeight - element.clientHeight,
        stride: element.clientHeight - element.querySelector('.dialog-top').clientHeight - 80,
      }));
      let top = 0;
      let part = 1;
      while (top < region.maximum) {
        top = Math.min(region.maximum, top + region.stride);
        await dialog.evaluate((e, offset) => e.scrollTo({ top: offset, behavior: 'instant' }), top);
        await capture(`${title} · scroll ${part++}`, 'Inspectors', '#detail-dialog');
      }
      const pre = page.locator('#detail-dialog pre').first();
      if (await pre.count()) {
        const overflow = await pre.evaluate((e) => ({
          vertical: e.scrollHeight > e.clientHeight + 2,
          horizontal: e.scrollWidth > e.clientWidth + 2,
        }));
        if (overflow.vertical || overflow.horizontal) {
          await pre.evaluate((e) =>
            e.scrollTo({ top: e.scrollHeight, left: e.scrollWidth, behavior: 'instant' }),
          );
          await capture(`${title} · code scrolled`, 'Inspectors', '#detail-dialog');
        }
      }
      await page.keyboard.press('Escape');
    }

    if (profile.name !== 'tablet') {
      // Overlap plus the sticky header allowance ensures every page pixel is represented.
      const height = await page.evaluate(() => document.documentElement.scrollHeight);
      const header = await headerHeight();
      const filters = await page.locator('.atlas-controls').evaluate((e) => e.clientHeight);
      const step = profile.height - header - filters - 80;
      const positions = [];
      for (let top = 0; top < height - profile.height; top += step) positions.push(top);
      positions.push(Math.max(0, height - profile.height));
      for (let index = 0; index < positions.length; index += 1) {
        await page.evaluate(
          (top) => window.scrollTo({ top, behavior: 'instant' }),
          positions[index],
        );
        await capture(`Page scroll ${String(index + 1).padStart(2, '0')}`, 'Page scroll');
      }
      manifest.coverage[profile.name] = { documentHeight: height, scrollPositions: positions };
    } else {
      // The middle breakpoint has its own sidebar and compressed diagram layout.
      for (const selector of ['.hero', '#system-map', '#journal', '#using-gent', '#atlas']) {
        await scrollTo(selector);
        await capture(`Tablet ${selector.replace(/[.#]/g, '')}`, 'Responsive');
      }
    }
    await captureStates({ page, profile, payload, capture, inspect, scrollTo, setRange });
    const captures = manifest.captures.filter((capture) => capture.device === profile.name);
    for (const shot of captures) {
      assert.ok(shot.documentWidth <= profile.width, `${shot.id}: page overflows horizontally`);
    }
    if (profile.name !== 'tablet') {
      const chapters = await page
        .locator('.chapter')
        .evaluateAll((nodes) => nodes.map((e) => e.id));
      const visible = new Set(
        captures.filter((shot) => shot.group === 'Page scroll').flatMap((shot) => shot.chapters),
      );
      assert.deepEqual([...visible].sort(), [...chapters].sort(), 'Every chapter was captured');
      manifest.coverage[profile.name].chapters = chapters;
    }
    if (profile.name === 'desktop') {
      for (const component of payload.components) {
        assert.ok(
          captures.some((shot) => shot.title === `Atlas · ${component.name} · top`),
          `Missing inspector: ${component.name}`,
        );
      }
      manifest.coverage.desktop.inspectors = payload.components.map((component) => component.id);
    }
    await page.close();
  }
  assert.deepEqual(manifest.pageErrors, [], 'Browser errors during capture');
  assert.deepEqual(manifest.externalRequests, [], 'The guide must remain offline');
  manifest.sheets = await createGallery(browser, directory, manifest);
  await writeFile(join(directory, 'manifest.json'), JSON.stringify(manifest, null, 2));
  console.log(
    `Saved ${manifest.captures.length} screenshots and ${manifest.sheets.length} review sheets.`,
  );
  console.log(`Gallery: ${join(directory, 'index.html')}`);
} catch (error) {
  await writeFile(join(directory, 'manifest.partial.json'), JSON.stringify(manifest, null, 2));
  throw error;
} finally {
  await browser.close();
}
