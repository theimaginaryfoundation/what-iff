import JSZip from 'jszip';
import type { UploadPart } from '../sdk/client';

/**
 * Upload bodies for `POST /memory/import`.
 *
 * The endpoint decides what to do with an entry from its *filename*:
 * `chat.json` and `user.json` are read as memory JSONL, `personality-<uuid>.json`
 * is read as one personality's pinned memories, and anything else is skipped.
 * That routing is the whole reason these builders exist — most of what is
 * worth asserting is how the endpoint reacts to an archive that is a valid
 * ZIP but not the one it expected, and each variant below is a different way
 * for a user to arrive at that.
 */

/** Names an archive after its contents so a failure message says which variant it was. */
function part(name: string, buffer: Buffer): UploadPart {
  return { name, mimeType: 'application/zip', buffer };
}

/** Packs `entries` into a real ZIP. Deflate, like the server's own writer. */
async function zipOf(entries: Record<string, string>): Promise<Buffer> {
  const zip = new JSZip();
  for (const [name, content] of Object.entries(entries)) {
    zip.file(name, content);
  }
  return zip.generateAsync({ type: 'nodebuffer', compression: 'DEFLATE' });
}

/** A valid, empty ZIP — nothing to import and nothing to complain about. */
export async function emptyArchive(): Promise<UploadPart> {
  return part('empty.zip', await zipOf({}));
}

/**
 * A ZIP whose only entry the importer does not recognise. Distinct from an
 * empty one: here the user did upload something, and the endpoint has to
 * decide whether silently importing nothing counts as success.
 */
export async function unrecognisedEntryArchive(): Promise<UploadPart> {
  return part('notes.zip', await zipOf({ 'notes.txt': 'not a memory export\n' }));
}

/**
 * A *chat* export, uploaded to the memory importer.
 *
 * The plausible user error this endpoint has no defence against: both
 * features hand the user a ZIP, and this one's `chat.json` is read as memory
 * JSONL because the name matches, even though its contents are a single
 * thread's metadata.
 */
export async function chatExportShapedArchive(): Promise<UploadPart> {
  return part(
    'chat-export.zip',
    await zipOf({
      'chat.json': JSON.stringify({ id: '00000000-0000-4000-8000-000000000001', name: 'a thread', model_name: 'mock' }),
      'messages.jsonl': JSON.stringify({ origin: 'User', message: 'hello' }) + '\n',
    }),
  );
}

/**
 * An archive holding one otherwise-valid memory record under a filename that
 * looks like a personality file but does not carry a UUID.
 *
 * This is what renaming `user.json` produces, and the endpoint's filename
 * routing turns the rename into a parse error rather than an ignored entry.
 */
export async function misnamedPersonalityArchive(recordLine: string): Promise<UploadPart> {
  return part('renamed.zip', await zipOf({ 'personality-backup.json': recordLine.endsWith('\n') ? recordLine : `${recordLine}\n` }));
}

/** Bytes that are not a ZIP at all, under a .zip name — a mis-picked file in the dialog. */
export function nonZipArchive(): UploadPart {
  return part('not-really.zip', Buffer.from('this is plainly not a zip archive\n'));
}
