import { filenameFromContentDisposition, saveBlobAsFile } from './download.helpers';

describe('filenameFromContentDisposition', () => {
  it('reads a quoted filename', () => {
    expect(filenameFromContentDisposition('attachment; filename="Vera Calder.json"', 'x.json')).toBe('Vera Calder.json');
  });

  it('reads an unquoted filename', () => {
    expect(filenameFromContentDisposition('attachment; filename=card.json', 'x.json')).toBe('card.json');
  });

  it('prefers and decodes the RFC 5987 form used for non-ASCII names', () => {
    const header = `attachment; filename="fallback.json"; filename*=utf-8''V%C3%A9ra.json`;
    expect(filenameFromContentDisposition(header, 'x.json')).toBe('Véra.json');
  });

  it('falls back to the plain form when percent-encoding is malformed', () => {
    const header = `attachment; filename*=utf-8''%E0%A4%A; filename="ok.json"`;
    expect(filenameFromContentDisposition(header, 'x.json')).toBe('ok.json');
  });

  it('returns the fallback when the header is missing or has no filename', () => {
    expect(filenameFromContentDisposition(null, 'x.json')).toBe('x.json');
    expect(filenameFromContentDisposition(undefined, 'x.json')).toBe('x.json');
    expect(filenameFromContentDisposition('attachment', 'x.json')).toBe('x.json');
  });
});

describe('saveBlobAsFile', () => {
  afterEach(() => vi.restoreAllMocks());

  it('clicks a temporary download link and cleans up after itself', () => {
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:test');
    const revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined);
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      expect(this.download).toBe('card.json');
      expect(this.href).toBe('blob:test');
      expect(document.body.contains(this)).toBe(true);
    });

    saveBlobAsFile(new Blob(['{}']), 'card.json');

    expect(click).toHaveBeenCalledTimes(1);
    expect(revoke).toHaveBeenCalledWith('blob:test');
    expect(document.querySelector('a[download]')).toBeNull();
  });
});
