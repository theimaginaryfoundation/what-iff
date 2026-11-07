import PptxGenJS from 'pptxgenjs';
import { parse } from 'pptxtojson';
import { describe, expect, it } from 'vitest';

import { addElement, addSlide, deleteSlide, duplicateSlide, moveSlide, newImage, newTextBox, removeElement, updateElement, updateSlide } from '../slides/deck-commands';
import { OoxmlPackage } from '../shared/ooxml';
import { ChartElement, Deck, ImageElement, ShapeElement, TableElement, blankDeck, emptyTextBody, plainText } from './deck-model';
import { deckToPptx, normalizePackage } from './pptx-export';
import { pptxToDeck } from './pptx-import';
import { htmlToParagraphs, paragraphsToRuns } from './pptx-text';

const PNG_BASE64 =
  'iVBORw0KGgoAAAANSUhEUgAAAEAAAAAgCAYAAACinX6EAAAAQklEQVR42u3OMQEAAAgDoNm/9Cz4E4kwQ6ZJkyZNmjRp0qRJkyZNmjRp0qRJkyZNmjRp0qRJkyZNmjRp0qRJkyZN2uUBZl0AoYzvRCEAAAAASUVORK5CYII=';

async function samplePptx(): Promise<ArrayBuffer> {
  const pptx = new PptxGenJS();
  pptx.layout = 'LAYOUT_16x9';
  const s1 = pptx.addSlide();
  s1.background = { color: 'F3F4F6' };
  s1.addText(
    [
      { text: 'Quarterly ', options: { bold: true, color: '1F4E79', fontSize: 32 } },
      { text: 'review', options: { italic: true, fontSize: 32, color: '333333' } },
    ],
    { x: 0.5, y: 0.4, w: 9, h: 1, align: 'left', fontFace: 'Arial' },
  );
  s1.addText('A bullet\nAnother bullet', { x: 0.5, y: 1.6, w: 5, h: 1.5, bullet: true, fontSize: 18 });
  s1.addShape(pptx.ShapeType.roundRect, {
    x: 6,
    y: 1.6,
    w: 3,
    h: 1.5,
    fill: { color: 'FFC000' },
    line: { color: '7F6000', width: 2 },
    rectRadius: 0.1,
  });
  s1.addShape(pptx.ShapeType.line, { x: 0.5, y: 3.4, w: 8.5, h: 0, line: { color: '999999', width: 1, dashType: 'dash' } });
  s1.addTable(
    [
      [
        { text: 'Name', options: { bold: true, fill: { color: 'D9E2F3' } } },
        { text: 'Qty', options: { bold: true, fill: { color: 'D9E2F3' } } },
      ],
      [{ text: 'Widget' }, { text: '3' }],
      [{ text: 'Gadget' }, { text: '10' }],
    ],
    { x: 0.5, y: 3.6, w: 5, colW: [3, 2], fontSize: 14, border: { type: 'solid', color: '999999', pt: 1 } },
  );
  s1.addNotes('Speaker notes here.');
  s1.addImage({ data: 'image/png;base64,' + PNG_BASE64, x: 6, y: 3.6, w: 2, h: 1 });
  const s2 = pptx.addSlide();
  s2.addText('Second slide', {
    x: 1,
    y: 1,
    w: 8,
    h: 1,
    fontSize: 28,
    align: 'center',
    color: '2E7D32',
    hyperlink: { url: 'https://example.com' },
  });
  s2.addShape(pptx.ShapeType.ellipse, { x: 3, y: 2.5, w: 3, h: 2, fill: { color: '5B9BD5' } });
  s2.addChart(pptx.ChartType.bar, [{ name: 'Sales', labels: ['Q1', 'Q2', 'Q3'], values: [10, 20, 15] }], {
    x: 6,
    y: 2.5,
    w: 3.5,
    h: 2.5,
    barDir: 'col',
  });
  // pptxgenjs's own quirks are normalised away so the fixture reads like a PowerPoint-authored file.
  const bytes = await normalizePackage(new Uint8Array((await pptx.write({ outputType: 'arraybuffer' })) as ArrayBuffer));
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
}

function shapes(deck: Deck, slide: number): ShapeElement[] {
  return deck.slides[slide].elements.filter((e): e is ShapeElement => e.kind === 'shape');
}

