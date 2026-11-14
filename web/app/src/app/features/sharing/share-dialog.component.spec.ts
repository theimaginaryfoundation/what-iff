import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { of } from 'rxjs';
import { describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { ShareLink } from '../../core/models/share-link.model';
import { ShareLinkService } from '../../core/services/share-link.service';
import { ShareDialogComponent } from './share-dialog.component';

const link = (over: Partial<ShareLink> = {}): ShareLink => ({
  key: 'k'.repeat(22),
  file_id: 'f-1',
  file_name: 'index.html',
  folder: 'site',
  file_type: 'text/html',
  web_app: true,
  passcode_required: true,
  passcode: 'Abc123Def456',
  created_at: '',
  updated_at: '',
  ...over,
});

async function setup(existing: ShareLink | null) {
  const service = {
    get: vi.fn(() => of(existing)),
    share: vi.fn((_id: string, passcode: boolean) =>
      of(link({ passcode_required: passcode, passcode: passcode ? 'Abc123Def456' : undefined })),
    ),
    update: vi.fn(() => of(link({ passcode: 'Zyx987Wvu654' }))),
    stop: vi.fn(() => of(undefined)),
    urlOf: (l: ShareLink) => `https://share.test/s/${l.key}`,
  };
  await TestBed.configureTestingModule({
    imports: [ShareDialogComponent],
    providers: [provideZonelessChangeDetection(), { provide: ShareLinkService, useValue: service }],
  }).compileComponents();
  const fixture = TestBed.createComponent(ShareDialogComponent);
  fixture.componentRef.setInput('file', { id: 'f-1', name: 'index.html', folder: 'site', file_type: 'text/html' } as FileAttachment);
  fixture.detectChanges();
  await fixture.whenStable();
  fixture.detectChanges();
  const root = () => document.body;
  const button = (label: string) => [...root().querySelectorAll('button')].find(b => b.textContent?.trim() === label) as HTMLButtonElement;
  return { fixture, service, root, button };
}

describe('ShareDialogComponent', () => {
  it('shares a web app with a passcode by default, saying it shares the folder and what is added to it', async () => {
    const { fixture, service, root, button } = await setup(null);
    expect(root().textContent).toContain('every file in');
    expect(root().textContent).toContain('including files you add there later');
    button('Share').click();
    fixture.detectChanges();
    await fixture.whenStable();
    expect(service.share).toHaveBeenCalledWith('f-1', true);
    expect((root().querySelector('#share-dialog-url') as HTMLInputElement).value).toBe(`https://share.test/s/${'k'.repeat(22)}`);
    expect((root().querySelector('#share-dialog-passcode') as HTMLInputElement).value).toBe('Abc123Def456');
    fixture.destroy();
  });

  it('makes a new passcode, and stops sharing', async () => {
    const { fixture, service, root, button } = await setup(link());
    const changed: (ShareLink | null)[] = [];
    fixture.componentInstance.changed.subscribe(l => changed.push(l));
    button('New passcode').click();
    fixture.detectChanges();
    await fixture.whenStable();
    expect(service.update).toHaveBeenCalledWith('f-1', { new_passcode: true });
    expect((root().querySelector('#share-dialog-passcode') as HTMLInputElement).value).toBe('Zyx987Wvu654');
    button('Stop sharing').click();
    fixture.detectChanges();
    await fixture.whenStable();
    expect(service.stop).toHaveBeenCalledWith('f-1');
    expect(changed.at(-1)).toBeNull();
    expect(button('Share')).toBeDefined();
    fixture.destroy();
  });
});
