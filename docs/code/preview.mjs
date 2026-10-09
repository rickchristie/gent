import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';

const file = new URL('../dist/gent.html', import.meta.url);
const server = createServer(async (request, response) => {
  if (request.url !== '/' && request.url !== '/gent.html') {
    response.writeHead(404, { 'Content-Type': 'text/plain' });
    response.end('Not found.');
    return;
  }
  try {
    const html = await readFile(file);
    response.writeHead(200, {
      'Content-Type': 'text/html; charset=utf-8',
      'Cache-Control': 'no-store',
    });
    response.end(html);
  } catch (error) {
    console.error(error);
    response.writeHead(500, { 'Content-Type': 'text/plain' });
    response.end('Guide unavailable. Run npm run build first.');
  }
});
server.on('error', (error) => {
  console.error(error);
  process.exitCode = 1;
});
server.listen(4173, '127.0.0.1', () => console.log('Gent field guide: http://127.0.0.1:4173'));
