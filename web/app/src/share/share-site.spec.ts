import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideMarkdown } from 'ngx-markdown';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { TextViewerComponent } from '../app/features/file-workspace/viewers/text-viewer.component';
import { FileAttachmentService } from '../app/core/services/file-attachment.service';
import { CommentAgentService } from '../app/features/office/comments/comment-agent.service';

import { buildTree, foldersOf, treeRows } from './file-tree';
import { ShareApiService, SharedFile, sharedFileUrl } from './share-api.service';
import { SHARE_ROOT, ShareFolderFiles, sharedAttachment, sharedPath } from './share-folder-files';
import { previewUrlFor, readOnlyViewerProviders } from './share.providers';

const f = (path: string, file_type = 'text/plain'): SharedFile => ({ path, file_type, content_version: 1 });

describe('previewUrlFor', () => {
  it('takes the web apps site on another origin, on https or this machine, even on the same site', () => {
    const share = 'https://share.example.com';
    expect(previewUrlFor('https://webapps.example.com/index.html', share)).toBe('https://webapps.example.com/index.html');
    expect(previewUrlFor('http://127.0.0.1:4200/preview/index.html', 'http://localhost:4204')).toBe(
      'http://127.0.0.1:4200/preview/index.html',
    );
    expect(previewUrlFor('https://share.example.com/preview/', share)).toBeNull();
    expect(previewUrlFor('http://webapps.example.com/', share)).toBeNull();
    expect(previewUrlFor('__PREVIEW_URL__', share)).toBeNull();
    expect(previewUrlFor('', share)).toBeNull();
  });
});

describe('the file tree', () => {
  const files = [f('index.html'), f('js/app.js'), f('css/b.css'), f('css/A.css'), f('js/lib/x.js'), f('README.md')];

  it('lists folders first, then files, each by name; only open folders show what they hold', () => {
    const tree = buildTree(files);
    const names = (open: string[]) =>
      treeRows(tree, new Set(open)).map(r => `${'  '.repeat(r.depth)}${r.name}${r.kind === 'folder' ? '/' : ''}`);
    expect(names([])).toEqual(['css/', 'js/', 'index.html', 'README.md']);
    expect(names(['css'])).toEqual(['css/', '  A.css', '  b.css', 'js/', 'index.html', 'README.md']);
    expect(names(['js', 'js/lib'])).toEqual(['css/', 'js/', '  lib/', '    x.js', '  app.js', 'index.html', 'README.md']);
  });

  it('knows the folders a path is in', () => {
    expect(foldersOf('a/b/c.js')).toEqual(['a', 'a/b']);
    expect(foldersOf('c.js')).toEqual([]);
  });
});

describe('ShareFolderFiles', () => {
  afterEach(() => TestBed.resetTestingModule());

  const setup = (api: Partial<ShareApiService>) => {
    TestBed.configureTestingModule({ providers: [provideZonelessChangeDetection(), { provide: ShareApiService, useValue: api }] });
    return TestBed.inject(ShareFolderFiles);
  };

  it('stands a web app’s files in a folder of their own, by their paths, and reads each version once', async () => {
    const api = {
      files: vi.fn(async () => [f('index.html', 'text/html'), f('img/a.png', 'image/png')]),
      file: vi.fn(async () => new Blob(['x'])),
      content: vi.fn(),
    };
    const files = setup(api);
    files.now = () => 0;
    files.use('k'.repeat(22), null);
    expect([...(await files.list(SHARE_ROOT)).keys()]).toEqual(['index.html']);
    const img = (await files.list(`${SHARE_ROOT}/img`)).get('a.png')!;
    expect(img.id).toBe('shared:img/a.png');
    expect(sharedPath(img)).toBe('img/a.png');
    await files.download(img);
    await files.download(img);
    expect(api.file).toHaveBeenCalledTimes(1); // once per version
    await files.download({ ...img, content_version: 2 });
    expect(api.file).toHaveBeenCalledTimes(2);
    expect(api.files).toHaveBeenCalledTimes(1);
    await files.list(SHARE_ROOT, true);
    expect(api.files).toHaveBeenCalledTimes(1); // a fresh listing within its time is the last one
    files.now = () => 31_000;
    await files.list(SHARE_ROOT, true);
    expect(api.files).toHaveBeenCalledTimes(2);
    expect(await files.conversation()).toEqual(new Map());
  });

  it('a single file is its folder’s only file, read as the share’s content', async () => {
    const api = { files: vi.fn(), file: vi.fn(), content: vi.fn(async () => new Blob(['y'])) };
    const files = setup(api);
    files.use('k'.repeat(22), f('page.html', 'text/html'));
    const page = (await files.list(SHARE_ROOT)).get('page.html')!;
    expect(page).toEqual(sharedAttachment(f('page.html', 'text/html')));
    await files.download(page);
    expect(api.content).toHaveBeenCalledWith('k'.repeat(22));
    expect((await files.list('elsewhere')).size).toBe(0);
  });
});

