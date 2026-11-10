import { versionFromETag } from './file-attachment.service';

describe('versionFromETag', () => {
  it('reads a strong or weak quoted version, and nothing else', () => {
    expect(versionFromETag('"3"')).toBe(3);
    expect(versionFromETag('W/"12"')).toBe(12);
    expect(versionFromETag('"abc"')).toBeNull();
    expect(versionFromETag(null)).toBeNull();
  });
});
