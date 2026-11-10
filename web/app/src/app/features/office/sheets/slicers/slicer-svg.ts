import { escapeXml } from '../../shared/ooxml';

/** A slicer drawn as Excel draws one, for its place on the grid: a caption and a list of item buttons. */
export function slicerSvgDataUrl(caption: string, items: readonly { label: string; selected: boolean }[], width: number, height: number): string {
  const rowHeight = 22;
  const head = 28;
  const shown = Math.max(0, Math.floor((height - head - 6) / (rowHeight + 3)));
  const rows = items.slice(0, shown).map((item, i) => {
    const y = head + i * (rowHeight + 3);
    return (
      `<rect x="6" y="${y}" width="${Math.max(0, width - 12)}" height="${rowHeight}" rx="2" fill="${item.selected ? '#4472C4' : '#FFFFFF'}" stroke="${item.selected ? '#2F5597' : '#BFBFBF'}"/>` +
      `<text x="12" y="${y + 15}" font-size="12" fill="${item.selected ? '#FFFFFF' : '#595959'}">${escapeXml(item.label)}</text>`
    );
  });
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" font-family="Arial, sans-serif">` +
    `<rect x="0.5" y="0.5" width="${width - 1}" height="${height - 1}" fill="#FFFFFF" stroke="#7F7F7F"/>` +
    `<text x="8" y="18" font-size="13" font-weight="700" fill="#262626">${escapeXml(caption)}</text>${rows.join('')}</svg>`;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}
