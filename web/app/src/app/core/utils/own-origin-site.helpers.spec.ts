import { afterEach, describe, expect, it, vi } from 'vitest';

import { siteOf, usableOtherSite, usableOwnOriginSite } from './own-origin-site.helpers';

describe('usableOwnOriginSite', () => {
  afterEach(() => vi.restoreAllMocks());

  it('takes an https site on another origin, or a local http one, and refuses the app’s own origin', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    expect(usableOwnOriginSite('https://notebooks.example.com/lab', 'https://app.example.com', 'x')).toBe(
      'https://notebooks.example.com/lab',
    );
    expect(usableOwnOriginSite('http://127.0.0.1:4200/x/', 'http://localhost:4200', 'x')).toBe('http://127.0.0.1:4200/x/');
    expect(usableOwnOriginSite('http://evil.test/', 'https://app.example.com', 'x')).toBeNull();
    expect(usableOwnOriginSite('https://app.example.com/x', 'https://app.example.com', 'x')).toBeNull();
    expect(usableOwnOriginSite('__PREVIEW_URL__', 'https://app.example.com', 'x')).toBeNull();
  });
});

describe('usableOtherSite', () => {
  afterEach(() => vi.restoreAllMocks());

  it('refuses a host under the app’s domain, whose pages could read the app’s cookies there', () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    expect(usableOtherSite('https://preview.example.com/index.html', 'https://app.example.com', 'The preview site')).toBeNull();
    expect(error).toHaveBeenCalledWith(expect.stringContaining('domain of its own'));
    expect(usableOtherSite('https://preview.example-usercontent.com/index.html', 'https://app.example.com', 'p')).toBe(
      'https://preview.example-usercontent.com/index.html',
    );
    // Local development: localhost and 127.0.0.1 are different sites.
    expect(usableOtherSite('http://127.0.0.1:4200/preview/index.html', 'http://localhost:4200', 'p')).toBe(
      'http://127.0.0.1:4200/preview/index.html',
    );
  });

  it('names a host’s site by its last two labels; an address or a single name is its own', () => {
    expect(siteOf('preview.dev.Example.com.')).toBe('example.com');
    expect(siteOf('127.0.0.1')).toBe('127.0.0.1');
    expect(siteOf('localhost')).toBe('localhost');
  });
});
