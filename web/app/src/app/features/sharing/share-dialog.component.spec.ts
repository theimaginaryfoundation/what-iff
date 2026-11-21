import { HttpErrorResponse } from '@angular/common/http';
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { of, throwError } from 'rxjs';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { ShareLink, ShareScope } from '../../core/models/share-link.model';
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

const webApp: ShareScope = { file_name: 'index.html', web_app: true, folder: 'site' };

async function setup(
  opts: { link?: ShareLink | null; scope?: ShareScope; file?: Partial<FileAttachment>; service?: Record<string, unknown> } = {},
) {
  const service = {
    get: vi.fn(() => of({ scope: opts.scope ?? webApp, link: opts.link ?? null })),
    share: vi.fn((_id: string, passcode: boolean) =>
      of(link({ passcode_required: passcode, passcode: passcode ? 'Abc123Def456' : undefined })),
    ),
    update: vi.fn(() => of(link({ passcode: 'Zyx987Wvu654' }))),
    stop: vi.fn(() => of(undefined)),
    urlOf: (l: ShareLink) => `https://share.test/s/${l.key}`,
    ...opts.service,
  };
  await TestBed.configureTestingModule({
    imports: [ShareDialogComponent],
    providers: [provideZonelessChangeDetection(), { provide: ShareLinkService, useValue: service }],
  }).compileComponents();
  const fixture = TestBed.createComponent(ShareDialogComponent);
  fixture.componentRef.setInput('file', {
    id: 'f-1',
    name: 'index.html',
    folder: 'site',
    file_type: 'text/html',
    ...opts.file,
  } as FileAttachment);
  const settle = async () => {
    fixture.detectChanges();
    await fixture.whenStable();
    await new Promise(r => setTimeout(r));
    fixture.detectChanges();
  };
  await settle();
  // The dialog is shown at the end of the body (ui-modal's portal).
  const root = document.body;
  const button = (label: string) => [...root.querySelectorAll('button')].find(b => b.textContent?.trim() === label) as HTMLButtonElement;
  return { fixture, service, root, button, settle };
}

describe('ShareDialogComponent', () => {
  afterEach(() => TestBed.resetTestingModule());

  it('shares a web app with a passcode by default, saying it shares the folder and what is added to it, and focuses the link', async () => {
    const { service, root, button, settle } = await setup();
    expect(root.textContent).toContain('every file in');
    expect(root.textContent).toContain('including files you add there later');
    button('Share').click();
    await settle();
    expect(service.share).toHaveBeenCalledWith('f-1', true);
    const url = root.querySelector('#share-dialog-url') as HTMLInputElement;
    expect(url.value).toBe(`https://share.test/s/${'k'.repeat(22)}`);
    expect((root.querySelector('#share-dialog-passcode') as HTMLInputElement).value).toBe('Abc123Def456');
    expect(document.activeElement).toBe(url);
  });

  it('says what a chat copy shares as the server works it out, though the copy has no folder of its own', async () => {
    const { root } = await setup({ file: { folder: undefined }, scope: webApp });
    expect(root.textContent).toContain('Shares this page as a web app');
    expect(root.textContent).not.toContain('Shares this page on its own');
  });

  it('asks before it stops sharing, and makes a new passcode', async () => {
    const { service, root, button, settle } = await setup({ link: link() });
    button('New passcode').click();
    await settle();
    expect(service.update).toHaveBeenCalledWith('f-1', { new_passcode: true });
    expect((root.querySelector('#share-dialog-passcode') as HTMLInputElement).value).toBe('Zyx987Wvu654');

    button('Stop sharing').click();
    await settle();
    expect(service.stop).not.toHaveBeenCalled();
    expect(root.textContent).toContain('The link stops working for everyone who has it');
    button('Keep sharing').click();
    await settle();
    expect(service.stop).not.toHaveBeenCalled();

    button('Stop sharing').click();
    await settle();
    button('Stop sharing').click();
    await settle();
    expect(service.stop).toHaveBeenCalledWith('f-1');
    expect(button('Share')).toBeDefined();
  });

  it('puts the passcode box back when the change fails, and says why', async () => {
    const refused = new HttpErrorResponse({ status: 409, error: { message: 'No key to keep passcodes with' } });
    const { root, settle } = await setup({
      link: link({ passcode_required: false, passcode: undefined }),
      service: { update: vi.fn(() => throwError(() => refused)) },
    });
    const box = root.querySelector('ui-checkbox input') as HTMLInputElement;
    box.click();
    await settle();
    expect(box.checked).toBe(false);
    expect(root.querySelector('[role="alert"]')?.textContent).toContain('No key to keep passcodes with');
  });

  it('says when it cannot read how the file is shared, rather than offering to share it, and tries again', async () => {
    const get = vi
      .fn()
      .mockReturnValueOnce(throwError(() => new HttpErrorResponse({ status: 500 })))
      .mockReturnValue(of({ scope: webApp, link: link() }));
    const { root, button, settle } = await setup({ service: { get } });
    expect(root.textContent).toContain('Could not read how this file is shared');
    expect(button('Share')).toBeUndefined();
    button('Try again').click();
    await settle();
    expect(root.querySelector('#share-dialog-url')).not.toBeNull();
  });

  it('asks for a new passcode when the stored one cannot be read', async () => {
    const { root, button } = await setup({ link: link({ passcode: undefined, passcode_unreadable: true }) });
    expect(root.textContent).toContain('The passcode could not be read');
    expect(root.querySelector('#share-dialog-passcode')).toBeNull();
    expect(button('New passcode')).toBeDefined();
  });
});
