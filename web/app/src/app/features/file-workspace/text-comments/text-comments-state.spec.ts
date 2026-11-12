import { signal } from '@angular/core';
import { throwError } from 'rxjs';
import { describe, expect, it, vi } from 'vitest';

import { fakeTextComments } from './fake-text-comments.spec-helper';
import { TextCommentSurface, TextCommentsState, mergeThreads } from './text-comments-state';
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

  it('merges with comments written elsewhere (an agent reply) and writes again', async () => {
    const api = fakeTextComments();
    const state = new TextCommentsState(api as unknown as TextCommentsService, me, () => undefined);
    const { s } = surface();
    state.attach(s);
    await state.load('f-1');
    state.start({ start: 11, end: 25 });
    state.create({ text: 'Rename this?', mentions: [] });
    await state.save(1);
    const id = state.threads()[0].id;

    // The agent replies; the user resolves the thread without having seen the reply.
    const agentReply = { id: 'r-agent', authorId: 'personality:p', authorName: 'Vix', createdAt: '2026-11-12T10:00:00Z', text: 'Done.' };
    api.state.stored = {
      ...api.state.stored,
      revision: api.state.stored.revision + 1,
      threads: api.state.stored.threads.map(t => ({ ...t, comments: [...t.comments, agentReply] })),
    };
    state.resolve({ threadId: id, resolved: true });
    await state.save(1);

    expect(api.state.puts.map(p => p.revision)).toEqual([0, 1, 2]);
    const written = api.state.stored.threads[0];
    expect(written.resolved).toBe(true);
    expect(written.comments.map(c => c.id)).toEqual([id, 'r-agent']);
    expect(state.threads()[0].comments.map(c => c.id)).toEqual([id, 'r-agent']);
    expect(state.unsaved()).toBe(false);
    expect(state.status()).toBe('saved');
  });

  it('keeps comments written elsewhere across repeated conflicts, and after giving up', async () => {
    const api = fakeTextComments();
    const state = new TextCommentsState(api as unknown as TextCommentsService, me, () => undefined);
    state.attach(surface().s);
    await state.load('f-1');
    state.startOnFile();
    state.create({ text: 'Rename this?', mentions: [] });
    await state.save(1);
    const id = state.threads()[0].id;
    const reply = (rid: string) => ({
      id: rid,
      authorId: 'personality:p',
      authorName: 'Vix',
      createdAt: '2026-11-12T10:00:00Z',
      text: rid,
    });
    // The agent writes a reply before each of this side's writes lands.
    const agentWrites = (rid: string) => {
      api.state.stored = {
        ...api.state.stored,
        revision: api.state.stored.revision + 1,
        threads: api.state.stored.threads.map(t => ({ ...t, comments: [...t.comments, reply(rid)] })),
      };
    };
    const put = api.put;
    const races = ['r1', 'r2', 'r3'];
    api.put = (...args: Parameters<typeof put>) => {
      const next = races.shift();
      if (next) agentWrites(next);
      return put(...args);
    };

    state.resolve({ threadId: id, resolved: true });
    await expect(state.save(1)).rejects.toEqual({ status: 409 }); // three conflicts in a row
    expect(state.status()).toBe('conflict');
    // What is shown has their replies so far, so the next save does not read them as removed.
    expect(state.threads()[0].comments.map(c => c.id)).toEqual([id, 'r1', 'r2']);

    await state.save(1);
    expect(api.state.stored.threads[0].comments.map(c => c.id)).toEqual([id, 'r1', 'r2', 'r3']);
    expect(api.state.stored.threads[0].resolved).toBe(true);
  });

  it('reads the comments again and asks for the file when its content changed', async () => {
    const api = fakeTextComments();
    const contentChanged = vi.fn();
    const state = new TextCommentsState(api as unknown as TextCommentsService, me, () => undefined, contentChanged);
    state.attach(surface().s);
    await state.load('f-1');
    api.state.stored = { ...api.state.stored, contentVersion: 2, revision: 4 };
    api.state.failNext = 412;
    state.startOnFile();
    state.create({ text: 'Hm', mentions: [] });
    await expect(state.save(1)).rejects.toEqual({ status: 412 });
    expect(contentChanged).toHaveBeenCalledTimes(1);
    expect(state.status()).toBe('conflict');
    // The next save is over the revision the server has now.
    await state.save(2);
    expect(api.state.puts.at(-1)?.revision).toBe(4);
  });

  it('is not available for a file the server keeps no comments for', async () => {
    const api = { get: () => throwError(() => ({ status: 415 })) };
    const state = new TextCommentsState(api as unknown as TextCommentsService, me, () => undefined);
    await expect(state.load('f-1')).rejects.toBeTruthy();
    expect(state.available()).toBe(false);
  });

  describe('mergeThreads', () => {
    const c = (id: string, text = id) => ({ id, authorId: 'user:u-1', authorName: 'Pat', createdAt: '2026-11-12T10:00:00Z', text });
    const t = (id: string, comments: ReturnType<typeof c>[], resolved = false) => ({
      id,
      anchor: null,
      quote: '',
      comments,
      ...(resolved ? { resolved: true } : {}),
    });

    it('keeps additions from both sides and removals from either', () => {
      const base = [t('a', [c('a')]), t('b', [c('b')])];
      const mine = [t('a', [c('a'), c('a2')]), t('n', [c('n')])]; // replied on a, removed b, added n
      const theirs = [t('a', [c('a'), c('a3')]), t('b', [c('b')]), t('x', [c('x')])]; // replied on a, added x
      expect(mergeThreads(base, mine, theirs).map(th => [th.id, th.comments.map(cm => cm.id)])).toEqual([
        ['a', ['a', 'a3', 'a2']],
        ['x', ['x']],
        ['n', ['n']],
      ]);
    });

    it("prefers this side's edits and resolved state, and drops a thread with no comments left", () => {
      const base = [t('a', [c('a', 'old')]), t('b', [c('b')])];
      const mine = [t('a', [c('a', 'new')], true), t('b', [c('b')])];
      const theirs = [t('a', [c('a', 'old')]), t('b', [])];
      const merged = mergeThreads(base, mine, theirs);
      expect(merged).toEqual([expect.objectContaining({ id: 'a', resolved: true, comments: [expect.objectContaining({ text: 'new' })] })]);
    });
  });
});
