import { createHash } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { components, steps, recipes, questions } from './src/content.mjs';

const project = dirname(fileURLToPath(import.meta.url));
const repository = resolve(project, '../..');
const read = (path) => readFile(resolve(project, path), 'utf8');
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
const sourceFiles = new Map();

// The atlas uses real declarations so a renamed or removed contract fails the build.
// Top-level Go declarations end at column zero; nested struct fields remain indented.
function declaration(source, symbol) {
  const lines = source.split('\n');
  const start = lines.findIndex((line) => new RegExp(`^type ${symbol}(?:\\[|\\s|=)`).test(line));
  if (start < 0) throw new Error(`Missing Go declaration: ${symbol}`);
  const selected = [lines[start]];
  if (lines[start].includes('{') && !lines[start].trimEnd().endsWith('}')) {
    let closed = false;
    for (const line of lines.slice(start + 1)) {
      if (!line.trimStart().startsWith('//')) selected.push(line.replace(/\s+\/\/.*$/, ''));
      if (line === '}') {
        closed = true;
        break;
      }
    }
    if (!closed) throw new Error(`Unclosed Go declaration: ${symbol}`);
  }
  return selected
    .join('\n')
    .replace(/\t/g, '    ')
    .replace(/\n{3,}/g, '\n\n');
}

const ids = new Set(components.map((component) => component.id));
if (ids.size !== components.length) throw new Error('Duplicate atlas component ID.');
const resolvedComponents = [];
for (const component of components) {
  if (!sourceFiles.has(component.source)) {
    sourceFiles.set(
      component.source,
      await readFile(resolve(repository, component.source), 'utf8'),
    );
  }
  const extracted = declaration(sourceFiles.get(component.source), component.symbol);
  for (const link of component.links) {
    if (!ids.has(link)) throw new Error(`Unknown related component: ${component.id} → ${link}`);
  }
  resolvedComponents.push({
    ...component,
    code: component.code || extracted,
    extracted: !component.code,
  });
}

const example = await readFile(resolve(repository, 'examples/history/main.go'), 'utf8');
const licenseNames = ['dm-sans', 'instrument-serif', 'ibm-plex-mono'];
const licenses = await Promise.all(
  licenseNames.map(async (name) => ({
    name,
    text: await read(`assets/${name}-LICENSE.txt`),
  })),
);
const fingerprint = createHash('sha256')
  .update(JSON.stringify([...sourceFiles].sort()))
  .update(example)
  .digest('hex')
  .slice(0, 16);
const payload = {
  components: resolvedComponents,
  steps,
  recipes,
  questions,
  example,
  licenses,
  fingerprint,
};

let fonts = await read('assets/fonts.css');
for (const match of fonts.matchAll(/__FONT:([^_]+)__/g)) {
  const bytes = await readFile(resolve(project, 'assets', match[1]));
  fonts = fonts.replace(match[0], `data:font/woff2;base64,${bytes.toString('base64')}`);
}
const styles = await read('src/styles.css');
const simulations = (await read('src/simulations.mjs')).replace(/^export /gm, '');
const app = await read('src/app.js');
const script = `(() => {\n'use strict';\n${simulations}\n${app}\n})();`;
new Function(script); // Syntax validation only: browser globals are not executed during the build.

const layers = [...new Set(components.map((component) => component.layer))];
const atlas = components
  .map(
    (component) => `
  <button class="atlas-row" data-inspect="${component.id}" type="button">
    <span><strong>${escapeHTML(component.name)}</strong>
      <small>${escapeHTML(component.layer)}</small></span>
    <span class="atlas-job">${escapeHTML(component.label)}</span>
    <span class="atlas-kind">${escapeHTML(component.kind)}</span>
    <span class="atlas-arrow" aria-hidden="true">↗</span>
  </button>`,
  )
  .join('');
const replacements = {
  '/* __FONTS__ */': fonts,
  '/* __STYLES__ */': styles,
  '<!-- __LAYER_OPTIONS__ -->': layers
    .map((layer) => {
      return `<option value="${escapeHTML(layer)}">${escapeHTML(layer)}</option>`;
    })
    .join(''),
  '<!-- __ATLAS__ -->': atlas,
  '/* __DATA__ */': JSON.stringify(payload).replace(/</g, '\\u003c'),
  '/* __APP__ */': script,
};
let html = await read('src/template.html');
for (const [placeholder, value] of Object.entries(replacements)) {
  if (!html.includes(placeholder)) throw new Error(`Missing build placeholder ${placeholder}`);
  html = html.replace(placeholder, () => value);
}
if (/__(?:FONT:|[A-Z]+__)/.test(html)) throw new Error('Unresolved build placeholder.');
const output = resolve(project, '../dist/gent.html');
await mkdir(dirname(output), { recursive: true });
await writeFile(output, html);
console.log(`Built docs/dist/gent.html (${Math.round(Buffer.byteLength(html) / 1024)} KiB).`);
console.log(
  `${components.length} atlas entries; ${sourceFiles.size} Go sources; fingerprint ${fingerprint}.`,
);
