import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideMarkdown } from 'ngx-markdown';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { fakeMonaco } from '../file-workspace/editors/fake-monaco.spec-helper';
import { MONACO_LOADER } from '../file-workspace/editors/monaco/monaco-loader';
import { IPYNB_MIME, NotebookEditorComponent } from './notebook-editor.component';

const nb = (cells: unknown[]) => new Blob([JSON.stringify({ cells, metadata: { language_info: { name: 'python' } }, nbformat: 4, nbformat_minor: 5 }, null, 1) + '\n']);
const code = (id: string, source: string, outputs: unknown[] = []) => ({ cell_type: 'code', id, metadata: {}, source: [source], outputs, execution_count: outputs.length ? 1 : null });
const md = (id: string, source: string) => ({ cell_type: 'markdown', id, metadata: {}, source: [source] });
const file: FileAttachment = { id: 'f-1', user_id: 'u', name: 'a.ipynb', file_type: IPYNB_MIME, created_at: '2026-11-07T00:00:00Z' };

describe('NotebookEditorComponent', () => {
  let fixture: ComponentFixture<NotebookEditorComponent>;
  let fake: ReturnType<typeof fakeMonaco>;

  beforeEach(async () => {
    fake = fakeMonaco();
    await TestBed.configureTestingModule({
      imports: [NotebookEditorComponent],
      providers: [provideZonelessChangeDetection(), provideMarkdown(), { provide: MONACO_LOADER, useValue: () => Promise.resolve(fake.monaco) }],
    }).compileComponents();
  });

  async function settle(): Promise<void> {
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  async function open(blob: Blob, readOnly?: boolean): Promise<HTMLElement> {
    fixture = TestBed.createComponent(NotebookEditorComponent);
    fixture.componentRef.setInput('file', file);
    fixture.componentRef.setInput('blob', blob);
    if (readOnly !== undefined) fixture.componentRef.setInput('readOnly', readOnly);
    fixture.detectChanges();
    await vi.waitFor(() => expect(fixture.componentInstance.state()).not.toBe('loading'));
    await settle();
    return fixture.nativeElement as HTMLElement;
  }

  const saved = async () => JSON.parse(await (await fixture.componentInstance.content()).text());

  it('shows cells and outputs read-only, with no toolbar and no editor', async () => {
    const el = await open(nb([md('a', '# Title'), code('b', 'print(1)', [{ output_type: 'stream', name: 'stdout', text: ['1\n'] }])]));
    expect(el.querySelector('.notebook__toolbar')).toBeNull();
    expect(el.querySelector('markdown')?.textContent).toContain('Title');
    expect(el.querySelector('.nb-cell__code')?.textContent).toContain('print(1)');
    expect(el.querySelector('.nb-output--text')?.textContent).toContain('1');
    expect(el.querySelector('.nb-cell__prompt')?.textContent).toContain('');
    (el.querySelector('app-notebook-cell:last-child') as HTMLElement).click();
    await settle();
    expect(el.querySelector('.nb-cell__editor')).toBeNull();
    expect(fake.state.created).toBe(0);
    expect(fake.state.colorized).toContain('python');
  });

  it('says why a file cannot be read', async () => {
    const el = await open(new Blob(['{"nbformat":3,"worksheets":[]}']));
    expect(el.querySelector('[role="alert"]')?.textContent).toContain('older format 3');
  });

  it('edits a code cell in Monaco, marks dirty, and saves only the change', async () => {
    const el = await open(nb([code('b', 'print(1)')]), false);
    const dirty: boolean[] = [];
    fixture.componentInstance.dirtyChange.subscribe(v => dirty.push(v));
    (el.querySelector('app-notebook-cell') as HTMLElement).click();
    await settle();
    await vi.waitFor(() => expect(fake.state.created).toBe(1));
    fake.models.get('vfs:///notebook-cell/' + fixture.componentInstance.cells()[0].uid)!.type('print(2)\n');
    await settle();
    expect(dirty).toEqual([true]);
    const out = await saved();
    expect(out.cells[0].source).toEqual(['print(2)\n']);
    expect(out.cells[0].id).toBe('b');
  });

  it('is clean again when the text returns to what was loaded', async () => {
    const el = await open(nb([code('b', 'x')]), false);
    (el.querySelector('app-notebook-cell') as HTMLElement).click();
    await settle();
    await vi.waitFor(() => expect(fake.state.created).toBe(1));
    const model = fake.models.get('vfs:///notebook-cell/' + fixture.componentInstance.cells()[0].uid)!;
    model.type('y');
    expect(fixture.componentInstance.dirty()).toBe(true);
    model.type('x');
    await vi.waitFor(() => expect(fixture.componentInstance.dirty()).toBe(false), { timeout: 1000 });
  });

  it('adds, moves, retypes, duplicates, deletes and undoes through the toolbar', async () => {
    await open(nb([code('a', 'one'), md('b', 'two')]), false);
    const c = fixture.componentInstance;
    c.select(c.cells()[1]);
    c.moveActive(-1);
    expect(c.cells().map(x => x.raw['id'])).toEqual(['b', 'a']);
    c.setActiveType('code');
    expect(c.cells()[0].type).toBe('code');
    c.duplicateActive();
    expect(c.cells()).toHaveLength(3);
    c.deleteActive();
    expect(c.cells()).toHaveLength(2);
    c.addCell('markdown', 'above');
    expect(c.cells()).toHaveLength(3);
    expect(c.editingUid()).toBe(c.activeUid());
    for (let i = 0; i < 5; i++) c.undo();
    expect(c.cells().map(x => [x.raw['id'], x.type])).toEqual([['a', 'code'], ['b', 'markdown']]);
    expect(c.canUndo()).toBe(false);
    c.redo();
    expect(c.cells().map(x => x.raw['id'])).toEqual(['b', 'a']);
  });

  it('counts a run of typing in one cell as one undo step', async () => {
    await open(nb([code('a', 'one')]), false);
    const c = fixture.componentInstance;
    const uid = c.cells()[0].uid;
    c.startEditing(uid);
    c.onSourceChange(uid, 'one1');
    c.onSourceChange(uid, 'one12');
    c.undo();
    expect(c.cells()[0].source).toBe('one');
    expect(c.canUndo()).toBe(false);
  });

  it('clears outputs for one cell or all, and offers it only when there are some', async () => {
    const out = [{ output_type: 'stream', name: 'stdout', text: 'x' }];
    await open(nb([code('a', '1', out), code('b', '2', out)]), false);
    const c = fixture.componentInstance;
    c.select(c.cells()[0]);
    c.clearActiveOutputs();
    expect(c.cells()[0].outputs).toEqual([]);
    expect(c.cells()[1].outputs).toHaveLength(1);
    c.clearAllOutputs();
    expect(c.hasOutputs()).toBe(false);
  });

  it('runs Jupyter keys in command mode, and leaves them alone while a cell is being edited', async () => {
    const el = await open(nb([code('a', '1'), code('b', '2')]), false);
    const c = fixture.componentInstance;
    const key = (k: string) => el.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true }));
    c.activeUid.set(c.cells()[0].uid);
    key('ArrowDown');
    expect(c.activeUid()).toBe(c.cells()[1].uid);
    key('b');
    expect(c.cells()).toHaveLength(3);
    key('a'); // being edited now: the key is the cell's
    expect(c.cells()).toHaveLength(3);
    c.stopEditing();
    key('d');
    expect(c.cells()).toHaveLength(3);
    key('d');
    expect(c.cells()).toHaveLength(2);
    key('m');
    expect(c.activeCell()?.type).toBe('markdown');
  });

  it('asks to save on Ctrl+S, only when editing', async () => {
    const el = await open(nb([code('a', '1')]));
    const asked = vi.fn();
    fixture.componentInstance.saveRequested.subscribe(asked);
    const press = () => el.dispatchEvent(new KeyboardEvent('keydown', { key: 's', ctrlKey: true, bubbles: true }));
    press();
    expect(asked).not.toHaveBeenCalled();
    fixture.componentRef.setInput('readOnly', false);
    fixture.detectChanges();
    press();
    expect(asked).toHaveBeenCalledTimes(1);
  });

  it('stays dirty for a change made during a save', async () => {
    await open(nb([code('a', '1')]), false);
    const c = fixture.componentInstance;
    c.addCell('code', 'below');
    const saved = await c.content();
    c.addCell('code', 'below');
    c.markSaved(saved);
    expect(c.dirty()).toBe(true);
  });

  it('keeps its state when the host hands back the bytes it just saved', async () => {
    await open(nb([code('a', '1')]), false);
    const c = fixture.componentInstance;
    c.addCell('code', 'below');
    const blob = await c.content();
    c.markSaved();
    const uids = c.cells().map(x => x.uid);
    fixture.componentRef.setInput('blob', blob);
    fixture.detectChanges();
    await settle();
    expect(c.cells().map(x => x.uid)).toEqual(uids);
    expect(c.dirty()).toBe(false);
  });
});
