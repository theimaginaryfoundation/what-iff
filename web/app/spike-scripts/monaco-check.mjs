// Serves the built app and drives /spike/monaco in headless Chromium.
import { createServer } from 'node:http';
import { readFile, stat } from 'node:fs/promises';
import { join, extname } from 'node:path';
import { chromium } from 'playwright';

const root = process.argv[2];
const types = { '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html', '.ttf': 'font/ttf', '.json': 'application/json', '.svg': 'image/svg+xml', '.woff2': 'font/woff2' };
const server = createServer(async (req, res) => {
  let p = join(root, decodeURIComponent(new URL(req.url, 'http://x').pathname));
  try { if (!(await stat(p)).isFile()) throw 0; } catch { p = join(root, 'index.html'); }
  res.writeHead(200, { 'content-type': types[extname(p)] ?? 'application/octet-stream' });
  res.end(await readFile(p));
});
await new Promise(r => server.listen(0, r));
const port = server.address().port;
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1200, height: 900 } });
const errors = [], requests = [];
page.on('pageerror', e => errors.push(String(e)));
page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
page.on('response', r => requests.push({ url: r.url().replace(`http://localhost:${port}/`, ''), status: r.status(), size: Number(r.headers()['content-length'] ?? 0) }));
await page.goto(`http://localhost:${port}/spike/monaco`);
await page.getByTestId('status').filter({ hasText: /ready/ }).waitFor({ timeout: 30000 });
const read = async id => (await page.getByTestId(id).textContent())?.trim();
console.log('status:', await read('status'));
console.log('markers:', await read('markers'));
console.log('ts completions:', await read('suggest'));
console.log('colorize spans:', await page.locator('[data-testid=colorized] span').count());
console.log('editors rendered:', await page.locator('.monaco-editor').count(), 'view-lines:', await page.locator('.view-line').count());
// Trigger the suggest widget in the TS editor the way a user would.
await page.locator('[data-testid=ts] .view-lines').click();
await page.keyboard.press('Control+End');
await page.keyboard.press('Control+Space');
await page.waitForTimeout(1500);
console.log('suggest widget visible:', await page.locator('.suggest-widget.visible').count() > 0);
console.log('errors:', errors.length ? errors : 'none');
const workers = requests.filter(r => /worker|ttf/i.test(r.url));
console.log('worker requests:', workers);
const js = requests.filter(r => r.url.endsWith('.js'));
console.log('js files loaded:', js.length, 'bytes:', js.reduce((a, r) => a + r.size, 0));
await page.screenshot({ path: '/tmp/claude-1000/-home-chris-imaginary/0e9f0ecb-1368-4683-bcbf-ccb16bb824ec/scratchpad/spike/monaco.png' });
await browser.close(); server.close();
