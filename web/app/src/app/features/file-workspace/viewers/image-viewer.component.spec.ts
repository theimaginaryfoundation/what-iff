import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { ImageViewerComponent } from './image-viewer.component';

const file = { id: 'f-1', name: 'photo.png', file_type: 'image/png' } as FileAttachment;

describe('ImageViewerComponent', () => {
  let urls: number;

  beforeEach(() => {
    urls = 0;
    globalThis.URL.createObjectURL ??= () => '';
    globalThis.URL.revokeObjectURL ??= () => undefined;
    vi.spyOn(URL, 'createObjectURL').mockImplementation(() => `blob:image-${++urls}`);
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined);
    TestBed.configureTestingModule({ providers: [provideZonelessChangeDetection()] });
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
});
