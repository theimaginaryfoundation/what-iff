import { OFFICE_FONTS_STYLESHEET, acquireOfficeFonts } from './office-fonts';

describe('acquireOfficeFonts', () => {
  const links = () => document.head.querySelectorAll(`link[href="${OFFICE_FONTS_STYLESHEET}"]`);

  it('links the stylesheet once and removes it when the last user releases it', () => {
    const first = acquireOfficeFonts(document);
    const second = acquireOfficeFonts(document);
    expect(links()).toHaveLength(1);

    first.release();
    first.release(); // Releasing twice counts once.
    expect(links()).toHaveLength(1);

    second.release();
    expect(links()).toHaveLength(0);
  });

  it('waits for the stylesheet where fonts can load, and does not hang if it fails', async () => {
    Object.defineProperty(document, 'fonts', { configurable: true, value: {} });
    const fonts = acquireOfficeFonts(document);
    (links()[0] as HTMLLinkElement).dispatchEvent(new Event('error'));
    await expect(fonts.ready).resolves.toBeUndefined();
    fonts.release();
    delete (document as unknown as Record<string, unknown>)['fonts'];
  });
});