describe('ShareApiService', () => {
  const key = 'k'.repeat(22);
  let http: HttpTestingController;
  let api: ShareApiService;
  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideZonelessChangeDetection(), provideHttpClient(), provideHttpClientTesting()] });
    http = TestBed.inject(HttpTestingController);
    api = TestBed.inject(ShareApiService);
  });
  afterEach(() => {
    http.verify();
    TestBed.resetTestingModule();
  });

  it('asks nothing about a key that cannot be a share’s', async () => {
    expect(await api.open('../x')).toEqual({ state: 'missing' });
    expect(await api.unlock('short', 'x')).toEqual({ state: 'missing' });
    expect(() => sharedFileUrl('../../api', 'a')).toThrow();
  });

  it('maps the answers: locked, a wrong passcode with tries left, a ban with how long', async () => {
    const open = api.open(key);
    http.expectOne(`/api/shared/${key}`).flush({ locked: true }, { status: 401, statusText: 'Unauthorized' });
    expect(await open).toEqual({ state: 'locked' });

    const wrong = api.unlock(key, 'nope');
    const req = http.expectOne(`/api/shared/${key}/unlock`);
    expect(req.request.body).toEqual({ passcode: 'nope' });
    req.flush({ attempts_left: 3 }, { status: 401, statusText: 'Unauthorized' });
    expect(await wrong).toEqual({ state: 'wrong', attemptsLeft: 3 });

    const banned = api.unlock(key, 'nope');
    http.expectOne(`/api/shared/${key}/unlock`).flush({ retry_after: 3600 }, { status: 429, statusText: 'Too Many Requests' });
    expect(await banned).toEqual({ state: 'banned', retryAfterSeconds: 3600 });
  });

  it('reads a share’s files, and says when an unlock finds no share or fails', async () => {
    const content = api.content(key);
    http.expectOne(`/api/shared/${key}/content`).flush(new Blob(['a']));
    expect(await content).toBeInstanceOf(Blob);

    const files = api.files(key);
    http.expectOne(`/api/shared/${key}/files`).flush({ files: [{ path: 'a.txt', file_type: 'text/plain', content_version: 1 }] });
    expect((await files).map(f => f.path)).toEqual(['a.txt']);

    const file = api.file(key, 'css/a.css');
    http.expectOne(`/api/shared/${key}/files/css/a.css`).flush(new Blob(['b']));
    expect(await file).toBeInstanceOf(Blob);

    const missing = api.unlock(key, 'x');
    http.expectOne(`/api/shared/${key}/unlock`).flush({}, { status: 404, statusText: 'Not Found' });
    expect(await missing).toEqual({ state: 'missing' });

    const failed = api.unlock(key, 'x');
    http.expectOne(`/api/shared/${key}/unlock`).flush({}, { status: 500, statusText: 'Server Error' });
    expect(await failed).toEqual({ state: 'error' });

    const unlocked = api.unlock(key, 'x');
    http.expectOne(`/api/shared/${key}/unlock`).flush(null, { status: 204, statusText: 'No Content' });
    expect(await unlocked).toEqual({ state: 'unlocked' });
  });

  it('encodes each segment of a file’s path', () => {
    expect(sharedFileUrl(key, 'css/a b#c.css')).toBe(`/api/shared/${key}/files/css/a%20b%23c.css`);
  });
});

describe('the share site’s stand-ins', () => {
  afterEach(() => TestBed.resetTestingModule());

  it('let an app viewer show a file without one request to any API', async () => {
    TestBed.configureTestingModule({
      imports: [TextViewerComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        provideMarkdown(),
        ...readOnlyViewerProviders(),
      ],
    });
    const http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(TextViewerComponent);
    fixture.componentRef.setInput('file', sharedAttachment(f('notes.md', 'text/markdown')));
    fixture.componentRef.setInput('blob', new Blob(['# Hello\n\nShared.']));
    fixture.detectChanges();
    await fixture.whenStable();
    await new Promise(r => setTimeout(r, 20));
    fixture.detectChanges();
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('Hello');
    http.verify();
  });

  it('stand in for the agent: no agent to mention, and nothing posted reaches anyone', async () => {
    TestBed.configureTestingModule({
      providers: [provideZonelessChangeDetection(), provideHttpClient(), provideRouter([]), ...readOnlyViewerProviders()],
    });
    const agent = TestBed.inject(CommentAgentService);
    expect(agent.agent()).toBeNull();
    const people = [{ id: 'user:a', name: 'A' }];
    expect(agent.withAgent(people)).toBe(people);
    const hooks = agent.hooks(() => ({ id: 'f', name: 'f' }));
    expect(hooks.people(people)).toBe(people);
    expect(hooks.mark()).toBe(0);
    expect(() => {
      hooks.posted({ id: 't', anchor: null, comments: [] }, 'c', '');
      hooks.saved(0);
      hooks.discard();
    }).not.toThrow();
    expect(await agent.posted({ id: 'f', name: 'f' }, { id: 't', anchor: null, comments: [] }, 'c', '')).toBe(false);
    expect(TestBed.inject(FileAttachmentService).hasPreview('f', false)).toBeTruthy();
  });
});
