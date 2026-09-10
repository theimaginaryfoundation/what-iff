import { readFile } from 'node:fs/promises';
import type { Download } from '@playwright/test';
import JSZip from 'jszip';

/**
 * Reading ZIP responses in the e2e suite.
 *
 * Both export endpoints stream their archive rather than building it in
 * memory, so "did a complete archive arrive" is a real question — a run that
 * fails midway still sends a 200 and a plausible prefix. Every helper here
 * therefore decompresses, rather than only listing the central directory:
 * a truncated stream survives a name listing and fails on inflate.
 */

export interface ZipEntry {
  name: string;
  text: string;
}

/**
 * Parses `bytes` as a ZIP and returns every entry with its contents, in the
 * archive's own order.
 *
 * Directory entries are dropped: the two archives this suite reads are flat,
 * and a caller asserting on exact entry names should not have to filter out
 * something no producer here emits.
 */
export async function readZipEntries(bytes: ArrayBuffer): Promise<ZipEntry[]> {
  const zip = await JSZip.loadAsync(bytes);
  const entries: ZipEntry[] = [];
  for (const file of Object.values(zip.files)) {
    if (file.dir) {
      continue;
    }
    entries.push({ name: file.name, text: await file.async('string') });
  }
  return entries;
}

/** The entry names in an archive, in order. */
export async function readZipEntryNames(bytes: ArrayBuffer): Promise<string[]> {
  return (await readZipEntries(bytes)).map(entry => entry.name);
}

/**
 * Splits a JSONL entry into parsed records.
 *
 * Trailing newlines are tolerated but blank lines *between* records are not:
 * an empty line means the producer emitted a record it could not serialise,
 * which is exactly the kind of gap a test reading `lines.length` would
 * otherwise miss.
 */
export function parseJsonl<T = unknown>(text: string): T[] {
  const lines = text.replace(/\n$/, '').split('\n');
  return lines.map((line, i) => {
    if (line.trim() === '') {
      throw new Error(`JSONL line ${i + 1} is blank`);
    }
    return JSON.parse(line) as T;
  });
}

/**
 * Reads a ZIP the browser actually downloaded.
 *
 * Going through `download.path()` rather than re-fetching the URL is the
 * point: it is the only way to assert on the bytes that reached the user,
 * after the app's own blob handling. The chat export revokes its object URL
 * as soon as the click is dispatched, so a test that re-requested the export
 * would be exercising the server a second time instead of the download.
 */
export async function readDownloadedZip(download: Download): Promise<ZipEntry[]> {
  const path = await download.path();
  if (!path) {
    throw new Error(`download ${download.suggestedFilename()} produced no file on disk`);
  }
  const bytes = await readFile(path);
  return readZipEntries(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer);
}
