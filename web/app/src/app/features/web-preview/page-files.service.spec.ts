import { TestBed } from '@angular/core/testing';
import { of } from 'rxjs';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { FolderFiles } from './folder-files.service';
import { PageFiles } from './page-files.service';

const page = { id: 'page', name: 'index.html', folder: 'site', file_type: 'text/html' } as FileAttachment;

function setup(contents: Record<string, string>) {
  const folder = Object.keys(contents).map(
    (name, i) =>
      ({
        id: `f-${name}`,
        name,
        folder: 'site',
        file_type: name.endsWith('.png') ? 'image/png' : '',
        content_version: 1,
        created_at: `2026-11-0${i + 1}`,
      }) as FileAttachment,
  );
  const gallery = { listImages: vi.fn(() => of({ results: folder, total_count: folder.length, page: 1 })) };
  const files = { downloadFileAttachment: vi.fn((id: string) => of(new Blob([contents[id.slice(2)]]))) };
  TestBed.configureTestingModule({
    providers: [
      { provide: ImageGalleryService, useValue: gallery },
      { provide: FileAttachmentService, useValue: files },
    ],
  });
  return { pages: TestBed.inject(PageFiles), gallery, files };
}

/** The text a `data:` URL holds. */
function decoded(url: string): { type: string; text: string } {
  const match = /^data:([^;]+);base64,(.*)$/.exec(url);
  if (!match) throw new Error(`not a data URL: ${url.slice(0, 40)}`);
  return { type: match[1], text: atob(match[2]) };
}

describe('PageFiles', () => {
  afterEach(() => TestBed.resetTestingModule());

  it('builds a page that names no file without a request: its elements’ source ranges and our bridge only', async () => {
    const { pages, gallery } = setup({});
    const html = '<p>Hello</p><script>document.body.append("hi")</script>';
    const built = await pages.build(page, html);
    expect(built).toMatchObject({ missing: [], skipped: [] });
    expect(built.html).toMatch(/^<script>\(function \(\) \{/);
    expect(built.html).toContain('<p data-wi-src="page:0-3">Hello</p>');
    expect(gallery.listImages).not.toHaveBeenCalled();
  });

  it('loads the files a page names in its folder, stylesheets with their own files, and says what is missing', async () => {
    const { pages, gallery } = setup({
      'styles.css': '@import "base.css"; body { background: url(bg.png) }',
      'base.css': 'h1 { color: red }',
      'bg.png': 'png',
      'app.js': 'console.log(1)',
    });
    const built = await pages.build(page, '<link rel="stylesheet" href="styles.css"><script src="app.js"></script><img src="gone.png">');
    expect(gallery.listImages).toHaveBeenCalledWith(1, 200, { folder: 'site', kind: 'all' });
    expect(built.missing).toEqual(['gone.png']);
    expect(built.html).toContain('src="gone.png"');
    // data: URLs, as an opaque-origin frame may not load the app's blob: URLs.
    const doc = new DOMParser().parseFromString(built.html, 'text/html');
    expect(decoded(doc.querySelector('script[src]')!.getAttribute('src')!)).toEqual({ type: 'text/javascript', text: 'console.log(1)' });
    const styles = decoded(doc.querySelector('link')!.getAttribute('href')!);
    expect(styles.type).toBe('text/css');
    // The stylesheet points at its own import's and image's URLs.
    const [, base, bg] = /@import "([^"]+)"; body \{ background: url\(([^)]+)\) \}/.exec(styles.text)!;
    expect(decoded(base)).toEqual({ type: 'text/css', text: 'h1 { color: red }' });
    expect(decoded(bg)).toEqual({ type: 'image/png', text: 'png' });
  });

  it('lists the folder once while rebuilt quickly, and downloads each file version once', async () => {
    const { pages, gallery, files } = setup({ 'styles.css': 'p { color: blue }' });
    let now = 0;
    TestBed.inject(FolderFiles).now = () => now;
    await pages.build(page, '<link rel="stylesheet" href="styles.css"><p>a</p>');
    await pages.build(page, '<link rel="stylesheet" href="styles.css"><p>ab</p>');
    expect(gallery.listImages).toHaveBeenCalledTimes(1);
    expect(files.downloadFileAttachment).toHaveBeenCalledTimes(1);
    now = 10_000;
    await pages.build(page, '<link rel="stylesheet" href="styles.css"><p>abc</p>');
    expect(gallery.listImages).toHaveBeenCalledTimes(2);
    expect(files.downloadFileAttachment).toHaveBeenCalledTimes(1);
  });
});
