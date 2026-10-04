import {
  DEFAULT_MEMORY_SENSITIVITY,
  MEMORY_SENSITIVITY_OPTIONS,
  normalizeSensitivity,
  sensitivityDescription,
  sensitivityLabel,
} from './memory-sensitivity.helpers';

describe('memory-sensitivity.helpers', () => {
  it('lists levels from least to most sensitive', () => {
    expect(MEMORY_SENSITIVITY_OPTIONS.map(option => option.value)).toEqual(['public', 'personal', 'sensitive']);
  });

  it('defaults to personal, matching existing memories', () => {
    expect(DEFAULT_MEMORY_SENSITIVITY).toBe('personal');
  });

  it('normalizes known values and rejects everything else', () => {
    expect(normalizeSensitivity('public')).toBe('public');
    expect(normalizeSensitivity('sensitive')).toBe('sensitive');
    expect(normalizeSensitivity('secret')).toBeNull();
    expect(normalizeSensitivity('')).toBeNull();
    expect(normalizeSensitivity(undefined)).toBeNull();
    expect(normalizeSensitivity(3)).toBeNull();
  });

  it('labels each level in sentence case and falls back to Personal', () => {
    expect(sensitivityLabel('public')).toBe('Public');
    expect(sensitivityLabel('personal')).toBe('Personal');
    expect(sensitivityLabel('sensitive')).toBe('Sensitive');
    expect(sensitivityLabel(undefined)).toBe('Personal');
  });

  it('gives every level a distinct tooltip', () => {
    const hints = MEMORY_SENSITIVITY_OPTIONS.map(option => sensitivityDescription(option.value));
    expect(new Set(hints).size).toBe(3);
    expect(hints.every(hint => hint.length > 0)).toBe(true);
  });
});
