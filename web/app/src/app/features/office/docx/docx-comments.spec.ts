import { describe, expect, it } from 'vitest';

import { addReply, newComment, setResolved } from '../comments/comment-threads';
import { OoxmlPackage } from '../shared/ooxml';
import { COMMENTS_XML, EXTENDED_XML, PEOPLE_XML, commentedDocx as fixture } from './docx-comments.spec-helper';
import { DocxCommentThread, WordComment, WordComments, newWordCommentId, planCommentParts } from './docx-comments';
import { documentToDocx } from './docx-export';
import { docxToDocument } from './docx-import';
import { CUSTOM_DECORATION_COMMENT, DocDocument } from './univer-doc-types';

/**
 * Word comments as threads (ADR 0x045): replies and resolved state from commentsExtended, durable
 * ids from commentsIds, UTC dates from commentsExtensible, people from people.xml; and back.
 */

const marks = (document: DocDocument) => (document.body.customDecorations ?? []).filter(d => d.type === CUSTOM_DECORATION_COMMENT);
const ME = { id: 'user:u-42', name: 'Chris Writer' };

describe('docx comments', () => {
  it('reads threads, replies, resolved state, durable ids and mentions', async () => {
    const { document, comments } = await docxToDocument(await fixture(), { id: 'doc', name: 'plan.docx' });
    expect(comments.map(t => t.id)).toEqual(['5D3E2F01', '5D3E2F03']);
    const [first, second] = comments;
    expect(first.quote).toBe('three features');
    expect(first.resolved).toBeUndefined();
    expect(first.comments.map(c => [c.id, c.authorName, c.text])).toEqual([
      ['5D3E2F01', 'Dana Reyes', 'Is three realistic?'],
      ['5D3E2F02', 'Sam Okafor', 'Yes. @Dana Reyes can you confirm?'],
    ]);
    expect(first.comments[0].createdAt).toBe('2026-11-02T17:15:00.000Z');
    expect(first.comments[1].mentions).toEqual([{ id: 'author:Dana Reyes', name: 'Dana Reyes' }]);
    expect(second.resolved).toBe(true);
    // One mark per thread, over the first comment's text; the reply shares it.
    const stream = document.body.dataStream;
    expect(marks(document).map(m => [m.id, stream.slice(m.startIndex, m.endIndex + 1)])).toEqual([
      ['5D3E2F01', 'three features'],
      ['5D3E2F03', 'fixed'],
    ]);
  });

  it('keeps the comment parts byte for byte when the threads are unchanged', async () => {
    const original = await fixture();
    const { document, comments } = await docxToDocument(original, { id: 'doc', name: 'plan.docx' });
    const saved = await OoxmlPackage.open((await documentToDocx(document, original, { comments })).bytes);
    expect(await saved.text('word/comments.xml')).toBe(COMMENTS_XML);
    expect(await saved.text('word/commentsExtended.xml')).toBe(EXTENDED_XML);
    expect(await saved.text('word/people.xml')).toBe(PEOPLE_XML);
    const xml = (await saved.text('word/document.xml'))!;
    expect(xml).toContain('<w:commentRangeStart w:id="0"/><w:commentRangeStart w:id="1"/>');
    expect(xml).toContain('<w:commentReference w:id="2"/>');
  });

  it('writes replies, new threads and resolved state, and reads them back with the same ids', async () => {
    const original = await fixture();
    const { document, comments } = await docxToDocument(original, { id: 'doc', name: 'plan.docx' });
    const replyId = newWordCommentId();
    let threads: DocxCommentThread[] = addReply(
      comments,
      '5D3E2F01',
      newComment(replyId, ME, { text: 'Agreed, @Sam Okafor.', mentions: [{ id: 'author:Sam Okafor', name: 'Sam Okafor' }] }),
    );
    threads = setResolved(threads, '5D3E2F03', false);
    // A new thread on "this quarter": the editor adds its mark, then the thread.
    const newId = newWordCommentId();
    const at = document.body.dataStream.indexOf('this quarter');
    document.body.customDecorations!.push({
      id: newId,
      type: CUSTOM_DECORATION_COMMENT,
      startIndex: at,
      endIndex: at + 'this quarter'.length - 1,
    });
    threads = [
      ...threads,
      {
        id: newId,
        anchor: { segmentId: '' },
        quote: 'this quarter',
        comments: [newComment(newId, ME, { text: 'Which quarter?', mentions: [] })],
      },
    ];

    const bytes = (await documentToDocx(document, original, { comments: threads })).bytes;
    const saved = await OoxmlPackage.open(bytes);
    expect(await saved.text('word/people.xml')).toContain('w15:providerId="WhatIff" w15:userId="u-42"');
    expect(await saved.text('word/comments.xml')).toContain('w:author="Chris Writer"');

    const reread = await docxToDocument(bytes, { id: 'doc', name: 'plan.docx' });
    expect(reread.comments.map(t => [t.id, t.resolved ?? false, t.comments.map(c => c.id)])).toEqual([
      ['5D3E2F01', false, ['5D3E2F01', '5D3E2F02', replyId]],
      [newId, false, [newId]],
      ['5D3E2F03', false, ['5D3E2F03']],
    ]);
    const reply = reread.comments[0].comments[2];
    expect(reply).toEqual(expect.objectContaining({ authorId: 'user:u-42', authorName: 'Chris Writer', text: 'Agreed, @Sam Okafor.' }));
    expect(reply.mentions).toEqual([{ id: 'author:Sam Okafor', name: 'Sam Okafor' }]);
    expect(reread.comments[1].quote).toBe('this quarter');
    // Comments kept as they were are written from their original XML.
    expect(await saved.text('word/comments.xml')).toContain('w14:paraId="1A2B3C02"');
  });

  it('drops a thread whose text was deleted', async () => {
    const original = await fixture();
    const { document, comments } = await docxToDocument(original, { id: 'doc', name: 'plan.docx' });
    document.body.customDecorations = document.body.customDecorations!.filter(d => d.id !== '5D3E2F03');
    const bytes = (await documentToDocx(document, original, { comments })).bytes;
    const reread = await docxToDocument(bytes, { id: 'doc', name: 'plan.docx' });
    expect(reread.comments.map(t => t.id)).toEqual(['5D3E2F01']);
    const xml = (await (await OoxmlPackage.open(bytes)).text('word/comments.xml'))!;
    expect(xml).not.toContain('Finance signed');
  });

  describe('planCommentParts', () => {
    const word = (wordId: string, id: string, author: string, text: string): WordComment => ({
      wordId,
      id,
      author,
      date: '2026-11-02T17:15:00.000Z',
      text,
      lastParaId: id,
      durableId: id,
      done: false,
      xml: `<w:comment w:id="${wordId}" w:author="${author}"><w:p w14:paraId="${id}"><w:r><w:t>${text}</w:t></w:r></w:p></w:comment>`,
    });
    const original: WordComments = {
      comments: [word('3', '0000000A', 'Dana', 'On the body'), word('7', '0000000B', '', 'In a text box')],
      people: [],
      paths: {},
    };
    const onBody: DocxCommentThread = {
      id: '0000000A',
      anchor: { segmentId: '' },
      comments: [
        { id: '0000000A', authorId: 'author:Dana', authorName: 'Dana', createdAt: '2026-11-02T17:15:00.000Z', text: 'On the body' },
      ],
    };

    it("keeps each comment's w:id and numbers new ones above the file's", () => {
      const added: DocxCommentThread = {
        id: '0000000C',
        anchor: { segmentId: '' },
        comments: [newComment('0000000C', ME, { text: 'New', mentions: [] })],
      };
      const plan = planCommentParts([onBody, added], original, { keep: ['0000000B'] });
      expect(plan.wordIds.get('0000000A')).toEqual(['3']);
      expect(plan.wordIds.get('0000000C')).toEqual(['8']);
      // The comment with no place in the text is written as the file has it, id and all.
      expect(plan.parts?.comments).toContain(original.comments[1].xml);
      expect(plan.parts?.comments).toContain(original.comments[0].xml);
    });

    it('leaves the parts alone when only comments with no place in the text are left out of the threads', () => {
      expect(planCommentParts([onBody], original, { keep: ['0000000B'] }).parts).toBeNull();
    });

    it('does not rewrite a comment whose author is empty', () => {
      const unnamed: DocxCommentThread = {
        id: '0000000B',
        anchor: { segmentId: '' },
        comments: [
          { id: '0000000B', authorId: 'author:', authorName: 'Unknown', createdAt: '2026-11-02T17:15:00.000Z', text: 'In a text box' },
        ],
      };
      expect(planCommentParts([onBody, unnamed], original).parts).toBeNull();
    });
  });
});
