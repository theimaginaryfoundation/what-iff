/** A run of an entity's text: plain text, or a [[Name]] link to another entity. */
export interface EntitySegment {
  text: string;
  link: boolean;
}

const ENTITY_LINK = /\[\[([^[\]\n]{1,200})\]\]/g;

/** Splits entity text into plain runs and [[Name]] links, so links can be followed in the UI. */
export function entitySegments(body: string): EntitySegment[] {
  const out: EntitySegment[] = [];
  let last = 0;
  for (const match of body.matchAll(ENTITY_LINK)) {
    const start = match.index ?? 0;
    if (start > last) out.push({ text: body.slice(last, start), link: false });
    out.push({ text: match[1].trim().replace(/\s+/g, ' '), link: true });
    last = start + match[0].length;
  }
  if (last < body.length) out.push({ text: body.slice(last), link: false });
  return out;
}

/** The server's entity key normalization (models.NormalizeEntityKey): lowercase, single spaces. */
export function normalizeEntityName(name: string): string {
  return name.trim().replace(/\s+/g, ' ').toLowerCase();
}
