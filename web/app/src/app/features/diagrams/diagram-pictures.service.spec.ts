import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom, of } from 'rxjs';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { DiagramPictures, MAX_PICTURES } from './diagram-pictures.service';
import { DIAGRAMS_URL } from './diagram-site.config';

const PNG = 'data:image/png;base64,iVBORw0KGgo=';
const diagram = (id: string, version = 2) => ({ id, name: `${id}.drawio`, content_version: version }) as FileAttachment;

function setup(site: string | null = 'https://diagrams.example.test/index.html') {
  const files = {
    previewUrl: (id: string, size: string) => `/api/file-attachment/${id}/preview?size=${size}`,
    downloadFileAttachment: vi.fn(() => of(new Blob(['<mxfile/>']))),
    putPreview: vi.fn(() => of(undefined as void)),
  };
  TestBed.configureTestingModule({
    providers: [
      provideHttpClient(),
      provideHttpClientTesting(),
      { provide: DIAGRAMS_URL, useValue: site },
      { provide: FileAttachmentService, useValue: files },
    ],
  });
  const pictures = TestBed.inject(DiagramPictures);
  const render = vi.fn(async () => PNG);
  pictures.render = render;
  return { pictures, files, render, http: TestBed.inject(HttpTestingController) };
}

describe('DiagramPictures', () => {
  afterEach(() => TestBed.inject(HttpTestingController).verify());

  it('shows the stored preview when there is one', async () => {
    const { pictures, render, http } = setup();
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:preview');
    const picture = firstValueFrom(pictures.picture(diagram('a')));
    http.expectOne('/api/file-attachment/a/preview?size=full').flush(new Blob(['png']));
    await expect(picture).resolves.toBe('blob:preview');
    expect(render).not.toHaveBeenCalled();
  });

  it('draws one with none, stores it as the preview, and does each version once', async () => {
    const { pictures, files, render, http } = setup();
    const picture = firstValueFrom(pictures.picture(diagram('b', 3)));
    http.expectOne('/api/file-attachment/b/preview?size=full').flush(new Blob(['none']), { status: 404, statusText: 'Not Found' });
    await expect(picture).resolves.toBe(PNG);
    expect(render).toHaveBeenCalledWith(expect.anything(), 'https://diagrams.example.test/index.html', '<mxfile/>');
    expect(files.putPreview).toHaveBeenCalledWith('b', 3, expect.any(Blob));
    // Asked again for the same version: nothing fetched or drawn again.
    await expect(firstValueFrom(pictures.picture(diagram('b', 3)))).resolves.toBe(PNG);
    expect(render).toHaveBeenCalledTimes(1);
  });

  it('draws one diagram at a time', async () => {
    const { pictures, render, http } = setup();
    let release!: () => void;
    render.mockImplementationOnce(() => new Promise<string>(resolve => (release = () => resolve(PNG))));
    const first = firstValueFrom(pictures.picture(diagram('c')));
    const second = firstValueFrom(pictures.picture(diagram('d')));
    for (const id of ['c', 'd'])
      http.expectOne(`/api/file-attachment/${id}/preview?size=full`).flush(new Blob(['none']), { status: 404, statusText: 'Not Found' });
    await vi.waitFor(() => expect(render).toHaveBeenCalledTimes(1));
    release();
    await expect(first).resolves.toBe(PNG);
    await expect(second).resolves.toBe(PNG);
    expect(render).toHaveBeenCalledTimes(2);
  });

  it('tries again after there was nothing to show', async () => {
    const { pictures, render, http } = setup();
    render.mockRejectedValueOnce(new Error('The diagram editor did not answer.'));
    const failed = firstValueFrom(pictures.picture(diagram('f')));
    http.expectOne('/api/file-attachment/f/preview?size=full').flush(new Blob(['none']), { status: 404, statusText: 'Not Found' });
    await expect(failed).resolves.toBeNull();
    const again = firstValueFrom(pictures.picture(diagram('f')));
    http.expectOne('/api/file-attachment/f/preview?size=full').flush(new Blob(['none']), { status: 404, statusText: 'Not Found' });
    await expect(again).resolves.toBe(PNG);
    expect(render).toHaveBeenCalledTimes(2);
  });

  it('keeps the latest pictures only, revoking the object URLs of those it lets go', async () => {
    const { pictures, http } = setup();
    let n = 0;
    vi.spyOn(URL, 'createObjectURL').mockImplementation(() => `blob:${n++}`);
    const revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined);
    const show = async (id: string) => {
      const picture = firstValueFrom(pictures.picture(diagram(id)));
      http.match(`/api/file-attachment/${id}/preview?size=full`).forEach(r => r.flush(new Blob(['png'])));
      return picture;
    };
    await expect(show('p0')).resolves.toBe('blob:0');
    for (let i = 1; i < MAX_PICTURES; i++) await show(`p${i}`);
    await show('p0'); // asked for again: now the latest, nothing fetched
    expect(revoke).not.toHaveBeenCalled();
    await show('extra');
    expect(revoke).toHaveBeenCalledExactlyOnceWith('blob:1');
    await expect(show('p0')).resolves.toBe('blob:0');
  });

  it('keeps a picture let go while it was being found, so its object URL is not lost', async () => {
    const { pictures, http } = setup();
    let n = 0;
    vi.spyOn(URL, 'createObjectURL').mockImplementation(() => `blob:${n++}`);
    const revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined);
    const preview = (id: string) => `/api/file-attachment/${id}/preview?size=full`;
    const slow = firstValueFrom(pictures.picture(diagram('slow')));
    const pending = http.expectOne(preview('slow'));
    for (let i = 0; i < MAX_PICTURES; i++) {
      const picture = firstValueFrom(pictures.picture(diagram(`p${i}`)));
      http.expectOne(preview(`p${i}`)).flush(new Blob(['png']));
      await picture;
    }
    // 'slow' was let go to make room; when its preview arrives it is kept again as the latest,
    // and the oldest finished one makes room instead.
    pending.flush(new Blob(['png']));
    const url = await slow;
    expect(revoke).toHaveBeenCalledWith('blob:0');
    expect(revoke).not.toHaveBeenCalledWith(url);
    await expect(firstValueFrom(pictures.picture(diagram('slow')))).resolves.toBe(url);
  });

  it('revokes a spare picture when another request for the same version is kept', async () => {
    const { pictures, http } = setup();
    let n = 0;
    vi.spyOn(URL, 'createObjectURL').mockImplementation(() => `blob:${n++}`);
    const revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined);
    const preview = (id: string) => `/api/file-attachment/${id}/preview?size=full`;
    const first = firstValueFrom(pictures.picture(diagram('s')));
    const firstRequest = http.expectOne(preview('s'));
    for (let i = 0; i < MAX_PICTURES; i++) {
      const picture = firstValueFrom(pictures.picture(diagram(`p${i}`)));
      http.expectOne(preview(`p${i}`)).flush(new Blob(['png']));
      await picture;
    }
    // Asked again after it was let go: a second request, which is kept.
    const second = firstValueFrom(pictures.picture(diagram('s')));
    http.expectOne(preview('s')).flush(new Blob(['png']));
    const kept = await second;
    firstRequest.flush(new Blob(['png']));
    const spare = await first;
    expect(spare).not.toBe(kept);
    expect(revoke).toHaveBeenCalledWith(spare);
    expect(revoke).not.toHaveBeenCalledWith(kept);
    await expect(firstValueFrom(pictures.picture(diagram('s')))).resolves.toBe(kept);
  });

  it('has nothing to show for a diagram with no preview where there is no editor', async () => {
    const { pictures, render, http } = setup(null);
    const picture = firstValueFrom(pictures.picture(diagram('e')));
    http.expectOne('/api/file-attachment/e/preview?size=full').flush(new Blob(['none']), { status: 404, statusText: 'Not Found' });
    await expect(picture).resolves.toBeNull();
    expect(render).not.toHaveBeenCalled();
  });
});
