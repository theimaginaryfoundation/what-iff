import { Component, input, output, provideZonelessChangeDetection, signal } from '@angular/core';
import { HttpErrorResponse } from '@angular/common/http';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { NEVER, Observable, map, of, Subject, throwError } from 'rxjs';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { FileWorkspaceComponent } from './file-workspace.component';
import { FileEditor } from './editors/file-editor';
import { EDITOR_LOADER, VIEWER_LOADER } from './viewers/registry';

@Component({ standalone: true, template: `<p class="fake-viewer">{{ file().name }}:{{ blob().size }}</p>` })
class FakeViewerComponent {
  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();
}

@Component({ standalone: true, template: `<p class="fake-editor">{{ file().name }}</p>` })
class FakeEditorComponent implements FileEditor {
  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();
  readonly readOnly = input(true);
  readonly dirtyChange = output<boolean>();
  readonly busyChange = output<boolean>();
  readonly saveRequested = output<void>();
  readonly dirty = signal(false);
  readonly losses = signal<readonly string[]>([]);
  text = 'edited';
  marked = 0;
  content(): Promise<Blob> {
    return Promise.resolve(new Blob([this.text], { type: 'text/plain' }));
  }
  markSaved(): void {
    this.marked += 1;
    this.dirty.set(false);
    this.dirtyChange.emit(false);
  }
  edit(text: string): void {
    this.text = text;
    this.dirty.set(true);
    this.dirtyChange.emit(true);
  }
}

function file(overrides: Partial<FileAttachment> = {}): FileAttachment {
  return { id: 'f-1', user_id: 'u-1', name: 'notes.txt', file_type: 'text/plain', created_at: '2026-05-01T00:00:00Z', ...overrides };
}

const textBlob = (text: string) => new Blob([text], { type: 'text/plain' });

