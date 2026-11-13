import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom, of } from 'rxjs';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { DiagramPictures } from './diagram-pictures.service';
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

  it('has nothing to show for a diagram with no preview where there is no editor', async () => {
    const { pictures, render, http } = setup(null);
    const picture = firstValueFrom(pictures.picture(diagram('e')));
    http.expectOne('/api/file-attachment/e/preview?size=full').flush(new Blob(['none']), { status: 404, statusText: 'Not Found' });
    await expect(picture).resolves.toBeNull();
    expect(render).not.toHaveBeenCalled();
  });
});
