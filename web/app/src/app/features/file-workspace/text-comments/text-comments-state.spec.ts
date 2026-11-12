import { signal } from '@angular/core';
import { describe, expect, it } from 'vitest';

import { fakeTextComments } from './fake-text-comments.spec-helper';
import { TextCommentSurface, TextCommentsState } from './text-comments-state';
import { TextCommentsService } from './text-comments.service';

const TEXT = 'This is an inline comment in a text file';

/** A surface over fixed text whose ranges a test can move, as an editor would. */
function surface() {
  const ranges = new Map<string, { start: number; end: number } | null>();
  const s: TextCommentSurface & { ranges(): Map<string, { start: number; end: number } | null> } = {
    place: (id, range) => (range ? ranges.set(id, range) : ranges.delete(id)),
    ranges: () => new Map(ranges),
    paint: () => undefined,
    reveal: () => undefined,
    text: range => TEXT.slice(range.start, range.end),
  };
  return { s, ranges };
}

describe('TextCommentsState', () => {
  const me = signal({ id: 'user:u-1', name: 'Pat Lee' });

  it('places loaded threads, orders them by text, and quotes them', async () => {
    const api = fakeTextComments({
      contentVersion: 3,
      revision: 2,
      threads: [
        { id: 'b', anchor: { start: 11, end: 25 }, quote: 'inline comment', comments: [] },
        { id: 'whole', anchor: null, quote: 'gone words', comments: [] },
        { id: 'a', anchor: { start: 0, end: 4 }, quote: 'This', comments: [] },
      ],
    });
    const state = new TextCommentsState(api as unknown as TextCommentsService, me, () => undefined);
    const { s } = surface();
    state.attach(s);
    await state.load('f-1');
    expect(state.shown().map(t => [t.id, t.quote])).toEqual([
      ['whole', 'The whole file · was on “gone words”'],
      ['a', 'This'],
      ['b', 'inline comment'],
    ]);
    expect(state.threadAt(15)).toBe('b');
    expect(state.placedOn).toBe(3);
  });

  it('a thread whose text is all gone is saved on the whole file, with the quote it had', async () => {
    const api = fakeTextComments();
    const changed = vi.fn();
    const state = new TextCommentsState(api as unknown as TextCommentsService, me, changed);
    const { s, ranges } = surface();
    state.attach(s);
    await state.load('f-1');
    state.start({ start: 11, end: 25 });
    state.create({ text: 'Rename this?', mentions: [] });
    expect(changed).toHaveBeenCalledTimes(1);
    const id = state.threads()[0].id;
    ranges.set(id, null);
    await state.save(2);
    expect(api.state.puts[0]).toEqual(
      expect.objectContaining({
        contentVersion: 2,
        revision: 0,
        threads: [expect.objectContaining({ id, anchor: null, quote: 'inline comment' })],
      }),
    );
  });
});
