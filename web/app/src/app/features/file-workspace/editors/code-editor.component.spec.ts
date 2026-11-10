import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MARKED_EXTENSIONS, provideMarkdown } from 'ngx-markdown';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { latexMathExtension } from '../../../core/utils/latex-math.helpers';
import { CodeEditorComponent } from './code-editor.component';
import { fakeMonaco } from './fake-monaco.spec-helper';
import { MONACO_LOADER } from './monaco/monaco-loader';

function file(overrides: Partial<FileAttachment> = {}): FileAttachment {
  return { id: 'f-1', user_id: 'u', name: 'notes.py', file_type: 'text/x-python', created_at: '2026-11-01T00:00:00Z', ...overrides };
}

describe('CodeEditorComponent', () => {
  let fixture: ComponentFixture<CodeEditorComponent>;
  let fake: ReturnType<typeof fakeMonaco>;

  beforeEach(async () => {
    fake = fakeMonaco();
    await TestBed.configureTestingModule({
      imports: [CodeEditorComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideMarkdown({ markedExtensions: [{ provide: MARKED_EXTENSIONS, useFactory: latexMathExtension, multi: true }] }),
        { provide: MONACO_LOADER, useValue: () => Promise.resolve(fake.monaco) },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(CodeEditorComponent);
  });

  async function open(value: FileAttachment, text: string): Promise<void> {
    fixture.componentRef.setInput('file', value);
    fixture.componentRef.setInput('blob', new Blob([text]));
    fixture.detectChanges();
    await vi.waitFor(() => expect(fixture.componentInstance.state()).toBe('ready'));
    fixture.detectChanges();
  }

  it('keeps edits when the file is renamed or moved', async () => {
    await open(file(), 'a');
    const model = fake.models.get('vfs:///notes.py?id=f-1')!;
    model.type('edited');
    fixture.componentRef.setInput('file', file({ name: 'renamed.py' }));
    fixture.detectChanges();
    await fixture.whenStable();
    expect(model.getValue()).toBe('edited');
    expect(fixture.componentInstance.dirty()).toBe(true);
  });

  it('keeps typing that lands during a save, and does not reload its own saved bytes', async () => {
    await open(file(), 'a');
    const model = fake.models.get('vfs:///notes.py?id=f-1')!;
    model.type('ab');
    const saved = await fixture.componentInstance.content();
    model.type('abc');

    fixture.componentRef.setInput('blob', saved);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.componentInstance.markSaved(saved);

    expect(model.getValue()).toBe('abc');
    expect(fixture.componentInstance.dirty()).toBe(true);
    model.type('ab');
    expect(fixture.componentInstance.dirty()).toBe(false);
  });

  it('loads the text into a model with the file language and a VFS URI, and tracks dirtiness', async () => {
    const dirty: boolean[] = [];
    fixture.componentInstance.dirtyChange.subscribe(d => dirty.push(d));
    await open(file(), 'print(1)');
    expect(fixture.componentInstance.language()).toBe('python');
    expect(fake.state.created).toBe(1);
    const model = fake.models.get('vfs:///notes.py?id=f-1')!;
    expect(model.getValue()).toBe('print(1)');
    expect(fixture.componentInstance.dirty()).toBe(false);

    model.type('print(2)');
    expect(fixture.componentInstance.dirty()).toBe(true);
    expect(dirty).toEqual([true]);
    expect(await (await fixture.componentInstance.content()).text()).toBe('print(2)');

    fixture.componentInstance.markSaved();
    expect(fixture.componentInstance.dirty()).toBe(false);
    model.type('print(2)');
    expect(fixture.componentInstance.dirty()).toBe(false);
  });

  it('asks the host to save on Ctrl+S and follows readOnly', async () => {
    const saves = vi.fn();
    fixture.componentInstance.saveRequested.subscribe(saves);
    await open(file(), 'x');
    fake.state.commands[0]();
    expect(saves).toHaveBeenCalledTimes(1);
    fixture.componentRef.setInput('readOnly', true);
    fixture.detectChanges();
    expect(fake.state.options).toContainEqual({ readOnly: true });
  });

  it('shows a live Markdown preview that can be hidden', async () => {
    await open(file({ name: 'notes.md', file_type: 'text/markdown' }), '# Title');
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelector('.code-editor__preview')).not.toBeNull();
    fake.models.get('vfs:///notes.md?id=f-1')!.type('# Changed');
    await vi.waitFor(() => expect(fixture.componentInstance.previewText()).toBe('# Changed'));
    fixture.componentInstance.togglePreview();
    fixture.detectChanges();
    expect(el.querySelector('.code-editor__preview')).toBeNull();
  });

  it('replaces the text when new bytes arrive for the same file, without a second editor', async () => {
    await open(file(), 'v1');
    fixture.componentRef.setInput('blob', new Blob(['v2']));
    fixture.detectChanges();
    await vi.waitFor(() => expect(fake.models.get('vfs:///notes.py?id=f-1')!.getValue()).toBe('v2'));
    expect(fake.state.created).toBe(1);
    expect(fixture.componentInstance.dirty()).toBe(false);
  });
});
