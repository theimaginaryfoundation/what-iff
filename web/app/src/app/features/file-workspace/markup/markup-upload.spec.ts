import { of } from 'rxjs';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { ChatUpload, MARKUP_FOLDER, uploadMarkup } from './markup-upload';

function attachment(name: string): FileAttachment {
  return { id: `id-${name}`, user_id: 'u', name, file_type: 'image/png', created_at: '' };
}

describe('uploadMarkup', () => {
  let uploads: { chatId: string; name: string; type: string; folder?: string; description?: string }[];
  let upload: ChatUpload;

  beforeEach(() => {
    uploads = [];
    upload = (chatId, file, folder, description) => {
      uploads.push({ chatId, name: file.name, type: file.type, folder, description });
      return of(attachment(file.name));
    };
  });

  it('attaches the original as it is, then a described mask and markup copy', async () => {
    const source = attachment('beach.jpg');
    const out = await uploadMarkup(upload, 'chat-1', source, { mask: new Blob(['m']), markup: new Blob(['k']) });

    expect(out.map(a => a.name)).toEqual(['beach.jpg', 'beach-mask.png', 'beach-markup.png']);
    expect(out[0]).toBe(source);
    expect(uploads.map(u => [u.chatId, u.name, u.type, u.folder])).toEqual([
      ['chat-1', 'beach-mask.png', 'image/png', MARKUP_FOLDER],
      ['chat-1', 'beach-markup.png', 'image/png', MARKUP_FOLDER],
    ]);
    expect(uploads[0].description).toContain('Mask for beach.jpg: white marks the area');
    expect(uploads[1].description).toContain('instructions, not content');
  });

  it('uploads only what was drawn', async () => {
    const out = await uploadMarkup(upload, 'chat-1', attachment('a.png'), { mask: null, markup: new Blob(['k']) });
    expect(out.map(a => a.name)).toEqual(['a.png', 'a-markup.png']);
  });

  it('turns a sketch into one described file named for when it was drawn', async () => {
    const out = await uploadMarkup(upload, 'chat-1', null, { mask: null, markup: new Blob(['s']) }, new Date(2026, 10, 11, 9, 5));
    expect(out.map(a => a.name)).toEqual(['sketch-2026-11-11-0905.png']);
    expect(uploads[0].description).toContain('rough sketch');
  });
});
