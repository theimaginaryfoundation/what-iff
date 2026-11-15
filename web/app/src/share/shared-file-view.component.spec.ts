import { provideHttpClient } from '@angular/common/http';
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideMarkdown } from 'ngx-markdown';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { SharedView } from './share-api.service';
import { readOnlyViewerProviders } from './share.providers';
import { ShareFolderFiles } from './share-folder-files';
import { SharedFileViewComponent } from './shared-file-view.component';

const key = 'k'.repeat(22);

async function setup(view: Partial<SharedView>, download: () => Promise<Blob> = async () => new Blob(['# Shared notes'])) {
  const files = { use: vi.fn(), download: vi.fn(download) };
  await TestBed.configureTestingModule({
    imports: [SharedFileViewComponent],
    providers: [
      provideZonelessChangeDetection(),
      provideHttpClient(),
      provideRouter([]),
      provideMarkdown(),
      ...readOnlyViewerProviders(),
      { provide: ShareFolderFiles, useValue: files },
    ],
  }).compileComponents();
  const fixture = TestBed.createComponent(SharedFileViewComponent);
  fixture.componentRef.setInput('shareKey', key);
  fixture.componentRef.setInput('view', {
    name: 'notes.md',
    file_type: 'text/markdown',
    content_version: 1,
    web_app: false,
    ...view,
  } as SharedView);
  for (let i = 0; i < 6; i++) {
    fixture.detectChanges();
    await fixture.whenStable();
    await new Promise(r => setTimeout(r));
  }
  fixture.detectChanges();
  return { el: fixture.nativeElement as HTMLElement, files };
}

describe('SharedFileViewComponent', () => {
  afterEach(() => TestBed.resetTestingModule());

  it('shows a file in its viewer, with a download link to its content', async () => {
    const { el, files } = await setup({});
    await vi.waitFor(() => expect(el.textContent).toContain('Shared notes'));
    expect(files.download).toHaveBeenCalledTimes(1);
    expect(el.querySelector('a[download]')?.getAttribute('href')).toBe(`/api/shared/${key}/content`);
  });

  it('reads nothing of a file with no viewer until it is downloaded', async () => {
    const { el, files } = await setup({ name: 'archive.zip', file_type: 'application/zip' });
    expect(el.textContent).toContain('has no preview');
    expect(files.download).not.toHaveBeenCalled();
  });

  it('says when the file cannot be loaded', async () => {
    const { el } = await setup({}, async () => {
      throw new Error('gone');
    });
    expect(el.textContent).toContain('could not be loaded');
  });
});
