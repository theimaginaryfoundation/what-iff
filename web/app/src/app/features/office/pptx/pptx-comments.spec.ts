import PptxGenJS from 'pptxgenjs';
import { parse } from 'pptxtojson';
import { describe, expect, it } from 'vitest';

import { addReply, newComment, setResolved } from '../comments/comment-threads';
import { OoxmlPackage } from '../shared/ooxml';
import { blankDeck } from './deck-model';
import { SlideCommentThread, newCommentGuid } from './pptx-comments';
import { deckToPptx, normalizePackage } from './pptx-export';
import { pptxToDeck } from './pptx-import';

/**
 * Slide comments (ADR 0x045): modern comments (p188) on slide 1, one on a shape and one on the
 * slide with a reply and resolved; a legacy comment with a threaded reply on slide 2.
 */

const PPTX_MIME = 'application/vnd.openxmlformats-officedocument.presentationml.presentation';
const HEAD = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\r\n';
const A = 'http://schemas.openxmlformats.org/drawingml/2006/main';
const P = 'http://schemas.openxmlformats.org/presentationml/2006/main';
const R = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const P188 = 'http://schemas.microsoft.com/office/powerpoint/2018/8/main';
const PC = 'http://schemas.microsoft.com/office/powerpoint/2013/main/command';
const AC = 'http://schemas.microsoft.com/office/drawing/2013/main/command';
const P15 = 'http://schemas.microsoft.com/office/powerpoint/2012/main';
const DANA = '{11111111-1111-4111-8111-111111111111}';
const SAM = '{22222222-2222-4222-8222-222222222222}';
const SHAPE_THREAD = '{AAAAAAAA-0000-4000-8000-000000000001}';
const SLIDE_THREAD = '{AAAAAAAA-0000-4000-8000-000000000002}';
const SLIDE_REPLY = '{AAAAAAAA-0000-4000-8000-000000000003}';
const ME = { id: 'user:u-42', name: 'Chris Writer' };

const body = (text: string) =>
  `<p188:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US"/><a:t>${text}</a:t></a:r></a:p></p188:txBody>`;