describe('pptx import', () => {
  it('reads slides, backgrounds, notes, text with runs, bullets, shapes, lines, tables and images', async () => {
    const { deck, report } = await pptxToDeck(await samplePptx(), { id: 'deck', name: 'sample.pptx' }, parse);
    expect(report.dropped).toEqual([]);
    expect(deck.size).toEqual({ width: 720, height: 405 });
    expect(deck.slides).toHaveLength(2);
    const s1 = deck.slides[0];
    expect(s1.background).toEqual({ type: 'color', color: '#F3F4F6' });
    expect(s1.notes).toBe('Speaker notes here.');
    const title = shapes(deck, 0)[0];
    expect(title).toMatchObject({ left: 36, top: 28.8, width: 648, height: 72, isTextBox: true });
    expect(title.text!.paragraphs[0].runs).toEqual([
      { text: 'Quarterly ', bold: true, color: '#1F4E79', fontSize: 32, fontFace: 'Arial' },
      { text: 'review', italic: true, color: '#333333', fontSize: 32, fontFace: 'Arial' },
    ]);
    expect(title.text!.vAlign).toBe('middle');
    const bullets = shapes(deck, 0)[1];
    expect(bullets.text!.paragraphs.map(p => ({ text: p.runs.map(r => r.text).join(''), bullet: p.bullet?.type }))).toEqual([
      { text: 'A bullet', bullet: 'bullet' },
      { text: 'Another bullet', bullet: 'bullet' },
    ]);
    const rounded = shapes(deck, 0)[2];
    expect(rounded).toMatchObject({
      shapeType: 'roundRect',
      fill: { type: 'color', color: '#FFC000' },
      border: { color: '#7F6000', width: 2, style: 'solid' },
      isTextBox: false,
    });
    expect(rounded.adjustments?.['adj']).toBeCloseTo(0.133, 2);
    const line = s1.elements.find(e => e.kind === 'line')!;
    expect(line).toMatchObject({ kind: 'line', width: 612, height: 0, border: { color: '#999999', width: 1, style: 'dashed' } });
    const table = s1.elements.find((e): e is TableElement => e.kind === 'table')!;
    expect(table.colWidths).toEqual([216, 144]);
    expect(table.rows).toHaveLength(3);
    expect(table.rows[0][0].fill).toEqual({ type: 'color', color: '#D9E2F3' });
    expect(table.rows[0][0].text.paragraphs[0].runs[0]).toMatchObject({ text: 'Name', bold: true });
    expect(table.rows[0][0].borders.top).toMatchObject({ color: '#999999', width: 1 });
    expect(plainText(table.rows[2][1].text)).toBe('10');
    const image = s1.elements.find((e): e is ImageElement => e.kind === 'image')!;
    expect(image.src).toBe(`data:image/png;base64,${PNG_BASE64}`);
    expect(image).toMatchObject({ left: 432, top: 259.2, width: 144, height: 72 });
  });

  it('reads hyperlinks, ellipses and charts', async () => {
    const { deck } = await pptxToDeck(await samplePptx(), { id: 'deck', name: 'sample.pptx' }, parse);
    const s2 = deck.slides[1];
    const text = shapes(deck, 1)[0];
    expect(text.text!.paragraphs[0].align).toBe('center');
    expect(text.text!.paragraphs[0].runs[0]).toMatchObject({ text: 'Second slide', color: '#2E7D32', fontSize: 28 });
    expect(text.link ?? text.text!.paragraphs[0].runs[0].link).toBe('https://example.com');
    const ellipse = shapes(deck, 1)[1];
    expect(ellipse).toMatchObject({ shapeType: 'ellipse', fill: { type: 'color', color: '#5B9BD5' } });
    const chart = s2.elements.find((e): e is ChartElement => e.kind === 'chart')!;
    expect(chart).toMatchObject({ chartType: 'bar', barDirection: 'col' });
    expect(chart.series).toEqual([{ name: 'Sales', labels: ['Q1', 'Q2', 'Q3'], values: [10, 20, 15] }]);
  });
});

