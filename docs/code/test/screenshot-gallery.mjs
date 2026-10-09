import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const escapeHTML = (value) =>
  String(value).replace(
    /[&<>"']/g,
    (char) =>
      ({
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#39;',
      })[char],
  );

// Individual PNGs remain at native resolution. Sheets make it practical to review every capture,
// while the gallery links to each original for inspecting text, clipping, and control spacing.
export async function createGallery(browser, directory, manifest) {
  const tiles = manifest.captures.map(
    (capture) => `
    <figure data-group="${escapeHTML(capture.group)}" data-device="${capture.device}">
      <a href="${capture.file}"><img src="${capture.file}" loading="lazy"
        alt="${escapeHTML(capture.title)}" /></a>
      <figcaption><strong>${capture.id} · ${escapeHTML(capture.title)}</strong>
        <span>${capture.device} / ${escapeHTML(capture.group)} /
          ${capture.width} × ${capture.height}
        </span></figcaption>
    </figure>`,
  );
  const head = `<!doctype html><html lang="en"><meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Gent screenshot review · ${escapeHTML(manifest.label)}</title>
    <style>
      * { box-sizing: border-box; } body { margin: 0; background: #eeeae0; color: #182526; }
      body { font: 14px/1.5 system-ui, sans-serif; } header { padding: 24px; }
      h1 { margin: 0 0 8px; font-size: 25px; } p { margin: 0; }
      nav { display: flex; gap: 12px; padding: 0 24px 24px; } select { padding: 8px; }
      main { padding: 0 20px 24px; display: grid; gap: 18px; }
      main { grid-template-columns: repeat(3, 1fr); }
      figure { margin: 0; min-width: 0; background: white; border: 1px solid #cbd1c2; }
      figure[hidden] { display: none; } a { display: block; background: #e5e5df; }
      img { display: block; width: 100%; height: 300px; object-fit: contain; object-position: top; }
      figcaption { padding: 12px; } figcaption strong { display: block; }
      figcaption span { display: block; color: #59625c; font-size: 12px; margin-top: 4px; }
      .sheet main { grid-template-columns: repeat(2, 1fr); gap: 16px; }
      .sheet img { height: 690px; } .sheet figcaption { min-height: 80px; }
      .sheet img { max-width: 100%; } .sheet header { padding-block: 14px; }
      @media (max-width: 700px) { main { grid-template-columns: 1fr; } }
    </style>`;
  const groups = [...new Set(manifest.captures.map((capture) => capture.group))];
  const controls = `<nav><select id="device" aria-label="Device">
    <option value="all">All devices</option>
    <option>desktop</option><option>mobile</option><option>tablet</option></select>
    <select id="group" aria-label="Group"><option value="all">All groups</option>
    ${groups.map((group) => `<option>${escapeHTML(group)}</option>`).join('')}</select></nav>`;
  await writeFile(
    join(directory, 'index.html'),
    `${head}<body><header>
    <h1>Gent · ${escapeHTML(manifest.label)}</h1>
    <p>${manifest.captures.length} native screenshots.
      Click a capture to inspect it at full size.</p>
    </header>${controls}<main>${tiles.join('')}</main><script>
    function filter() {
      const device = document.getElementById('device').value;
      const group = document.getElementById('group').value;
      document.querySelectorAll('figure').forEach(figure => {
        figure.hidden = (device !== 'all' && figure.dataset.device !== device)
          || (group !== 'all' && figure.dataset.group !== group);
      });
    }
    for (const select of document.querySelectorAll('select')) {
      select.addEventListener('change', filter);
    }
    </script></body></html>`,
  );

  const page = await browser.newPage({ viewport: { width: 1640, height: 1750 }, offline: true });
  const sheets = [];
  try {
    for (let start = 0; start < tiles.length; start += 4) {
      const name = `sheet-${String(start / 4 + 1).padStart(3, '0')}`;
      const title = `${manifest.label} · ${start + 1}–${Math.min(start + 4, tiles.length)}`;
      const html = `${head}<body class="sheet"><header><h1>${escapeHTML(title)}</h1></header>
        <main>${tiles
          .slice(start, start + 4)
          .join('')
          .replaceAll('loading="lazy"', '')}</main>
        </body></html>`;
      const htmlPath = join(directory, `${name}.html`);
      await writeFile(htmlPath, html);
      await page.goto(pathToFileURL(htmlPath).href);
      await page.evaluate(() => Promise.all([...document.images].map((image) => image.decode())));
      await page.screenshot({ path: join(directory, `${name}.png`), fullPage: true });
      sheets.push({
        file: `${name}.png`,
        ids: manifest.captures.slice(start, start + 4).map((c) => c.id),
      });
    }
  } finally {
    await page.close();
  }
  return sheets;
}
