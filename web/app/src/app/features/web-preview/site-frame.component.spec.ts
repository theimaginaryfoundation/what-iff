import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FolderFiles } from './folder-files.service';
import { PREVIEW_URL } from './preview-site.config';
import { SITE_LOAD_TIMEOUT_MS, SITE_POLL_MS, SiteFrameComponent } from './site-frame.component';

const SITE = 'https://preview.example.test/index.html';
const ORIGIN = 'https://preview.example.test';
const page = { id: 'p', name: 'index.html', folder: 'site', file_type: 'text/html', content_version: 1 } as FileAttachment;

describe('SiteFrameComponent', () => {
  let fixture: ComponentFixture<SiteFrameComponent>;
  let stored: Map<string, FileAttachment>;
  let frameWindow: Window;
  let initMessages: { message: Record<string, unknown>; origin: string; port: MessagePort }[];

  beforeEach(() => {
    stored = new Map([['index.html', page]]);
    const folders = {
      list: vi.fn(async () => stored),
      download: vi.fn(async () => new Blob(['<h1>stored</h1>'])),
    };
    TestBed.configureTestingModule({
      imports: [SiteFrameComponent],
      providers: [provideZonelessChangeDetection(), { provide: PREVIEW_URL, useValue: SITE }, { provide: FolderFiles, useValue: folders }],
    });
    fixture = TestBed.createComponent(SiteFrameComponent);
    fixture.componentRef.setInput('file', page);
    fixture.detectChanges();
    initMessages = [];
    frameWindow = {
      postMessage: vi.fn((message: Record<string, unknown>, origin: string, transfer: MessagePort[]) =>
        initMessages.push({ message, origin, port: transfer[0] }),
      ),
    } as unknown as Window;
    Object.defineProperty(fixture.nativeElement.querySelector('iframe'), 'contentWindow', { value: frameWindow });
  });

  afterEach(() => vi.useRealTimers());

  const token = () => new URL(fixture.nativeElement.querySelector('iframe').getAttribute('src')).searchParams.get('t')!;

  function ready(origin = ORIGIN, source: Window = frameWindow, t = token()) {
    window.dispatchEvent(new MessageEvent('message', { data: { type: 'whatiff-preview-ready', token: t }, origin, source }));
  }

  /** Sends a request as the host page would, and resolves with the app's answer. */
  function ask(port: MessagePort, message: Record<string, unknown>): Promise<Record<string, unknown>> {
    return new Promise(resolve => {
      port.onmessage = event => resolve(event.data);
      port.postMessage(message);
    });
  }

  it('frames the site with a key of its own and introduces itself once the site is ready', () => {
    const iframe = fixture.nativeElement.querySelector('iframe') as HTMLIFrameElement;
    expect(iframe.getAttribute('sandbox')).toBe('allow-scripts allow-same-origin allow-forms allow-modals allow-popups allow-downloads');
    expect(token()).toMatch(/^[A-Za-z0-9_-]{24}$/);
    ready();
    expect(initMessages).toHaveLength(1);
    expect(initMessages[0].message).toEqual({ type: 'whatiff-preview-init', entry: 'index.html' });
    expect(initMessages[0].origin).toBe(ORIGIN);
    // Once only.
    ready();
    expect(initMessages).toHaveLength(1);
  });

  it('ignores messages from another origin, another window or with another key', () => {
    ready('https://evil.example.test');
    ready(ORIGIN, {} as Window);
    ready(ORIGIN, frameWindow, 'not-the-key-not-the-key0');
    expect(initMessages).toEqual([]);
  });

  it('answers the host page’s requests from the project, the editor’s text first', async () => {
    fixture.componentRef.setInput('html', '<h1>typed</h1>');
    fixture.detectChanges();
    ready();
    const port = initMessages[0].port;
    const reply = await ask(port, { type: 'get', id: 7, path: 'index.html' });
    expect(reply).toMatchObject({ type: 'file', id: 7, status: 200, contentType: 'text/html; charset=utf-8' });
    expect(new TextDecoder().decode(reply['body'] as ArrayBuffer)).toBe('<h1>typed</h1>');
    expect(await ask(port, { type: 'get', id: 8, path: 'missing.css' })).toMatchObject({ type: 'file', id: 8, status: 404 });
  });

  it('reloads the page when the editor’s text changes', async () => {
    fixture.componentRef.setInput('html', 'one');
    fixture.detectChanges();
    ready();
    const port = initMessages[0].port;
    const reloads: unknown[] = [];
    port.onmessage = event => reloads.push(event.data);
    fixture.componentRef.setInput('html', 'two');
    fixture.detectChanges();
    await vi.waitFor(() => expect(reloads).toEqual([{ type: 'reload' }]));
  });

  it('reloads the page when a file it used changed elsewhere', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    TestBed.resetTestingModule();
    stored = new Map([['index.html', page]]);
    TestBed.configureTestingModule({
      imports: [SiteFrameComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: PREVIEW_URL, useValue: SITE },
        { provide: FolderFiles, useValue: { list: vi.fn(async () => stored), download: vi.fn(async () => new Blob(['x'])) } },
      ],
    });
    fixture = TestBed.createComponent(SiteFrameComponent);
    fixture.componentRef.setInput('file', page);
    fixture.detectChanges();
    Object.defineProperty(fixture.nativeElement.querySelector('iframe'), 'contentWindow', { value: frameWindow });
    ready();
    const port = initMessages[0].port;
    await ask(port, { type: 'get', id: 1, path: 'index.html' });
    const seen: unknown[] = [];
    port.onmessage = event => seen.push(event.data);
    stored = new Map([['index.html', { ...page, content_version: 2 }]]);
    vi.advanceTimersByTime(SITE_POLL_MS);
    await vi.waitFor(() => expect(seen).toEqual([{ type: 'reload' }]));
  });

  it('says so when the site does not answer', () => {
    vi.useFakeTimers();
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      imports: [SiteFrameComponent],
      providers: [provideZonelessChangeDetection(), { provide: PREVIEW_URL, useValue: SITE }, { provide: FolderFiles, useValue: {} }],
    });
    fixture = TestBed.createComponent(SiteFrameComponent);
    fixture.componentRef.setInput('file', page);
    fixture.detectChanges();
    vi.advanceTimersByTime(SITE_LOAD_TIMEOUT_MS);
    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain(`The preview site didn't load from ${ORIGIN}`);
  });
});
