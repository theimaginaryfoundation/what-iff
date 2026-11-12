import { provideZonelessChangeDetection, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { CommentPeopleService } from '../../office/comments/comment-people.service';
import { COMMENT_SAVER } from '../editors/file-editor';
import { fakeTextComments } from '../text-comments/fake-text-comments.spec-helper';
import { TextCommentsService } from '../text-comments/text-comments.service';
import { ImageViewerComponent } from './image-viewer.component';

const file = { id: 'f-1', name: 'photo.png', file_type: 'image/png' } as FileAttachment;

describe('ImageViewerComponent', () => {
  let urls: number;
  let textComments: ReturnType<typeof fakeTextComments>;
  const canComment = signal(true);

  beforeEach(() => {
    urls = 0;
    globalThis.URL.createObjectURL ??= () => '';
    globalThis.URL.revokeObjectURL ??= () => undefined;
    vi.spyOn(URL, 'createObjectURL').mockImplementation(() => `blob:image-${++urls}`);
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined);
    textComments = fakeTextComments();
    canComment.set(true);
    TestBed.configureTestingModule({
      providers: [
        provideZonelessChangeDetection(),
        { provide: CommentPeopleService, useValue: { me: signal({ id: 'user:u-1', name: 'Pat Lee' }) } },
        { provide: TextCommentsService, useValue: textComments },
        {
          provide: COMMENT_SAVER,
          useValue: { canComment, saveComments: () => Promise.resolve('saved'), showCurrentContent: () => undefined },
        },
      ],
    });
  });

  it('shows the bytes through an object URL, and revokes each one when it is replaced or destroyed', async () => {
    const fixture = TestBed.createComponent(ImageViewerComponent);
    fixture.componentRef.setInput('file', file);
    fixture.componentRef.setInput('blob', new Blob(['a']));
    fixture.detectChanges();
    await fixture.whenStable();
    const img = (fixture.nativeElement as HTMLElement).querySelector('img')!;
    expect(img.getAttribute('src')).toBe('blob:image-1');
    expect(img.alt).toBe('photo.png');

    fixture.componentRef.setInput('blob', new Blob(['b']));
    fixture.detectChanges();
    await fixture.whenStable();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:image-1');
    expect(fixture.componentInstance.objectUrl()).toBe('blob:image-2');

    fixture.destroy();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:image-2');
  });

  it('comments on the whole image, and saves the comment as it is made', async () => {
    const fixture = TestBed.createComponent(ImageViewerComponent);
    fixture.componentRef.setInput('file', file);
    fixture.componentRef.setInput('blob', new Blob(['a']));
    fixture.detectChanges();
    await vi.waitFor(() => expect(fixture.componentInstance.hasComments()).toBe(true));
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelector('app-comments-panel')).toBeNull();

    [...el.querySelectorAll<HTMLButtonElement>('.image-viewer__comment-tool')].find(b => b.textContent?.trim() === 'Comment')!.click();
    fixture.detectChanges();
    expect(el.querySelector('.thread--draft')).not.toBeNull();
    expect(el.querySelector('.thread--draft .thread__quote')).toBeNull();
    fixture.componentInstance.comments.create({ text: 'Brighter sky?', mentions: [] });
    await vi.waitFor(() => expect(textComments.state.puts).toHaveLength(1));
    expect(textComments.state.puts[0]).toEqual(
      expect.objectContaining({ fileId: 'f-1', contentVersion: 1, threads: [expect.objectContaining({ anchor: null, quote: '' })] }),
    );
    fixture.detectChanges();
    expect(el.querySelector('.thread .thread__quote')?.textContent).toBe('The whole file');
  });

  it('shows comments read-only where the user may not change the image', async () => {
    canComment.set(false);
    textComments.state.stored = {
      threads: [
        {
          id: 't1',
          anchor: null,
          quote: '',
          comments: [{ id: 't1', authorId: 'user:u-2', authorName: 'Sam', createdAt: '2026-11-01T00:00:00Z', text: 'Nice' }],
        },
      ],
      contentVersion: 1,
      revision: 1,
    };
    const fixture = TestBed.createComponent(ImageViewerComponent);
    fixture.componentRef.setInput('file', file);
    fixture.componentRef.setInput('blob', new Blob(['a']));
    fixture.detectChanges();
    await vi.waitFor(() => expect(fixture.componentInstance.hasComments()).toBe(true));
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    const tools = [...el.querySelectorAll('.image-viewer__comment-tool')].map(b => b.textContent?.trim());
    expect(tools).toEqual(['Comments 1']);
    expect(el.querySelector('app-comments-panel .comment__text')?.textContent).toBe('Nice');
  });
});
