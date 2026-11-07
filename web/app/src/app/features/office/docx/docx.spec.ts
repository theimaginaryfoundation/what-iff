import {
  AlignmentType,
  Document,
  ExternalHyperlink,
  Footer,
  Header,
  HeadingLevel,
  ImageRun,
  LevelFormat,
  Packer,
  PageBreak,
  PageNumber,
  Paragraph,
  ShadingType,
  Table,
  TableCell,
  TableRow,
  TextRun,
  WidthType,
} from 'docx';
import { describe, expect, it } from 'vitest';

import { OoxmlPackage, base64ToBytes } from '../shared/ooxml';
import { documentToDocx } from './docx-export';
import { docxToDocument } from './docx-import';
import { CUSTOM_RANGE, DocDocument, TOKEN } from './univer-doc-types';

// A 64x32 PNG.
const PNG_BASE64 =
  'iVBORw0KGgoAAAANSUhEUgAAAEAAAAAgCAYAAACinX6EAAAAQklEQVR42u3OMQEAAAgDoNm/9Cz4E4kwQ6ZJkyZNmjRp0qRJkyZNmjRp0qRJkyZNmjRp0qRJkyZNmjRp0qRJkyZN2uUBZl0AoYzvRCEAAAAASUVORK5CYII=';

async function sampleDocx(): Promise<Uint8Array> {
  const doc = new Document({
    numbering: {
      config: [{ reference: 'nums', levels: [{ level: 0, format: LevelFormat.DECIMAL, text: '%1.', alignment: AlignmentType.START }] }],
    },
    styles: {
      paragraphStyles: [
        { id: 'Quote', name: 'Quote', basedOn: 'Normal', run: { italics: true, color: '555555' }, paragraph: { indent: { left: 720 } } },
      ],
    },
    sections: [
      {
        properties: { page: { size: { width: 11906, height: 16838 }, margin: { top: 1440, bottom: 1440, left: 1440, right: 1440 } } },
        headers: { default: new Header({ children: [new Paragraph({ text: 'Spike header', alignment: AlignmentType.RIGHT })] }) },
        footers: {
          default: new Footer({
            children: [new Paragraph({ children: [new TextRun('Page '), new TextRun({ children: [PageNumber.CURRENT] })] })],
          }),
        },
        children: [
          new Paragraph({ text: 'Fidelity sample', heading: HeadingLevel.HEADING_1 }),
          new Paragraph({
            children: [
              new TextRun('Plain, '),
              new TextRun({ text: 'bold, ', bold: true }),
              new TextRun({ text: 'italic, ', italics: true }),
              new TextRun({ text: 'underlined, ', underline: {} }),
              new TextRun({ text: 'red 14pt', color: 'CC0000', size: 28 }),
              new TextRun({ text: ' highlighted', highlight: 'yellow' }),
            ],
          }),
          new Paragraph({
            children: [
              new ExternalHyperlink({ children: [new TextRun({ text: 'A hyperlink', style: 'Hyperlink' })], link: 'https://example.com' }),
            ],
          }),
          new Paragraph({ text: 'Second level', heading: HeadingLevel.HEADING_2 }),
          new Paragraph({ text: 'Bullet one', bullet: { level: 0 } }),
          new Paragraph({ text: 'Nested bullet', bullet: { level: 1 } }),
          new Paragraph({ text: 'Numbered one', numbering: { reference: 'nums', level: 0 } }),
          new Paragraph({ text: 'Numbered two', numbering: { reference: 'nums', level: 0 } }),
          new Paragraph({ text: 'A quoted paragraph using a custom style.', style: 'Quote' }),
          new Table({
            width: { size: 100, type: WidthType.PERCENTAGE },
            rows: [
              new TableRow({
                tableHeader: true,
                children: ['Name', 'Qty', 'Price'].map(
                  t =>
                    new TableCell({
                      shading: { type: ShadingType.CLEAR, fill: 'D9E2F3' },
                      children: [new Paragraph({ children: [new TextRun({ text: t, bold: true })] })],
                    }),
                ),
              }),
              new TableRow({
                children: [
                  new TableCell({ rowSpan: 2, children: [new Paragraph('Widget')] }),
                  new TableCell({ children: [new Paragraph('3')] }),
                  new TableCell({ children: [new Paragraph('$4.50')] }),
                ],
              }),
              new TableRow({
                children: [new TableCell({ children: [new Paragraph('5')] }), new TableCell({ children: [new Paragraph('$7.50')] })],
              }),
              new TableRow({
                children: [
                  new TableCell({ columnSpan: 2, children: [new Paragraph('Merged cell')] }),
                  new TableCell({ children: [new Paragraph('$13.50')] }),
                ],
              }),
            ],
          }),
          new Paragraph({
            children: [new ImageRun({ type: 'png', data: base64ToBytes(PNG_BASE64), transformation: { width: 128, height: 64 } })],
          }),
          new Paragraph({ children: [new PageBreak()] }),
          new Paragraph({ text: 'After the page break', heading: HeadingLevel.HEADING_2 }),
          new Paragraph({ text: 'Centered text.', alignment: AlignmentType.CENTER, spacing: { before: 240, after: 120 } }),
        ],
      },
    ],
  });
  return new Uint8Array(await Packer.toArrayBuffer(doc));
}

