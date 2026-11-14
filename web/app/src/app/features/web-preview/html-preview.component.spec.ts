import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FolderFiles } from './folder-files.service';
import { HtmlPreviewComponent } from './html-preview.component';
import { PageFiles } from './page-files.service';
import { PREVIEW_URL } from './preview-site.config';

const page = { id: 'p', name: 'index.html', folder: 'site', file_type: 'text/html' } as FileAttachment;

function render(site: string | null): HTMLElement {
  TestBed.configureTestingModule({
    imports: [HtmlPreviewComponent],
    providers: [
      provideZonelessChangeDetection(),
      { provide: PREVIEW_URL, useValue: site },
      { provide: FolderFiles, useValue: {} },
      { provide: PageFiles, useValue: { build: vi.fn(async (_f: FileAttachment, html: string) => ({ html, missing: [], skipped: [] })) } },
    ],
  });
  const fixture = TestBed.createComponent(HtmlPreviewComponent);
  fixture.componentRef.setInput('file', page);
  fixture.componentRef.setInput('html', '<p>Hi</p>');
  fixture.detectChanges();
  return fixture.nativeElement as HTMLElement;
}

describe('HtmlPreviewComponent', () => {
  it('serves the page from the preview site where there is one', () => {
    const el = render('https://preview.example.test/index.html');
    expect(el.querySelector('app-site-frame')).not.toBeNull();
    expect(el.querySelector('app-html-page-frame')).toBeNull();
  });

  it('renders the page on its own where there is none', () => {
    const el = render(null);
    expect(el.querySelector('app-html-page-frame')).not.toBeNull();
    expect(el.querySelector('app-site-frame')).toBeNull();
  });
});
