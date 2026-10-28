import { createHash } from 'node:crypto';
import { readFile, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { groups, descriptions, supporting, phases, boundaries } from './content.mjs';
import { lessons } from './lesson-models.mjs';

const directory = dirname(fileURLToPath(import.meta.url));
const read = (file) => readFile(resolve(directory, file), 'utf8');
// Extraction is an explicit Go command so this dependency-free build also works in runtimes
// that prohibit Node subprocesses. Keep the catalog in /tmp as reusable verification evidence.
const catalogPath = process.argv[2] || '/tmp/gent-interface-catalog.json';
const catalog = JSON.parse(await readFile(catalogPath, 'utf8'));
const interfaces = catalog.declarations.filter((item) => item.kind === 'interface');
const names = new Set(interfaces.map((item) => item.id));
const taught = lessons.flatMap((lesson) => lesson.contracts);
if (new Set(taught).size !== taught.length) throw new Error('Duplicate lesson contract.');
if (taught.length !== names.size || taught.some((id) => !names.has(id))) {
  throw new Error('Lessons must cover every named interface exactly once.');
}
for (const name of Object.keys(descriptions)) {
  if (!names.has(name)) throw new Error(`Documented interface is absent: ${name}`);
}

const entries = interfaces.map((item) => {
  if (!descriptions[item.id]) throw new Error(`Interface needs an explanation: ${item.id}`);
  return { ...item, ...descriptions[item.id] };
});
for (const [id, [group, purpose, why]] of Object.entries(supporting)) {
  const item = catalog.declarations.find((item) => item.id === id);
  if (!item) throw new Error(`Missing supporting type: ${id}`);
  entries.push({
    ...item,
    group,
    purpose,
    why,
    related: [],
    rules: [],
    implementations: [],
    question: 'Does the ownership and lifecycle of this value fit the application’s needs?',
  });
}
const ids = new Set(entries.map((item) => item.id));
if (ids.size !== entries.length) throw new Error('Duplicate documented declarations.');
for (const entry of entries) {
  for (const id of entry.related) {
    if (!ids.has(id)) throw new Error(`Missing related contract: ${entry.id} → ${id}`);
  }
  entry.methods ||= [];
  entry.embeds ||= [];
  entry.methodCount = entry.methods.length;
}
for (const item of [...phases, ...boundaries]) {
  for (const id of item.at(-1)) {
    if (!ids.has(id)) throw new Error(`Missing walkthrough contract: ${id}`);
  }
}

const capabilities = Object.values(
  catalog.capabilities.reduce((found, item) => {
    const key = item.name;
    found[key] ||= { name: key, code: item.code, sites: [] };
    found[key].sites.push({ file: item.file, line: item.line });
    return found;
  }, {}),
);
const capabilityDescriptions = {
  DeduplicateSummary: 'A summary function used by history rendering without owning tool execution.',
  Destroy: 'Native resource cleanup that reports errors through the caller.',
  RegisterCodecs:
    'Optional type registration used by ReAct and tool plans before saving typed values.',
  ModelName: 'Structural check equivalent to ModelNamer, used in call identity and configuration.',
  Provider: 'Optional provider identity used in managed model calls and recovery configuration.',
  Unwrap: 'Standard joined-error capability used to preserve multiple saved causes.',
  EventBase: 'Optional access to BaseEvent metadata during event capture and trace projection.',
  ConfigureExecution: 'Optional loop setup used by the executor before recovery validation.',
};
for (const item of capabilities) {
  if (!capabilityDescriptions[item.name]) throw new Error(`Unexplained capability: ${item.name}`);
  item.purpose = capabilityDescriptions[item.name];
}

// Embedded font bytes keep the deliverable usable with networking disabled.
const fonts = [
  ['GuideSans', 'dm-sans-latin-400-normal.woff2', 400],
  ['GuideSans', 'dm-sans-latin-600-normal.woff2', 600],
  ['GuideMono', 'ibm-plex-mono-latin-400-normal.woff2', 400],
  ['GuideSerif', 'instrument-serif-latin-400-normal.woff2', 400],
];
let fontCSS = '';
for (const [family, file, weight] of fonts) {
  const bytes = await readFile(resolve(directory, 'assets', file));
  fontCSS +=
    `@font-face{font-family:${family};font-weight:${weight};font-display:swap;` +
    `src:url(data:font/woff2;base64,${bytes.toString('base64')}) format('woff2');}\n`;
}
const licenses = await Promise.all(
  ['dm-sans', 'ibm-plex-mono', 'instrument-serif'].map(async (name) => ({
    name,
    text: await read(`assets/${name}-LICENSE.txt`),
  })),
);
const payload = {
  entries,
  groups,
  phases,
  boundaries,
  capabilities,
  licenses,
  head: catalog.head,
  fingerprint: catalog.fingerprint,
  sourceFiles: catalog.files,
  interfaceCount: interfaces.length,
  supportingCount: Object.keys(supporting).length,
};
const modelSource = (await read('lesson-models.mjs')).replace(/^export /gm, '');
const learning = `(() => {\n'use strict';\n${modelSource}\n${await read('lessons.js')}\n})();`;
const app = `${await read('app.js')}\n${learning}`;
new Function(app);
const styles = `${await read('styles.css')}\n${await read('lessons.css')}`;
let template = await read('template.html');
for (const [key, value] of Object.entries({
  '/* FONTS */': fontCSS,
  '/* STYLES */': styles,
  '/* APPLICATION */': app,
  '/* DATA */': JSON.stringify(payload).replaceAll('<', '\\u003c'),
})) {
  if (!template.includes(key)) throw new Error(`Missing template slot: ${key}`);
  template = template.replace(key, () => value);
}
const output = resolve(directory, 'gent-interfaces.html');
await writeFile(output, template);
const hash = createHash('sha256').update(template).digest('hex');
console.log(`Built ${output}`);
console.log(
  `${interfaces.length} interfaces, ${payload.supportingCount} supporting types, ` +
    `${capabilities.length} structural capabilities; ${catalog.files} Go sources.`,
);
console.log(`Source ${catalog.fingerprint}; HTML ${hash}; ${Buffer.byteLength(template)} bytes.`);
