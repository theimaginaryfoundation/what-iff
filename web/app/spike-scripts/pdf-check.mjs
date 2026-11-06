import fs from 'node:fs';
import { chromium } from 'playwright';
import { PDFDocument, PDFName } from 'pdf-lib';
import { serve } from './serve.mjs';
const here = new URL('.', import.meta.url).pathname;
const { url, close } = await serve(here);
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 900, height: 1100 } });
const errors = []; page.on('pageerror', e => errors.push(String(e))); page.on('console', m => { if (m.type() === 'error') errors.push(m.text().slice(0, 300)); });
await page.goto(`${url}/pdf.html`);
await page.waitForFunction(() => typeof window.annotateAndSave === 'function');
const extra = JSON.parse(process.argv[2] ?? '{}');
const r = await page.evaluate(([u, e]) => window.annotateAndSave(u, e), ['/sample.pdf', extra]);
console.log('source: pages', r.pages, 'annotations on p1 before', r.before, 'saved bytes', r.bytes.length);
fs.writeFileSync(here + 'annotated.pdf', Buffer.from(r.bytes));
const back = await page.evaluate(b => window.countAnnotations(b), r.bytes);
console.log('pdf.js re-read annotations on p1:', JSON.stringify(back));
await page.screenshot({ path: here + 'pdf.png' });
console.log('errors:', errors.length ? errors : 'none');
await browser.close(); close();
// Second, independent parser: pdf-lib.
const doc = await PDFDocument.load(fs.readFileSync(here + 'annotated.pdf'));
const p1 = doc.getPage(0);
const annots = p1.node.Annots();
const subtypes = [];
for (let i = 0; i < (annots?.size() ?? 0); i++) { const a = annots.lookup(i); subtypes.push(a.get(PDFName.of('Subtype'))?.toString()); }
console.log('pdf-lib sees on p1:', subtypes);
