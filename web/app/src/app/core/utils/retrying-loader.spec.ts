import { describe, expect, it, vi } from 'vitest';

import { retryingLoader } from './retrying-loader';

describe('retryingLoader', () => {
  it('loads once and shares the result', async () => {
    const load = vi.fn(async () => 'module');
    const get = retryingLoader(load);
    expect(await get()).toBe('module');
    expect(await get()).toBe('module');
    expect(load).toHaveBeenCalledTimes(1);
  });

  it('tries again after a failed load', async () => {
    const load = vi.fn().mockRejectedValueOnce(new Error('Failed to fetch dynamically imported module')).mockResolvedValue('module');
    const get = retryingLoader(load);
    await expect(get()).rejects.toThrow('Failed to fetch');
    expect(await get()).toBe('module');
    expect(load).toHaveBeenCalledTimes(2);
  });
});