describe('pptx export', () => {
  it('round-trips the sample through bytes', async () => {
    const first = await pptxToDeck(await samplePptx(), { id: 'deck', name: 'sample.pptx' }, parse);
    const exported = await deckToPptx(first.deck, async () => new PptxGenJS());
    expect(exported.report.dropped).toEqual([]);
    const second = await pptxToDeck(exported.bytes, { id: 'deck', name: 'sample.pptx' }, parse);
    const a = first.deck;
    const b = second.deck;
    expect(b.size).toEqual(a.size);
    expect(b.slides.map(s => s.background)).toEqual(a.slides.map(s => s.background));
    expect(b.slides.map(s => s.notes)).toEqual(a.slides.map(s => s.notes));
    expect(b.slides.map(s => s.elements.map(e => e.kind))).toEqual(a.slides.map(s => s.elements.map(e => e.kind)));
    for (const [si, slide] of a.slides.entries()) {
      for (const [ei, el] of slide.elements.entries()) {
        const other = b.slides[si].elements[ei];
        expect({ l: other.left, t: other.top, w: other.width, h: other.height }, `${el.kind} ${si}/${ei}`).toEqual({
          l: el.left,
          t: el.top,
          w: el.width,
          h: el.height,
        });
        if (el.kind === 'shape' && other.kind === 'shape') {
          expect(other.shapeType).toBe(el.shapeType);
          expect(other.fill).toEqual(el.fill);
          expect(other.border).toEqual(el.border);
          expect(other.text?.paragraphs.map(p => ({ align: p.align, bullet: p.bullet?.type, runs: p.runs }))).toEqual(
            el.text?.paragraphs.map(p => ({ align: p.align, bullet: p.bullet?.type, runs: p.runs })),
          );
        }
        if (el.kind === 'image' && other.kind === 'image') expect(other.src).toBe(el.src);
        if (el.kind === 'table' && other.kind === 'table') {
          expect(other.colWidths).toEqual(el.colWidths);
          expect(other.rows.map(r => r.map(c => ({ text: plainText(c.text), fill: c.fill })))).toEqual(
            el.rows.map(r => r.map(c => ({ text: plainText(c.text), fill: c.fill }))),
          );
        }
        if (el.kind === 'chart' && other.kind === 'chart') expect(other.series).toEqual(el.series);
        if (el.kind === 'line' && other.kind === 'line') expect(other.border).toEqual(el.border);
      }
    }
  });

  it('writes a deck authored in the editor', async () => {
    const deck = blankDeck('new', 'New deck');
    deck.slides[0].elements.push({
      id: 'title',
      name: 'Title',
      kind: 'shape',
      shapeType: 'rect',
      left: 36,
      top: 36,
      width: 648,
      height: 72,
      rotate: 0,
      flipH: false,
      flipV: false,
      fill: null,
      border: null,
      isTextBox: true,
      text: {
        ...emptyTextBody([
          {
            align: 'center',
            runs: [
              { text: 'Hello ', bold: true, fontSize: 40 },
              { text: 'world', color: '#FF0000', fontSize: 40 },
            ],
          },
        ]),
        vAlign: 'middle',
      },
    });
    const { bytes, report } = await deckToPptx(deck, async () => new PptxGenJS());
    expect(report.dropped).toEqual([]);
    const back = await pptxToDeck(bytes, { id: 'new', name: 'new.pptx' }, parse);
    const title = shapes(back.deck, 0)[0];
    expect(title).toMatchObject({ left: 36, top: 36, width: 648, height: 72 });
    expect(title.text!.paragraphs[0].align).toBe('center');
    expect(title.text!.paragraphs[0].runs.map(r => r.text).join('')).toBe('Hello world');
    expect(title.text!.paragraphs[0].runs[0]).toMatchObject({ bold: true, fontSize: 40 });
    expect(title.text!.paragraphs[0].runs[1]).toMatchObject({ color: '#FF0000' });
  });
});

describe('pptx text', () => {
  it('parses nested lists, links and line breaks', () => {
    const paragraphs = htmlToParagraphs(
      '<ol><li><p style="text-align: right;"><span style="font-size: 12pt;">One</span><br>two</p></li></ol><p><a href="https://x.y"><span style="text-decoration: underline;">link</span></a></p>',
    );
    expect(paragraphs).toEqual([
      { align: 'right', bullet: { type: 'number', level: 0 }, runs: [{ text: 'One', fontSize: 12 }, { text: '\ntwo' }] },
      { align: 'left', runs: [{ text: 'link', link: 'https://x.y', underline: true }] },
    ]);
    const runs = paragraphsToRuns(paragraphs);
    expect(runs.map(r => r.text)).toEqual(['One', '', 'two', 'link']);
    expect(runs[2].options?.softBreakBefore).toBe(true);
    expect(runs[2].options?.breakLine).toBe(true);
    expect(runs[3].options?.hyperlink).toEqual({ url: 'https://x.y' });
  });
});