function paragraphTexts(doc: DocDocument): string[] {
  const stream = doc.body.dataStream;
  const out: string[] = [];
  let start = 0;
  for (const p of doc.body.paragraphs ?? []) {
    out.push(stream.slice(start, p.startIndex).replace(/[\u0000-\u001f]/g, ''));
    start = p.startIndex + 1;
  }
  return out;
}

function paragraphByText(doc: DocDocument, text: string) {
  const texts = paragraphTexts(doc);
  const index = texts.indexOf(text);
  if (index === -1) throw new Error(`no paragraph "${text}" in ${JSON.stringify(texts)}`);
  return doc.body.paragraphs![index];
}

function runsOf(doc: DocDocument, text: string) {
  const start = doc.body.dataStream.indexOf(text);
  const end = start + text.length;
  return (doc.body.textRuns ?? [])
    .filter(r => r.st < end && r.ed > start)
    .map(r => ({ text: doc.body.dataStream.slice(Math.max(r.st, start), Math.min(r.ed, end)), ts: r.ts, sId: r.sId }));
}

describe('docx import', () => {
  it('reads paragraphs, named styles, direct run formatting and lists', async () => {
    const { document, report } = await docxToDocument(await sampleDocx(), { id: 'doc', name: 'sample.docx' });
    expect(report.dropped).toEqual([]);
    expect(document.documentStyle.documentFlavor).toBe(1);
    expect(document.documentStyle.pageSize).toEqual({ width: 793.73, height: 1122.53 });
    expect(document.documentStyle.marginLeft).toBe(96);
    const texts = paragraphTexts(document);
    expect(texts.slice(0, 4)).toEqual([
      'Fidelity sample',
      'Plain, bold, italic, underlined, red 14pt highlighted',
      'A hyperlink',
      'Second level',
    ]);
    const heading = paragraphByText(document, 'Fidelity sample');
    expect(heading.styleId).toBe('Heading1');
    expect(heading.paragraphStyle?.textStyle).toMatchObject({ cl: { rgb: '#2E74B5' }, fs: 16 });
    expect(document.styles?.['Heading1']?.paragraphStyle?.namedStyleType).toBe(4);
    const runs = runsOf(document, 'Plain, bold, italic, underlined, red 14pt highlighted');
    expect(runs.map(r => r.text)).toEqual(['bold, ', 'italic, ', 'underlined, ', 'red 14pt', ' highlighted']);
    expect(runs[0].ts).toEqual({ bl: 1 });
    expect(runs[1].ts).toEqual({ it: 1 });
    expect(runs[2].ts).toEqual({ ul: { s: 1, t: 12 } });
    expect(runs[3].ts).toEqual({ cl: { rgb: '#CC0000' }, fs: 14 });
    expect(runs[4].ts).toEqual({ bg: { rgb: '#FFFF00' } });
    const quote = paragraphByText(document, 'A quoted paragraph using a custom style.');
    expect(quote.styleId).toBe('Quote');
    expect(quote.paragraphStyle?.textStyle).toEqual({ it: 1, cl: { rgb: '#555555' } });
    expect(document.styles?.['Quote']?.paragraphStyle?.indentStart).toEqual({ v: 48 });
    expect(paragraphByText(document, 'Bullet one').bullet).toMatchObject({ nestingLevel: 0 });
    expect(paragraphByText(document, 'Nested bullet').bullet).toMatchObject({ nestingLevel: 1 });
    const numbered = paragraphByText(document, 'Numbered one').bullet!;
    expect(document.lists?.[numbered.listType]?.nestingLevel[0]).toMatchObject({ glyphType: 2, glyphFormat: '%1.', startNumber: 1 });
    const bulletList = document.lists?.[paragraphByText(document, 'Bullet one').bullet!.listType];
    expect(bulletList?.nestingLevel[0]).toMatchObject({ glyphType: 0, glyphSymbol: '●' });
    const centered = paragraphByText(document, 'Centered text.');
    expect(centered.paragraphStyle).toMatchObject({ horizontalAlign: 2, spaceAbove: { v: 16 }, spaceBelow: { v: 8 } });
  });

  it('reads hyperlinks, page breaks, images, headers and footers with fields', async () => {
    const { document } = await docxToDocument(await sampleDocx(), { id: 'doc', name: 'sample.docx' });
    const link = document.body.customRanges!.find(r => r.rangeType === CUSTOM_RANGE.HYPERLINK)!;
    expect(document.body.dataStream.slice(link.startIndex, link.endIndex + 1)).toBe('A hyperlink');
    expect(link.properties).toEqual({ url: 'https://example.com' });
    expect(runsOf(document, 'A hyperlink')[0]).toMatchObject({ sId: 'Hyperlink' });
    expect(document.body.dataStream).toContain(TOKEN.PAGE_BREAK);
    expect(document.body.customBlocks).toHaveLength(1);
    const drawing = document.drawings![document.body.customBlocks![0].blockId];
    expect(drawing.source).toBe(`data:image/png;base64,${PNG_BASE64}`);
    expect(drawing.docTransform.size).toEqual({ width: 128, height: 64 });
    expect(drawing.layoutType).toBe(0);
    const header = document.headers![document.documentStyle.defaultHeaderId!];
    expect(header.body.dataStream).toBe('Spike header\r\n');
    expect(header.body.paragraphs![0].paragraphStyle?.horizontalAlign).toBe(3);
    const footer = document.footers![document.documentStyle.defaultFooterId!];
    expect(footer.body.dataStream).toBe('Page 1\r\n');
    const field = footer.body.customRanges!.find(r => r.rangeType === CUSTOM_RANGE.FIELD)!;
    expect(field.properties).toMatchObject({ fieldType: 'PAGE' });
    expect(footer.body.dataStream.slice(field.startIndex, field.endIndex + 1)).toBe('1');
  });

  it('reads tables with spans, shading and header rows', async () => {
    const { document } = await docxToDocument(await sampleDocx(), { id: 'doc', name: 'sample.docx' });
    expect(document.body.tables).toHaveLength(1);
    const table = document.tableSource![document.body.tables![0].tableId];
    expect(table.tableColumns).toHaveLength(3);
    expect(table.size).toEqual({ type: 1, width: { v: 100, u: 4 } });
    expect(table.tableRows).toHaveLength(4);
    expect(table.tableRows[0].repeatHeaderRow).toBe(1);
    expect(table.tableRows[0].tableCells[0].backgroundColor).toEqual({ rgb: '#D9E2F3' });
    expect(table.tableRows[0].tableCells[0].borderTop).toMatchObject({ dashStyle: 1 });
    expect(table.tableRows[1].tableCells[0].rowSpan).toBe(2);
    expect(table.tableRows[2].tableCells[0]).toMatchObject({ rowSpan: 0, columnSpan: 0 });
    expect(table.tableRows[3].tableCells[0].columnSpan).toBe(2);
    expect(table.tableRows[3].tableCells[1]).toMatchObject({ rowSpan: 0, columnSpan: 0 });
    expect(table.tableRows.every(r => r.tableCells.length === 3)).toBe(true);
    const { startIndex, endIndex } = document.body.tables![0];
    const slice = document.body.dataStream.slice(startIndex, endIndex);
    expect(slice.startsWith(TOKEN.TABLE_START)).toBe(true);
    expect(slice.endsWith(TOKEN.TABLE_END)).toBe(true);
    expect(slice.split(TOKEN.TABLE_ROW_START)).toHaveLength(5);
    expect(slice.split(TOKEN.TABLE_CELL_START)).toHaveLength(13);
    expect(slice).toContain(`${TOKEN.TABLE_CELL_START}Name\r\n${TOKEN.TABLE_CELL_END}`);
  });

  it('keeps every structural offset consistent', async () => {
    const { document } = await docxToDocument(await sampleDocx(), { id: 'doc', name: 'sample.docx' });
    const s = document.body.dataStream;
    for (const p of document.body.paragraphs!) expect(s[p.startIndex], `paragraph at ${p.startIndex}`).toBe('\r');
    for (const b of document.body.sectionBreaks!) expect(s[b.startIndex], `section at ${b.startIndex}`).toBe('\n');
    for (const b of document.body.customBlocks!) expect(s[b.startIndex]).toBe('\b');
    for (const t of document.body.tables!) {
      expect(s[t.startIndex]).toBe(TOKEN.TABLE_START);
      expect(s[t.endIndex - 1]).toBe(TOKEN.TABLE_END);
    }
    for (const r of document.body.textRuns!) expect(r.ed).toBeGreaterThan(r.st);
    expect(s.endsWith('\r\n')).toBe(true);
  });
});

