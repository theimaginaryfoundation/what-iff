import { signal } from '@angular/core';
import { describe, expect, it } from 'vitest';

import { CommentSaveResult, CommentSaver } from '../../file-workspace/editors/file-editor';
import { CommentAutosave, saveStatusText } from './comment-autosave';

describe('CommentAutosave', () => {
  const editor = { content: () => Promise.resolve(new Blob()), markSaved: () => undefined };

  function saver(results: CommentSaveResult[]) {
    const releases: (() => void)[] = [];
    const calls = { n: 0 };
    const s: CommentSaver = {
      canComment: signal(true),
      saveComments: () => {
        calls.n += 1;
        const result = results.shift() ?? 'saved';
        return new Promise(resolve => releases.push(() => resolve(result)));
      },
    };
    return { s, releases, calls };
  }

  it('saves one at a time, and writes changes made during a save with one more save', async () => {
    const { s, releases, calls } = saver([]);
    const autosave = new CommentAutosave(s, editor);
    autosave.request();
    autosave.request();
    autosave.request();
    expect(calls.n).toBe(1);
    expect(autosave.status()).toBe('saving');
    releases.shift()!();
    await vi.waitFor(() => expect(calls.n).toBe(2));
    releases.shift()!();
    await vi.waitFor(() => expect(autosave.status()).toBe('saved'));
    expect(calls.n).toBe(2);
  });

  it('stops after a save that did not land, and says why', async () => {
    const { s, releases, calls } = saver(['conflict']);
    const autosave = new CommentAutosave(s, editor);
    autosave.request();
    autosave.request();
    releases.shift()!();
    await vi.waitFor(() => expect(autosave.status()).toBe('conflict'));
    expect(calls.n).toBe(1);
    expect(saveStatusText('conflict')).toContain('reloaded');
  });

  it('is unavailable without a saver', () => {
    const autosave = new CommentAutosave(null, editor);
    autosave.request();
    expect(autosave.available()).toBe(false);
    expect(autosave.status()).toBe('idle');
  });
});
