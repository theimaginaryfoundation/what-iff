// Drives /spike/vfs against a mocked API: gallery file info, file bytes, and the step 1 save endpoint.
import { chromium } from 'playwright';
import { serve } from './serve.mjs';
const { url, close } = await serve(process.argv[2], { spa: true });
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1200, height: 800 } });
const errors = []; page.on('pageerror', e => errors.push(String(e))); page.on('console', m => { if (m.type() === 'error') errors.push(m.text().slice(0, 200)); });
const files = {
  'f-py': { id: 'f-py', name: 'notes.py', folder: 'projects/demo', file_type: 'text/x-python', content_version: 3, body: 'def greet(name):\n    return "hi " + name\n' },
  'f-json': { id: 'f-json', name: 'config.json', folder: '', file_type: 'application/json', content_version: 1, body: '{\n  "a": 1,\n  "a": 2\n}\n' },
};
const saves = [];
await page.route('**/image-gallery/*/info', r => { const id = r.request().url().split('/').at(-2); const f = files[id]; r.fulfill({ json: { id, user_id: 'u', name: f.name, folder: f.folder, file_type: f.file_type, content_version: f.content_version, created_at: '2026-11-06T00:00:00Z' } }); });
await page.route('**/file-attachment/*/content', async r => {
  const id = r.request().url().split('/').at(-2); const f = files[id];
  const ifMatch = r.request().headers()['if-match'];
  saves.push({ id, ifMatch, contentType: r.request().headers()['content-type'], body: r.request().postData() });
  if (Number(ifMatch) !== f.content_version) return r.fulfill({ status: 412, json: { error: 'stale' } });
  f.content_version += 1; f.body = r.request().postData();
  r.fulfill({ json: { content_version: f.content_version } });
});
await page.route('**/file-attachment/*', r => { if (r.request().method() !== 'GET') return r.fallback(); const id = r.request().url().split('/').at(-1); const f = files[id]; r.fulfill({ body: f.body, contentType: f.file_type, headers: { 'content-disposition': `attachment; filename="${f.name}"` } }); });
await page.route('**/auth/**', r => r.fulfill({ status: 401, json: {} }));

const read = async id => (await page.getByTestId(id).textContent())?.trim();
const reqs = []; page.on('request', r => { if (!r.url().endsWith('.js') && !r.url().endsWith('.css')) reqs.push(r.method() + ' ' + r.url()); });
await page.goto(`${url}/spike/vfs?file=f-py`);
try { await page.getByTestId('state').filter({ hasText: /^ready$/ }).waitFor({ timeout: 15000 }); }
catch { console.log('NOT READY. url:', page.url(), '| state:', await read('state').catch(() => '?'), '| requests:', reqs, '| errors:', errors); await browser.close(); close(); process.exit(1); }
console.log('opened:', await read('name'), '| language:', await read('language'), '| version:', await read('version'));
console.log('model text equals VFS bytes:', await page.evaluate(() => document.querySelector('.monaco-editor') && window.monaco === undefined ? null : null) ?? (await page.evaluate(() => { const m = globalThis.__spikeModel; return m ? m.getValue() : null; })) === files['f-py'].body);
// Edit as a user would: append a line, then Ctrl+S.
await page.locator('.view-lines').click(); await page.keyboard.press('Control+End'); await page.keyboard.type('\nprint(greet("vfs"))');
console.log('after typing, state:', await read('state'), '| save enabled:', await page.getByTestId('save').isEnabled());
await page.keyboard.press('Control+s');
await page.getByTestId('state').filter({ hasText: /^saved$/ }).waitFor({ timeout: 10000 });
console.log('after Ctrl+S: state', await read('state'), '| version', await read('version'), '| PUT If-Match', saves[0].ifMatch, '| PUT content-type', saves[0].contentType, '| body ends with', JSON.stringify(saves[0].body.slice(-22)));
// Conflict: someone else (the agent) saved in between, so our version is stale.
files['f-py'].content_version = 9;
await page.keyboard.type('\n# one more'); await page.keyboard.press('Control+s');
await page.getByTestId('state').filter({ hasText: /conflict/ }).waitFor({ timeout: 10000 });
console.log('stale save ->', await read('state'), '| PUT If-Match sent', saves[1].ifMatch, '| server has', files['f-py'].content_version);
// A JSON file gets the JSON worker's diagnostics on VFS content.
await page.goto(`${url}/spike/vfs?file=f-json`);
await page.getByTestId('state').filter({ hasText: /^ready$/ }).waitFor({ timeout: 30000 });
await page.waitForTimeout(2000);
console.log('json file language:', await read('language'), '| squiggles:', await page.locator('.squiggly-warning, .squiggly-error').count());
await page.screenshot({ path: new URL('./vfs.png', import.meta.url).pathname });
console.log('errors:', errors.length ? errors : 'none');
await browser.close(); close();
