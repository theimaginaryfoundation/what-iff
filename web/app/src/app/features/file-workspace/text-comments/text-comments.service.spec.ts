import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';

import { environment } from '../../../../environments/environment';
import { TextCommentThread, TextCommentsService } from './text-comments.service';

describe('TextCommentsService', () => {
  let service: TextCommentsService;
  let http: HttpTestingController;
  const url = `${environment.apiUrl}/file-attachment/f1/comments`;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideZonelessChangeDetection(), provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(TextCommentsService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('reads the threads in the client shape, leaving out what is not set', async () => {
    const got = firstValueFrom(service.get('f1'));
    http.expectOne({ method: 'GET', url }).flush({
      content_version: 3,
      revision: 2,
      threads: [
        {
          id: 't1',
          anchor: { start: 4, end: 8 },
          quote: 'plan',
          resolved: false,
          comments: [
            {
              id: 't1',
              author_id: 'user:1',
              author_name: 'Ada',
              created_at: '2026-11-12T00:00:00Z',
              text: '@Scribe why?',
              mentions: [{ id: 'agent:p1', name: 'Scribe' }],
            },
          ],
        },
        {
          id: 't2',
          anchor: null,
          quote: '',
          resolved: true,
          comments: [{ id: 't2', author_id: 'user:1', author_name: 'Ada', created_at: '', text: 'Done', mentions: [] }],
        },
      ],
    });

    expect(await got).toEqual({
      contentVersion: 3,
      revision: 2,
      threads: [
        {
          id: 't1',
          anchor: { start: 4, end: 8 },
          quote: 'plan',
          comments: [
            {
              id: 't1',
              authorId: 'user:1',
              authorName: 'Ada',
              createdAt: '2026-11-12T00:00:00Z',
              text: '@Scribe why?',
              mentions: [{ id: 'agent:p1', name: 'Scribe' }],
            },
          ],
        },
        {
          id: 't2',
          anchor: null,
          quote: '',
          resolved: true,
          comments: [{ id: 't2', authorId: 'user:1', authorName: 'Ada', createdAt: '', text: 'Done' }],
        },
      ],
    });
  });

  it('reads a file with no comments as no threads', async () => {
    const got = firstValueFrom(service.get('f1'));
    http.expectOne(url).flush({ content_version: 1, revision: 0, threads: null });
    expect(await got).toEqual({ contentVersion: 1, revision: 0, threads: [] });
  });

  it('writes the threads with the content version and revision they were read at', async () => {
    const threads: TextCommentThread[] = [
      {
        id: 't1',
        anchor: { start: 0, end: 3 },
        comments: [
          { id: 't1', authorId: 'user:1', authorName: 'Ada', createdAt: 'now', text: 'Hi', mentions: [{ id: 'user:2', name: 'Bo' }] },
        ],
      },
      {
        id: 't2',
        anchor: null,
        quote: 'gone',
        resolved: true,
        comments: [{ id: 't2', authorId: 'user:1', authorName: 'Ada', createdAt: 'now', text: 'Old' }],
      },
    ];
    const got = firstValueFrom(service.put('f1', threads, 3, 2));
    const req = http.expectOne({ method: 'PUT', url });
    expect(req.request.body).toEqual({
      content_version: 3,
      revision: 2,
      threads: [
        {
          id: 't1',
          anchor: { start: 0, end: 3 },
          quote: '',
          resolved: false,
          comments: [
            { id: 't1', author_id: 'user:1', author_name: 'Ada', created_at: 'now', text: 'Hi', mentions: [{ id: 'user:2', name: 'Bo' }] },
          ],
        },
        {
          id: 't2',
          anchor: null,
          quote: 'gone',
          resolved: true,
          comments: [{ id: 't2', author_id: 'user:1', author_name: 'Ada', created_at: 'now', text: 'Old' }],
        },
      ],
    });
    req.flush({ content_version: 3, revision: 3, threads: [] });
    expect((await got).revision).toBe(3);
  });
});
