import { TestBed } from '@angular/core/testing';
import { of } from 'rxjs';
import { describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { FolderFiles } from './folder-files.service';

const file = (id: string, name: string, folder = '') => ({ id, name, folder }) as FileAttachment;

describe('FolderFiles', () => {
  it('lists a conversation’s files by workspace path, without the shadowed ones, and reuses the listing briefly', async () => {
    const listChatContextFiles = vi.fn(() =>
      of({
        files: [
          { file: file('new', 'app.css', 'site'), workspace_path: 'site/app.css', reasons: [] },
          { file: file('old', 'app.css', 'site'), workspace_path: 'site/app.css', reasons: [], shadowed_by: 'new' },
          { file: file('top', 'notes.md'), workspace_path: 'notes.md', reasons: [] },
        ],
      }),
    );
    TestBed.configureTestingModule({
      providers: [
        { provide: FileAttachmentService, useValue: { listChatContextFiles } },
        { provide: ImageGalleryService, useValue: {} },
      ],
    });
    const folders = TestBed.inject(FolderFiles);
    let now = 0;
    folders.now = () => now;
    const byPath = await folders.conversation('c1');
    expect([...byPath].map(([p, f]) => [p, f.id])).toEqual([
      ['site/app.css', 'new'],
      ['notes.md', 'top'],
    ]);
    await folders.conversation('c1');
    expect(listChatContextFiles).toHaveBeenCalledTimes(1);
    await folders.conversation('c1', true);
    expect(listChatContextFiles).toHaveBeenCalledTimes(2);
    expect(listChatContextFiles).toHaveBeenCalledWith('c1');
    now = 10_000;
    await folders.conversation('c1');
    expect(listChatContextFiles).toHaveBeenCalledTimes(3);
  });
});
