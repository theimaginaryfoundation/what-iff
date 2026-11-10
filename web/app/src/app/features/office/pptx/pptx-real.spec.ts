/**
 * A developer harness, not a test of the suite: when OFFICE_SAMPLES_DIR names a folder with a
 * sample.pptx, it reads the deck, edits the first text on slide 1, saves by patching the
 * original, writes `pptx-roundtrip.pptx` and `pptx-patch-report.json` beside it, and checks
 * the package is consistent (every part well-formed, every relationship target present, every
 * part typed). Skipped otherwise.
 */
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { parse } from 'pptxtojson';
import { describe, expect, it } from 'vitest';

import { updateElement } from '../slides/deck-commands';
import { OoxmlPackage, parseXml, relsPathFor, resolveTarget } from '../shared/ooxml';
import { ShapeElement, plainText } from './deck-model';
import { deckToPptx } from './pptx-export';
import { pptxToDeck } from './pptx-import';

const dir = process.env['OFFICE_SAMPLES_DIR'];
const have = (name: string) => !!dir && existsSync(join(dir, name));
const bytes = (name: string) => new Blob([readFileSync(join(dir!, name))]).arrayBuffer();

async function partsOf(data: ArrayBuffer | Uint8Array): Promise<Map<string, Uint8Array>> {
  const pkg = await OoxmlPackage.open(data);
  const out = new Map<string, Uint8Array>();
  for (const path of pkg.paths()) out.set(path, (await pkg.bytes(path))!);
  return out;
}

function same(a: Uint8Array | undefined, b: Uint8Array | undefined): boolean {
  if (!a || !b || a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

/** Every XML part parses, every relationship points at a part, every part has a content type. */
async function packageProblems(data: Uint8Array): Promise<string[]> {
  const pkg = await OoxmlPackage.open(data);
  const problems: string[] = [];
  const types = await pkg.xml('[Content_Types].xml');
  const defaults = new Set<string>();
  const overrides = new Set<string>();
  for (const el of Array.from(types?.getElementsByTagNameNS('http://schemas.openxmlformats.org/package/2006/content-types', 'Default') ?? []))
    defaults.add((el.getAttribute('Extension') ?? '').toLowerCase());
  for (const el of Array.from(types?.getElementsByTagNameNS('http://schemas.openxmlformats.org/package/2006/content-types', 'Override') ?? []))
    overrides.add(el.getAttribute('PartName') ?? '');
  for (const path of pkg.paths()) {
    if (path === '[Content_Types].xml') continue;
    if (/\.(xml|rels)$/.test(path)) {
      try {
        parseXml((await pkg.text(path))!);
      } catch (error) {
        problems.push(`${path}: ${(error as Error).message}`);
      }
    }
    const ext = path.slice(path.lastIndexOf('.') + 1).toLowerCase();
    if (!overrides.has(`/${path}`) && !defaults.has(ext)) problems.push(`${path}: no content type`);
    if (path.endsWith('.rels')) {
      const owner = path.replace('_rels/', '').replace(/\.rels$/, '');
      for (const rel of (await pkg.rels(owner)).values()) {
        if (rel.external) continue;
        const target = resolveTarget(owner, rel.target);
        if (!pkg.has(target)) problems.push(`${path}: ${rel.id} -> ${target} missing`);
      }
    }
  }
  for (const part of overrides) if (!pkg.has(part.replace(/^\//, ''))) problems.push(`content types: ${part} missing`);
  return problems;
}

describe.skipIf(!dir)('real pptx file', () => {
  it.skipIf(!have('sample.pptx'))('patches the original on save', async () => {
    const original = new Uint8Array(await bytes('sample.pptx'));
    const { deck, report } = await pptxToDeck(original, { id: 'deck', name: 'sample.pptx' }, parse);
    const slide = deck.slides[0];
    const first = slide.elements.find((e): e is ShapeElement => e.kind === 'shape' && !!e.text && plainText(e.text).trim() !== '')!;
    const paragraphs = first.text!.paragraphs.map((p, i) =>
      i === 0 ? { ...p, runs: [{ ...p.runs[0], text: `Edited: ${p.runs[0]?.text ?? ''}` }, ...p.runs.slice(1)] } : p,
    );
    const edited = updateElement(deck, slide.id, first.id, { text: { ...first.text!, paragraphs }, left: first.left + 10 });
    const saved = await deckToPptx(edited, { original, imported: deck });
    const before = await partsOf(original);
    const after = await partsOf(saved.bytes);
    const changed = [...after.keys()].filter(p => !same(before.get(p), after.get(p)));
    const missing = [...before.keys()].filter(p => !after.has(p));
    const problems = await packageProblems(saved.bytes);
    const back = await pptxToDeck(saved.bytes, { id: 'deck', name: 'sample.pptx' }, parse);
    const backFirst = back.deck.slides[0].elements.find(e => e.sourceId === first.sourceId) as ShapeElement | undefined;
    writeFileSync(join(dir!, 'pptx-roundtrip.pptx'), saved.bytes);
    writeFileSync(
      join(dir!, 'pptx-patch-report.json'),
      JSON.stringify(
        {
          importReport: report,
          saveReport: saved.report,
          slides: deck.slides.map(s => s.sourcePath),
          editedShape: { sourceId: first.sourceId, name: first.name, was: plainText(first.text), now: backFirst ? plainText(backFirst.text) : null },
          changed,
          missing,
          problems,
          slideCountBack: back.deck.slides.length,
          relsOfSlide1: (await (await OoxmlPackage.open(saved.bytes)).text(relsPathFor(slide.sourcePath!)))?.slice(0, 600),
        },
        null,
        1,
      ),
    );
    expect(problems).toEqual([]);
    expect(missing).toEqual([]);
    expect(changed).toEqual([slide.sourcePath]);
    expect(backFirst && plainText(backFirst.text)).toContain('Edited: ');
    expect(back.deck.slides.length).toBe(deck.slides.length);
  });
});
