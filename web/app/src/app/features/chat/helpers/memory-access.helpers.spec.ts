import {
  DEFAULT_MEMORY_ACCESS,
  isMemoryRestricted,
  MEMORY_ACCESS_OPTIONS,
  memoryAccessLabel,
  memoryAccessOf,
  RESTRICTED_WITHHELD_COPY,
  restrictedTooltip,
} from './memory-access.helpers';

describe('memory-access.helpers', () => {
  it('orders options from most to least access with the plain labels', () => {
    expect(MEMORY_ACCESS_OPTIONS.map(o => [o.value, o.label])).toEqual([
      ['sensitive', 'Everything'],
      ['personal', 'Not sensitive'],
      ['public', 'Public memories only'],
    ]);
  });

  it('treats a missing limit as unrestricted', () => {
    expect(DEFAULT_MEMORY_ACCESS).toBe('sensitive');
    expect(memoryAccessOf(null)).toBe('sensitive');
    expect(memoryAccessOf({})).toBe('sensitive');
    expect(isMemoryRestricted(undefined)).toBe(false);
  });

  it('reads the limit from the chat', () => {
    expect(memoryAccessOf({ memory_sensitivity_limit: 'public' })).toBe('public');
  });

  it('is restricted for every limit below sensitive', () => {
    expect(isMemoryRestricted('sensitive')).toBe(false);
    expect(isMemoryRestricted('personal')).toBe(true);
    expect(isMemoryRestricted('public')).toBe(true);
  });

  it('labels a limit with its option label', () => {
    expect(memoryAccessLabel('personal')).toBe('Not sensitive');
    expect(memoryAccessLabel(undefined)).toBe('Everything');
  });

  it('spells out what a restricted thread loses', () => {
    for (const part of ['scratchpad', 'other conversations', 'file and job lists', 'notebook']) {
      expect(RESTRICTED_WITHHELD_COPY).toContain(part);
    }
  });

  it('describes the limit and the withheld context in the header tooltip', () => {
    expect(restrictedTooltip('public')).toContain('only Public memories');
    expect(restrictedTooltip('personal')).toContain('no Sensitive memories');
    expect(restrictedTooltip('personal')).toContain(RESTRICTED_WITHHELD_COPY);
  });
});