describe('FileWorkspaceComponent', () => {
  let fixture: ComponentFixture<FileWorkspaceComponent>;
  let download: ReturnType<typeof vi.fn>;
  let downloadVersion: number | null;
  let alert: ReturnType<typeof vi.fn>;
  let replaceContent: ReturnType<typeof vi.fn>;
  let confirmDiscard: ReturnType<typeof vi.fn>;
  let confirm: ReturnType<typeof vi.fn>;
  let loadViewer: ReturnType<typeof vi.fn<(kind: string) => Promise<unknown> | null>>;
  /** The chat/file pairs whose chat agent may change the file (ADR 0x046). */
  let granted: Set<string>;

  beforeEach(async () => {
    download = vi.fn().mockReturnValue(of(textBlob('hello')));
    downloadVersion = null;
    granted = new Set();
    alert = vi.fn().mockResolvedValue(undefined);
    replaceContent = vi.fn().mockImplementation((id: string) => of(file({ id, content_version: 2 })));
    confirmDiscard = vi.fn().mockResolvedValue(true);
    confirm = vi.fn().mockResolvedValue(true);
    loadViewer = vi.fn((kind: string) => (kind === 'none' ? null : Promise.resolve(FakeViewerComponent)));
    await TestBed.configureTestingModule({
      imports: [FileWorkspaceComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        {
          provide: FileAttachmentService,
          useValue: {
            downloadFileAttachment: download,
            downloadFileContent: (id: string) =>
              (download as unknown as (id: string) => Observable<Blob>)(id).pipe(map((blob: Blob) => ({ blob, version: downloadVersion }))),
            replaceContent,
            fileEditGranted: (chatId: string, fileId: string) => of(granted.has(`${chatId}/${fileId}`)),
            revokeFileEdit: (chatId: string, fileId: string) => {
              granted.delete(`${chatId}/${fileId}`);
              return of(undefined);
            },
          },
        },
        { provide: ConfirmationService, useValue: { alert, confirmDiscardChanges: confirmDiscard, confirm } },
        { provide: VIEWER_LOADER, useValue: (kind: string) => loadViewer(kind) },
        { provide: EDITOR_LOADER, useValue: (kind: string) => (kind === 'none' ? null : Promise.resolve(FakeEditorComponent)) },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(FileWorkspaceComponent);
  });

  afterEach(() => vi.restoreAllMocks());

  async function show(value: FileAttachment): Promise<HTMLElement> {
    fixture.componentRef.setInput('file', value);
    fixture.detectChanges();
    await fixture.whenStable();
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  }

  /** Names the workspace saved files as, by watching the temporary download link it clicks. */
  function watchSaves(): string[] {
    const saved: string[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      saved.push(this.download);
    });
    return saved;
  }

  const button = (el: HTMLElement, label: string) =>
    [...el.querySelectorAll('button')].find(b => b.textContent?.trim() === label) as HTMLButtonElement | undefined;

  it('shows an error, instead of loading forever, when the viewer cannot be loaded', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    loadViewer.mockReturnValueOnce(Promise.reject(new Error('stale chunk')));
    await show(file());
    expect(fixture.componentInstance.state()).toBe('error');
  });

  it("says when the chat's agent may change the file, and takes that back on Revoke", async () => {
    granted.add('chat-1/f-1');
    fixture.componentRef.setInput('chatId', 'chat-1');
    const el = await show(file());
    expect(el.querySelector('.file-workspace__grant')?.textContent).toContain('can edit this file');
    button(el, 'Revoke')!.click();
    fixture.detectChanges();
    expect(el.querySelector('.file-workspace__grant')).toBeNull();
    expect(granted.size).toBe(0);

    // Outside a chat there is no agent to grant.
    fixture.componentRef.setInput('chatId', null);
    granted.add('chat-1/f-1');
    fixture.detectChanges();
    await fixture.whenStable();
    expect(el.querySelector('.file-workspace__grant')).toBeNull();
  });

  it('fetches the bytes once and hands them with the file to the viewer for its kind', async () => {
    const el = await show(file());
    expect(download).toHaveBeenCalledWith('f-1');
    expect(loadViewer).toHaveBeenCalledWith('text');
    expect(el.querySelector('.fake-viewer')?.textContent).toBe('notes.txt:5');
  });

  it('offers a download, without fetching, for a file it cannot show', async () => {
    const saved = watchSaves();
    download.mockReturnValue(of(new Blob(['PK'])));
    const el = await show(file({ name: 'bundle.zip', file_type: 'application/zip' }));
    expect(download).not.toHaveBeenCalled();
    expect(el.textContent).toContain('There is no preview for this kind of file.');
    button(el, 'Download')!.click();
    expect(download).toHaveBeenCalledWith('f-1');
    expect(saved).toEqual(['bundle.zip']);
  });

  it('downloads the bytes it already has instead of fetching them again', async () => {
    const saved = watchSaves();
    const el = await show(file());
    download.mockClear();
    (el.querySelector('button[aria-label="Download file"]') as HTMLButtonElement).click();
    expect(download).not.toHaveBeenCalled();
    expect(saved).toEqual(['notes.txt']);
  });

  it('ignores a second download click while one is in flight', async () => {
    const pending = new Subject<Blob>();
    const saved = watchSaves();
    await show(file({ name: 'bundle.zip', file_type: 'application/zip' }));
    download.mockReturnValue(pending);
    fixture.componentInstance.download();
    fixture.componentInstance.download();
    expect(download).toHaveBeenCalledTimes(1);
    pending.next(new Blob(['PK']));
    expect(saved).toEqual(['bundle.zip']);
    expect(fixture.componentInstance.downloading()).toBe(false);
  });

  it('reports a failed load and still offers the download', async () => {
    download.mockReturnValue(throwError(() => new Error('boom')));
    const el = await show(file());
    expect(el.querySelector('[role="alert"]')?.textContent).toContain('Could not load this file.');
    expect(button(el, 'Try downloading it')).toBeDefined();
  });

  it('does not refetch when only the metadata changes (a move or rename)', async () => {
    await show(file());
    download.mockClear();
    await show(file({ folder: 'docs', name: 'renamed.txt' }));
    expect(download).not.toHaveBeenCalled();
  });

  it('drops bytes for a file that is no longer shown', async () => {
    const slow = new Subject<Blob>();
    download.mockReturnValueOnce(slow).mockReturnValueOnce(of(textBlob('second')));
    fixture.componentRef.setInput('file', file({ id: 'first' }));
    fixture.detectChanges();
    const el = await show(file({ id: 'second' }));
    slow.next(textBlob('first!'));
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
    expect(el.querySelector('.fake-viewer')?.textContent).toBe('notes.txt:6');
  });

  it('renames keeping the extension, and emits nothing for an unchanged name', async () => {
    const renamed = vi.fn();
    fixture.componentInstance.rename.subscribe(renamed);
    await show(file({ name: 'notes.txt' }));
    fixture.componentInstance.startRename();
    expect(fixture.componentInstance.renameDraft()).toBe('notes');
    fixture.componentInstance.renameDraft.set('journal');
    fixture.componentInstance.submitRename();
    expect(renamed).toHaveBeenCalledWith({ id: 'f-1', name: 'journal.txt' });
    renamed.mockClear();
    fixture.componentInstance.startRename();
    fixture.componentInstance.submitRename();
    expect(renamed).not.toHaveBeenCalled();
  });

  it('names a personality document as such', async () => {
    download.mockReturnValue(NEVER);
    fixture.componentRef.setInput('personalityName', 'Aster');
    const el = await show(file({ personality_id: 'p-1', source: 'imported' }));
    expect(el.querySelector('.file-workspace__meta')?.textContent).toContain('Document of Aster');
  });

  it('shows the host-specific controls it is asked for', async () => {
    download.mockReturnValue(NEVER);
    fixture.componentRef.setInput('backLabel', 'Gallery');
    fixture.componentRef.setInput('showNavigation', true);
    fixture.componentRef.setInput('showMove', true);
    let el = await show(file());
    expect(el.querySelector('.file-workspace__back')?.textContent).toContain('Gallery');
    expect(el.querySelector('button[aria-label="Close file"]')).toBeNull();
    expect(button(el, 'Prev')).toBeDefined();
    expect(button(el, 'Move to…')).toBeDefined();

    fixture.componentRef.setInput('backLabel', null);
    fixture.componentRef.setInput('showNavigation', false);
    fixture.componentRef.setInput('showMove', false);
    fixture.componentRef.setInput('expandable', true);
    fixture.componentRef.setInput('showGalleryLink', true);
    el = await show(file({ chat_id: 'c-1' }));
    expect(el.querySelector('.file-workspace__back')).toBeNull();
    expect(el.querySelector('button[aria-label="Close file"]')).not.toBeNull();
    expect(el.querySelector('button[aria-label="Expand file"]')).not.toBeNull();
    expect(button(el, 'Prev')).toBeUndefined();
    expect(el.querySelector('a.file-workspace__link')?.textContent).toContain('Open in gallery');
  });

  async function startEditing(): Promise<{ el: HTMLElement; editor: FakeEditorComponent }> {
    const el = await show(file({ content_version: 3 }));
    button(el, 'Edit')!.click();
    await fixture.whenStable();
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
    const editor = fixture.componentInstance['editorRef']!.instance as FakeEditorComponent;
    return { el, editor };
  }

  it('asks before leaving an editor that is busy, such as a notebook running, even when it is clean', async () => {
    const { editor } = await startEditing();
    editor.busyChange.emit(true);
    confirmDiscard.mockResolvedValueOnce(false);
    expect(await fixture.componentInstance.stopEdit()).toBe(false);
    expect(confirmDiscard).toHaveBeenCalled();
    editor.busyChange.emit(false);
    confirmDiscard.mockClear();
    expect(await fixture.componentInstance.stopEdit()).toBe(true);
    expect(confirmDiscard).not.toHaveBeenCalled();
  });

  it('offers Edit for kinds with an editor, not on a phone, and never for kinds without one', async () => {
    let el = await show(file());
    expect(button(el, 'Edit')).toBeDefined();
    fixture.componentRef.setInput('canEdit', false);
    fixture.detectChanges();
    expect(button(el, 'Edit')).toBeUndefined();
    fixture.componentRef.setInput('canEdit', true);
    download.mockReturnValue(of(new Blob(['PK'])));
    el = await show(file({ name: 'a.zip', file_type: 'application/zip' }));
    expect(button(el, 'Edit')).toBeUndefined();
  });

  it('creates the editor, saves with the loaded version as If-Match, and adopts the new version', async () => {
    const changed = vi.fn();
    fixture.componentInstance.fileChanged.subscribe(changed);
    const { el, editor } = await startEditing();
    expect(el.querySelector('.fake-editor')?.textContent).toBe('notes.txt');
    expect(el.querySelector('.fake-viewer')).toBeNull();
    expect((button(el, 'Save') as HTMLButtonElement).disabled).toBe(true);

    editor.edit('new text');
    fixture.detectChanges();
    expect((button(el, 'Save') as HTMLButtonElement).disabled).toBe(false);
    await fixture.componentInstance.save();
    expect(replaceContent).toHaveBeenCalledWith('f-1', expect.any(Blob), 'text/plain', 3, null);
    expect(await (replaceContent.mock.calls[0][1] as Blob).text()).toBe('new text');
    expect(editor.marked).toBe(1);
    expect(changed).toHaveBeenCalledWith(expect.objectContaining({ id: 'f-1', content_version: 2 }));
    expect(await fixture.componentInstance.blob()!.text()).toBe('new text');
  });

  it('sends the version of the downloaded bytes, not the possibly stale file row', async () => {
    downloadVersion = 5;
    const { editor } = await startEditing();
    editor.edit('new text');
    await fixture.componentInstance.save();
    expect(replaceContent.mock.calls[0][3]).toBe(5);
  });

  it('shows why the editor could not prepare the file, and sends nothing', async () => {
    const { editor } = await startEditing();
    editor.edit('new text');
    vi.spyOn(editor, 'content').mockRejectedValueOnce(new Error('This browser cannot write WEBP images.'));
    await fixture.componentInstance.save();
    expect(replaceContent).not.toHaveBeenCalled();
    expect(fixture.componentInstance.saveError()).toBe('This browser cannot write WEBP images.');
    expect(fixture.componentInstance.saving()).toBe(false);
  });

  it('turns a stale version into the conflict dialog, with reload, overwrite and save-as', async () => {
    const { el, editor } = await startEditing();
    editor.edit('mine');
    replaceContent.mockReturnValueOnce(
      throwError(() => new HttpErrorResponse({ status: 412, error: { details: { content_version: 7 } } })),
    );
    await fixture.componentInstance.save();
    fixture.detectChanges();
    expect(fixture.componentInstance.conflictVersion()).toBe(7);
    expect(el.textContent).toContain('now version 7');

    fixture.componentInstance.overwriteAfterConflict();
    await vi.waitFor(() => expect(replaceContent).toHaveBeenCalledTimes(2));
    expect(replaceContent.mock.calls[1][3]).toBeNull();
    await vi.waitFor(() => expect(fixture.componentInstance.conflictVersion()).toBeNull());

    editor.edit('again');
    replaceContent.mockReturnValueOnce(
      throwError(() => new HttpErrorResponse({ status: 412, error: { details: { content_version: 8 } } })),
    );
    await fixture.componentInstance.save();
    download.mockReturnValue(of(textBlob('theirs')));
    fixture.componentInstance.reloadAfterConflict();
    await fixture.whenStable();
    expect(fixture.componentInstance.mode()).toBe('view');
    await vi.waitFor(async () => expect(await fixture.componentInstance.blob()!.text()).toBe('theirs'));

    replaceContent.mockReturnValueOnce(throwError(() => new HttpErrorResponse({ status: 500 })));
    await startEditing();
    (fixture.componentInstance['editorRef']!.instance as FakeEditorComponent).edit('x');
    await fixture.componentInstance.save();
    expect(fixture.componentInstance.saveError()).toContain('Could not save');
  });

  describe('comments made while viewing (ADR 0x045)', () => {
    const viewer = (losses: string[] = []) => {
      const markSaved = vi.fn();
      return { editor: { content: () => Promise.resolve(textBlob('with comment')), markSaved, losses: signal(losses) }, markSaved };
    };

    it('writes them as the new version, with the loaded version as If-Match, only while viewing a file the user may change', async () => {
      downloadVersion = 3;
      await show(file());
      const ws = fixture.componentInstance;
      const changed = vi.fn();
      ws.fileChanged.subscribe(changed);
      expect(ws.canComment()).toBe(true);
      const { editor, markSaved } = viewer();
      expect(await ws.saveComments(editor)).toBe('saved');
      expect(replaceContent).toHaveBeenCalledWith('f-1', expect.any(Blob), 'text/plain', 3, null);
      expect(markSaved).toHaveBeenCalledTimes(1);
      expect(changed).toHaveBeenCalledTimes(1);
      // The next comment saves over the version just written.
      replaceContent.mockReturnValueOnce(of(file({ content_version: 3 })));
      await ws.saveComments(editor);
      expect(replaceContent.mock.calls[1][3]).toBe(2);

      fixture.componentRef.setInput('canEdit', false);
      fixture.detectChanges();
      expect(ws.canComment()).toBe(false);
      expect(await ws.saveComments(editor)).toBe('failed');
    });

    it('opens the editor on what a running comment save stores, not on the bytes it replaces', async () => {
      await show(file());
      const ws = fixture.componentInstance;
      const landing = new Subject<FileAttachment>();
      replaceContent.mockReturnValueOnce(landing);
      const saving = ws.saveComments(viewer().editor);
      const editing = ws.startEdit();
      await new Promise(resolve => setTimeout(resolve));
      expect(ws['editorRef']).toBeNull();
      landing.next(file({ content_version: 2 }));
      landing.complete();
      expect(await saving).toBe('saved');
      await editing;
      await fixture.whenStable();
      const editor = ws['editorRef']!.instance as FakeEditorComponent;
      expect(await editor.blob().text()).toBe('with comment');
    });

    it('lets the user comment on a file no editor can write, but writes none into it', async () => {
      await show(file({ name: 'loop.gif', file_type: 'image/gif' }));
      const ws = fixture.componentInstance;
      expect(ws.editable()).toBe(false);
      expect(ws.canComment()).toBe(true);
      expect(await ws.saveComments(viewer().editor)).toBe('failed');
      expect(replaceContent).not.toHaveBeenCalled();
    });

    it('reloads the file when it changed elsewhere, rather than overwrite it', async () => {
      await show(file());
      replaceContent.mockReturnValueOnce(
        throwError(() => new HttpErrorResponse({ status: 412, error: { details: { content_version: 7 } } })),
      );
      download.mockClear();
      expect(await fixture.componentInstance.saveComments(viewer().editor)).toBe('conflict');
      expect(download).toHaveBeenCalledWith('f-1');
    });

    it('asks once per file before a save that would lose content, and reloads when declined', async () => {
      await show(file());
      const ws = fixture.componentInstance;
      confirm.mockResolvedValueOnce(false);
      download.mockClear();
      expect(await ws.saveComments(viewer(['Charts']).editor)).toBe('cancelled');
      expect(replaceContent).not.toHaveBeenCalled();
      expect(download).toHaveBeenCalled();
      expect(await ws.saveComments(viewer(['Charts']).editor)).toBe('saved');
      expect(await ws.saveComments(viewer(['Charts']).editor)).toBe('saved');
      expect(confirm).toHaveBeenCalledTimes(2);
    });
  });

  it('asks before leaving edit mode or closing with unsaved changes', async () => {
    const closed = vi.fn();
    fixture.componentInstance.close.subscribe(closed);
    const { editor } = await startEditing();
    editor.edit('dirty');
    confirmDiscard.mockResolvedValueOnce(false);
    await fixture.componentInstance.requestClose();
    expect(closed).not.toHaveBeenCalled();
    expect(fixture.componentInstance.mode()).toBe('edit');
    confirmDiscard.mockResolvedValueOnce(true);
    await fixture.componentInstance.requestClose();
    expect(closed).toHaveBeenCalledTimes(1);
    expect(fixture.componentInstance.mode()).toBe('view');
  });

  it('flips the viewer into an editor for kinds that edit in place', async () => {
    download.mockReturnValue(of(new Blob(['a,b\n1,2'], { type: 'text/csv' })));
    const el = await show(file({ name: 'data.csv', file_type: 'text/csv', content_version: 1 }));
    button(el, 'Edit')!.click();
    await fixture.whenStable();
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
    const editor = fixture.componentInstance['editorRef']!.instance as FakeEditorComponent;
    expect(editor.readOnly()).toBe(false);
    const text = await show(file());
    button(text, 'Edit')!.click();
    await fixture.whenStable();
    await new Promise(resolve => setTimeout(resolve));
    expect((fixture.componentInstance['editorRef']!.instance as FakeEditorComponent).readOnly()).toBe(true);
  });

  it('asks before saving over an original the editor cannot keep whole', async () => {
    const { editor } = await startEditing();
    editor.losses.set(['Charts (Sales)']);
    editor.edit('lossy');
    confirm.mockResolvedValueOnce(false);
    await fixture.componentInstance.save();
    expect(confirm).toHaveBeenCalledWith(
      expect.objectContaining({ title: 'Save over the original?', message: expect.stringContaining('Charts (Sales)') }),
    );
    expect(replaceContent).not.toHaveBeenCalled();
    confirm.mockResolvedValueOnce(true);
    await fixture.componentInstance.save();
    expect(replaceContent).toHaveBeenCalledTimes(1);
    editor.losses.set([]);
    editor.edit('clean');
    await fixture.componentInstance.save();
    expect(confirm).toHaveBeenCalledTimes(2);
    expect(replaceContent).toHaveBeenCalledTimes(2);
  });

  it('validates Save As and hands the content, name and folder to the host', async () => {
    const saveAs = vi.fn();
    fixture.componentInstance.saveAs.subscribe(saveAs);
    const { editor } = await startEditing();
    editor.edit('copy me');
    fixture.componentInstance.openSaveAs();
    expect(fixture.componentInstance.saveAsName()).toBe('notes.txt');
    fixture.componentInstance.saveAsName.set('bad/name.txt');
    await fixture.componentInstance.submitSaveAs();
    expect(fixture.componentInstance.saveAsError()).toContain('without slashes');
    fixture.componentInstance.saveAsName.set('copy.txt');
    fixture.componentInstance.saveAsFolder.set(' Docs/Drafts ');
    await fixture.componentInstance.submitSaveAs();
    expect(saveAs).toHaveBeenCalledTimes(1);
    const payload = saveAs.mock.calls[0][0] as { name: string; folder: string; blob: Blob };
    expect(payload.name).toBe('copy.txt');
    expect(payload.folder).toBe('docs/drafts');
    expect(await payload.blob.text()).toBe('copy me');
    expect(fixture.componentInstance.saveAsOpen()).toBe(false);
  });
  describe('markup (ADR 0x047)', () => {
    const png = () => file({ name: 'photo.png', file_type: 'image/png' });

    async function startMarkup(): Promise<HTMLElement> {
      download.mockReturnValue(of(new Blob(['png'], { type: 'image/png' })));
      fixture.componentRef.setInput('markupAvailable', true);
      const el = await show(png());
      button(el, 'Mark up')!.click();
      fixture.detectChanges();
      await fixture.whenStable();
      return el;
    }

    it('offers Mark up for PNG, JPEG and WebP images where the host can take it, phones included', async () => {
      download.mockReturnValue(of(new Blob(['png'], { type: 'image/png' })));
      let el = await show(png());
      expect(button(el, 'Mark up')).toBeUndefined();

      fixture.componentRef.setInput('markupAvailable', true);
      fixture.componentRef.setInput('canEdit', false);
      fixture.detectChanges();
      expect(button(el, 'Mark up')).toBeDefined();

      el = await show(file({ name: 'loop.gif', file_type: 'image/gif' }));
      expect(button(el, 'Mark up')).toBeUndefined();
      download.mockReturnValue(of(textBlob('hi')));
      el = await show(file());
      expect(button(el, 'Mark up')).toBeUndefined();
    });

    it('swaps the viewer for the markup surface and asks before dropping a drawing', async () => {
      const el = await startMarkup();
      expect(el.querySelector('app-image-markup')).not.toBeNull();
      expect(el.querySelector('.fake-viewer')).toBeNull();
      expect(button(el, 'Edit')).toBeUndefined();

      fixture.componentInstance.markupDirty.set(true);
      confirmDiscard.mockResolvedValueOnce(false);
      expect(await fixture.componentInstance.stopEdit()).toBe(false);
      expect(fixture.componentInstance.mode()).toBe('markup');

      expect(await fixture.componentInstance.stopEdit()).toBe(true);
      expect(fixture.componentInstance.mode()).toBe('view');
    });

    it('hands the drawing to the host and goes back to the image once it is added', async () => {
      await startMarkup();
      const sent: { file: FileAttachment; result: unknown }[] = [];
      fixture.componentInstance.markupSubmit.subscribe(e => sent.push(e));
      const result = { mask: new Blob(['m']), markup: null };

      fixture.componentInstance.onMarkupSubmitted(result);
      fixture.componentInstance.onMarkupSubmitted(result);
      expect(sent).toEqual([{ file: png(), result }]);
      expect(await fixture.componentInstance.stopEdit()).toBe(false);

      fixture.componentInstance.finishMarkup(false);
      expect(fixture.componentInstance.mode()).toBe('markup');
      expect(fixture.componentInstance.saveError()).toContain('Could not add the markup');

      fixture.componentInstance.onMarkupSubmitted(result);
      fixture.componentInstance.finishMarkup(true);
      expect(fixture.componentInstance.mode()).toBe('view');
      expect(fixture.componentInstance.markupDirty()).toBe(false);
    });
  });
});
