/**
 * A developer harness, not a test of the suite: with OFFICE_SAMPLES_DIR naming a folder with
 * sample.docx, it round-trips the file through the converter in patch mode, writes
 * docx-roundtrip.docx and docx-roundtrip.json beside it, and reports which parts changed.
 * Skipped otherwise.
 */
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

import { OoxmlPackage } from '../shared/ooxml';
import { documentToDocx } from './docx-export';
import { docxToDocument } from './docx-import';
import { CUSTOM_RANGE } from './univer-doc-types';

const dir = process.env['OFFICE_SAMPLES_DIR'];
const have = (name: string) => !!dir && existsSync(join(dir, name));
// Through a Blob so the buffer belongs to the test realm JSZip checks against.
const bytes = (name: string) => new Blob([readFileSync(join(dir!, name))]).arrayBuffer();

describe.skipIf(!dir || !have('sample.docx'))('real docx round trip', () => {
  it('patches the original package and reports what changed', async () => {
    const original = await bytes('sample.docx');
    const before = await OoxmlPackage.open(original);
    const { document, report } = await docxToDocument(original, { id: 'doc', name: 'sample.docx' });
    const exported = await documentToDocx(document, original);
    const after = await OoxmlPackage.open(exported.bytes);
    const same: string[] = [];
    const changed: string[] = [];
    const added: string[] = [];
    const removed: string[] = [];
    for (const path of before.paths()) {
      if (!after.has(path)) removed.push(path);
      else if (Buffer.compare(Buffer.from((await before.bytes(path))!), Buffer.from((await after.bytes(path))!)) === 0) same.push(path);
      else changed.push(path);
    }
    for (const path of after.paths()) if (!before.has(path)) added.push(path);
    const back = await docxToDocument(exported.bytes, { id: 'doc', name: 'sample.docx' });
    const rangeKinds = (ranges: { rangeType: number }[]) =>
      Object.fromEntries(
        Object.entries(CUSTOM_RANGE)
          .map(([name, type]) => [name, ranges.filter(r => r.rangeType === type).length])
          .filter(([, n]) => n),
      );
    const summary = {
      importReport: report,
      exportReport: exported.report,
      parts: { same, changed, added, removed },
      streamLength: { before: document.body.dataStream.length, after: back.document.body.dataStream.length },
      sameStream: document.body.dataStream === back.document.body.dataStream,
      ranges: { before: rangeKinds(document.body.customRanges ?? []), after: rangeKinds(back.document.body.customRanges ?? []) },
      rawRuns: { before: document.body.docxRawCustomBlocks?.length ?? 0, after: back.document.body.docxRawCustomBlocks?.length ?? 0 },
      rawBlocks: { before: document.body.docxRawBlocks?.length ?? 0, after: back.document.body.docxRawBlocks?.length ?? 0 },
      notes: Object.keys(document.notes ?? {}),
      headers: Object.values(document.headers ?? {}).map(h => h.docxPath),
      documentXmlSize: { before: (await before.text('word/document.xml'))!.length, after: (await after.text('word/document.xml'))!.length },
    };
    writeFileSync(join(dir!, 'docx-roundtrip.docx'), exported.bytes);
    writeFileSync(join(dir!, 'docx-roundtrip.json'), JSON.stringify(summary, null, 1));
    expect(removed).toEqual([]);
    expect(summary.sameStream).toBe(true);
  });
});