async function commentedDeck(): Promise<{ bytes: Uint8Array; modernXml: string; authorsXml: string; shapeId: string }> {
  const pptx = new PptxGenJS();
  pptx.layout = 'LAYOUT_16x9';
  pptx.addSlide().addText('Quarterly plan', { x: 1, y: 1, w: 4, h: 1 });
  pptx.addSlide().addText('Budget', { x: 1, y: 1, w: 4, h: 1 });
  const raw = await pptx.write({ outputType: 'uint8array' });
  const pkg = await OoxmlPackage.open(await normalizePackage(raw as Uint8Array));
  const slide1 = (await pkg.text('ppt/slides/slide1.xml'))!;
  const shapeId = /<p:sp>[\s\S]*?<p:cNvPr id="(\d+)"/.exec(slide1)![1];
  const pres = (await pkg.text('ppt/presentation.xml'))!;
  const [sld1, sld2] = [...pres.matchAll(/<p:sldId id="(\d+)"/g)].map(m => m[1]);

  const modernXml =
    `${HEAD}<p188:cmLst xmlns:a="${A}" xmlns:r="${R}" xmlns:p188="${P188}">` +
    `<p188:cm id="${SHAPE_THREAD}" authorId="${DANA}" created="2026-11-02T17:15:00.000">` +
    `<ac:deMkLst xmlns:ac="${AC}"><pc:docMk xmlns:pc="${PC}"/><pc:sldMk xmlns:pc="${PC}" sldId="${sld1}"/><ac:spMk id="${shapeId}"/></ac:deMkLst>` +
    `<p188:pos x="0" y="0"/>${body('Is this title final?')}</p188:cm>` +
    `<p188:cm id="${SLIDE_THREAD}" authorId="${SAM}" created="2026-11-03T09:00:00.000" status="resolved">` +
    `<pc:sldMkLst xmlns:pc="${PC}"><pc:docMk/><pc:sldMk sldId="${sld1}"/></pc:sldMkLst><p188:pos x="127000" y="254000"/>` +
    `<p188:replyLst><p188:reply id="${SLIDE_REPLY}" authorId="${DANA}" created="2026-11-03T10:00:00.000">${body('Done, @Sam Okafor')}</p188:reply></p188:replyLst>` +
    `${body('Fix the colours')}</p188:cm></p188:cmLst>`;
  const authorsXml =
    `${HEAD}<p188:authorLst xmlns:a="${A}" xmlns:r="${R}" xmlns:p188="${P188}">` +
    `<p188:author id="${DANA}" name="Dana Reyes" initials="DR" userId="dana@example.com" providerId="AD"/>` +
    `<p188:author id="${SAM}" name="Sam Okafor" initials="SO" userId="sam@example.com" providerId="AD"/></p188:authorLst>`;
  pkg.put('ppt/comments/modernComment_100_0.xml', modernXml);
  pkg.put('ppt/authors.xml', authorsXml);
  pkg.put(
    'ppt/comments/comment1.xml',
    `${HEAD}<p:cmLst xmlns:p="${P}" xmlns:p15="${P15}"><p:cm authorId="0" dt="2026-11-04T12:00:00.000" idx="1"><p:pos x="80" y="160"/><p:text>Cut this slide?</p:text></p:cm>` +
      `<p:cm authorId="1" dt="2026-11-04T13:00:00.000" idx="1"><p:pos x="80" y="160"/><p:text>No, keep it.</p:text><p:extLst><p:ext uri="{C676402C-5697-4E1C-873F-D02D1690AC5C}"><p15:threadingInfo timeZoneBias="0"><p15:parentCm authorId="0" idx="1"/></p15:threadingInfo></p:ext></p:extLst></p:cm></p:cmLst>`,
  );
  pkg.put(
    'ppt/commentAuthors.xml',
    `${HEAD}<p:cmAuthorLst xmlns:p="${P}"><p:cmAuthor id="0" name="Kim Lee" initials="KL" lastIdx="1" clrIdx="0"/><p:cmAuthor id="1" name="Ola Berg" initials="OB" lastIdx="1" clrIdx="1"/></p:cmAuthorLst>`,
  );
  const addRel = async (part: string, xml: string) => {
    const path = part.replace(/([^/]+)$/, '_rels/$1.rels');
    pkg.put(path, (await pkg.text(path))!.replace('</Relationships>', `${xml}</Relationships>`));
  };
  await addRel(
    'ppt/slides/slide1.xml',
    `<Relationship Id="rId90" Type="http://schemas.microsoft.com/office/2018/10/relationships/comments" Target="../comments/modernComment_100_0.xml"/>`,
  );
  await addRel('ppt/slides/slide2.xml', `<Relationship Id="rId90" Type="${R}/comments" Target="../comments/comment1.xml"/>`);
  await addRel(
    'ppt/presentation.xml',
    `<Relationship Id="rId90" Type="http://schemas.microsoft.com/office/2018/10/relationships/authors" Target="authors.xml"/>`,
  );
  await addRel('ppt/presentation.xml', `<Relationship Id="rId91" Type="${R}/commentAuthors" Target="commentAuthors.xml"/>`);
  pkg.put(
    'ppt/slides/slide1.xml',
    slide1.replace(
      '</p:sld>',
      `<p:extLst><p:ext uri="{6950BFC3-D8DA-4A85-94F7-54DA5524770B}"><p188:commentRel xmlns:p188="${P188}" r:id="rId90"/></p:ext></p:extLst></p:sld>`,
    ),
  );
  void sld2;
  const ct = (await pkg.text('[Content_Types].xml'))!.replace(
    '</Types>',
    `<Override PartName="/ppt/comments/modernComment_100_0.xml" ContentType="application/vnd.ms-powerpoint.comments+xml"/>` +
      `<Override PartName="/ppt/authors.xml" ContentType="application/vnd.ms-powerpoint.authors+xml"/>` +
      `<Override PartName="/ppt/comments/comment1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.comments+xml"/>` +
      `<Override PartName="/ppt/commentAuthors.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.commentAuthors+xml"/></Types>`,
  );
  pkg.put('[Content_Types].xml', ct);
  return { bytes: await pkg.save(PPTX_MIME), modernXml, authorsXml, shapeId };
}

const read = (bytes: Uint8Array) => pptxToDeck(bytes, { id: 'deck', name: 'plan.pptx' }, parse);

