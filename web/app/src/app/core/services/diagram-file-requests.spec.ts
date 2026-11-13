import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { environment } from '../../../environments/environment';
import { DIAGRAMS_URL } from '../../features/diagrams/diagram-site.config';
import { DiagramUploads } from '../../features/diagrams/diagram-uploads';
import { FileAttachment } from '../models/file-attachment.model';
import { FileAttachmentService } from './file-attachment.service';
import { ImageGalleryService } from './image-gallery.service';

/** The diagram requests of FileAttachmentService and ImageGalleryService (ADR 0x049 §7, §8). */
describe('diagram file requests', () => {
  const api = environment.apiUrl;
  let http: HttpTestingController;
  let files: FileAttachmentService;
  let gallery: ImageGalleryService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: DIAGRAMS_URL, useValue: 'https://diagrams.example.test/index.html' },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    files = TestBed.inject(FileAttachmentService);
    gallery = TestBed.inject(ImageGalleryService);
    TestBed.inject(DiagramUploads).convertVisio = vi.fn(async () => '<mxfile/>');
  });

  afterEach(() => http.verify());

  const visio = () => new File(['vsdx'], 'Network.vsdx');
  const sent = (body: unknown) => (body as FormData).get('attachment') as File;

  it('uploads a Visio drawing to a chat as the .drawio it converts to', async () => {
    const done = firstValueFrom(files.uploadChatFileAttachment('c-1', visio(), 'plans'));
    const request = await vi.waitFor(() => http.expectOne(`${api}/chat/c-1/file-attachment`));
    const file = sent(request.request.body);
    expect(file.name).toBe('Network.drawio');
    expect(await file.text()).toBe('<mxfile/>');
    expect((request.request.body as FormData).get('folder')).toBe('plans');
    request.flush({ id: 'f-1' });
    await expect(done).resolves.toEqual({ id: 'f-1' });
  });

  it('imports a Visio drawing to the gallery under a .drawio title, other files as they are', async () => {
    const done = firstValueFrom(gallery.importImage(visio(), { title: 'Network.vsdx', folder: 'plans' }));
    const request = await vi.waitFor(() => http.expectOne(`${api}/image-gallery/import`));
    const body = request.request.body as FormData;
    expect(sent(body).name).toBe('Network.drawio');
    expect(body.get('title')).toBe('Network.drawio');
    expect(body.get('folder')).toBe('plans');
    request.flush({ id: 'f-2' } as FileAttachment);
    await done;

    const plain = new File(['x'], 'notes.txt');
    const second = firstValueFrom(gallery.importImage(plain));
    const other = await vi.waitFor(() => http.expectOne(`${api}/image-gallery/import`));
    expect(sent(other.request.body)).toBe(plain);
    expect((other.request.body as FormData).get('title')).toBeNull();
    other.flush({ id: 'f-3' });
    await second;
  });

  it('reads, checks and stores a diagram preview', async () => {
    expect(files.previewUrl('f-1')).toBe(`${api}/file-attachment/f-1/preview?size=full`);
    expect(files.previewUrl('f-1', 'thumbnail')).toBe(`${api}/file-attachment/f-1/preview?size=thumbnail`);

    const has = firstValueFrom(files.hasPreview('f-1'));
    http.expectOne(`${api}/file-attachment/f-1/preview?size=thumbnail`).flush(new Blob(['jpg']));
    await expect(has).resolves.toBe(true);
    const none = firstValueFrom(files.hasPreview('f-2'));
    http.expectOne(`${api}/file-attachment/f-2/preview?size=thumbnail`).flush(new Blob(['none']), { status: 404, statusText: 'Not Found' });
    await expect(none).resolves.toBe(false);

    const png = new Blob(['png'], { type: 'image/png' });
    const put = firstValueFrom(files.putPreview('f-1', 4, png));
    const request = http.expectOne(r => r.method === 'PUT' && r.url === `${api}/file-attachment/f-1/preview`);
    expect(request.request.params.get('version')).toBe('4');
    expect(request.request.headers.get('Content-Type')).toBe('image/png');
    expect(request.request.body).toBe(png);
    request.flush(null);
    await put;
  });
});