// --- patching the original package ---------------------------------------------------------------

async function partsOf(bytes: ArrayBuffer | Uint8Array): Promise<Map<string, string>> {
  const pkg = await OoxmlPackage.open(bytes);
  const out = new Map<string, string>();
  for (const path of pkg.paths()) {
    const data = (await pkg.bytes(path))!;
    out.set(path, /\.(xml|rels)$/.test(path) ? new TextDecoder().decode(data) : `bytes:${data.length}`);
  }
  return out;
}

/** Paths whose content differs between two packages, plus paths only one of them has. */
function changedParts(a: Map<string, string>, b: Map<string, string>): string[] {
  const out = new Set<string>();
  for (const [path, content] of a) if (b.get(path) !== content) out.add(path);
  for (const path of b.keys()) if (!a.has(path)) out.add(path);
  return [...out].sort();
}

function withText(deck: Deck, slideIndex: number, elementIndex: number, text: string): Deck {
  const slide = deck.slides[slideIndex];
  const el = slide.elements[elementIndex] as ShapeElement;
  const paragraphs = el.text!.paragraphs.map((p, i) => (i === 0 ? { ...p, runs: [{ ...p.runs[0], text }, ...p.runs.slice(1)] } : p));
  return updateElement(deck, slide.id, el.id, { text: { ...el.text!, paragraphs } });
}

