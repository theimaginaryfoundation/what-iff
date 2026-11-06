import { createServer } from 'node:http';
import { readFile, stat } from 'node:fs/promises';
import { join, extname } from 'node:path';
const types = { '.js': 'text/javascript', '.mjs': 'text/javascript', '.css': 'text/css', '.html': 'text/html', '.ttf': 'font/ttf', '.json': 'application/json', '.svg': 'image/svg+xml', '.woff2': 'font/woff2', '.pdf': 'application/pdf', '.docx': 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', '.xlsx': 'application/octet-stream', '.png': 'image/png' };
export async function serve(root) {
  const server = createServer(async (req, res) => {
    let p = join(root, decodeURIComponent(new URL(req.url, 'http://x').pathname));
    try { if (!(await stat(p)).isFile()) throw 0; } catch { res.writeHead(404); res.end(); return; }
    res.writeHead(200, { 'content-type': types[extname(p)] ?? 'application/octet-stream' });
    res.end(await readFile(p));
  });
  await new Promise(r => server.listen(0, r));
  return { url: `http://localhost:${server.address().port}`, close: () => server.close() };
}
