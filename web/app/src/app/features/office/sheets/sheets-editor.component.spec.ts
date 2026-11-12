import { provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import ExcelJS from 'exceljs';
import JSZip from 'jszip';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { COMMENT_SAVER, CommentSaver } from '../../file-workspace/editors/file-editor';
import { CommentPeopleService } from '../comments/comment-people.service';
import { CommentsPanelState } from '../comments/comments-panel-state.service';
import { takeSheetComments } from '../xlsx/sheet-comments';
import { UNIVER_SHEETS_LOADER, UniverSheetsKit } from '../univer/univer-loader';
import { UniverWorkbook } from '../xlsx/univer-sheet-types';
import { xlsxToWorkbook } from '../xlsx/xlsx-import';
import { MUTATION, SheetsEditorComponent, XLSX_MIME } from './sheets-editor.component';

/** A Univer double: records how it was created, keeps the workbook data, and lets tests fire commands. */
function fakeUniverKit() {
  const state = {
    created: [] as { container: HTMLElement; readOnly: boolean | undefined; worker?: unknown }[],
    disposed: 0,
    editable: [] as boolean[],
    listeners: [] as ((event: { id: string; type: number }) => void)[],
    data: null as UniverWorkbook | null,
    workers: 0,
    terminated: 0,
    /** The selected cell, as the grid would report it. */
    active: null as { sheetId: string; row: number; col: number } | null,
    painted: [] as { activeId: string | null; hidden: string[] }[],
    revealed: [] as string[],
  };
  const cell = (sheetId: string, row: number, col: number) => {
    const rows = (state.data!.sheets[sheetId].cellData[row] ??= {});
    return (rows[col] ??= {});
  };
  const kit: UniverSheetsKit = {
    addContextMenuItems: () => ({ dispose: () => undefined }),
    commentCells: () => ({
      paint: next => state.painted.push({ activeId: next.activeId, hidden: [...next.hidden] }),
      tag: async (sheetId, row, col, threadId) => {
        const c = cell(sheetId, row, col);
        c.custom = { ...(c.custom ?? {}), whatiffCommentThread: threadId };
        return true;
      },
      locate: () => {
        const out = new Map<string, { sheetId: string; row: number; col: number }>();
        for (const sheetId of state.data!.sheetOrder) {
          for (const [r, row] of Object.entries(state.data!.sheets[sheetId].cellData)) {
            for (const [c, value] of Object.entries(row ?? {})) {
              const id = value?.custom?.['whatiffCommentThread'];
              if (typeof id === 'string' && !out.has(id)) out.set(id, { sheetId, row: Number(r), col: Number(c) });
            }
          }
        }
        return out;
      },
      at: (sheetId, row, col) => {
        const id = state.data!.sheets[sheetId].cellData[row]?.[col]?.custom?.['whatiffCommentThread'];
        return typeof id === 'string' ? id : null;
      },
      reveal: (sheetId, row, col) => state.revealed.push(`${sheetId}!${row},${col}`),
      activeCell: () => state.active,
      activeSheetId: () => state.data!.sheetOrder[0],
      dispose: () => undefined,
    }),
    options: (container, opts) => ({ presets: [], container, readOnly: opts?.readOnly, worker: opts?.formulaWorker }) as never,
    repaintDrawing: () => undefined,
    createFormulaWorker: () => {
      state.workers += 1;
      return { terminate: () => (state.terminated += 1) } as unknown as Worker;
    },
    createUniver: (options => {
      const o = options as unknown as { container: HTMLElement; readOnly?: boolean };
      state.created.push({ container: o.container, readOnly: o.readOnly, worker: (o as { worker?: unknown }).worker });
      o.container.appendChild(document.createElement('canvas'));
      const univerAPI = {
        Event: { CommandExecuted: 'CommandExecuted' },
        addEvent: (_name: string, fn: (event: { id: string; type: number }) => void) => {
          state.listeners.push(fn);
          return { dispose: () => undefined };
        },
        createWorkbook: (data: UniverWorkbook) => {
          state.data = data;
          return {
            save: () => state.data,
            setEditable: (v: boolean) => {
              state.editable.push(v);
            },
          };
        },
      };
      const univer = {
        dispose: () => {
          state.disposed += 1;
        },
      };
      return { univer, univerAPI } as never;
    }) as UniverSheetsKit['createUniver'],
  };
  return { kit, state };
}

async function xlsxBlob(): Promise<Blob> {
  const wb = new ExcelJS.Workbook();
  const ws = wb.addWorksheet('Data');
  ws.getCell('A1').value = 'Item';
  ws.getCell('B1').value = 42;
  const buffer = await wb.xlsx.writeBuffer();
  return new Blob([buffer as ArrayBuffer], { type: XLSX_MIME });
}

function attachment(overrides: Partial<FileAttachment> = {}): FileAttachment {
  return { id: 'f-1', user_id: 'u-1', name: 'book.xlsx', file_type: XLSX_MIME, created_at: '2026-05-01T00:00:00Z', ...overrides };
}

/** The comment saver the next editor gets, if any. */
let testSaver: CommentSaver | null = null;

describe('SheetsEditorComponent', () => {
  let fixture: ComponentFixture<SheetsEditorComponent>;
  let fake: ReturnType<typeof fakeUniverKit>;

  beforeEach(async () => {
    testSaver = null;
    fake = fakeUniverKit();
    await TestBed.configureTestingModule({
      imports: [SheetsEditorComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: UNIVER_SHEETS_LOADER, useValue: () => Promise.resolve(fake.kit) },
        { provide: CommentPeopleService, useValue: { me: signal({ id: 'user:u-1', name: 'Pat Lee' }) } },
        { provide: COMMENT_SAVER, useFactory: () => testSaver },
      ],
    }).compileComponents();
  });

  /** Creates the component with its inputs in one go, as a host does; a tick before that would find them missing. */
  async function open(file: FileAttachment, blob: Blob, readOnly?: boolean): Promise<HTMLElement> {
    fixture = TestBed.createComponent(SheetsEditorComponent);
    fixture.componentRef.setInput('file', file);
    fixture.componentRef.setInput('blob', blob);
    if (readOnly !== undefined) fixture.componentRef.setInput('readOnly', readOnly);
    fixture.detectChanges();
    await vi.waitFor(() => expect(fixture.componentInstance.state()).not.toBe('loading'));
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('opens an xlsx read-only by default: no toolbar, not editable, sheet shown', async () => {
    const el = await open(attachment(), await xlsxBlob());
    expect(fixture.componentInstance.state()).toBe('ready');
    expect(fake.state.created).toHaveLength(1);
    expect(fake.state.created[0].readOnly).toBe(true);
    expect(fake.state.editable).toEqual([false]);
    expect(fake.state.data?.sheets[fake.state.data.sheetOrder[0]].cellData[0][1]).toMatchObject({ v: 42 });
    expect(el.querySelector('.univer-editor__univer canvas')).not.toBeNull();
    expect(el.querySelector('.fidelity-notice')).toBeNull();
  });

  it('as the editor: a data mutation marks it dirty, Ctrl+S asks to save, and content is a fresh xlsx', async () => {
    const dirty = vi.fn();
    const save = vi.fn();
    const el = await open(attachment(), await xlsxBlob(), false);
    fixture.componentInstance.dirtyChange.subscribe(dirty);
    fixture.componentInstance.saveRequested.subscribe(save);
    expect(fake.state.created[0].readOnly).toBe(false);
    expect(fake.state.editable).toEqual([true]);
    // Univer's own start-up mutations are ignored for a moment after the unit is created.
    await new Promise(resolve => setTimeout(resolve, 1100));
    fake.state.listeners.forEach(fn => fn({ id: 'sheet.operation.set-selections', type: 1 }));
    fake.state.listeners.forEach(fn => fn({ id: 'sheet.mutation.set-scroll', type: MUTATION }));
    expect(fixture.componentInstance.dirty()).toBe(false);
    fake.state.listeners.forEach(fn => fn({ id: 'sheet.mutation.set-range-values', type: MUTATION }));
    expect(fixture.componentInstance.dirty()).toBe(true);
    expect(dirty).toHaveBeenCalledWith(true);

    el.dispatchEvent(new KeyboardEvent('keydown', { key: 's', ctrlKey: true, bubbles: true, cancelable: true }));
    expect(save).toHaveBeenCalledTimes(1);

    fake.state.data!.sheets[fake.state.data!.sheetOrder[0]].cellData[0][1] = { v: 43, t: 2 };
    const blob = await fixture.componentInstance.content();
    expect(blob.type).toBe(XLSX_MIME);
    const back = await xlsxToWorkbook(await blob.arrayBuffer(), { id: 'x', name: 'x' });
    expect(back.workbook.sheets[back.workbook.sheetOrder[0]].cellData[0][1]).toMatchObject({ v: 43 });
    fixture.componentInstance.markSaved();
    expect(fixture.componentInstance.dirty()).toBe(false);
    expect(dirty).toHaveBeenLastCalledWith(false);
  });

  it('never asks to save while read-only', async () => {
    const save = vi.fn();
    const el = await open(attachment(), await xlsxBlob());
    fixture.componentInstance.saveRequested.subscribe(save);
    el.dispatchEvent(new KeyboardEvent('keydown', { key: 's', ctrlKey: true, bubbles: true, cancelable: true }));
    expect(save).not.toHaveBeenCalled();
    fake.state.listeners.forEach(fn => fn({ id: 'sheet.mutation.set-range-values', type: MUTATION }));
    expect(fixture.componentInstance.dirty()).toBe(false);
  });

  it('opens a CSV as a one-sheet workbook and saves it back as CSV', async () => {
    await open(attachment({ name: 'data.csv', file_type: 'text/csv' }), new Blob(['a,b\n1,2\n'], { type: 'text/csv' }), false);
    const sheet = fake.state.data!.sheets[fake.state.data!.sheetOrder[0]];
    expect(sheet.name).toBe('data');
    expect(sheet.cellData[1][1]).toEqual({ v: 2, t: 2 });
    sheet.cellData[1][1] = { v: 'two', t: 1 };
    const blob = await fixture.componentInstance.content();
    expect(blob.type).toBe('text/csv');
    expect(await blob.text()).toBe('a,b\r\n1,two\r\n');
  });

  it('shows what the converter dropped and reports it as losses', async () => {
    const wb = new ExcelJS.Workbook();
    wb.addWorksheet('S').getCell('A1').value = 1;
    const el = await open(attachment(), new Blob([(await wb.xlsx.writeBuffer()) as ArrayBuffer]), false);
    fixture.componentInstance.report.set({ dropped: [{ feature: 'Something', where: 'S', count: 1 }], warnings: [] });
    fixture.detectChanges();
    expect(fixture.componentInstance.losses()).toEqual(['Something (S)']);
    expect(el.querySelector('.fidelity-notice')?.textContent).toContain('Something');
    fixture.componentInstance.dismissNotice();
    fixture.detectChanges();
    expect(el.querySelector('.fidelity-notice')).toBeNull();
  });

  it('recreates the instance for new bytes and disposes it when destroyed', async () => {
    await open(attachment(), await xlsxBlob());
    fixture.componentRef.setInput('blob', await xlsxBlob());
    fixture.detectChanges();
    await vi.waitFor(() => expect(fake.state.created).toHaveLength(2));
    expect(fake.state.disposed).toBe(1);
    fixture.destroy();
    expect(fake.state.disposed).toBe(2);
  });

  it('runs formulas in a worker per instance and terminates it with the instance', async () => {
    await open(attachment(), await xlsxBlob());
    expect(fake.state.workers).toBe(1);
    expect((fake.state.created[0] as { worker?: unknown }).worker).toBeTruthy();
    fixture.componentRef.setInput('blob', await xlsxBlob());
    fixture.detectChanges();
    await vi.waitFor(() => expect(fake.state.created).toHaveLength(2));
    expect(fake.state.workers).toBe(2);
    expect(fake.state.terminated).toBe(1);
    fixture.destroy();
    expect(fake.state.terminated).toBe(2);
  });

  it('keeps the instance when the host hands back the bytes it just saved', async () => {
    await open(attachment(), await xlsxBlob(), false);
    const saved = await fixture.componentInstance.content();
    fixture.componentRef.setInput('blob', saved);
    fixture.detectChanges();
    await new Promise(resolve => setTimeout(resolve, 50));
    expect(fake.state.created).toHaveLength(1);
    expect(fake.state.disposed).toBe(0);
  });

  it('reports bytes it cannot read', async () => {
    const el = await open(attachment(), new Blob(['not a workbook']));
    expect(fixture.componentInstance.state()).toBe('error');
    expect(el.querySelector('[role="alert"]')?.textContent).toContain('could not be opened');
  });
  describe('comments', () => {
    beforeEach(() => TestBed.inject(CommentsPanelState).open.set(null));
    const reread = async () => {
      const result = await xlsxToWorkbook(await (await fixture.componentInstance.content()).arrayBuffer(), { id: 'x', name: 'x.xlsx' });
      return takeSheetComments(result.workbook, result.preserved?.rules?.threads.persons ?? []);
    };

    it('comments on the selected cell, marks it, and saves the thread as an Excel threaded comment', async () => {
      const el = await open(attachment(), await xlsxBlob(), false);
      const editor = fixture.componentInstance;
      const sheetId = fake.state.data!.sheetOrder[0];
      fake.state.active = { sheetId, row: 0, col: 1 };
      editor.startComment();
      fixture.detectChanges();
      expect(el.querySelector('.thread--draft .thread__quote')?.textContent).toBe('B1');
      await editor.comments.create({ text: 'Is 42 right, @Pat Lee?', mentions: [{ id: 'user:u-1', name: 'Pat Lee' }] });
      fixture.detectChanges();
      expect(editor.dirty()).toBe(true);
      expect(el.querySelector('.sheets-editor__count')?.textContent?.trim()).toBe('1');
      expect(fake.state.painted.at(-1)?.activeId).toBe(editor.comments.threads()[0].id);
      // A second Comment on the same cell opens its thread, as Excel does.
      expect(editor.comments.start()).toBe('existing');

      const back = await reread();
      expect(back).toEqual([
        expect.objectContaining({
          id: editor.comments.threads()[0].id,
          quote: 'B1',
          comments: [expect.objectContaining({ authorId: 'user:u-1', authorName: 'Pat Lee', text: 'Is 42 right, @Pat Lee?' })],
        }),
      ]);
    });

    it('a thread whose cell was deleted is not saved', async () => {
      await open(attachment(), await xlsxBlob(), false);
      const editor = fixture.componentInstance;
      const sheetId = fake.state.data!.sheetOrder[0];
      fake.state.active = { sheetId, row: 0, col: 0 };
      editor.startComment();
      await editor.comments.create({ text: 'On A1', mentions: [] });
      delete fake.state.data!.sheets[sheetId].cellData[0][0].custom;
      editor.comments.refresh();
      expect(editor.comments.onSheet()).toEqual([]);
      expect(await reread()).toEqual([]);
    });

    it('while viewing, with a saver: a comment is saved as it is made', async () => {
      const saveComments = vi.fn().mockResolvedValue('saved');
      testSaver = { canComment: signal(true), saveComments, showCurrentContent: vi.fn() };
      await open(attachment(), await xlsxBlob());
      const editor = fixture.componentInstance;
      expect(editor.commentable()).toBe(true);
      fake.state.active = { sheetId: fake.state.data!.sheetOrder[0], row: 0, col: 1 };
      editor.startComment();
      await editor.comments.create({ text: 'Seen while viewing', mentions: [] });
      await vi.waitFor(() => expect(saveComments).toHaveBeenCalledTimes(1));
    });
  });
});
