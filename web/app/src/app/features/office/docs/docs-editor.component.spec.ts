import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Document as DocxDocument, Packer, Paragraph, TextRun } from 'docx';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { OoxmlPackage } from '../shared/ooxml';
import { DOCX_MIME } from '../docx/docx-export';
import { docxToDocument } from '../docx/docx-import';
import { DocDocument } from '../docx/univer-doc-types';
import { MUTATION, UniverKit } from '../univer/univer-editor-base';
import { UNIVER_DOCS_LOADER } from '../univer/univer-loader';
import { DocsEditorComponent } from './docs-editor.component';

function fakeUniverKit() {
  const state = {
    created: [] as { readOnly: boolean | undefined }[],
    disposed: 0,
    editable: [] as boolean[],
    listeners: [] as ((event: { id: string; type: number }) => void)[],
    data: null as DocDocument | null,
  };
  const kit: UniverKit = {
    options: (container, opts) => ({ presets: [], container, readOnly: opts?.readOnly }) as never,
    createUniver: (options => {
      const o = options as unknown as { container: HTMLElement; readOnly?: boolean };
      state.created.push({ readOnly: o.readOnly });
      o.container.appendChild(document.createElement('canvas'));
      const univerAPI = {
        Event: { CommandExecuted: 'CommandExecuted' },
        addEvent: (_name: string, fn: (event: { id: string; type: number }) => void) => {
          state.listeners.push(fn);
          return { dispose: () => undefined };
        },
        createDocument: (data: DocDocument) => {
          state.data = data;
          return {
            save: () => state.data,
            getPermission: () => ({
              setEditable: (v?: boolean) => {
                state.editable.push(v ?? true);
                return Promise.resolve();
              },
            }),
          };
        },
      };
      return { univer: { dispose: () => void state.disposed++ }, univerAPI } as never;
    }) as UniverKit['createUniver'],
  };
  return { kit, state };
}

async function docxBlob(): Promise<Blob> {
  const doc = new DocxDocument({
    sections: [{ children: [new Paragraph({ children: [new TextRun({ text: 'Hello', bold: true }), new TextRun(' world')] })] }],
  });
  return new Blob([await Packer.toArrayBuffer(doc)], { type: DOCX_MIME });
}

function attachment(overrides: Partial<FileAttachment> = {}): FileAttachment {
  return { id: 'f-1', user_id: 'u-1', name: 'letter.docx', file_type: DOCX_MIME, created_at: '2026-05-01T00:00:00Z', ...overrides };
}

describe('DocsEditorComponent', () => {
  let fixture: ComponentFixture<DocsEditorComponent>;
  let fake: ReturnType<typeof fakeUniverKit>;

  beforeEach(async () => {
    fake = fakeUniverKit();
    await TestBed.configureTestingModule({
      imports: [DocsEditorComponent],
      providers: [provideZonelessChangeDetection(), { provide: UNIVER_DOCS_LOADER, useValue: () => Promise.resolve(fake.kit) }],
    }).compileComponents();
  });

  async function open(file: FileAttachment, blob: Blob, readOnly?: boolean): Promise<HTMLElement> {
    fixture = TestBed.createComponent(DocsEditorComponent);
    fixture.componentRef.setInput('file', file);
    fixture.componentRef.setInput('blob', blob);
    if (readOnly !== undefined) fixture.componentRef.setInput('readOnly', readOnly);
    fixture.detectChanges();
    await vi.waitFor(() => expect(fixture.componentInstance.state()).not.toBe('loading'));
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('opens a docx read-only by default and locks the document', async () => {
    const el = await open(attachment(), await docxBlob());
    expect(fixture.componentInstance.state()).toBe('ready');
    expect(fake.state.created[0].readOnly).toBe(true);
    expect(fake.state.editable).toEqual([]);
    expect((fake.state.data as { disabled?: boolean }).disabled).toBe(true);
    expect(fake.state.data?.body.dataStream).toBe('Hello world\r\n');
    expect(fake.state.data?.documentStyle.documentFlavor).toBe(1);
    expect(el.querySelector('.univer-editor__univer canvas')).not.toBeNull();
    expect(el.textContent).not.toContain('Loading');
  });

  it('as the editor: content mutations mark it dirty and the content is a fresh docx', async () => {
    const dirty = vi.fn();
    await open(attachment(), await docxBlob(), false);
    fixture.componentInstance.dirtyChange.subscribe(dirty);
    expect(fake.state.created[0].readOnly).toBe(false);
    expect(fake.state.editable).toEqual([]);
    // Univer's own start-up mutations are ignored for a moment after the unit is created.
    await new Promise(resolve => setTimeout(resolve, 1100));
    fake.state.listeners.forEach(fn => fn({ id: 'doc.mutation.set-text-selections', type: MUTATION }));
    expect(fixture.componentInstance.dirty()).toBe(false);
    fake.state.listeners.forEach(fn => fn({ id: 'doc.mutation.rich-text-editing', type: MUTATION }));
    expect(fixture.componentInstance.dirty()).toBe(true);
    expect(dirty).toHaveBeenCalledWith(true);
    fake.state.data!.body.dataStream = 'Hello there\r\n';
    const blob = await fixture.componentInstance.content();
    expect(blob.type).toBe(DOCX_MIME);
    const back = await docxToDocument(await blob.arrayBuffer(), { id: 'x', name: 'x.docx' });
    expect(back.document.body.dataStream).toBe('Hello there\r\n');
    expect(back.document.body.textRuns).toEqual([{ st: 0, ed: 5, ts: { bl: 1 } }]);
  });

  it('saves over the original package, keeping parts it does not model', async () => {
    const pkg = await OoxmlPackage.open(await (await docxBlob()).arrayBuffer());
    pkg.put('customXml/item1.xml', '<root xmlns="urn:test">kept</root>');
    const bytes = await pkg.save(DOCX_MIME);
    await open(attachment(), new Blob([bytes as BlobPart], { type: DOCX_MIME }), false);
    fake.state.data!.body.dataStream = 'Hello again\r\n';
    const saved = await OoxmlPackage.open(await (await fixture.componentInstance.content()).arrayBuffer());
    expect(await saved.text('customXml/item1.xml')).toBe('<root xmlns="urn:test">kept</root>');
    expect(await saved.text('word/document.xml')).toContain('> again</w:t>');
    expect(await saved.text('word/styles.xml')).toBe(await pkg.text('word/styles.xml'));
  });

  it('reports bytes it cannot read', async () => {
    const el = await open(attachment(), new Blob(['nope']));
    expect(fixture.componentInstance.state()).toBe('error');
    expect(el.querySelector('[role="alert"]')?.textContent).toContain('document could not be opened');
  });
});
