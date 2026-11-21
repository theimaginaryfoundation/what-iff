import { Component, input, provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../app/core/models/file-attachment.model';
import { CodeEditorComponent } from '../app/features/file-workspace/editors/code-editor.component';
import { HtmlPreviewComponent } from '../app/features/web-preview/html-preview.component';
import { PREVIEW_URL } from '../app/features/web-preview/preview-site.config';
import { SharedFile, SharedView } from './share-api.service';
import { ShareFolderFiles } from './share-folder-files';
import { SharedWebAppComponent } from './shared-web-app.component';
import { readOnlyViewerProviders } from './share.providers';

@Component({
  selector: 'app-html-preview',
  standalone: true,
  template: 'preview:{{ file().name }}:{{ html() === null ? "stored" : html() }}',
})
class FakePreview {
  readonly file = input.required<FileAttachment>();
  readonly html = input.required<string | null>();
}
@Component({ selector: 'app-code-editor', standalone: true, template: 'code:{{ file().name }}' })
class FakeCode {
  readonly file = input.required<FileAttachment>();
  readonly blob = input<Blob>();
  readonly readOnly = input(false);
  readonly codeOnly = input(false);
}

const f = (path: string, file_type = 'text/plain'): SharedFile => ({ path, file_type, content_version: 1 });

async function setup(
  listing: SharedFile[],
  download: (file: FileAttachment) => Promise<Blob> = async () => new Blob(['x']),
  previewUrl: string | null = 'https://webapps.test/index.html',
) {
  const files = { use: vi.fn(), all: vi.fn(async () => listing), download: vi.fn(download) };
  await TestBed.configureTestingModule({
    imports: [SharedWebAppComponent],
    providers: [
      provideZonelessChangeDetection(),
      provideHttpClient(),
      provideRouter([]),
      // The share site's stand-ins, as the viewers it loads (an image's) need them; then this test's own.
      ...readOnlyViewerProviders(),
      { provide: ShareFolderFiles, useValue: files },
      { provide: PREVIEW_URL, useValue: previewUrl },
    ],
  })
    .overrideComponent(SharedWebAppComponent, {
      remove: { imports: [HtmlPreviewComponent, CodeEditorComponent] },
      add: { imports: [FakePreview, FakeCode] },
    })
    .compileComponents();
  const fixture = TestBed.createComponent(SharedWebAppComponent);
  fixture.componentRef.setInput('shareKey', 'k'.repeat(22));
  fixture.componentRef.setInput('view', {
    name: 'index.html',
    file_type: 'text/html',
    content_version: 1,
    web_app: true,
    page: 'index.html',
    project_name: 'site',
  } as SharedView);
  const settle = async () => {
    for (let i = 0; i < 3; i++) {
      fixture.detectChanges();
      await fixture.whenStable();
      await new Promise(r => setTimeout(r));
    }
    fixture.detectChanges();
  };
  await settle();
  const el = fixture.nativeElement as HTMLElement;
  const button = (label: string) => [...el.querySelectorAll('button')].find(b => b.textContent?.trim() === label) as HTMLButtonElement;
  return { fixture, el, button, settle, files };
}

describe('SharedWebAppComponent', () => {
  afterEach(() => TestBed.resetTestingModule());

  it('opens on Preview, the web apps site serving the stored page; Code shows the tree and the chosen file', async () => {
    const { el, button, settle } = await setup([f('index.html', 'text/html'), f('about.html', 'text/html'), f('css/app.css', 'text/css')]);
    expect(el.textContent).toContain('preview:index.html:stored');

    button('Code').click();
    await settle();
    expect(el.querySelector('nav[aria-label="Files"]')?.textContent).toContain('app.css');
    expect(el.textContent).toContain('code:index.html');

    // An HTML file chosen in Code is the page Preview shows next.
    button('about.html').click();
    await settle();
    button('Preview').click();
    await settle();
    expect(el.textContent).toContain('preview:about.html:stored');
  });

  it('says a file could not be loaded in its own pane, keeping the tree', async () => {
    const { el, button, settle } = await setup([f('index.html', 'text/html'), f('data.txt')], async file => {
      if (file.name === 'data.txt') throw new Error('gone');
      return new Blob(['x']);
    });
    button('Code').click();
    await settle();
    button('data.txt').click();
    await settle();
    expect(el.textContent).toContain('This file could not be loaded');
    expect(el.querySelector('nav[aria-label="Files"]')).not.toBeNull();
    button('index.html').click();
    await settle();
    expect(el.textContent).toContain('code:index.html');
  });

  it('opens on Code when the folder has no page, and says so in Preview', async () => {
    const { el, button, settle } = await setup([f('notes.txt')]);
    expect(el.querySelector('nav[aria-label="Files"]')).not.toBeNull();
    button('Preview').click();
    await settle();
    expect(el.textContent).toContain('This project has no page to show');
  });

  it('opens and closes folders, and downloads the chosen file by a link of its own path', async () => {
    const { el, button, settle } = await setup([f('index.html', 'text/html'), f('img/a b.png', 'image/png')]);
    button('Code').click();
    await settle();
    expect(el.textContent).toContain('a b.png');
    expect(el.querySelector('a[download]')?.getAttribute('href')).toBe(`/api/shared/${'k'.repeat(22)}/files/index.html`);
    const folder = el.querySelector('button[aria-label="Folder img"]') as HTMLButtonElement;
    expect(folder.getAttribute('aria-expanded')).toBe('true');
    folder.click();
    await settle();
    expect(el.textContent).not.toContain('a b.png');
    folder.click();
    await settle();
    button('a b.png').click();
    await settle();
    expect(el.querySelector('a[download]')?.getAttribute('href')).toBe(`/api/shared/${'k'.repeat(22)}/files/img/a%20b.png`);
  });

  it('reads the page itself where there is no web apps site, and says when it cannot', async () => {
    const ok = await setup([f('index.html', 'text/html')], async () => new Blob(['<p>hi</p>']), null);
    expect(ok.el.textContent).toContain('preview:index.html:<p>hi</p>');
    TestBed.resetTestingModule();
    const failed = await setup(
      [f('index.html', 'text/html')],
      async () => {
        throw new Error('gone');
      },
      null,
    );
    expect(failed.el.textContent).toContain('The page could not be loaded');
  });
});