describe('pptx comments', () => {
  it('reads modern and legacy threads: anchors, replies, resolved state and mentions', async () => {
    const { bytes } = await commentedDeck();
    const { deck, comments } = await read(bytes);
    const [s1, s2] = deck.slides;
    const title = s1.elements[0];
    expect(
      comments.threads.map(t => [t.id, t.anchor.slideId, t.resolved ?? false, t.comments.map(c => `${c.authorName}: ${c.text}`)]),
    ).toEqual([
      [SHAPE_THREAD, s1.id, false, ['Dana Reyes: Is this title final?']],
      [SLIDE_THREAD, s1.id, true, ['Sam Okafor: Fix the colours', 'Dana Reyes: Done, @Sam Okafor']],
      ['{00000000-0000-4000-8000-000000000001}', s2.id, false, ['Kim Lee: Cut this slide?', 'Ola Berg: No, keep it.']],
    ]);
    const [onShape, onSlide, legacy] = comments.threads;
    // A shape thread's pin is at the shape (its offset is 0, 0); positions are EMU from the anchor.
    expect(onShape.anchor).toEqual({ slideId: s1.id, x: title.left, y: title.top, elementId: title.id });
    expect(onSlide.anchor).toEqual({ slideId: s1.id, x: 10, y: 20 });
    expect(legacy.anchor).toEqual({ slideId: s2.id, x: 10, y: 20 });
    expect(onSlide.comments[1].mentions).toEqual([{ id: 'author:Sam Okafor', name: 'Sam Okafor' }]);
  });

  it('leaves the comment parts as they were when the threads did not change', async () => {
    const { bytes, modernXml, authorsXml } = await commentedDeck();
    const { deck, comments } = await read(bytes);
    const saved = await OoxmlPackage.open(
      (await deckToPptx(deck, { original: bytes, imported: deck, comments: comments.threads, importedComments: comments })).bytes,
    );
    expect(await saved.text('ppt/comments/modernComment_100_0.xml')).toBe(modernXml);
    expect(await saved.text('ppt/authors.xml')).toBe(authorsXml);
    expect(await saved.text('ppt/comments/comment1.xml')).toContain('Cut this slide?');
  });

  it('writes changed slides in the modern form and reads them back with the same ids', async () => {
    const { bytes } = await commentedDeck();
    const { deck, comments } = await read(bytes);
    const [s1, s2] = deck.slides;
    const replyId = newCommentGuid();
    const newId = newCommentGuid();
    let threads: SlideCommentThread[] = addReply(
      comments.threads,
      '{00000000-0000-4000-8000-000000000001}',
      newComment(replyId, ME, { text: 'Agreed, keep it.', mentions: [] }),
    );
    threads = setResolved(threads, SLIDE_THREAD, false);
    threads = [
      ...threads,
      {
        id: newId,
        anchor: { slideId: s2.id, x: 100, y: 50 },
        comments: [newComment(newId, ME, { text: 'Add a chart, @Kim Lee', mentions: [{ id: 'author:Kim Lee', name: 'Kim Lee' }] })],
      },
    ];

    const out = (await deckToPptx(deck, { original: bytes, imported: deck, comments: threads, importedComments: comments })).bytes;
    const saved = await OoxmlPackage.open(out);
    // The legacy part is replaced by a modern one, linked from the slide.
    expect(saved.has('ppt/comments/comment1.xml')).toBe(false);
    const slide2 = (await saved.text('ppt/slides/slide2.xml'))!;
    expect(slide2).toContain('{6950BFC3-D8DA-4A85-94F7-54DA5524770B}');
    expect(await saved.text('[Content_Types].xml')).toContain('application/vnd.ms-powerpoint.comments+xml');
    const authors = (await saved.text('ppt/authors.xml'))!;
    expect(authors).toContain('name="Chris Writer"');
    expect(authors).toContain('userId="u-42" providerId="WhatIff"');
    // The shape thread keeps its anchor as PowerPoint wrote it.
    expect(await saved.text('ppt/comments/modernComment_100_0.xml')).toContain('<ac:spMk id=');

    const back = await read(out);
    const ids = (slide: string) =>
      back.comments.threads.filter(t => t.anchor.slideId === slide).map(t => [t.id, t.resolved ?? false, t.comments.map(c => c.id)]);
    expect(ids(back.deck.slides[0].id)).toEqual([
      [SHAPE_THREAD, false, [SHAPE_THREAD]],
      [SLIDE_THREAD, false, [SLIDE_THREAD, SLIDE_REPLY]],
    ]);
    expect(ids(back.deck.slides[1].id)).toEqual([
      [
        '{00000000-0000-4000-8000-000000000001}',
        false,
        ['{00000000-0000-4000-8000-000000000001}', '{00000000-0000-4000-8000-000100000001}', replyId],
      ],
      [newId, false, [newId]],
    ]);
    const added = back.comments.threads.find(t => t.id === newId)!;
    expect(added.anchor).toEqual({ slideId: back.deck.slides[1].id, x: 100, y: 50 });
    expect(added.comments[0]).toEqual(
      expect.objectContaining({ authorId: 'user:u-42', authorName: 'Chris Writer', mentions: [{ id: 'author:Kim Lee', name: 'Kim Lee' }] }),
    );
    expect(back.comments.threads.find(t => t.id === SHAPE_THREAD)!.anchor.elementId).toBe(back.deck.slides[0].elements[0].id);
  });

  it('removes a slide comment part when its last thread is deleted', async () => {
    const { bytes } = await commentedDeck();
    const { deck, comments } = await read(bytes);
    const threads = comments.threads.filter(t => t.anchor.slideId !== deck.slides[0].id);
    const saved = await OoxmlPackage.open(
      (await deckToPptx(deck, { original: bytes, imported: deck, comments: threads, importedComments: comments })).bytes,
    );
    expect(saved.has('ppt/comments/modernComment_100_0.xml')).toBe(false);
    expect(await saved.text('ppt/slides/slide1.xml')).not.toContain('commentRel');
    expect(await saved.text('ppt/slides/_rels/slide1.xml.rels')).not.toContain('2018/10/relationships/comments');
  });

  it('adds comments to a deck made here, saved for the first time', async () => {
    const deck = blankDeck('d', 'new.pptx');
    const id = newCommentGuid();
    const threads: SlideCommentThread[] = [
      { id, anchor: { slideId: deck.slides[0].id, x: 30, y: 40 }, comments: [newComment(id, ME, { text: 'First!', mentions: [] })] },
    ];
    const out = (await deckToPptx(deck, { comments: threads })).bytes;
    const back = await read(out);
    expect(back.comments.threads.map(t => [t.id, t.anchor.x, t.anchor.y, t.comments[0].text])).toEqual([[id, 30, 40, 'First!']]);
  });
});