describe('pptx patch export', () => {
  async function imported() {
    const original = new Uint8Array(await samplePptx());
    const { deck } = await pptxToDeck(original, { id: 'deck', name: 'sample.pptx' }, parse);
    return { original, deck };
  }

  it('gives slides and elements their package identity', async () => {
    const { deck } = await imported();
    expect(deck.slides.map(s => s.sourcePath)).toEqual(['ppt/slides/slide1.xml', 'ppt/slides/slide2.xml']);
    expect(deck.slides[0].elements.every(el => el.sourceId)).toBe(true);
    expect(new Set(deck.slides[0].elements.map(el => el.sourceId)).size).toBe(deck.slides[0].elements.length);
  });

  it('writes the same package when nothing changed', async () => {
    const { original, deck } = await imported();
    const { bytes, report } = await deckToPptx(deck, { original, imported: deck });
    expect(report.dropped).toEqual([]);
    expect(changedParts(await partsOf(original), await partsOf(bytes))).toEqual([]);
  });

  it('patches only the slide whose text changed, keeping run formatting', async () => {
    const { original, deck } = await imported();
    const edited = withText(deck, 0, 0, 'Yearly ');
    const { bytes } = await deckToPptx(edited, { original, imported: deck });
    expect(changedParts(await partsOf(original), await partsOf(bytes))).toEqual(['ppt/slides/slide1.xml']);
    const back = await pptxToDeck(bytes, { id: 'deck', name: 'sample.pptx' }, parse);
    const title = shapes(back.deck, 0)[0];
    expect(title.text!.paragraphs[0].runs).toEqual([
      { text: 'Yearly ', bold: true, color: '#1F4E79', fontSize: 32, fontFace: 'Arial' },
      { text: 'review', italic: true, color: '#333333', fontSize: 32, fontFace: 'Arial' },
    ]);
    // The untouched shapes of the slide are byte-identical in the slide part.
    const slideXml = (await partsOf(bytes)).get('ppt/slides/slide1.xml')!;
    const originalXml = (await partsOf(original)).get('ppt/slides/slide1.xml')!;
    const table = /<p:graphicFrame>[\s\S]*?<\/p:graphicFrame>/.exec(originalXml)![0];
    expect(slideXml).toContain(table);
  });

  it('moves, resizes, rotates and recolours in place', async () => {
    const { original, deck } = await imported();
    const slide = deck.slides[0];
    const rounded = shapes(deck, 0)[2];
    let edited = updateElement(deck, slide.id, rounded.id, { left: 100, top: 50, width: 200, height: 80, rotate: 30 });
    edited = updateElement(edited, slide.id, rounded.id, { fill: { type: 'color', color: '#00FF00' }, border: { color: '#0000FF', width: 3, style: 'dashed' } });
    const { bytes } = await deckToPptx(edited, { original, imported: deck });
    expect(changedParts(await partsOf(original), await partsOf(bytes))).toEqual(['ppt/slides/slide1.xml']);
    const back = await pptxToDeck(bytes, { id: 'deck', name: 'sample.pptx' }, parse);
    const other = shapes(back.deck, 0)[2];
    expect(other).toMatchObject({ left: 100, top: 50, width: 200, height: 80, rotate: 30, shapeType: 'roundRect' });
    expect(other.fill).toEqual({ type: 'color', color: '#00FF00' });
    expect(other.border).toEqual({ color: '#0000FF', width: 3, style: 'dashed' });
    expect(other.adjustments?.['adj']).toBeCloseTo(0.133, 2);
  });

  it('removes a shape and adds a text box and a picture', async () => {
    const { original, deck } = await imported();
    const slide = deck.slides[0];
    const bullets = shapes(deck, 0)[1];
    let edited = removeElement(deck, slide.id, bullets.id);
    const box = newTextBox(50, 300, 300, 40, 'Added here');
    box.text!.paragraphs[0].runs[0] = { text: 'Added here', bold: true, color: '#FF0000', fontSize: 20 };
    edited = addElement(edited, slide.id, box);
    edited = addElement(edited, slide.id, newImage(`data:image/png;base64,${PNG_BASE64}`, 64, 32, deck.size));
    const { bytes, report } = await deckToPptx(edited, { original, imported: deck });
    expect(report.dropped).toEqual([]);
    const changed = changedParts(await partsOf(original), await partsOf(bytes));
    expect(changed).toContain('ppt/slides/slide1.xml');
    expect(changed).toContain('ppt/slides/_rels/slide1.xml.rels');
    expect(changed.some(p => /^ppt\/media\/image\d+\.png$/.test(p))).toBe(true);
    expect(changed.filter(p => /slide2|slideLayout|slideMaster|theme/.test(p))).toEqual([]);
    const back = await pptxToDeck(bytes, { id: 'deck', name: 'sample.pptx' }, parse);
    const s1 = back.deck.slides[0];
    expect(s1.elements).toHaveLength(slide.elements.length + 1);
    expect(shapes(back.deck, 0).map(sh => plainText(sh.text))).not.toContain('A bullet\nAnother bullet');
    const added = shapes(back.deck, 0).find(sh => plainText(sh.text) === 'Added here')!;
    expect(added).toMatchObject({ left: 50, top: 300, width: 300, height: 40, isTextBox: true });
    expect(added.text!.paragraphs[0].runs[0]).toMatchObject({ bold: true, color: '#FF0000', fontSize: 20 });
    const pictures = s1.elements.filter((e): e is ImageElement => e.kind === 'image');
    expect(pictures).toHaveLength(2);
    expect(pictures[1].src).toBe(`data:image/png;base64,${PNG_BASE64}`);
  });

  it('deletes, duplicates, adds and reorders slides', async () => {
    const { original, deck } = await imported();
    let edited = deleteSlide(deck, deck.slides[1].id);
    const dup = duplicateSlide(edited, edited.slides[0].id);
    edited = withText(dup.deck, 1, 0, 'Copy ');
    const added = addSlide(edited, 1);
    edited = added.deck;
    edited = addElement(edited, added.slide.id, newTextBox(36, 36, 400, 60, 'Brand new'));
    edited = moveSlide(edited, added.slide.id, 0);
    const { bytes, report } = await deckToPptx(edited, { original, imported: deck });
    expect(report.dropped).toEqual([]);
    const parts = await partsOf(bytes);
    expect(parts.has('ppt/slides/slide2.xml')).toBe(false);
    expect(parts.has('ppt/slides/slide3.xml')).toBe(true);
    expect(parts.has('ppt/slides/slide4.xml')).toBe(true);
    expect(parts.get('[Content_Types].xml')).toContain('/ppt/slides/slide4.xml');
    expect(parts.get('[Content_Types].xml')).not.toContain('/ppt/slides/slide2.xml');
    expect(parts.get('ppt/slides/_rels/slide4.xml.rels')).toContain('slideLayout');
    const back = await pptxToDeck(bytes, { id: 'deck', name: 'sample.pptx' }, parse);
    expect(back.deck.slides).toHaveLength(3);
    expect(back.deck.slides.map(s => plainText(shapes(back.deck, back.deck.slides.indexOf(s))[0]?.text))).toEqual([
      'Brand new',
      'Quarterly review',
      'Copy review',
    ]);
    // The copy keeps the notes, in a notes part of its own.
    expect(back.deck.slides.map(s => s.notes)).toEqual(['', 'Speaker notes here.', 'Speaker notes here.']);
    expect(parts.get('ppt/slides/_rels/slide3.xml.rels')).not.toContain('notesSlide1.xml');
    expect(back.deck.slides[2].elements).toHaveLength(deck.slides[0].elements.length);
  });

  it('adds and changes speaker notes', async () => {
    // pptxgenjs gives every slide a notes part; slide 2 loses its own so the save has to make one.
    const pkg = await OoxmlPackage.open(await samplePptx());
    const rels = (await pkg.text('ppt/slides/_rels/slide2.xml.rels'))!;
    pkg.put('ppt/slides/_rels/slide2.xml.rels', rels.replace(/<Relationship [^>]*notesSlide[^>]*\/>/, ''));
    const original = await pkg.save('application/vnd.openxmlformats-officedocument.presentationml.presentation');
    const { deck } = await pptxToDeck(original, { id: 'deck', name: 'sample.pptx' }, parse);
    expect(deck.slides[1].notes).toBe('');
    let edited = updateSlide(deck, deck.slides[1].id, { notes: 'New notes\nSecond line' });
    edited = updateSlide(edited, deck.slides[0].id, { notes: 'Changed notes' });
    const { bytes } = await deckToPptx(edited, { original, imported: deck });
    const changed = changedParts(await partsOf(original), await partsOf(bytes));
    expect(changed).toContain('ppt/slides/_rels/slide2.xml.rels');
    expect(changed).toContain('ppt/notesSlides/notesSlide1.xml');
    expect(changed).toContain('ppt/notesSlides/notesSlide3.xml');
    expect(changed).toContain('[Content_Types].xml');
    expect(changed).not.toContain('ppt/slides/slide1.xml');
    expect(changed).not.toContain('ppt/slides/slide2.xml');
    const back = await pptxToDeck(bytes, { id: 'deck', name: 'sample.pptx' }, parse);
    expect(back.deck.slides.map(s => s.notes)).toEqual(['Changed notes', 'New notes\nSecond line']);
  });

  it('keeps animations and custom geometry through an unrelated edit', async () => {
    const base = await samplePptx();
    const pkg = await OoxmlPackage.open(base);
    let xml = (await pkg.text('ppt/slides/slide1.xml'))!;
    const timing = '<p:timing><p:tnLst><p:par><p:cTn id="1" dur="indefinite" restart="never" nodeType="tmRoot"/></p:par></p:tnLst></p:timing>';
    xml = xml.replace('</p:cSld>', `</p:cSld>${timing}`.replace(`${timing}`, '')).replace('</p:sld>', `${timing}</p:sld>`);
    const custom =
      '<a:custGeom><a:avLst/><a:gdLst/><a:ahLst/><a:cxnLst/><a:rect l="0" t="0" r="r" b="b"/><a:pathLst><a:path w="100" h="100"><a:moveTo><a:pt x="0" y="0"/></a:moveTo><a:lnTo><a:pt x="100" y="50"/></a:lnTo><a:lnTo><a:pt x="0" y="100"/></a:lnTo><a:close/></a:path></a:pathLst></a:custGeom>';
    xml = xml.replace(/<a:prstGeom prst="roundRect">[\s\S]*?<\/a:prstGeom>/, custom);
    pkg.put('ppt/slides/slide1.xml', xml);
    const original = await pkg.save('application/vnd.openxmlformats-officedocument.presentationml.presentation');
    const { deck } = await pptxToDeck(original, { id: 'deck', name: 'sample.pptx' }, parse);
    const edited = withText(deck, 0, 0, 'Edited ');
    const { bytes } = await deckToPptx(edited, { original, imported: deck });
    const slideXml = (await partsOf(bytes)).get('ppt/slides/slide1.xml')!;
    expect(slideXml).toContain(timing);
    expect(slideXml).toContain('<a:custGeom>');
    expect(slideXml).toContain('<a:t>Edited </a:t>');
  });
});