describe('docx export', () => {
  it('round-trips the sample through bytes', async () => {
    const first = await docxToDocument(await sampleDocx(), { id: 'doc', name: 'sample.docx' });
    const exported = await documentToDocx(first.document);
    expect(exported.report.dropped).toEqual([]);
    const second = await docxToDocument(exported.bytes, { id: 'doc', name: 'sample.docx' });
    const a = first.document;
    const b = second.document;
    expect(paragraphTexts(b)).toEqual(paragraphTexts(a));
    expect(b.body.dataStream).toBe(a.body.dataStream);
    expect(b.body.textRuns).toEqual(a.body.textRuns);
    expect(b.body.paragraphs!.map(p => ({ styleId: p.styleId, bullet: p.bullet?.nestingLevel, style: p.paragraphStyle }))).toEqual(
      a.body.paragraphs!.map(p => ({ styleId: p.styleId, bullet: p.bullet?.nestingLevel, style: p.paragraphStyle })),
    );
    expect(b.body.customRanges!.map(r => ({ ...r, rangeId: '' }))).toEqual(a.body.customRanges!.map(r => ({ ...r, rangeId: '' })));
    expect(b.body.customBlocks!.map(c => c.startIndex)).toEqual(a.body.customBlocks!.map(c => c.startIndex));
    expect(Object.values(b.drawings!)[0].source).toBe(Object.values(a.drawings!)[0].source);
    expect(Object.values(b.drawings!)[0].docTransform.size).toEqual(Object.values(a.drawings!)[0].docTransform.size);
    const ta = Object.values(a.tableSource!)[0];
    const tb = Object.values(b.tableSource!)[0];
    expect(tb.tableRows.map(r => r.tableCells.map(c => ({ rowSpan: c.rowSpan, columnSpan: c.columnSpan, bg: c.backgroundColor })))).toEqual(
      ta.tableRows.map(r => r.tableCells.map(c => ({ rowSpan: c.rowSpan, columnSpan: c.columnSpan, bg: c.backgroundColor }))),
    );
    expect(tb.size).toEqual(ta.size);
    expect(b.documentStyle).toEqual(a.documentStyle);
    expect(Object.values(b.headers!)[0].body.dataStream).toBe('Spike header\r\n');
    expect(Object.values(b.footers!)[0].body.dataStream).toBe('Page 1\r\n');
    expect(Object.values(b.footers!)[0].body.customRanges![0].properties).toMatchObject({ fieldType: 'PAGE' });
    expect(
      Object.values(b.lists!)
        .map(l => l.nestingLevel[0].glyphType)
        .sort(),
    ).toEqual(
      Object.values(a.lists!)
        .map(l => l.nestingLevel[0].glyphType)
        .sort(),
    );
  });

  it('writes a document Word can open: parts, relationships and content types agree', async () => {
    const first = await docxToDocument(await sampleDocx(), { id: 'doc', name: 'sample.docx' });
    const { bytes } = await documentToDocx(first.document);
    const pkg = await OoxmlPackage.open(bytes);
    const paths = pkg.paths();
    expect(paths).toEqual(
      expect.arrayContaining([
        '[Content_Types].xml',
        '_rels/.rels',
        'word/document.xml',
        'word/styles.xml',
        'word/numbering.xml',
        'word/settings.xml',
        'word/_rels/document.xml.rels',
        'word/header1.xml',
        'word/footer1.xml',
        'word/media/image1.png',
      ]),
    );
    const types = await pkg.text('[Content_Types].xml');
    for (const part of ['/word/document.xml', '/word/styles.xml', '/word/numbering.xml', '/word/header1.xml', '/word/footer1.xml'])
      expect(types).toContain(`PartName="${part}"`);
    const rels = await pkg.rels('word/document.xml');
    const targets = [...rels.values()].map(r => r.target);
    expect(targets).toEqual(
      expect.arrayContaining([
        'styles.xml',
        'numbering.xml',
        'settings.xml',
        'header1.xml',
        'footer1.xml',
        'media/image1.png',
        'https://example.com',
      ]),
    );
    const document = await pkg.xml('word/document.xml');
    expect(document?.documentElement.localName).toBe('document');
    expect((await pkg.text('word/document.xml'))!).toContain('<w:pStyle w:val="Heading1"/>');
    expect((await pkg.text('word/document.xml'))!).toContain('<w:vMerge w:val="restart"/>');
    expect((await pkg.text('word/document.xml'))!).toContain('<w:gridSpan w:val="2"/>');
  });

  it('exports a document authored in Univer without named styles or lists', async () => {
    const document: DocDocument = {
      id: 'new',
      body: {
        dataStream: 'Title\rBody text with a tab\there.\r\n',
        textRuns: [{ st: 0, ed: 5, ts: { bl: 1, fs: 20 } }],
        paragraphs: [
          { startIndex: 5, paragraphId: 'p1', paragraphStyle: { namedStyleType: 4 } },
          { startIndex: 31, paragraphId: 'p2' },
        ],
        sectionBreaks: [{ sectionId: 's1', startIndex: 32 }],
      },
      documentStyle: {
        documentFlavor: 1,
        pageSize: { width: 816, height: 1056 },
        marginTop: 72,
        marginBottom: 72,
        marginLeft: 72,
        marginRight: 72,
      },
    };
    const { bytes, report } = await documentToDocx(document);
    expect(report.dropped).toEqual([]);
    const back = await docxToDocument(bytes, { id: 'new', name: 'new.docx' });
    expect(back.document.body.dataStream).toBe('Title\rBody text with a tab\there.\r\n');
    expect(back.document.body.textRuns).toEqual([{ st: 0, ed: 5, ts: { bl: 1, fs: 20 } }]);
    expect(back.document.documentStyle.pageSize).toEqual({ width: 816, height: 1056 });
  });

  it('preserves unsupported block content verbatim', async () => {
    const first = await docxToDocument(await sampleDocx(), { id: 'doc', name: 'sample.docx' });
    first.document.body.docxRawBlocks = [
      { startIndex: 0, xml: `<w:p xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:r><w:t>raw</w:t></w:r></w:p>` },
    ];
    const { bytes } = await documentToDocx(first.document);
    const pkg = await OoxmlPackage.open(bytes);
    const xml = (await pkg.text('word/document.xml'))!;
    expect(xml.indexOf('<w:t>raw</w:t>')).toBeLessThan(xml.indexOf('Fidelity sample'));
  });
});
