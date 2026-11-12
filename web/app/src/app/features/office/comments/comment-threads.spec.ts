import { describe, expect, it } from 'vitest';

import {
  CommentThread,
  addReply,
  commentAuthors,
  editComment,
  keptMentions,
  newComment,
  removeComment,
  setResolved,
  personIdFromProvider,
  providerUserIdOf,
} from './comment-threads';

const ME = { id: 'user:u-1', name: 'Pat Lee' };
const SAM = { id: 'author:Sam Okafor', name: 'Sam Okafor' };

function thread(): CommentThread<null>[] {
  const first = newComment('t1', SAM, { text: 'First', mentions: [] }, new Date('2026-11-01T00:00:00Z'));
  return [{ id: 't1', anchor: null, comments: [first] }];
}

describe('comment threads', () => {
  it('keeps only mentions still written into the text, once each', () => {
    expect(keptMentions('Hi @Pat Lee and @Pat Lee', [ME, ME, SAM])).toEqual([ME]);
  });

  it('makes a comment from a draft: trimmed, with its author and mentions', () => {
    const c = newComment('c1', ME, { text: '  Ask @Sam Okafor  ', mentions: [SAM] }, new Date('2026-11-11T10:00:00Z'));
    expect(c).toEqual({
      id: 'c1',
      authorId: ME.id,
      authorName: ME.name,
      createdAt: '2026-11-11T10:00:00.000Z',
      text: 'Ask @Sam Okafor',
      mentions: [SAM],
    });
  });

  it('replies, edits, resolves and removes without changing the input', () => {
    const start = thread();
    const replied = addReply(start, 't1', newComment('c2', ME, { text: 'Reply', mentions: [] }));
    expect(start[0].comments).toHaveLength(1);
    expect(replied[0].comments.map(c => c.id)).toEqual(['t1', 'c2']);

    const edited = editComment(replied, 't1', 'c2', { text: 'Reply to @Sam Okafor', mentions: [SAM] });
    expect(edited[0].comments[1]).toEqual(expect.objectContaining({ text: 'Reply to @Sam Okafor', mentions: [SAM] }));
    expect(editComment(edited, 't1', 'c2', { text: 'Plain', mentions: [SAM] })[0].comments[1].mentions).toBeUndefined();

    expect(setResolved(edited, 't1', true)[0].resolved).toBe(true);
    expect(removeComment(edited, 't1', 'c2')[0].comments.map(c => c.id)).toEqual(['t1']);
    expect(removeComment(edited, 't1', 't1')).toEqual([]);
  });

  it('lists the authors once each', () => {
    const threads = addReply(
      addReply(thread(), 't1', newComment('c2', ME, { text: 'a', mentions: [] })),
      't1',
      newComment('c3', SAM, { text: 'b', mentions: [] }),
    );
    expect(commentAuthors(threads)).toEqual([SAM, ME]);
  });

  it('names WhatIff users and agents in files by an id that comes back the same (ADR 0x046)', () => {
    for (const id of ['user:u-1', 'agent:p-1']) expect(personIdFromProvider(providerUserIdOf(id)!)).toBe(id);
    expect(providerUserIdOf('author:Pat')).toBeNull();
  });
});
