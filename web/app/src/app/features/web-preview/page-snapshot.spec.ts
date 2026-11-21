import { afterEach, describe, expect, it, vi } from 'vitest';

import { PageSnapshot, SNAPSHOT_TIMEOUT_MS } from './page-snapshot';

describe('PageSnapshot', () => {
  afterEach(() => vi.useRealTimers());

  it('asks the page for its picture and gives the PNG it answers with', async () => {
    const send = vi.fn();
    const snapshot = new PageSnapshot(send);
    const png = snapshot.draw();
    expect(send).toHaveBeenCalledWith({ type: 'snapshot' });
    snapshot.handle({ type: 'snapshot', png: 'data:image/png;base64,AAA' });
    await expect(png).resolves.toBe('data:image/png;base64,AAA');
  });

  it('gives null when the page could not draw it, answers with something else, or is superseded', async () => {
    const snapshot = new PageSnapshot(vi.fn());
    const failed = snapshot.draw();
    snapshot.handle({ type: 'snapshot-failed', message: 'tainted' });
    await expect(failed).resolves.toBeNull();
    const odd = snapshot.draw();
    snapshot.handle({ type: 'snapshot', png: 'javascript:alert(1)' });
    await expect(odd).resolves.toBeNull();
    const first = snapshot.draw();
    const second = snapshot.draw();
    await expect(first).resolves.toBeNull();
    snapshot.handle({ type: 'snapshot', png: 'data:image/png;base64,B' });
    await expect(second).resolves.toBe('data:image/png;base64,B');
  });

  it('gives null when the page does not answer in time', async () => {
    vi.useFakeTimers();
    const png = new PageSnapshot(vi.fn()).draw();
    vi.advanceTimersByTime(SNAPSHOT_TIMEOUT_MS);
    await expect(png).resolves.toBeNull();
  });
});
