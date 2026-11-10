import { hasEditor } from './registry';

describe('hasEditor', () => {
  it('offers the image editor only for types it can write back', () => {
    for (const type of ['image/png', 'image/jpeg', 'image/jpg', 'IMAGE/WEBP']) expect(hasEditor('image', type)).toBe(true);
    for (const type of ['image/gif', 'image/svg+xml', 'image/heic', '', null]) expect(hasEditor('image', type)).toBe(false);
  });

  it('offers editors for every type of a kind without a type list', () => {
    expect(hasEditor('text', 'text/plain')).toBe(true);
    expect(hasEditor('pdf', 'application/pdf')).toBe(true);
  });
});
