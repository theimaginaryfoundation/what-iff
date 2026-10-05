import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, TestRequest, provideHttpClientTesting } from '@angular/common/http/testing';
import { environment } from '@environments/environment';
import { Personality, PersonalityCardImportResult } from '../models/personality.model';
import { PersonalityCardService } from './personality-card.service';

const PERSONALITY: Personality = {
  id: 'p-1',
  name: 'Ada',
  system_prompt: 'You are Ada.',
  auto_pin_memories: false,
  expressions_enabled: false,
  image_style: 'auto',
  cover_image_id: null,
  cover_image_url: null,
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
  stats: { chat_count: 0, last_used_at: null },
};

function cardFile(text: string): File {
  return new File([text], 'ada.json', { type: 'application/json' });
}

describe('PersonalityCardService', () => {
  const api = `${environment.apiUrl}/personality`;
  let service: PersonalityCardService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideZonelessChangeDetection(), provideHttpClient(withXhr()), provideHttpClientTesting()],
    });
    service = TestBed.inject(PersonalityCardService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    // The import refreshes the personality list afterwards; that GET is not what these tests are about.
    http.match(req => req.method === 'GET' && req.url === api).forEach(req => req.flush({ results: [], total_count: 0, page: 1 }));
    http.verify();
    vi.restoreAllMocks();
  });

  /** file.text() resolves asynchronously, so the request appears a tick after subscribe. */
  function nextRequest(url: string): Promise<TestRequest> {
    return vi.waitFor(() => http.expectOne(url));
  }

  async function answerImport(result: PersonalityCardImportResult): Promise<void> {
    (await nextRequest(`${api}/import/sillytavern`)).flush(result);
  }

  it('posts the raw card text as JSON', async () => {
    const outcome = new Promise(resolve => service.importCardFile(cardFile('{"spec":"chara_card_v2"}')).subscribe(resolve));

    const req = await nextRequest(`${api}/import/sillytavern`);
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toBe('{"spec":"chara_card_v2"}');
    expect(req.request.headers.get('Content-Type')).toBe('application/json');
    req.flush({ personality: PERSONALITY, lore_files: [], warnings: ['careful'] } satisfies PersonalityCardImportResult);

    expect(await outcome).toEqual({ personality: PERSONALITY, warnings: ['careful'], coverFailed: false, attachedLoreFiles: 0, failedLoreFiles: [] });
  });

  it('uploads each lore file to the new personality, keywords as the description', async () => {
    const outcome = new Promise(resolve => service.importCardFile(cardFile('{}')).subscribe(resolve));
    await answerImport({
      personality: PERSONALITY,
      warnings: [],
      lore_files: [
        { name: 'Dragons', file_name: 'Dragons.md', keys: ['dragon', 'wyrm'], content: 'Big.' },
        { name: 'Elves', file_name: 'Elves.md', keys: [], content: 'Pointy.' },
      ],
    });

    for (const [fileName, description] of [
      ['Dragons.md', 'Keywords: dragon, wyrm'],
      ['Elves.md', null],
    ] as const) {
      const upload = await nextRequest(`${api}/p-1/file-attachment`);
      const form = upload.request.body as FormData;
      expect((form.get('attachment') as File).name).toBe(fileName);
      expect(form.get('description')).toBe(description);
      upload.flush({ id: 'f' });
    }

    expect(await outcome).toMatchObject({ attachedLoreFiles: 2, failedLoreFiles: [] });
  });

  it('keeps going when a lore upload fails and reports it', async () => {
    const outcome = new Promise(resolve => service.importCardFile(cardFile('{}')).subscribe(resolve));
    await answerImport({
      personality: PERSONALITY,
      warnings: [],
      lore_files: [
        { name: 'Bad', file_name: 'Bad.md', keys: [], content: 'x' },
        { name: 'Good', file_name: 'Good.md', keys: [], content: 'y' },
      ],
    });

    (await nextRequest(`${api}/p-1/file-attachment`)).flush('nope', { status: 500, statusText: 'Server Error' });
    (await nextRequest(`${api}/p-1/file-attachment`)).flush({ id: 'f' });

    expect(await outcome).toMatchObject({ attachedLoreFiles: 1, failedLoreFiles: ['Bad'] });
  });

  it('surfaces a rejected card as an error', async () => {
    const failure = new Promise(resolve => service.importCardFile(cardFile('nope')).subscribe({ error: resolve }));

    (await nextRequest(`${api}/import/sillytavern`)).flush(
      { message: 'unsupported character card spec' },
      { status: 400, statusText: 'Bad Request' },
    );

    expect(await failure).toMatchObject({ status: 400 });
  });

  /** Captures the file name of the download link the export clicks. */
  function captureDownloads(): string[] {
    const saved: string[] = [];
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:test');
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined);
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      saved.push(this.download);
    });
    return saved;
  }

  describe('PNG cards', () => {
    const pngCard = () => new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], 'ada.png', { type: 'image/png' });

    it('posts the PNG bytes, then attaches the picture as the cover image', async () => {
      const outcome = new Promise(resolve => service.importCardFile(pngCard()).subscribe(resolve));

      const imp = await nextRequest(`${api}/import/sillytavern`);
      expect(imp.request.headers.get('Content-Type')).toBe('image/png');
      expect(imp.request.body).toBeInstanceOf(ArrayBuffer);
      imp.flush({ personality: PERSONALITY, lore_files: [], warnings: [] } satisfies PersonalityCardImportResult);

      const upload = await nextRequest(`${api}/p-1/file-attachment`);
      expect((upload.request.body as FormData).get('attachment')).toBeInstanceOf(File);
      upload.flush({ id: 'cover-1' });

      const update = await nextRequest(`${api}/p-1`);
      expect(update.request.method).toBe('PUT');
      expect(update.request.body).toMatchObject({ name: 'Ada', cover_image_id: 'cover-1' });
      update.flush({ ...PERSONALITY, cover_image_id: 'cover-1' });

      expect(await outcome).toMatchObject({ personality: { cover_image_id: 'cover-1' }, coverFailed: false });
    });

    it('still succeeds, and says so, when the cover cannot be attached', async () => {
      const outcome = new Promise(resolve => service.importCardFile(pngCard()).subscribe(resolve));
      await answerImport({ personality: PERSONALITY, warnings: [], lore_files: [] });

      (await nextRequest(`${api}/p-1/file-attachment`)).flush('nope', { status: 500, statusText: 'Server Error' });

      expect(await outcome).toMatchObject({ personality: { id: 'p-1' }, coverFailed: true });
    });

    it('requests the PNG export and saves it as a png', async () => {
      const saved = captureDownloads();
      const done = new Promise<void>(resolve => service.exportCard({ id: 'p-1', name: 'Ada' }, 'png').subscribe(() => resolve()));

      const req = http.expectOne(`${api}/p-1/export/sillytavern?format=png`);
      req.flush(new Blob(['png']));
      await done;

      expect(saved).toEqual(['Ada.png']);
    });
  });

  it('exports a card and saves it under the server-chosen file name', async () => {
    const saved = captureDownloads();
    const done = new Promise<void>(resolve => service.exportCard({ id: 'p-1', name: 'Ada' }).subscribe(() => resolve()));

    const req = http.expectOne(`${api}/p-1/export/sillytavern`);
    expect(req.request.responseType).toBe('blob');
    req.flush(new Blob(['{}']), { headers: { 'Content-Disposition': 'attachment; filename="Ada Card.json"' } });
    await done;

    expect(saved).toEqual(['Ada Card.json']);
  });

  it('falls back to the personality name when the server sends no file name', async () => {
    const saved = captureDownloads();
    const done = new Promise<void>(resolve => service.exportCard({ id: 'p-1', name: 'Ada' }).subscribe(() => resolve()));

    http.expectOne(`${api}/p-1/export/sillytavern`).flush(new Blob(['{}']));
    await done;

    expect(saved).toEqual(['Ada.json']);
  });
});
