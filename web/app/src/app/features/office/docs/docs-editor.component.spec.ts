import { provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Document as DocxDocument, Packer, Paragraph, TextRun } from 'docx';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { OoxmlPackage } from '../shared/ooxml';
import { DOCX_MIME } from '../docx/docx-export';
import { docxToDocument } from '../docx/docx-import';
import { DocDocument } from '../docx/univer-doc-types';
import { MUTATION, UniverKit } from '../univer/univer-editor-base';
import { UNIVER_DOCS_LOADER } from '../univer/univer-loader';
import { CommentPeopleService } from '../comments/comment-people.service';
import { CommentsPanelState } from '../comments/comments-panel-state.service';
import { commentedDocx } from '../docx/docx-comments.spec-helper';
import { CUSTOM_DECORATION_COMMENT } from '../docx/univer-doc-types';
import { DocsEditorComponent } from './docs-editor.component';

function fakeUniverKit() {
  const state = {
    created: [] as { readOnly: boolean | undefined }[],
    disposed: 0,
    editable: [] as boolean[],
    listeners: [] as ((event: { id: string; type: number }) => void)[],
    data: null as DocDocument | null,
    guides: [] as boolean[],
    painted: [] as { activeId: string | null; hidden: string[] }[],
    revealed: [] as string[],
  };
  // Comment marks over the fake's document, as Univer's decorations would be.
  const decorations = () => (state.data!.body.customDecorations ??= []);
  const marks = {
    paint: (s: { activeId: string | null; hidden: ReadonlySet<string> }) =>
      state.painted.push({ activeId: s.activeId, hidden: [...s.hidden] }),
    add: async (id: string, _segmentId: string, start: number, end: number) => {
      decorations().push({ id, type: CUSTOM_DECORATION_COMMENT, startIndex: start, endIndex: end - 1 });
      return true;
    },
    remove: async (id: string) => {
      state.data!.body.customDecorations = decorations().filter(d => d.id !== id);
      return true;
    },
    reveal: (id: string) => state.revealed.push(id),
    positions: () => new Map(decorations().map(d => [d.id, d.startIndex])),
    at: (offset: number) => decorations().find(d => d.startIndex <= offset && offset <= d.endIndex)?.id ?? null,
    text: (_segmentId: string, start: number, end: number) => state.data!.body.dataStream.slice(start, end),
    dispose: () => undefined,
  };
  const kit: UniverKit & { setDocGuides(univer: unknown, unitId: string, show: boolean): boolean; commentMarks(): typeof marks } = {
    setDocGuides: (_univer, _unitId, show) => {
      state.guides.push(show);
      return true;
    },
    commentMarks: () => marks,
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
      providers: [
        provideZonelessChangeDetection(),
        { provide: UNIVER_DOCS_LOADER, useValue: () => Promise.resolve(fake.kit) },
        { provide: CommentPeopleService, useValue: { me: signal({ id: 'user:u-1', name: 'Pat Lee' }) } },
      ],
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

  it('hides the guides by default, and shows them only when asked while editing', async () => {
    const guidesButton = (root: HTMLElement) =>
      [...root.querySelectorAll<HTMLButtonElement>('.univer-editor__tool')].find(b => b.textContent?.includes('Show guides'));
    const view = await open(attachment(), await docxBlob());
    expect(fake.state.guides.at(-1)).toBe(false);
    expect(guidesButton(view)).toBeUndefined();
    fixture.destroy();

    const el = await open(attachment(), await docxBlob(), false);
    expect(fake.state.guides.at(-1)).toBe(false);
    const toggle = guidesButton(el)!;
    expect(toggle.getAttribute('aria-pressed')).toBe('false');
    toggle.click();
    fixture.detectChanges();
    expect(fake.state.guides.at(-1)).toBe(true);
    expect(toggle.getAttribute('aria-pressed')).toBe('true');
    toggle.click();
    expect(fake.state.guides.at(-1)).toBe(false);
  });

  describe('comments', () => {
    const select = (start: number, end: number) =>
      fake.state.listeners.forEach(fn =>
        (fn as (e: unknown) => void)({
          id: 'doc.operation.set-selections',
          type: 1,
          params: { unitId: 'doc-f-1', segmentId: '', ranges: [{ startOffset: start, endOffset: end }] },
        }),
      );
    const commentedBlob = async () => new Blob([(await commentedDocx()) as BlobPart], { type: DOCX_MIME });

    beforeEach(() => TestBed.inject(CommentsPanelState).open.set(null));

    it('shows the open threads beside the document, and clicking commented text selects its thread', async () => {
      const el = await open(attachment(), await commentedBlob());
      await vi.waitFor(() => expect(fake.state.painted.length).toBeGreaterThan(0));
      fixture.detectChanges();
      // Open threads open the panel; the resolved one is listed only on request, and unmarked.
      expect(el.querySelector('.docs-editor__count')?.textContent?.trim()).toBe('1');
      const threads = [...el.querySelectorAll('app-comments-panel .thread')];
      expect(threads.map(t => t.getAttribute('data-thread-id'))).toEqual(['5D3E2F01']);
      expect(threads[0].textContent).toContain('Is three realistic?');
      expect(threads[0].querySelector('.comment__mention')?.textContent).toBe('@Dana Reyes');
      expect(fake.state.painted.at(-1)).toEqual({ activeId: null, hidden: ['5D3E2F03'] });
      // Read-only: no boxes or buttons that would change a comment.
      expect(el.querySelector('app-comments-panel .thread__reply')).toBeNull();

      select(fake.state.data!.body.dataStream.indexOf('three') + 2, fake.state.data!.body.dataStream.indexOf('three') + 2);
      fixture.detectChanges();
      expect(fixture.componentInstance.activeThreadId()).toBe('5D3E2F01');
      expect(fake.state.painted.at(-1)?.activeId).toBe('5D3E2F01');
    });

    it('starts a thread on the selection, posts it with a mention, and saves it into the file', async () => {
      const el = await open(attachment(), await commentedBlob(), false);
      await vi.waitFor(() => expect(fake.state.painted.length).toBeGreaterThan(0));
      const editor = fixture.componentInstance;
      const stream = fake.state.data!.body.dataStream;
      const at = stream.indexOf('this quarter');
      select(at, at + 'this quarter'.length);
      fixture.detectChanges();
      expect(editor.canComment()).toBe(true);
      editor.startComment();
      fixture.detectChanges();
      expect(el.querySelector('.thread--draft .thread__quote')?.textContent).toBe('this quarter');

      await editor.createThread({ text: 'Which quarter, @Sam Okafor?', mentions: [{ id: 'author:Sam Okafor', name: 'Sam Okafor' }] });
      fixture.detectChanges();
      expect(editor.dirty()).toBe(true);
      const created = editor.threads().find(t => t.quote === 'this quarter')!;
      expect(created.comments[0]).toEqual(
        expect.objectContaining({ authorId: 'user:u-1', authorName: 'Pat Lee', text: 'Which quarter, @Sam Okafor?' }),
      );
      expect(editor.threads().map(t => t.id)).toEqual(['5D3E2F01', created.id, '5D3E2F03']);
      editor.reply({ threadId: '5D3E2F01', draft: { text: 'Agreed.', mentions: [] } });

      const back = await docxToDocument(await (await editor.content()).arrayBuffer(), { id: 'x', name: 'x.docx' });
      expect(back.comments.map(t => [t.id, t.comments.map(c => c.text)])).toEqual([
        ['5D3E2F01', ['Is three realistic?', 'Yes. @Dana Reyes can you confirm?', 'Agreed.']],
        [created.id, ['Which quarter, @Sam Okafor?']],
        ['5D3E2F03', ['Finance signed this off.']],
      ]);
      expect(back.comments[1].comments[0].mentions).toEqual([{ id: 'author:Sam Okafor', name: 'Sam Okafor' }]);
    });

    it('deleting a thread removes its mark, and resolving keeps it but hides it', async () => {
      await open(attachment(), await commentedBlob(), false);
      await vi.waitFor(() => expect(fake.state.painted.length).toBeGreaterThan(0));
      const editor = fixture.componentInstance;
      editor.resolve({ threadId: '5D3E2F01', resolved: true });
      fixture.detectChanges();
      expect(fake.state.painted.at(-1)?.hidden).toEqual(['5D3E2F01', '5D3E2F03']);
      await editor.remove({ threadId: '5D3E2F03', commentId: '5D3E2F03' });
      expect(fake.state.data!.body.customDecorations!.map(d => d.id)).toEqual(['5D3E2F01']);
      const back = await docxToDocument(await (await editor.content()).arrayBuffer(), { id: 'x', name: 'x.docx' });
      expect(back.comments.map(t => [t.id, t.resolved])).toEqual([['5D3E2F01', true]]);
    });
  });

  it('reports bytes it cannot read', async () => {
    const el = await open(attachment(), new Blob(['nope']));
    expect(fixture.componentInstance.state()).toBe('error');
    expect(el.querySelector('[role="alert"]')?.textContent).toContain('document could not be opened');
  });
});
