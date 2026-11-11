import { deflateSync } from 'node:zlib';

import { test, expect } from '../../../fixtures';
import type { ApiClient } from '../../../sdk/client';

/**
 * The image editor (Filerobot) in the file workspace.
 *
 * Its unit tests run against a fake Filerobot, so they cannot catch the editor failing in a real
 * browser. Two such failures shipped: the tab ids read from the wrong place, and the editor's React
 * tree rendering with two copies of React. This opens a real image, edits it and saves it, and
 * fails on any page error.
 */

test('an image opens in the image editor, and an edit saves', async ({ isMobile, userWithPersonality, apiClient, seed }) => {
  /* eslint-disable-next-line playwright/no-skipped-test -- a conditional, per-project skip; the rule can't tell it from a blanket skip. */
  test.skip(isMobile, 'Phones are view only: the workspace offers no editors there (ADR 0x043 §1).');
  const { page } = userWithPersonality;
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));

  // Opaque, so the upload stores it as JPEG (imageutil.NormalizeForUpload). Smaller than the editor's
  // canvas, so Filerobot scales it up to fit: the case where opening on the crop tool saved a
  // 400x300 image as 321x240.
  const width = 400;
  const height = 300;
  const chat = await seed.thread(undefined, { personalityId: userWithPersonality.personality.id });
  const { data: file, response } = await apiClient.POST('/chat/{chatId}/file-attachment', {
    params: { path: { chatId: chat.id! } },
    body: {} as never,
    bodySerializer: () => {
      const form = new FormData();
      form.append('attachment', new Blob([new Uint8Array(twoColourPng(width, height))], { type: 'image/png' }), 'two-colour.png');
      return form;
    },
  });
  expect(response.status, 'upload').toBeLessThan(300);
  const id = file!.id!;
  const bytesBefore = await contentOf(apiClient, id);

  await page.goto(`/chat/${chat.id}?file=${id}`);
  await page.getByRole('button', { name: 'Edit', exact: true }).click();

  // Filerobot's own controls: there only once its React tree has rendered.
  const editor = page.getByLabel(`Image editor for ${file!.name}`);
  await expect(editor.getByText('Adjust', { exact: true }).first()).toBeVisible();
  await expect(editor.getByText(`${width} x ${height} px`)).toBeVisible();
  await expect(page.getByText('The image editor could not load.')).toHaveCount(0);

  // Flip X mirrors the image: left red / right blue becomes the other way round.
  const save = page.getByRole('button', { name: 'Save', exact: true });
  await expect(save).toBeDisabled();
  await editor.getByText('Flip X', { exact: true }).click();
  await expect(save).toBeEnabled();
  await save.click();

  await expect
    .poll(async () => (await contentOf(apiClient, id)).equals(bytesBefore), { message: 'the edited image was stored' })
    .toBe(false);
  await expect(save).toBeDisabled();
  await expect(page.getByRole('alert')).toHaveCount(0);
  const bytesAfter = await contentOf(apiClient, id);
  expect(jpegSize(bytesAfter), 'the whole image was saved, not a crop of it').toStrictEqual({ width, height });

  expect(errors).toStrictEqual([]);
});

/** The file's stored bytes: GET /file-attachment/{id} serves the content itself. */
async function contentOf(client: ApiClient, id: string): Promise<Buffer> {
  const { data, response } = await client.GET('/file-attachment/{id}', { params: { path: { id } }, parseAs: 'arrayBuffer' });
  expect(response.status, 'download').toBe(200);
  expect(data, 'download body').toBeInstanceOf(ArrayBuffer);
  return Buffer.from(data as unknown as ArrayBuffer);
}

/** An RGB PNG, red on the left half and blue on the right. */
function twoColourPng(width: number, height: number): Buffer {
  const row = Buffer.alloc(1 + width * 3);
  for (let x = 0; x < width; x++) row[1 + x * 3 + (x < width / 2 ? 0 : 2)] = 220;
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8; // bit depth
  header[9] = 2; // RGB
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    pngChunk('IHDR', header),
    pngChunk('IDAT', deflateSync(Buffer.concat(Array.from({ length: height }, () => row)))),
    pngChunk('IEND', Buffer.alloc(0)),
  ]);
}

function pngChunk(type: string, data: Buffer): Buffer {
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, 'ascii'), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([length, body, crc]);
}

function crc32(bytes: Buffer): number {
  let crc = 0xffffffff;
  for (const byte of bytes) {
    crc ^= byte;
    for (let k = 0; k < 8; k++) crc = crc & 1 ? 0xedb88320 ^ (crc >>> 1) : crc >>> 1;
  }
  return (crc ^ 0xffffffff) >>> 0;
}

/** A baseline or progressive JPEG's dimensions, from its start-of-frame marker. */
function jpegSize(jpeg: Buffer): { width: number; height: number } {
  if (jpeg[0] !== 0xff || jpeg[1] !== 0xd8) throw new Error('not a JPEG');
  let at = 2;
  while (at + 9 <= jpeg.length && jpeg[at] === 0xff) {
    const marker = jpeg[at + 1];
    const length = jpeg.readUInt16BE(at + 2);
    if (marker === 0xc0 || marker === 0xc2) return { height: jpeg.readUInt16BE(at + 5), width: jpeg.readUInt16BE(at + 7) };
    if (length < 2) break;
    at += 2 + length;
  }
  throw new Error('not a JPEG with a start-of-frame marker');
}
