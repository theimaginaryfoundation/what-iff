export interface InlineCodePart {
  readonly kind: 'text' | 'code';
  readonly value: string;
}

export interface ExtractedImage {
  readonly alt: string;
  readonly src: string;
}

const MARKDOWN_IMAGE_RE = /!\[([^\]]*)\]\(([^)]+)\)/g;
const IMAGE_URL_RE = /(https?:\/\/[^\s)]+?\.(?:png|jpe?g|gif|webp|svg)(?:\?[^\s)]*)?)/gi;

export function extractImages(content: string): ExtractedImage[] {
  const images: ExtractedImage[] = [];
  const markdownMatches = content.matchAll(MARKDOWN_IMAGE_RE);
  const markdownSources = new Set<string>();

  for (const match of markdownMatches) {
    const src = match[2]?.trim();
    if (!src) continue;
    markdownSources.add(src);
    images.push({ alt: match[1] ?? '', src });
  }

  for (const match of content.matchAll(IMAGE_URL_RE)) {
    const src = match[1]?.trim();
    if (!src || markdownSources.has(src)) continue;
    images.push({ alt: '', src });
  }

  return images;
}

export function splitInlineCode(text: string): InlineCodePart[] {
  if (!text) return [];
  const parts: InlineCodePart[] = [];
  const segments = text.split(/(`[^`]+`)/g);

  for (const segment of segments) {
    if (!segment) continue;
    if (segment.startsWith('`') && segment.endsWith('`') && segment.length > 1) {
      parts.push({ kind: 'code', value: segment.slice(1, -1) });
      continue;
    }
    parts.push({ kind: 'text', value: segment });
  }

  return parts;
}

export function detectModelChange(previous?: string | null, current?: string | null): boolean {
  const prev = previous?.trim();
  const next = current?.trim();
  return Boolean(prev && next && prev !== next);
}
